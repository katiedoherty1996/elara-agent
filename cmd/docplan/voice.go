package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/katiedoherty1996/elara-agent/internal/gdoc"
	"github.com/katiedoherty1996/elara-agent/internal/tasks"
)

// read prints plans so a live session (e.g. Katie's phone) can read them back:
// with no -task it lists every plan; with -task it prints that plan's tab text.
func read(args []string) error {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	tasksPath := fs.String("tasks", ".agent/tasks.json", "task file")
	id := fs.String("task", "", "task id (empty: list the plans)")
	_ = fs.Parse(args)

	f, err := readTasks(*tasksPath)
	if err != nil {
		return err
	}
	if *id == "" {
		for _, t := range f.Tasks {
			if t.PlanTabID != "" {
				fmt.Printf("%s\t%s\t%s\n", t.ID, t.Status, t.Title)
			}
		}
		return nil
	}
	t := findTask(&f, *id)
	if t == nil || t.PlanTabID == "" {
		return fmt.Errorf("no plan for task %q", *id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := client(ctx, gdoc.ScopeRead)
	if err != nil {
		return err
	}
	tabs, err := c.Tabs(ctx)
	if err != nil {
		return err
	}
	tab, ok := gdoc.FindTab(tabs, t.PlanTabID)
	if !ok {
		return fmt.Errorf("plan tab %s is gone from the Doc", t.PlanTabID)
	}
	fmt.Println(tab.TabProperties.Title)
	fmt.Print(tab.DocumentTab.Body.Text())
	return nil
}

// decide records Katie's decision on a plan without her opening the Doc: approve
// (optionally an option) or ask for changes. It does what the Decision line does.
func decide(args []string) error {
	fs := flag.NewFlagSet("decide", flag.ExitOnError)
	tasksPath := fs.String("tasks", ".agent/tasks.json", "task file")
	id := fs.String("task", "", "task id")
	kind := fs.String("decision", "", "approved | changes")
	choice := fs.String("choice", "", "approved option, e.g. B (optional)")
	notes := fs.String("notes", "", "Katie's words: what to change, or notes for the build")
	_ = fs.Parse(args)

	f, err := readTasks(*tasksPath)
	if err != nil {
		return err
	}
	t := findTask(&f, *id)
	if t == nil {
		return fmt.Errorf("no task %q", *id)
	}
	if err := applyDecision(t, *kind, *choice, *notes); err != nil {
		return err
	}
	fmt.Printf("docplan: %s -> %s\n", t.ID, t.Status)
	return writeTasks(*tasksPath, f)
}

// applyDecision changes the task the same way the Doc's Decision line would.
func applyDecision(t *tasks.Task, kind, choice, notes string) error {
	if t.PlanTabID == "" {
		return fmt.Errorf("task %s has no plan yet", t.ID)
	}
	if t.Status != "plan_review" && t.Status != "plan_changes" {
		return fmt.Errorf("task %s is %s, not waiting for a decision", t.ID, t.Status)
	}
	notes = strings.TrimSpace(notes)
	switch kind {
	case "approved":
		t.Status, t.PlanApproved, t.PlanDecision = "pending", true, "approved"
		t.PlanChoice, t.PlanFeedback = strings.TrimSpace(choice), notes
	case "changes":
		if notes == "" {
			return errors.New("say what to change (-notes)")
		}
		if t.Status == "plan_changes" && t.PlanFeedback != "" {
			notes = t.PlanFeedback + "\n" + notes
		}
		t.Status, t.PlanDecision, t.PlanFeedback = "plan_changes", "changes", notes
	default:
		return fmt.Errorf("decision must be approved or changes, not %q", kind)
	}
	return nil
}

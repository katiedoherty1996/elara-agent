// Command docplan puts the planner's plans into Katie's Google Doc, one tab per plan
// (under a "Plans" tab), and reads back her decision on each one.
//
//	docplan publish -task <id> -title "Plan: Library screen" -file plan.md [-image a.png=https://...]
//	    Creates the plan's tab (or rewrites it, if the task already has one) and prints
//	    the tab id. Stores the tab id on the task in tasks.json.
//	docplan decisions
//	    Reads the "Decision:" line of every plan tab and updates the tasks:
//	    "Approved ..." → planApproved, back to pending; anything else but "Waiting" →
//	    plan_changes with Katie's feedback.
//
// Both need GOOGLE_SA_KEY_FILE and GOOGLE_DOC_ID. publish needs the doc shared with the
// service account as Editor.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/katiedoherty1996/elara-agent/internal/gdoc"
	"github.com/katiedoherty1996/elara-agent/internal/tasks"
)

// plansTabTitle is the top-level tab every plan tab sits under.
const plansTabTitle = "Plans"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: docplan publish|decisions [flags]")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "publish":
		err = publish(os.Args[2:])
	case "decisions":
		err = decisions(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "docplan:", err)
		os.Exit(1)
	}
}

// imageFlags collects repeated -image path=url flags.
type imageFlags map[string]gdoc.Image

func (f imageFlags) String() string { return "" }

func (f imageFlags) Set(v string) error {
	path, url, ok := strings.Cut(v, "=")
	if !ok || !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("-image wants path=https://url, got %q", v)
	}
	f[path] = gdoc.Image{URL: url}
	return nil
}

// publish writes one plan into its own tab.
func publish(args []string) error {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	tasksPath := fs.String("tasks", ".agent/tasks.json", "task file")
	taskID := fs.String("task", "", "task id the plan is for")
	title := fs.String("title", "", `tab title, e.g. "Plan: Library screen"`)
	file := fs.String("file", "", "the plan, in markdown")
	images := imageFlags{}
	fs.Var(images, "image", "path=https://url for an ![alt](path) line (repeatable)")
	_ = fs.Parse(args)
	if *taskID == "" || *title == "" || *file == "" {
		return errors.New("publish needs -task, -title and -file")
	}
	md, err := os.ReadFile(filepath.Clean(*file))
	if err != nil {
		return err
	}
	file0, err := readTasks(*tasksPath)
	if err != nil {
		return err
	}
	task := findTask(&file0, *taskID)
	if task == nil {
		return fmt.Errorf("task %q not in %s", *taskID, *tasksPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := client(ctx, gdoc.ScopeWrite)
	if err != nil {
		return err
	}
	tabs, err := c.Tabs(ctx)
	if err != nil {
		return err
	}

	tabID := task.PlanTabID
	existing, ok := gdoc.FindTab(tabs, tabID)
	if tabID != "" && ok {
		// Revision: clear the tab and rename it, keeping its place and id.
		var reqs []map[string]any
		if end := existing.DocumentTab.Body.EndIndex(); end > 2 {
			reqs = append(reqs, map[string]any{"deleteContentRange": map[string]any{
				"range": map[string]any{"startIndex": 1, "endIndex": end - 1, "tabId": tabID},
			}})
		}
		reqs = append(reqs, map[string]any{"updateDocumentTabProperties": map[string]any{
			"tabProperties": map[string]any{"tabId": tabID, "title": *title}, "fields": "title",
		}})
		if _, err := c.BatchUpdate(ctx, reqs); err != nil {
			return err
		}
	} else {
		parentID, err := ensurePlansTab(ctx, c, tabs)
		if err != nil {
			return err
		}
		if tabID, err = addTab(ctx, c, map[string]any{"title": *title, "parentTabId": parentID}); err != nil {
			return err
		}
	}

	if _, err := c.BatchUpdate(ctx, gdoc.MarkdownRequests(string(md), tabID, images)); err != nil {
		return fmt.Errorf("writing the plan into tab %s: %w", tabID, err)
	}

	task.PlanTabID, task.PlanDecision, task.PlanFeedback = tabID, "waiting", ""
	task.Status = "plan_review"
	if err := writeTasks(*tasksPath, file0); err != nil {
		return err
	}
	fmt.Println(tabID)
	return nil
}

// ensurePlansTab returns the id of the top-level "Plans" tab, creating it if needed.
func ensurePlansTab(ctx context.Context, c *gdoc.Client, tabs []gdoc.Tab) (string, error) {
	if t, ok := gdoc.FindTopLevelTitle(tabs, plansTabTitle); ok {
		return t.TabProperties.TabID, nil
	}
	id, err := addTab(ctx, c, map[string]any{"title": plansTabTitle})
	if err != nil {
		return "", err
	}
	intro := "Plans from the Elara agent, one tab per task.\n\n" +
		"To decide, edit the Decision line at the top of a plan:\n" +
		"- Approved (or: Approved option B) — the agent builds it\n" +
		"- anything else, e.g. Changes: make it one step — the agent rewrites the plan\n" +
		"You can also write under \"Your notes\" at the bottom of a plan.\n"
	_, err = c.BatchUpdate(ctx, []map[string]any{{"insertText": map[string]any{
		"text": intro, "location": map[string]any{"index": 1, "tabId": id},
	}}})
	return id, err
}

// addTab creates a tab with the given properties and returns its id.
func addTab(ctx context.Context, c *gdoc.Client, props map[string]any) (string, error) {
	replies, err := c.BatchUpdate(ctx, []map[string]any{{"addDocumentTab": map[string]any{"tabProperties": props}}})
	if err != nil {
		return "", err
	}
	var r struct {
		AddDocumentTab struct {
			TabProperties struct {
				TabID string `json:"tabId"`
			} `json:"tabProperties"`
		} `json:"addDocumentTab"`
	}
	if len(replies) == 0 || json.Unmarshal(replies[0], &r) != nil || r.AddDocumentTab.TabProperties.TabID == "" {
		return "", errors.New("docs API didn't return the new tab's id")
	}
	return r.AddDocumentTab.TabProperties.TabID, nil
}

// decisions reads Katie's decision on every plan waiting for review.
func decisions(args []string) error {
	fs := flag.NewFlagSet("decisions", flag.ExitOnError)
	tasksPath := fs.String("tasks", ".agent/tasks.json", "task file")
	_ = fs.Parse(args)

	f, err := readTasks(*tasksPath)
	if err != nil {
		return err
	}
	waiting := 0
	for _, t := range f.Tasks {
		if t.Status == "plan_review" && t.PlanTabID != "" {
			waiting++
		}
	}
	if waiting == 0 {
		return nil
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
	changed := false
	for i := range f.Tasks {
		t := &f.Tasks[i]
		if t.Status != "plan_review" || t.PlanTabID == "" {
			continue
		}
		tab, ok := gdoc.FindTab(tabs, t.PlanTabID)
		if !ok {
			fmt.Printf("docplan: plan tab for %s is gone; leaving it in plan_review\n", t.ID)
			continue
		}
		d := ReadDecision(tab.DocumentTab.Body.Text())
		switch d.Kind {
		case "approved":
			t.Status, t.PlanApproved, t.PlanDecision, t.PlanChoice, t.PlanFeedback = "pending", true, "approved", d.Choice, d.Notes
			fmt.Printf("docplan: %s approved %s\n", t.ID, d.Choice)
			changed = true
		case "changes":
			t.Status, t.PlanDecision, t.PlanFeedback = "plan_changes", "changes", d.Notes
			fmt.Printf("docplan: %s needs plan changes\n", t.ID)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeTasks(*tasksPath, f)
}

// Decision is what Katie wrote on a plan.
type Decision struct {
	Kind   string // waiting | approved | changes
	Choice string // e.g. "option B", from "Approved option B"
	Notes  string // the rest of the Decision line plus anything under "Your notes"
}

var (
	decisionLine = regexp.MustCompile(`(?i)^\s*(?:##\s*)?decision\s*:\s*(.*)$`)
	approvedRe   = regexp.MustCompile(`(?i)^\W*(approved?|yes|go|build it)\b[\s:,.-]*(.*)$`)
	waitingRe    = regexp.MustCompile(`(?i)^\W*(waiting|pending|tbd|todo)\b`)
)

// ReadDecision finds the plan's "Decision:" line and the "Your notes" section.
func ReadDecision(text string) Decision {
	lines := strings.Split(text, "\n")
	value, found := "", false
	var notes []string
	inNotes := false
	for _, l := range lines {
		if !found {
			if m := decisionLine.FindStringSubmatch(l); m != nil {
				value, found = strings.TrimSpace(m[1]), true
				continue
			}
		}
		if strings.HasPrefix(l, "## ") {
			inNotes = strings.Contains(strings.ToLower(l), "your notes")
			continue
		}
		if inNotes && strings.TrimSpace(l) != "" {
			notes = append(notes, strings.TrimSpace(strings.TrimPrefix(l, "- ")))
		}
	}
	noteText := strings.Join(notes, "\n")

	if m := approvedRe.FindStringSubmatch(value); m != nil {
		return Decision{Kind: "approved", Choice: strings.TrimSpace(m[2]), Notes: noteText}
	}
	// Still the placeholder (or blank): keep waiting, even if notes were started.
	if value == "" || strings.Contains(value, "⏳") || waitingRe.MatchString(value) {
		return Decision{Kind: "waiting"}
	}
	return Decision{Kind: "changes", Notes: strings.TrimSpace(value + "\n" + noteText)}
}

// client builds a Docs client from the environment.
func client(ctx context.Context, scope string) (*gdoc.Client, error) {
	keyFile, docID := os.Getenv("GOOGLE_SA_KEY_FILE"), os.Getenv("GOOGLE_DOC_ID")
	if keyFile == "" || docID == "" {
		return nil, errors.New("GOOGLE_SA_KEY_FILE and GOOGLE_DOC_ID must be set")
	}
	return gdoc.New(ctx, keyFile, docID, scope)
}

// findTask returns a pointer to the task with this id, or nil.
func findTask(f *tasks.File, id string) *tasks.Task {
	for i := range f.Tasks {
		if f.Tasks[i].ID == id {
			return &f.Tasks[i]
		}
	}
	return nil
}

// readTasks loads tasks.json.
func readTasks(path string) (tasks.File, error) {
	var f tasks.File
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return f, err
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		return f, fmt.Errorf("reading %s: %w", path, err)
	}
	return f, nil
}

// writeTasks saves tasks.json atomically (write a temp file, then rename).
func writeTasks(path string, f tasks.File) error {
	out, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

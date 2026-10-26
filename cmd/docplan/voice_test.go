package main

import (
	"testing"

	"github.com/katiedoherty1996/elara-agent/internal/tasks"
)

func TestApplyDecisionApproved(t *testing.T) {
	tk := tasks.Task{ID: "x", Status: "plan_review", PlanTabID: "t.1"}
	if err := applyDecision(&tk, "approved", "B", "keep it simple"); err != nil {
		t.Fatal(err)
	}
	if tk.Status != "pending" || !tk.PlanApproved || tk.PlanChoice != "B" || tk.PlanFeedback != "keep it simple" {
		t.Errorf("got %+v", tk)
	}
}

func TestApplyDecisionChangesAddsToEarlierNotes(t *testing.T) {
	tk := tasks.Task{ID: "x", Status: "plan_changes", PlanTabID: "t.1", PlanFeedback: "first"}
	if err := applyDecision(&tk, "changes", "", "second"); err != nil {
		t.Fatal(err)
	}
	if tk.Status != "plan_changes" || tk.PlanFeedback != "first\nsecond" {
		t.Errorf("got %+v", tk)
	}
}

func TestApplyDecisionRefuses(t *testing.T) {
	cases := []struct {
		name string
		tk   tasks.Task
		kind string
		note string
	}{
		{"no plan", tasks.Task{Status: "plan_review"}, "approved", ""},
		{"already building", tasks.Task{Status: "in_progress", PlanTabID: "t.1"}, "approved", ""},
		{"changes without notes", tasks.Task{Status: "plan_review", PlanTabID: "t.1"}, "changes", ""},
		{"unknown decision", tasks.Task{Status: "plan_review", PlanTabID: "t.1"}, "maybe", ""},
	}
	for _, c := range cases {
		tk := c.tk
		if err := applyDecision(&tk, c.kind, "", c.note); err == nil {
			t.Errorf("%s: want an error", c.name)
		}
	}
}

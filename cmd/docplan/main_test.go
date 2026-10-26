package main

import "testing"

func TestReadDecision(t *testing.T) {
	const notes = "## Your notes\n- keep it to one screen\n"
	tests := []struct {
		name, text string
		want       Decision
	}{
		{"placeholder", "Decision: ⏳ Waiting for Katie\n", Decision{Kind: "waiting"}},
		{"blank", "Decision:\n", Decision{Kind: "waiting"}},
		{"notes while still waiting", "Decision: ⏳ Waiting for Katie\n" + notes, Decision{Kind: "waiting"}},
		{"approved", "Decision: Approved\n", Decision{Kind: "approved"}},
		{"approved option", "Decision: approved option B\n", Decision{Kind: "approved", Choice: "option B"}},
		{"approved with notes", "Decision: Approved\n" + notes, Decision{Kind: "approved", Notes: "keep it to one screen"}},
		{"changes", "Decision: Changes: fewer steps\n", Decision{Kind: "changes", Notes: "Changes: fewer steps"}},
		{"changes with notes", "Decision: no, too many clicks\n" + notes, Decision{Kind: "changes", Notes: "no, too many clicks\nkeep it to one screen"}},
		{"no decision line", "## Plan\nsomething\n", Decision{Kind: "waiting"}},
		{"heading style", "## Decision: Approved\n", Decision{Kind: "approved"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ReadDecision(tt.text); got != tt.want {
				t.Errorf("ReadDecision = %+v, want %+v", got, tt.want)
			}
		})
	}
}

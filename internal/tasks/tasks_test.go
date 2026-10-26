package tasks

import (
	"reflect"
	"testing"
)

const sampleDoc = `Elara plan

Some intro text that is not a task.

Task: Library screen
Priority: high
Design: library
What: Show all of a user's posts with filter chips.
Done when:
- Posts list shows title, type, status
- Filter chips filter the list

## Task: Sign up and log in
Users can create an account and an organisation.
Done when:
1. Sign up creates user + org
2. Login rate limited

Task: Library screen
What: duplicate title
`

func TestParse(t *testing.T) {
	got := Parse(sampleDoc)
	if len(got) != 3 {
		t.Fatalf("parsed %d tasks, want 3: %+v", len(got), got)
	}

	lib := got[0]
	if lib.ID != "library-screen" || lib.Priority != "high" || lib.Design != "library" {
		t.Errorf("library task fields wrong: %+v", lib)
	}
	wantDone := []string{"Posts list shows title, type, status", "Filter chips filter the list"}
	if !reflect.DeepEqual(lib.DoneWhen, wantDone) {
		t.Errorf("DoneWhen = %q, want %q", lib.DoneWhen, wantDone)
	}

	auth := got[1]
	if auth.ID != "sign-up-and-log-in" || auth.Priority != "normal" || auth.What != "Users can create an account and an organisation." {
		t.Errorf("auth task fields wrong: %+v", auth)
	}
	if len(auth.DoneWhen) != 2 || auth.DoneWhen[1] != "Login rate limited" {
		t.Errorf("numbered bullets not parsed: %q", auth.DoneWhen)
	}

	if got[2].ID != "library-screen-2" {
		t.Errorf("duplicate title id = %q, want library-screen-2", got[2].ID)
	}
	for _, task := range got {
		if task.Status != StatusPending || !task.FromDoc {
			t.Errorf("new task %s should be pending and fromDoc", task.ID)
		}
	}
}

func TestMerge(t *testing.T) {
	existing := File{Tasks: []Task{
		{ID: "library-screen", Title: "Library screen", What: "old", Status: StatusInReview, Branch: "task/library-screen", PR: 7, FromDoc: true},
		{ID: "done-task", Title: "Done", What: "original", Status: StatusDone, FromDoc: true},
		{ID: "fix-login-500", Title: "fix: login 500", Status: StatusPending, ParentID: "sign-up", FromDoc: false},
		{ID: "deleted-from-doc", Title: "Gone", Status: StatusPending, FromDoc: true},
	}}
	parsed := []Task{
		{ID: "library-screen", Title: "Library screen", What: "new description", Priority: "high", Status: StatusPending, FromDoc: true},
		{ID: "done-task", Title: "Done", What: "edited after done", Status: StatusPending, FromDoc: true},
		{ID: "brand-new", Title: "Brand new", Status: StatusPending, FromDoc: true},
	}

	got := Merge(existing, parsed)
	byID := map[string]Task{}
	for _, task := range got.Tasks {
		byID[task.ID] = task
	}

	lib := byID["library-screen"]
	if lib.Status != StatusInReview || lib.PR != 7 || lib.Branch != "task/library-screen" {
		t.Errorf("existing progress lost: %+v", lib)
	}
	if lib.What != "new description" || lib.Priority != "high" {
		t.Errorf("description not refreshed from doc: %+v", lib)
	}
	if byID["done-task"].What != "original" {
		t.Error("a done task's description was changed")
	}
	if _, ok := byID["fix-login-500"]; !ok {
		t.Error("agent-created fix task was dropped")
	}
	if !byID["deleted-from-doc"].RemovedInDoc {
		t.Error("task removed from doc was not flagged")
	}
	if byID["brand-new"].Status != StatusPending {
		t.Error("new doc task not added as pending")
	}
	if len(got.Tasks) != 5 {
		t.Errorf("got %d tasks, want 5", len(got.Tasks))
	}
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"Library screen!":         "library-screen",
		"  AI Chat — streaming  ": "ai-chat-streaming",
		"!!!":                     "task",
	}
	for in, want := range tests {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

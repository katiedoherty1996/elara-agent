// Package tasks turns Katie's Google Doc (as plain text) into the agent's task list and
// merges it into .agent/tasks.json without losing the status of work already started.
package tasks

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Status values a task moves through.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusInReview   = "in_review"
	StatusDone       = "done"
	// A plan is in the Google Doc, waiting for Katie (see cmd/docplan).
	StatusPlanReview = "plan_review"
	// Katie asked for changes to the plan; the planner rewrites it.
	StatusPlanChanges = "plan_changes"
)

// Task is one unit of work for the agent.
type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Priority     string   `json:"priority"`
	Design       string   `json:"design,omitempty"`
	What         string   `json:"what,omitempty"`
	DoneWhen     []string `json:"doneWhen,omitempty"`
	Status       string   `json:"status"`
	Branch       string   `json:"branch,omitempty"`
	PR           int      `json:"pr,omitempty"`
	ParentID     string   `json:"parentId,omitempty"` // set on fix: tasks created by QA or review
	Notes        string   `json:"notes,omitempty"`
	FromDoc      bool     `json:"fromDoc"`                // false for tasks the agent created itself
	RemovedInDoc bool     `json:"removedInDoc,omitempty"` // task heading was deleted from the doc

	// Plan review (ux-planner + cmd/docplan).
	PlanTabID    string `json:"planTabId,omitempty"`    // the plan's tab in the Google Doc
	PlanDecision string `json:"planDecision,omitempty"` // waiting | approved | changes
	PlanChoice   string `json:"planChoice,omitempty"`   // e.g. "option B"
	PlanFeedback string `json:"planFeedback,omitempty"` // Katie's notes on the plan
	PlanApproved bool   `json:"planApproved,omitempty"` // build only once this is true

	// Extra keeps fields this struct doesn't know, so a doc sync never drops them.
	Extra map[string]json.RawMessage `json:"-"`
}

// File is the shape of .agent/tasks.json.
type File struct {
	Tasks []Task `json:"tasks"`
}

var (
	taskLine  = regexp.MustCompile(`(?i)^\s*#*\s*task\s*:\s*(.+?)\s*$`)
	fieldLine = regexp.MustCompile(`(?i)^\s*(priority|design|what|done when)\s*:\s*(.*?)\s*$`)
	bulletRe  = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+(.*\S)\s*$`)
	slugStrip = regexp.MustCompile(`[^a-z0-9]+`)
)

// Parse reads the document text and returns the tasks in document order. A task starts
// at a line "Task: <title>" (optionally a heading) and runs until the next one. Known
// fields are Priority, Design, What and "Done when" (followed by bullet lines).
func Parse(doc string) []Task {
	var out []Task
	var cur *Task
	inDoneWhen := false

	flush := func() {
		if cur != nil {
			out = append(out, *cur)
		}
	}

	for _, line := range strings.Split(doc, "\n") {
		if m := taskLine.FindStringSubmatch(line); m != nil {
			flush()
			cur = &Task{ID: Slug(m[1]), Title: m[1], Priority: "normal", Status: StatusPending, FromDoc: true}
			inDoneWhen = false
			continue
		}
		if cur == nil {
			continue
		}
		if m := fieldLine.FindStringSubmatch(line); m != nil {
			inDoneWhen = false
			switch strings.ToLower(m[1]) {
			case "priority":
				cur.Priority = normalisePriority(m[2])
			case "design":
				cur.Design = strings.TrimSpace(m[2])
			case "what":
				cur.What = m[2]
			case "done when":
				inDoneWhen = true
				if m[2] != "" {
					cur.DoneWhen = append(cur.DoneWhen, m[2])
				}
			}
			continue
		}
		if b := bulletRe.FindStringSubmatch(line); b != nil {
			if inDoneWhen {
				cur.DoneWhen = append(cur.DoneWhen, b[1])
			} else {
				cur.What = joinSentence(cur.What, b[1])
			}
			continue
		}
		if text := strings.TrimSpace(line); text != "" && !inDoneWhen {
			// Free text under a task (no field label) is treated as more description.
			cur.What = joinSentence(cur.What, text)
		}
	}
	flush()
	if len(out) == 0 {
		// No "Task:" lines: treat every bullet as a task (Katie's "Future Plans" list).
		out = parseBullets(doc)
	}
	return dedupeIDs(out)
}

// bulletIDWords is how many opening words of a bullet make its id, so small edits
// later in the bullet keep the same task (and the same plan tab).
const bulletIDWords = 6

// parseBullets makes one task per "- " line. The title is the bullet's first sentence
// (shortened), the whole bullet is the description, and the heading above it, if any,
// is kept in the description for context.
func parseBullets(doc string) []Task {
	var out []Task
	heading := ""
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") {
			heading = strings.TrimSpace(line[3:])
			continue
		}
		b := bulletRe.FindStringSubmatch(line)
		if b == nil {
			continue
		}
		text := strings.TrimSpace(b[1])
		words := strings.Fields(text)
		idWords := words
		if len(idWords) > bulletIDWords {
			idWords = idWords[:bulletIDWords]
		}
		what := text
		if heading != "" {
			what = heading + ": " + text
		}
		out = append(out, Task{
			ID: Slug(strings.Join(idWords, " ")), Title: bulletTitle(text), Priority: "normal",
			What: what, Status: StatusPending, FromDoc: true,
		})
	}
	return out
}

// bulletTitle is the bullet's first sentence, cut to about 80 characters on a word.
func bulletTitle(text string) string {
	if i := strings.IndexAny(text, ".!?"); i > 0 {
		text = text[:i]
	}
	if len(text) <= 80 {
		return text
	}
	cut := strings.LastIndex(text[:80], " ")
	if cut < 40 {
		cut = 80
	}
	return strings.TrimSpace(text[:cut]) + "…"
}

// Merge combines tasks parsed from the doc with the existing task file. Existing tasks
// keep their status, branch, PR and notes; their description is refreshed from the doc
// unless they are already done. New doc tasks are appended as pending. Tasks the agent
// created itself (fix: tasks) are kept. Tasks deleted from the doc are kept but flagged.
func Merge(existing File, parsed []Task) File {
	byID := make(map[string]int, len(existing.Tasks))
	for i, t := range existing.Tasks {
		byID[t.ID] = i
	}
	inDoc := make(map[string]bool, len(parsed))

	merged := File{Tasks: make([]Task, len(existing.Tasks), len(existing.Tasks)+len(parsed))}
	copy(merged.Tasks, existing.Tasks)

	for _, p := range parsed {
		inDoc[p.ID] = true
		i, ok := byID[p.ID]
		if !ok {
			merged.Tasks = append(merged.Tasks, p)
			continue
		}
		t := &merged.Tasks[i]
		t.RemovedInDoc = false
		if t.Status != StatusDone {
			t.Title, t.Priority, t.Design, t.What, t.DoneWhen = p.Title, p.Priority, p.Design, p.What, p.DoneWhen
		}
	}
	for i := range merged.Tasks {
		t := &merged.Tasks[i]
		if t.FromDoc && !inDoc[t.ID] {
			t.RemovedInDoc = true
		}
	}
	return merged
}

// Slug turns a title into a stable id: "Library screen!" → "library-screen".
func Slug(title string) string {
	s := strings.Trim(slugStrip.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		s = "task"
	}
	return s
}

// normalisePriority maps free text to high, normal or low.
func normalisePriority(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "high", "urgent", "p0", "p1":
		return "high"
	case "low", "p3", "later":
		return "low"
	default:
		return "normal"
	}
}

// joinSentence appends more text to a description with a single space.
func joinSentence(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

// dedupeIDs makes ids unique when two tasks share a title ("x", "x-2", ...).
func dedupeIDs(ts []Task) []Task {
	seen := make(map[string]int, len(ts))
	for i := range ts {
		base := ts[i].ID
		seen[base]++
		if n := seen[base]; n > 1 {
			ts[i].ID = base + "-" + strconv.Itoa(n)
		}
	}
	return ts
}

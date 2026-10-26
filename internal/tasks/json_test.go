package tasks

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTaskKeepsUnknownFields(t *testing.T) {
	in := `{"tasks":[{"id":"a","title":"A","priority":"normal","status":"pending","fromDoc":false,"linkedTo":"b","parent":{"x":1}}]}`
	var f File
	if err := json.Unmarshal([]byte(in), &f); err != nil {
		t.Fatal(err)
	}
	// A doc sync merges and writes the file back.
	merged := Merge(f, nil)
	out, err := json.Marshal(merged)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"linkedTo":"b"`, `"parent":{"x":1}`, `"id":"a"`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("round trip lost %s: %s", want, out)
		}
	}
}

func TestTaskPlanFieldsRoundTrip(t *testing.T) {
	in := Task{ID: "a", Status: "plan_review", PlanTabID: "t.1", PlanApproved: true, PlanChoice: "option B"}
	b, _ := json.Marshal(in)
	var got Task
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.PlanTabID != "t.1" || !got.PlanApproved || got.PlanChoice != "option B" || got.Extra != nil {
		t.Errorf("plan fields lost or leaked into Extra: %+v", got)
	}
}

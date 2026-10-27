package tasks

import "encoding/json"

// UnmarshalJSON reads a task and keeps any fields this struct doesn't know (the agent
// adds its own, like "linkedTo"), so syncing the doc never drops them.
func (t *Task) UnmarshalJSON(b []byte) error {
	type plain Task
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for _, k := range knownKeys() {
		delete(all, k)
	}
	*t = Task(p)
	t.Extra = nil
	if len(all) > 0 {
		t.Extra = all
	}
	return nil
}

// MarshalJSON writes the known fields followed by the kept unknown ones.
func (t Task) MarshalJSON() ([]byte, error) {
	type plain Task
	known, err := json.Marshal(plain(t))
	if err != nil || len(t.Extra) == 0 {
		return known, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(known, &merged); err != nil {
		return nil, err
	}
	for k, v := range t.Extra {
		if _, clash := merged[k]; !clash {
			merged[k] = v
		}
	}
	return json.Marshal(merged)
}

// knownKeys lists the JSON names of Task's own fields.
func knownKeys() []string {
	var m map[string]json.RawMessage
	type plain Task
	b, _ := json.Marshal(plain{Status: "x", FromDoc: true, Branch: "x", PR: 1, ParentID: "x", Notes: "x",
		Design: "x", What: "x", DoneWhen: []string{"x"}, RemovedInDoc: true, PlanTabID: "x", PlanTabURL: "x", PlanDecision: "x",
		PlanChoice: "x", PlanFeedback: "x", PlanApproved: true})
	_ = json.Unmarshal(b, &m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

package main

import (
	"encoding/json"
	"testing"
)

// sampleResponse is a trimmed Docs API response with a nested tab.
const sampleResponse = `{"tabs": [
  {"tabProperties": {"tabId": "t.0", "title": "Notes"},
   "documentTab": {"body": {"content": [{"paragraph": {"elements": [{"textRun": {"content": "not tasks\n"}}]}}]}},
   "childTabs": [
     {"tabProperties": {"tabId": "t.exampletab01", "title": "Elara tasks"},
      "documentTab": {"body": {"content": [
        {"paragraph": {"paragraphStyle": {"namedStyleType": "HEADING_2"}, "elements": [{"textRun": {"content": "Task: Library screen\n"}}]}},
        {"paragraph": {"elements": [{"textRun": {"content": "Done when:\n"}}]}},
        {"paragraph": {"bullet": {}, "elements": [{"textRun": {"content": "Shows posts\n"}}]}},
        {"sectionBreak": {}}
      ]}}}
   ]}
]}`

func TestFindTabAndFlatten(t *testing.T) {
	var doc docsResponse
	if err := json.Unmarshal([]byte(sampleResponse), &doc); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		tabID    string
		wantOK   bool
		wantText string
	}{
		{name: "nested tab by id", tabID: "t.exampletab01", wantOK: true, wantText: "## Task: Library screen\nDone when:\n- Shows posts\n"},
		{name: "empty id means first tab", tabID: "", wantOK: true, wantText: "not tasks\n"},
		{name: "unknown tab", tabID: "t.nope", wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tab, ok := findTab(doc.Tabs, tt.tabID)
			if ok != tt.wantOK {
				t.Fatalf("findTab ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got := flatten(tab.DocumentTab.Body); got != tt.wantText {
				t.Errorf("flatten = %q, want %q", got, tt.wantText)
			}
		})
	}
}

func TestTabTitlesListsNestedTabs(t *testing.T) {
	var doc docsResponse
	if err := json.Unmarshal([]byte(sampleResponse), &doc); err != nil {
		t.Fatal(err)
	}
	if got := tabTitles(doc.Tabs); len(got) != 2 {
		t.Errorf("tabTitles = %v, want 2 entries", got)
	}
}

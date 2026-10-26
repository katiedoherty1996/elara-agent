package gdoc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseInline(t *testing.T) {
	tests := []struct {
		in, want string
		spans    []span
	}{
		{"plain", "plain", nil},
		{"a **b** c", "a b c", []span{{2, 3, ""}}},
		{"see [Notion](https://notion.so) now", "see Notion now", []span{{4, 10, "https://notion.so"}}},
		// "é" is one UTF-16 unit, the emoji is two.
		{"é😀 **x**", "é😀 x", []span{{4, 5, ""}}},
	}
	for _, tt := range tests {
		got, spans := parseInline(tt.in)
		if got != tt.want {
			t.Errorf("parseInline(%q) text = %q, want %q", tt.in, got, tt.want)
		}
		if len(spans) != len(tt.spans) {
			t.Fatalf("parseInline(%q) spans = %v, want %v", tt.in, spans, tt.spans)
		}
		for i := range spans {
			if spans[i] != tt.spans[i] {
				t.Errorf("parseInline(%q) span %d = %v, want %v", tt.in, i, spans[i], tt.spans[i])
			}
		}
	}
}

func TestMarkdownRequestsIndexes(t *testing.T) {
	md := "# Title\n- one\n![desktop](a.png)\n|A|B|\n|---|---|\nend"
	reqs := MarkdownRequests(md, "t.1", map[string]Image{"a.png": {URL: "https://x/a.png"}})
	raw, _ := json.Marshal(reqs)
	s := string(raw)

	// Inserts land one after another: "Title\n" at 1 (6 units), "one\n" at 7, image at 11,
	// its newline at 12, "A  ·  B\n" at 13, "end\n" at 21.
	for _, want := range []string{
		`"location":{"index":1,"tabId":"t.1"},"text":"Title\n"`,
		`"location":{"index":7,"tabId":"t.1"},"text":"one\n"`,
		`"insertInlineImage":{"location":{"index":11,`,
		`"location":{"index":12,"tabId":"t.1"},"text":"\n"`,
		`"location":{"index":13,"tabId":"t.1"},"text":"A  ·  B\n"`,
		`"location":{"index":21,"tabId":"t.1"},"text":"end\n"`,
		`"namedStyleType":"HEADING_1"`,
		`"createParagraphBullets"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("requests missing %s\n%s", want, s)
		}
	}
	if strings.Contains(s, "---") {
		t.Error("table separator row was written")
	}
}

func TestMarkdownRequestsUnknownImage(t *testing.T) {
	raw, _ := json.Marshal(MarkdownRequests("![x](missing.png)", "t.1", nil))
	if !strings.Contains(string(raw), "(image: missing.png)") {
		t.Errorf("unknown image not written as text: %s", raw)
	}
}

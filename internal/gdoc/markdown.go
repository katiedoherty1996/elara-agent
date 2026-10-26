package gdoc

import (
	"regexp"
	"strings"
	"unicode/utf16"
)

// Image is a picture to place in the tab: the markdown line "![alt](path)" is replaced
// by the image at URL (Docs copies it when the request runs, so the URL can be
// short-lived).
type Image struct {
	Alt, URL string
}

var (
	imageLine = regexp.MustCompile(`^!\[([^\]]*)\]\(([^)]+)\)\s*$`)
	// inline matches **bold** or [text](url).
	inline   = regexp.MustCompile(`\*\*(.+?)\*\*|\[([^\]]+)\]\((https?://[^)\s]+)\)`)
	tableSep = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
)

// MarkdownRequests turns a small markdown subset into Docs batchUpdate requests that
// write it into an empty tab, starting at index 1. Supported: "#", "##", "###"
// headings, "- " bullets, **bold**, [links](https://...), "![alt](path)" image lines
// (looked up in images by path; unknown paths become a plain "(image: path)" line) and
// "|" tables, which are written as plain lines with the cells separated by " · ".
func MarkdownRequests(md, tabID string, images map[string]Image) []map[string]any {
	var reqs []map[string]any
	cur := 1
	loc := func(i int) map[string]any { return map[string]any{"index": i, "tabId": tabID} }
	rng := func(a, b int) map[string]any { return map[string]any{"startIndex": a, "endIndex": b, "tabId": tabID} }

	for _, raw := range strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")

		if m := imageLine.FindStringSubmatch(line); m != nil {
			if img, ok := images[m[2]]; ok {
				width := 460.0
				if strings.Contains(strings.ToLower(m[1]), "phone") || strings.Contains(strings.ToLower(m[1]), "mobile") {
					width = 200
				}
				reqs = append(reqs,
					map[string]any{"insertInlineImage": map[string]any{
						"uri": img.URL, "location": loc(cur),
						"objectSize": map[string]any{"width": map[string]any{"magnitude": width, "unit": "PT"}},
					}},
					map[string]any{"insertText": map[string]any{"text": "\n", "location": loc(cur + 1)}},
				)
				cur += 2
				continue
			}
			line = "(image: " + m[2] + ")"
		}

		style, bullet := "NORMAL_TEXT", false
		switch {
		case strings.HasPrefix(line, "### "):
			style, line = "HEADING_3", line[4:]
		case strings.HasPrefix(line, "## "):
			style, line = "HEADING_2", line[3:]
		case strings.HasPrefix(line, "# "):
			style, line = "HEADING_1", line[2:]
		case strings.HasPrefix(line, "- "), strings.HasPrefix(line, "* "):
			bullet, line = true, line[2:]
		case tableSep.MatchString(line) && strings.Contains(line, "-"):
			continue
		case strings.HasPrefix(line, "|"):
			cells := strings.Split(strings.Trim(line, "| "), "|")
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
			line = strings.Join(cells, "  ·  ")
		}

		text, spans := parseInline(line)
		n := u16len(text) + 1 // + the paragraph's newline
		reqs = append(reqs,
			map[string]any{"insertText": map[string]any{"text": text + "\n", "location": loc(cur)}},
			map[string]any{"updateParagraphStyle": map[string]any{
				"range": rng(cur, cur+n), "paragraphStyle": map[string]any{"namedStyleType": style},
				"fields": "namedStyleType",
			}},
			// Inserted text takes the style of the text before it, so reset first.
			map[string]any{"updateTextStyle": map[string]any{
				"range": rng(cur, cur+n), "textStyle": map[string]any{}, "fields": "bold,link",
			}},
		)
		if bullet {
			reqs = append(reqs, map[string]any{"createParagraphBullets": map[string]any{
				"range": rng(cur, cur+n), "bulletPreset": "BULLET_DISC_CIRCLE_SQUARE",
			}})
		}
		for _, s := range spans {
			st, fields := map[string]any{"bold": true}, "bold"
			if s.url != "" {
				st, fields = map[string]any{"link": map[string]any{"url": s.url}}, "link"
			}
			reqs = append(reqs, map[string]any{"updateTextStyle": map[string]any{
				"range": rng(cur+s.start, cur+s.end), "textStyle": st, "fields": fields,
			}})
		}
		cur += n
	}
	return reqs
}

// span is a bold or linked stretch of a paragraph, in UTF-16 offsets from its start.
type span struct {
	start, end int
	url        string
}

// parseInline strips **bold** and [text](url) markers and returns the plain text with
// the spans to style.
func parseInline(s string) (string, []span) {
	var out strings.Builder
	var spans []span
	last := 0
	for _, m := range inline.FindAllStringSubmatchIndex(s, -1) {
		out.WriteString(s[last:m[0]])
		start := u16len(out.String())
		if m[2] >= 0 { // **bold**
			out.WriteString(s[m[2]:m[3]])
			spans = append(spans, span{start: start, end: u16len(out.String())})
		} else { // [text](url)
			out.WriteString(s[m[4]:m[5]])
			spans = append(spans, span{start: start, end: u16len(out.String()), url: s[m[6]:m[7]]})
		}
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String(), spans
}

// u16len is the length Docs uses for indexes: UTF-16 code units.
func u16len(s string) int { return len(utf16.Encode([]rune(s))) }

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"golang.org/x/oauth2/google"
)

// docBody is the part of a Google Doc (or one of its tabs) that holds the text.
type docBody struct {
	Content []struct {
		Paragraph *struct {
			Elements []struct {
				TextRun *struct {
					Content string `json:"content"`
				} `json:"textRun"`
			} `json:"elements"`
			Bullet         *struct{} `json:"bullet"`
			ParagraphStyle struct {
				NamedStyleType string `json:"namedStyleType"`
			} `json:"paragraphStyle"`
		} `json:"paragraph"`
	} `json:"content"`
}

// docTab is one tab of a Google Doc. Tabs can contain child tabs.
type docTab struct {
	TabProperties struct {
		TabID string `json:"tabId"`
		Title string `json:"title"`
	} `json:"tabProperties"`
	DocumentTab struct {
		Body docBody `json:"body"`
	} `json:"documentTab"`
	ChildTabs []docTab `json:"childTabs"`
}

// docsResponse is the small part of the Docs API response we read. With
// includeTabsContent=true the text lives under Tabs instead of Body.
type docsResponse struct {
	Tabs []docTab `json:"tabs"`
}

// fetchDocText downloads the doc and returns the text of the tab with id tabID (the
// "?tab=t.xxxx" part of the doc URL), or of the first tab when tabID is empty.
func fetchDocText(ctx context.Context, keyFile, docID, tabID string) (string, error) {
	key, err := os.ReadFile(keyFile) //nolint:gosec // path from the operator's .env
	if err != nil {
		return "", fmt.Errorf("reading service account key: %w", err)
	}
	creds, err := google.CredentialsFromJSONWithType(ctx, key, google.ServiceAccount, "https://www.googleapis.com/auth/documents.readonly")
	if err != nil {
		return "", fmt.Errorf("loading service account key: %w", err)
	}
	tok, err := creds.TokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("getting access token: %w", err)
	}

	// Host is fixed and docID was validated against docIDPattern, so this can't be
	// pointed anywhere else.
	url := "https://docs.googleapis.com/v1/documents/" + docID + "?includeTabsContent=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // validated id, fixed host
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // validated id, fixed host
	if err != nil {
		return "", fmt.Errorf("calling Docs API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return "", fmt.Errorf("docs API returned %s: %s (is the doc shared with the service account email?)", resp.Status, body)
	}

	var doc docsResponse
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("decoding doc: %w", err)
	}

	tab, ok := findTab(doc.Tabs, tabID)
	if !ok {
		return "", fmt.Errorf("tab %q not found in the doc (tabs: %s)", tabID, strings.Join(tabTitles(doc.Tabs), ", "))
	}
	return flatten(tab.DocumentTab.Body), nil
}

// findTab returns the tab with the given id, searching child tabs too. An empty id
// means the first tab.
func findTab(tabs []docTab, id string) (docTab, bool) {
	if id == "" {
		if len(tabs) == 0 {
			return docTab{}, false
		}
		return tabs[0], true
	}
	for _, t := range tabs {
		if t.TabProperties.TabID == id {
			return t, true
		}
		if found, ok := findTab(t.ChildTabs, id); ok {
			return found, true
		}
	}
	return docTab{}, false
}

// tabTitles lists "title (id)" for every tab, for a helpful error message.
func tabTitles(tabs []docTab) []string {
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, fmt.Sprintf("%q (%s)", t.TabProperties.Title, t.TabProperties.TabID))
		out = append(out, tabTitles(t.ChildTabs)...)
	}
	return out
}

// flatten turns a doc body into plain text, keeping headings as "## " lines and list
// items as "- " lines so the task parser can read them.
func flatten(body docBody) string {
	var b strings.Builder
	for _, c := range body.Content {
		if c.Paragraph == nil {
			continue
		}
		var line strings.Builder
		for _, e := range c.Paragraph.Elements {
			if e.TextRun != nil {
				line.WriteString(e.TextRun.Content)
			}
		}
		text := strings.TrimRight(line.String(), "\n")
		switch {
		case strings.HasPrefix(c.Paragraph.ParagraphStyle.NamedStyleType, "HEADING_"):
			b.WriteString("## " + text)
		case c.Paragraph.Bullet != nil:
			b.WriteString("- " + text)
		default:
			b.WriteString(text)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Package gdoc is a small Google Docs API client for the agent: read a doc's tabs and
// send batchUpdate requests, authenticated with a service account key.
package gdoc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"

	"golang.org/x/oauth2/google"
)

// Scopes for reading only, and for adding and editing plan tabs.
const (
	ScopeRead  = "https://www.googleapis.com/auth/documents.readonly"
	ScopeWrite = "https://www.googleapis.com/auth/documents"
)

// docIDPattern matches Google Doc ids (letters, digits, - and _).
var docIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

// Client talks to one Google Doc.
type Client struct {
	docID string
	http  *http.Client
}

// New reads the service account key and returns a client for docID with the given scope.
func New(ctx context.Context, keyFile, docID, scope string) (*Client, error) {
	// The doc id goes into a URL path, so only allow the characters Google uses.
	if !docIDPattern.MatchString(docID) {
		return nil, fmt.Errorf("GOOGLE_DOC_ID %q doesn't look like a Google Doc id (copy the part between /d/ and /edit)", docID)
	}
	key, err := os.ReadFile(keyFile) //nolint:gosec // path from the operator's .env
	if err != nil {
		return nil, fmt.Errorf("reading service account key: %w", err)
	}
	creds, err := google.CredentialsFromJSONWithType(ctx, key, google.ServiceAccount, scope)
	if err != nil {
		return nil, fmt.Errorf("loading service account key: %w", err)
	}
	return &Client{docID: docID, http: oauth2Client(ctx, creds)}, nil
}

// Tab is one tab of a Google Doc. Tabs can contain child tabs.
type Tab struct {
	TabProperties struct {
		TabID       string `json:"tabId"`
		Title       string `json:"title"`
		ParentTabID string `json:"parentTabId"`
	} `json:"tabProperties"`
	DocumentTab struct {
		Body Body `json:"body"`
	} `json:"documentTab"`
	ChildTabs []Tab `json:"childTabs"`
}

// Body is the part of a tab that holds the text.
type Body struct {
	Content []struct {
		EndIndex  int `json:"endIndex"`
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

// EndIndex is the index just past the tab's last character (its final newline).
func (b Body) EndIndex() int {
	if len(b.Content) == 0 {
		return 1
	}
	return b.Content[len(b.Content)-1].EndIndex
}

// Text turns the body into plain text, keeping headings as "## " lines and list items
// as "- " lines.
func (b Body) Text() string {
	var out strings.Builder
	for _, c := range b.Content {
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
			out.WriteString("## " + text)
		case c.Paragraph.Bullet != nil:
			out.WriteString("- " + text)
		default:
			out.WriteString(text)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// Tabs downloads the doc and returns its top-level tabs (with content and child tabs).
func (c *Client) Tabs(ctx context.Context) ([]Tab, error) {
	var doc struct {
		Tabs []Tab `json:"tabs"`
	}
	// Host is fixed and docID was validated, so this can't be pointed anywhere else.
	url := "https://docs.googleapis.com/v1/documents/" + c.docID + "?includeTabsContent=true"
	if err := c.do(ctx, http.MethodGet, url, nil, &doc); err != nil {
		return nil, err
	}
	return doc.Tabs, nil
}

// BatchUpdate sends requests (applied in order, atomically) and returns the replies.
func (c *Client) BatchUpdate(ctx context.Context, requests []map[string]any) ([]json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{"requests": requests})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Replies []json.RawMessage `json:"replies"`
	}
	url := "https://docs.googleapis.com/v1/documents/" + c.docID + ":batchUpdate"
	if err := c.do(ctx, http.MethodPost, url, body, &resp); err != nil {
		return nil, err
	}
	return resp.Replies, nil
}

// do sends one request and decodes the JSON response into out.
func (c *Client) do(ctx context.Context, method, url string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body)) //nolint:gosec // validated id, fixed host
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req) //nolint:gosec // validated id, fixed host
	if err != nil {
		return fmt.Errorf("calling Docs API: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("docs API returned %s: %s (is the doc shared with the service account email, as Editor for writes?)", resp.Status, msg)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// FindTab returns the tab with the given id, searching child tabs too. An empty id
// means the first tab.
func FindTab(tabs []Tab, id string) (Tab, bool) {
	if id == "" {
		if len(tabs) == 0 {
			return Tab{}, false
		}
		return tabs[0], true
	}
	for _, t := range tabs {
		if t.TabProperties.TabID == id {
			return t, true
		}
		if found, ok := FindTab(t.ChildTabs, id); ok {
			return found, true
		}
	}
	return Tab{}, false
}

// FindTopLevelTitle returns the top-level tab with this exact title.
func FindTopLevelTitle(tabs []Tab, title string) (Tab, bool) {
	for _, t := range tabs {
		if t.TabProperties.Title == title {
			return t, true
		}
	}
	return Tab{}, false
}

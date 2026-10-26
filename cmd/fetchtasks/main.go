// Command fetchtasks reads Katie's task Google Doc with a service account (read-only),
// saves its text to .agent/tasks-source.md and merges the tasks into .agent/tasks.json.
//
//	GOOGLE_SA_KEY_FILE=$HOME/.config/elara/gdocs-sa.json GOOGLE_DOC_ID=<id> \
//	  go run ./cmd/fetchtasks -tasks ~/elara/.agent/tasks.json -out ~/elara/.agent/tasks-source.md
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/katiedoherty1996/elara-agent/internal/tasks"
)

// docIDPattern matches Google Doc ids (letters, digits, - and _).
var docIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

func main() {
	tasksPath := flag.String("tasks", ".agent/tasks.json", "task file to merge into")
	outPath := flag.String("out", ".agent/tasks-source.md", "where to save the doc text")
	flag.Parse()

	if err := run(*tasksPath, *outPath); err != nil {
		fmt.Fprintln(os.Stderr, "fetchtasks:", err)
		os.Exit(1)
	}
}

// run fetches the doc, writes its text, and merges the parsed tasks.
func run(tasksPath, outPath string) error {
	keyFile, docID := os.Getenv("GOOGLE_SA_KEY_FILE"), os.Getenv("GOOGLE_DOC_ID")
	if keyFile == "" || docID == "" {
		return errors.New("GOOGLE_SA_KEY_FILE and GOOGLE_DOC_ID must be set")
	}
	// The doc id goes into a URL path, so only allow the characters Google uses.
	if !docIDPattern.MatchString(docID) {
		return fmt.Errorf("GOOGLE_DOC_ID %q doesn't look like a Google Doc id (copy the part between /d/ and /edit)", docID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Optional: which tab holds the tasks (the "t.xxxx" in "?tab=t.xxxx" of the doc URL).
	text, err := fetchDocText(ctx, keyFile, docID, os.Getenv("GOOGLE_DOC_TAB_ID"))
	if err != nil {
		return err
	}
	// outPath and tasksPath come from the operator's own command-line flags, not user input.
	outPath, tasksPath = filepath.Clean(outPath), filepath.Clean(tasksPath)
	if err := os.WriteFile(outPath, []byte(text), 0o600); err != nil { //nolint:gosec // operator-supplied path
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	var existing tasks.File
	if raw, err := os.ReadFile(tasksPath); err == nil { //nolint:gosec // operator-supplied path
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("reading %s: %w", tasksPath, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	merged := tasks.Merge(existing, tasks.Parse(text))
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tasksPath, append(out, '\n'), 0o600); err != nil { //nolint:gosec // operator-supplied path
		return fmt.Errorf("writing %s: %w", tasksPath, err)
	}
	fmt.Printf("fetchtasks: %d tasks in doc, %d in task file\n", len(tasks.Parse(text)), len(merged.Tasks))
	return nil
}

package tasks

import (
	"strings"
	"testing"
)

const bulletDoc = `## Future Plans
- Better and easier way to hook elara up to a website so we can publish. More detail here.
- Add elevenlabs as the voice over for articles, the user can choose a voice.
Some text that is not a bullet.
`

func TestParseBulletsWhenNoTaskLines(t *testing.T) {
	got := Parse(bulletDoc)
	if len(got) != 2 {
		t.Fatalf("parsed %d tasks, want 2: %+v", len(got), got)
	}
	if got[0].ID != "better-and-easier-way-to-hook" {
		t.Errorf("id = %q, want the first six words", got[0].ID)
	}
	if got[0].Title != "Better and easier way to hook elara up to a website so we can publish" {
		t.Errorf("title = %q, want the first sentence", got[0].Title)
	}
	if !strings.HasPrefix(got[0].What, "Future Plans: ") || !strings.Contains(got[0].What, "More detail here.") {
		t.Errorf("what = %q, want heading + whole bullet", got[0].What)
	}
	if !got[1].FromDoc || got[1].Status != StatusPending {
		t.Errorf("bullet task should be pending and fromDoc: %+v", got[1])
	}
}

func TestBulletEditKeepsID(t *testing.T) {
	a := Parse("- Add elevenlabs as the voice over for articles.")
	b := Parse("- Add elevenlabs as the voice over for articles and the website too, with a picker.")
	if a[0].ID != b[0].ID {
		t.Errorf("editing the end of a bullet changed its id: %q -> %q", a[0].ID, b[0].ID)
	}
}

func TestTaskLinesStillWin(t *testing.T) {
	got := Parse("Task: Library\n- a bullet that is part of the task\n")
	if len(got) != 1 || got[0].ID != "library" {
		t.Errorf("Task: lines should still be used when present: %+v", got)
	}
}

func TestBulletTitleLong(t *testing.T) {
	long := strings.Repeat("word ", 40)
	if got := bulletTitle(long); len(got) > 84 || !strings.HasSuffix(got, "…") {
		t.Errorf("bulletTitle(long) = %q", got)
	}
}

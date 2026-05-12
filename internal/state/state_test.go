package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_MissingReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	s, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.LastSlug != "" {
		t.Errorf("LastSlug = %q, want empty", s.LastSlug)
	}
	if s.Version != Version {
		t.Errorf("Version = %d, want %d", s.Version, Version)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	in := &State{
		LastSlug:      "sts-launch",
		LastNoteUsed:  "Footer image refresh",
		LastCaptureAt: time.Date(2026, 5, 12, 10, 34, 11, 0, time.UTC),
	}
	if err := Save(root, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.LastSlug != in.LastSlug {
		t.Errorf("LastSlug = %q, want %q", out.LastSlug, in.LastSlug)
	}
	if out.LastNoteUsed != in.LastNoteUsed {
		t.Errorf("LastNoteUsed = %q, want %q", out.LastNoteUsed, in.LastNoteUsed)
	}
	if !out.LastCaptureAt.Equal(in.LastCaptureAt) {
		t.Errorf("LastCaptureAt = %v, want %v", out.LastCaptureAt, in.LastCaptureAt)
	}
	if out.Version != Version {
		t.Errorf("Version = %d, want %d", out.Version, Version)
	}
}

func TestSave_CreatesGitignore(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, &State{LastSlug: "x"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	gi, err := os.ReadFile(filepath.Join(root, ".fp", ".gitignore"))
	if err != nil {
		t.Fatalf("read .fp/.gitignore: %v", err)
	}
	if len(gi) == 0 {
		t.Error(".fp/.gitignore is empty")
	}
}

func TestSave_AtomicallyReplaces(t *testing.T) {
	root := t.TempDir()
	if err := Save(root, &State{LastSlug: "first"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Save(root, &State{LastSlug: "second"}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.LastSlug != "second" {
		t.Errorf("LastSlug = %q, want second", s.LastSlug)
	}
	// Verify no .tmp files survived.
	entries, err := os.ReadDir(filepath.Join(root, ".fp"))
	if err != nil {
		t.Fatalf("readdir .fp: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Errorf("stray tempfile survived: %s", e.Name())
		}
	}
}

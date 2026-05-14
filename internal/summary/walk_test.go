package summary

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWalk_SortsByCreatedDesc(t *testing.T) {
	root := t.TempDir()
	writeWalkSnapshot(t, root, "a-oldest", "2026-01-01T00:00:00Z")
	writeWalkSnapshot(t, root, "z-middle", "2026-03-15T12:00:00Z")
	writeWalkSnapshot(t, root, "m-newest", "2026-05-14T09:18:00Z")

	entries, err := Walk(root, "web/imports")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	want := []string{"m-newest", "z-middle", "a-oldest"}
	for i, e := range entries {
		if e.Slug != want[i] {
			t.Errorf("entries[%d].Slug = %q, want %q", i, e.Slug, want[i])
		}
	}
}

func TestWalk_SkipsDirsWithoutManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports", "no-manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeWalkSnapshot(t, root, "real-snap", "2026-05-14T09:18:00Z")

	entries, err := Walk(root, "web/imports")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (no-manifest dir should skip)", len(entries))
	}
	if entries[0].Slug != "real-snap" {
		t.Errorf("entries[0].Slug = %q, want real-snap", entries[0].Slug)
	}
}

func TestWalk_KeepsManifestsMissingCreated_SortsThemLast(t *testing.T) {
	root := t.TempDir()
	// One snapshot has Created, one doesn't. Walk keeps both;
	// missing-Created sorts last so list-callers still surface it.
	dir := filepath.Join(root, "web", "imports", "no-created")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: no-created\nadapter: fse\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	writeWalkSnapshot(t, root, "has-created", "2026-05-14T09:18:00Z")

	entries, err := Walk(root, "web/imports")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Slug != "has-created" {
		t.Errorf("entries[0].Slug = %q, want has-created (newest non-empty Created)", entries[0].Slug)
	}
	if entries[1].Slug != "no-created" {
		t.Errorf("entries[1].Slug = %q, want no-created (empty Created sorts last)", entries[1].Slug)
	}
}

func TestWalk_StableSortByCreatedThenSlug(t *testing.T) {
	// Two snapshots with the same Created sort by slug ascending for
	// stable output regardless of ReadDir order.
	root := t.TempDir()
	ts := "2026-05-14T09:18:00Z"
	writeWalkSnapshot(t, root, "zzz", ts)
	writeWalkSnapshot(t, root, "aaa", ts)

	entries, err := Walk(root, "web/imports")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Slug != "aaa" || entries[1].Slug != "zzz" {
		t.Errorf("got %q,%q want aaa,zzz", entries[0].Slug, entries[1].Slug)
	}
}

func TestWalk_OutputDirMissing_Errors(t *testing.T) {
	root := t.TempDir()
	_, err := Walk(root, "web/imports")
	if err == nil {
		t.Fatal("expected error when output dir doesn't exist")
	}
}

func TestWalk_EmptyOutputDir_NoEntries_NoError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := Walk(root, "web/imports")
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("got %d entries, want 0", len(entries))
	}
}

// writeWalkSnapshot drops a minimal manifest.yaml at
// <root>/web/imports/<slug>/. Mirrors the helper in
// internal/apply/picklatest_test.go for self-contained tests.
func writeWalkSnapshot(t *testing.T, root, slug, created string) {
	t.Helper()
	dir := filepath.Join(root, "web", "imports", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: " + slug + "\ncreated: " + created + "\nadapter: fse\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

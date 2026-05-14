package prune

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Delete --------------------------------------------------------

func TestDelete_BareSlug_RemovesDir(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "sts-launch", "2026-05-12T14:01:00Z")
	writeSnap(t, root, "keep-me", "2026-05-14T09:18:00Z")

	var buf bytes.Buffer
	err := Delete(DeleteOptions{
		Target:    "sts-launch",
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if exists(filepath.Join(root, "web", "imports", "sts-launch")) {
		t.Error("sts-launch should be removed")
	}
	if !exists(filepath.Join(root, "web", "imports", "keep-me")) {
		t.Error("keep-me should still exist")
	}
	if !strings.Contains(buf.String(), "sts-launch") {
		t.Errorf("output should mention the removed slug, got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "2026-05-12T14:01:00Z") {
		t.Errorf("output should include the created timestamp, got: %q", buf.String())
	}
}

func TestDelete_RelativePath_Removes(t *testing.T) {
	// On macOS t.TempDir() returns an unresolved /var path while
	// os.Chdir + os.Getwd canonicalises to /private/var. Pre-resolve
	// so the repoRoot we pass matches what resolveTarget will compute
	// from cwd.
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeSnap(t, root, "via-relpath", "2026-05-14T09:18:00Z")

	prevCwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(prevCwd) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err = Delete(DeleteOptions{
		Target:   "web/imports/via-relpath",
		RepoRoot: root,
		Stdout:   &buf,
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists(filepath.Join(root, "web", "imports", "via-relpath")) {
		t.Error("relative-path target should be removed")
	}
}

func TestDelete_AbsolutePath_Removes(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "via-abs", "2026-05-14T09:18:00Z")

	abs := filepath.Join(root, "web", "imports", "via-abs")
	err := Delete(DeleteOptions{
		Target:   abs,
		RepoRoot: root,
		Stdout:   &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if exists(abs) {
		t.Error("absolute-path target should be removed")
	}
}

func TestDelete_RefusesDirWithoutManifest(t *testing.T) {
	root := t.TempDir()
	// Directory exists but no manifest.yaml — safety net per the
	// "is this even a snapshot dir" check.
	dir := filepath.Join(root, "web", "imports", "not-a-snapshot")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Delete(DeleteOptions{
		Target:    "not-a-snapshot",
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected refusal when dir has no manifest.yaml")
	}
	if !strings.Contains(err.Error(), "manifest.yaml") {
		t.Errorf("error should mention the missing manifest, got: %v", err)
	}
	if !exists(dir) {
		t.Error("dir should be untouched after refusal")
	}
}

func TestDelete_NonExistentTarget_Errors(t *testing.T) {
	root := t.TempDir()
	err := Delete(DeleteOptions{
		Target:    "does-not-exist",
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error for non-existent target")
	}
}

func TestDelete_RefusesTargetOutsideRepo(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a different tempdir
	outsideSnap := filepath.Join(outside, "snap")
	if err := os.MkdirAll(outsideSnap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideSnap, "manifest.yaml"),
		[]byte("schema: fp.snapshot/v5\nid: snap\ncreated: 2026-05-14T09:18:00Z\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := Delete(DeleteOptions{
		Target:   outsideSnap,
		RepoRoot: root,
		Stdout:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected refusal for target outside repo root")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("error should mention 'outside', got: %v", err)
	}
}

// --- Prune ---------------------------------------------------------

func TestPrune_DryRun_ListsCandidatesDoesNotRemove(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")
	writeSnap(t, root, "snap-2", "2026-05-13T09:18:00Z")
	writeSnap(t, root, "snap-3", "2026-05-12T09:18:00Z")
	writeSnap(t, root, "snap-4", "2026-05-11T09:18:00Z")

	var buf bytes.Buffer
	err := Prune(PruneOptions{
		Keep:      2,
		Apply:     false, // dry run
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "would remove 2") {
		t.Errorf("dry-run should announce candidate count, got: %q", out)
	}
	if !strings.Contains(out, "snap-3") || !strings.Contains(out, "snap-4") {
		t.Errorf("dry-run should list both oldest snaps, got: %q", out)
	}
	if strings.Contains(out, "snap-1") || strings.Contains(out, "snap-2") {
		t.Errorf("dry-run should NOT list the kept snaps, got: %q", out)
	}
	// Nothing actually deleted.
	for _, slug := range []string{"snap-1", "snap-2", "snap-3", "snap-4"} {
		if !exists(filepath.Join(root, "web", "imports", slug)) {
			t.Errorf("dry-run must not delete: %s missing", slug)
		}
	}
}

func TestPrune_Apply_RemovesOldest(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")
	writeSnap(t, root, "snap-2", "2026-05-13T09:18:00Z")
	writeSnap(t, root, "snap-3", "2026-05-12T09:18:00Z")
	writeSnap(t, root, "snap-4", "2026-05-11T09:18:00Z")

	var buf bytes.Buffer
	err := Prune(PruneOptions{
		Keep:      2,
		Apply:     true,
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}

	if !exists(filepath.Join(root, "web", "imports", "snap-1")) {
		t.Error("snap-1 (newest) should still exist")
	}
	if !exists(filepath.Join(root, "web", "imports", "snap-2")) {
		t.Error("snap-2 should still exist")
	}
	if exists(filepath.Join(root, "web", "imports", "snap-3")) {
		t.Error("snap-3 should be removed")
	}
	if exists(filepath.Join(root, "web", "imports", "snap-4")) {
		t.Error("snap-4 should be removed")
	}
	if !strings.Contains(buf.String(), "removing 2") {
		t.Errorf("output should announce removal count, got: %q", buf.String())
	}
}

func TestPrune_KeepGTECount_NoOp(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")
	writeSnap(t, root, "snap-2", "2026-05-13T09:18:00Z")

	var buf bytes.Buffer
	err := Prune(PruneOptions{
		Keep:      5,
		Apply:     true,
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !exists(filepath.Join(root, "web", "imports", "snap-1")) {
		t.Error("snap-1 should still exist (Keep > count)")
	}
	if !exists(filepath.Join(root, "web", "imports", "snap-2")) {
		t.Error("snap-2 should still exist")
	}
	if !strings.Contains(buf.String(), "nothing to prune") {
		t.Errorf("output should explain the no-op, got: %q", buf.String())
	}
}

func TestPrune_KeepZero_RemovesAll(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")
	writeSnap(t, root, "snap-2", "2026-05-13T09:18:00Z")

	err := Prune(PruneOptions{
		Keep:      0,
		Apply:     true,
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if exists(filepath.Join(root, "web", "imports", "snap-1")) {
		t.Error("snap-1 should be removed with Keep=0")
	}
	if exists(filepath.Join(root, "web", "imports", "snap-2")) {
		t.Error("snap-2 should be removed with Keep=0")
	}
}

func TestPrune_NegativeKeep_Errors(t *testing.T) {
	root := t.TempDir()
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")

	err := Prune(PruneOptions{
		Keep:      -1,
		Apply:     true,
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error for negative --keep")
	}
}

func TestPrune_EmptyDir_Reports(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	err := Prune(PruneOptions{
		Keep:      3,
		Apply:     true,
		RepoRoot:  root,
		OutputDir: "web/imports",
		Stdout:    &buf,
	})
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if !strings.Contains(buf.String(), "nothing to prune") {
		t.Errorf("empty dir should report 'nothing to prune', got: %q", buf.String())
	}
}

// --- helpers -------------------------------------------------------

func writeSnap(t *testing.T, root, slug, created string) {
	t.Helper()
	dir := filepath.Join(root, "web", "imports", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(fmt.Sprintf("schema: fp.snapshot/v5\nid: %s\ncreated: %s\nadapter: fse\n", slug, created))
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

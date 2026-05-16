package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickLatest_PicksHighestCreated(t *testing.T) {
	root := t.TempDir()
	writeSnapshot(t, root, "a-oldest", "2026-01-01T00:00:00Z")
	writeSnapshot(t, root, "z-middle", "2026-03-15T12:00:00Z")
	writeSnapshot(t, root, "m-newest", "2026-05-14T09:18:00Z")

	slug, hostDir, err := PickLatest(root, "web/imports")
	if err != nil {
		t.Fatalf("PickLatest: %v", err)
	}
	if slug != "m-newest" {
		t.Errorf("slug = %q, want m-newest", slug)
	}
	wantDir := filepath.Join(root, "web", "imports", "m-newest")
	if hostDir != wantDir {
		t.Errorf("hostDir = %q, want %q", hostDir, wantDir)
	}
}

func TestPickLatest_SkipsDirsWithoutManifest(t *testing.T) {
	// .gitkeep-only dirs and half-deleted captures shouldn't trip
	// PickLatest. It silently skips and considers the rest.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "web", "imports", "no-manifest"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, root, "real-snap", "2026-05-14T09:18:00Z")

	slug, _, err := PickLatest(root, "web/imports")
	if err != nil {
		t.Fatalf("PickLatest: %v", err)
	}
	if slug != "real-snap" {
		t.Errorf("slug = %q, want real-snap", slug)
	}
}

func TestPickLatest_SkipsManifestsMissingCreated(t *testing.T) {
	root := t.TempDir()
	// Manifest exists but has no `created` field.
	dir := filepath.Join(root, "web", "imports", "no-created")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: no-created\nadapter: fse\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	writeSnapshot(t, root, "has-created", "2026-05-14T09:18:00Z")

	slug, _, err := PickLatest(root, "web/imports")
	if err != nil {
		t.Fatalf("PickLatest: %v", err)
	}
	if slug != "has-created" {
		t.Errorf("slug = %q, want has-created (no-created should be skipped)", slug)
	}
}

func TestPickLatest_AllManifestsMissingCreated_Errors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "web", "imports", "broken")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: broken\nadapter: fse\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := PickLatest(root, "web/imports")
	if err == nil {
		t.Fatal("expected error when no manifest carries `created`")
	}
	if !strings.Contains(err.Error(), "fp snapshot") {
		t.Errorf("error message missing 'fp snapshot' hint: %v", err)
	}
}

func TestPickLatest_OutputDirMissing_Errors(t *testing.T) {
	root := t.TempDir()
	_, _, err := PickLatest(root, "web/imports")
	if err == nil {
		t.Fatal("expected error when output dir doesn't exist")
	}
}

func TestPickLatest_TimestampNamedSlugs_LexSortAgrees(t *testing.T) {
	// Smoke test for the post-Phase-2 reality: timestamp-named dirs
	// sort lex-equal to their `created` field, so the picker is
	// self-consistent regardless of which side ordering wins.
	root := t.TempDir()
	writeSnapshot(t, root, "2026-01-01T00-00-00Z", "2026-01-01T00:00:00Z")
	writeSnapshot(t, root, "2026-05-14T09-18-00Z", "2026-05-14T09:18:00Z")

	slug, _, err := PickLatest(root, "web/imports")
	if err != nil {
		t.Fatalf("PickLatest: %v", err)
	}
	if slug != "2026-05-14T09-18-00Z" {
		t.Errorf("slug = %q, want 2026-05-14T09-18-00Z", slug)
	}
}

func writeSnapDir(t *testing.T, root, relDir, slug, created string) {
	t.Helper()
	dir := filepath.Join(root, relDir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: " + slug + "\ncreated: " + created + "\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPickLatestFromDirs_PrefersHighestCreatedAcrossDirs(t *testing.T) {
	root := t.TempDir()
	writeSnapDir(t, root, "web/imports", "committed-old", "2026-05-14T00:00:00Z")
	writeSnapDir(t, root, ".fp/prod-snapshots", "prod-2026-05-16T00-00-00Z", "2026-05-16T00:00:00Z")

	slug, hostDir, err := PickLatestFromDirs(root, []string{"web/imports", ".fp/prod-snapshots"})
	if err != nil {
		t.Fatalf("PickLatestFromDirs: %v", err)
	}
	if slug != "prod-2026-05-16T00-00-00Z" {
		t.Errorf("slug = %q, want prod-2026-05-16T00-00-00Z", slug)
	}
	if !strings.Contains(hostDir, filepath.Join(".fp", "prod-snapshots", "prod-2026-05-16T00-00-00Z")) {
		t.Errorf("hostDir = %q, want path under .fp/prod-snapshots", hostDir)
	}
}

func TestPickLatestFromDirs_MissingPullDirIsFine(t *testing.T) {
	root := t.TempDir()
	writeSnapDir(t, root, "web/imports", "designer-snap", "2026-05-14T00:00:00Z")

	slug, _, err := PickLatestFromDirs(root, []string{"web/imports", ".fp/prod-snapshots"})
	if err != nil {
		t.Fatalf("PickLatestFromDirs: %v", err)
	}
	if slug != "designer-snap" {
		t.Errorf("slug = %q, want designer-snap", slug)
	}
}

func TestPickLatestFromDirs_SlugCollisionErrors(t *testing.T) {
	root := t.TempDir()
	writeSnapDir(t, root, "web/imports", "shared-slug", "2026-05-14T00:00:00Z")
	writeSnapDir(t, root, ".fp/prod-snapshots", "shared-slug", "2026-05-16T00:00:00Z")

	_, _, err := PickLatestFromDirs(root, []string{"web/imports", ".fp/prod-snapshots"})
	if err == nil {
		t.Fatal("expected slug-collision error")
	}
	if !strings.Contains(err.Error(), "multiple dirs") {
		t.Errorf("error should mention 'multiple dirs': %v", err)
	}
}

func TestPickLatestFromDirs_AllEmptyErrors(t *testing.T) {
	root := t.TempDir()
	_, _, err := PickLatestFromDirs(root, []string{"web/imports", ".fp/prod-snapshots"})
	if err == nil {
		t.Fatal("expected error when no snapshots in any dir")
	}
	if !strings.Contains(err.Error(), "no snapshot dir") {
		t.Errorf("error should say 'no snapshot dir': %v", err)
	}
}

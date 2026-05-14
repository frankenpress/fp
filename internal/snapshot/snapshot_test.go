package snapshot

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/state"
)

func TestDefaultSlug_AlwaysTimestamp(t *testing.T) {
	// Post-Phase-2: the cascade (state.LastSlug → git branch →
	// composer name → timestamp) is gone. DefaultSlug returns the
	// timestamp unconditionally, regardless of state / composer /
	// git branch. --slug=<name> is the explicit override path.
	root := t.TempDir()
	writeComposer(t, root, "frankenpress/sts")
	opts := Options{
		RepoRoot: root,
		State:    &state.State{LastSlug: "sts-launch"},
		Now:      fixedNow,
	}
	got := DefaultSlug(opts)
	want := "2026-05-12T10-00-00Z"
	if got != want {
		t.Errorf("DefaultSlug with state.LastSlug = %q, want %q (cascade should be gone)", got, want)
	}
}

func TestTimestampedSlug(t *testing.T) {
	got := TimestampedSlug(time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC))
	want := "2026-05-12T10-00-00Z"
	if got != want {
		t.Errorf("TimestampedSlug = %q, want %q", got, want)
	}
}

func TestRun_QuickMode_TimestampedSlug_NoStatePersisted_RealCapture(t *testing.T) {
	root := t.TempDir()
	writeFrankenpressTOML(t, root)

	hostOutputDir := filepath.Join(root, "web", "imports")
	if err := os.MkdirAll(hostOutputDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeRunner := docker.NewFake()
	fakeRunner.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	fakeRunner.StreamingFunc = func(_ context.Context, _, _ string, _ []string, _, _ io.Writer) error {
		// The fake docker-cp step writes the manifest; this just
		// needs to succeed.
		return nil
	}
	fakeRunner.CopyFunc = func(_ context.Context, _, dst string) error {
		// Land a tiny manifest into the host target so summary.Read
		// has something to parse.
		slugDir := filepath.Join(dst, "2026-05-12T10-00-00Z")
		if err := os.MkdirAll(slugDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(slugDir, "manifest.yaml"), fixtureManifest(), 0o644)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	var stdout, stderr bytes.Buffer
	opts := Options{
		RepoRoot:    root,
		Config:      cfg,
		State:       &state.State{},
		Runner:      fakeRunner,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Interactive: false,
		Quick:       true,
		Now:         fixedNow,
	}
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, stderr.String())
	}

	// In --quick mode .fp/state.json must NOT be created.
	if _, err := os.Stat(filepath.Join(root, ".fp", "state.json")); err == nil {
		t.Error("--quick mode wrote .fp/state.json; should not")
	}

	// The wp invocation must reference the timestamped slug, and the
	// args slice must start with "wp" (regression: an earlier version
	// passed wpArgs[1:] which made docker exec try to run --allow-root
	// as the binary).
	found := false
	for _, c := range fakeRunner.Calls {
		if c.Method != "ComposeExecStreaming" {
			continue
		}
		if len(c.Args) == 0 || c.Args[0] != "wp" {
			t.Errorf("wp call args[0] = %v, want %q", c.Args, "wp")
		}
		for _, a := range c.Args {
			if strings.HasPrefix(a, "--slug=") {
				if a != "--slug=2026-05-12T10-00-00Z" {
					t.Errorf("wp call slug arg = %q, want timestamped", a)
				}
				found = true
			}
		}
	}
	if !found {
		t.Error("no ComposeExecStreaming call with --slug observed")
	}
}

func TestRun_NormalMode_PersistsState(t *testing.T) {
	root := t.TempDir()
	writeFrankenpressTOML(t, root)

	hostOutputDir := filepath.Join(root, "web", "imports")
	if err := os.MkdirAll(hostOutputDir, 0o755); err != nil {
		t.Fatal(err)
	}

	fakeRunner := docker.NewFake()
	fakeRunner.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	fakeRunner.CopyFunc = func(_ context.Context, _, dst string) error {
		slugDir := filepath.Join(dst, "explicit-slug")
		if err := os.MkdirAll(slugDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(slugDir, "manifest.yaml"), fixtureManifest(), 0o644)
	}

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	var stdout, stderr bytes.Buffer
	opts := Options{
		RepoRoot:    root,
		Config:      cfg,
		State:       &state.State{},
		Runner:      fakeRunner,
		Stdout:      &stdout,
		Stderr:      &stderr,
		Interactive: false,
		Slug:        "explicit-slug",
		Note:        "test note",
		Now:         fixedNow,
	}
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v (stderr: %s)", err, stderr.String())
	}

	st, err := state.Load(root)
	if err != nil {
		t.Fatalf("state.Load: %v", err)
	}
	if st.LastSlug != "explicit-slug" {
		t.Errorf("state.LastSlug = %q, want explicit-slug", st.LastSlug)
	}
	if st.LastNoteUsed != "test note" {
		t.Errorf("state.LastNoteUsed = %q, want test note", st.LastNoteUsed)
	}
	if st.LastCaptureAt.IsZero() {
		t.Error("state.LastCaptureAt is zero")
	}
}

func TestRun_RefusesSubSecondCollision_WhenSlugFromTimestampDefault(t *testing.T) {
	// Designer fires `fp snapshot` twice within the same second.
	// The second invocation resolves the same timestamp slug and the
	// dir already exists from the first capture. Pre-clean would
	// silently wipe the first capture — refuse instead.
	root := t.TempDir()
	writeFrankenpressTOML(t, root)

	// Pre-create the dir the default timestamp slug would land in.
	existingSlug := TimestampedSlug(fixedNow())
	existingDir := filepath.Join(root, "web", "imports", existingSlug)
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existingDir, "manifest.yaml"), []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _ := config.Load(root)
	_, err := Run(context.Background(), Options{
		RepoRoot:    root,
		Config:      cfg,
		State:       &state.State{},
		Runner:      docker.NewFake(), // never reached
		Interactive: false,            // default → timestamp slug
		Quick:       true,             // skip prompt + uncommitted-changes guard
		Now:         fixedNow,
	})
	if err == nil {
		t.Fatal("Run: expected sub-second collision error, got nil")
	}
	if !strings.Contains(err.Error(), "wait a moment") {
		t.Errorf("error %v missing wait-a-moment hint", err)
	}

	// And the pre-existing manifest must still be there — refusal,
	// not silent overwrite.
	body, err := os.ReadFile(filepath.Join(existingDir, "manifest.yaml"))
	if err != nil {
		t.Fatalf("pre-existing manifest disappeared: %v", err)
	}
	if string(body) != "placeholder" {
		t.Errorf("pre-existing manifest was overwritten: %q", string(body))
	}
}

func TestRun_AllowsOverwrite_WhenSlugIsExplicit(t *testing.T) {
	// Designer iterates on a named slug (--slug=foo) and the dir
	// already exists from a previous capture. Pre-clean is intentional
	// here — the explicit-slug path is the "iterate on a milestone"
	// flow. Refuse-on-collision is only for the timestamp default.
	root := t.TempDir()
	writeFrankenpressTOML(t, root)

	existingDir := filepath.Join(root, "web", "imports", "explicit-slug")
	if err := os.MkdirAll(existingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existingDir, "manifest.yaml"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakeRunner := docker.NewFake()
	fakeRunner.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	fakeRunner.CopyFunc = func(_ context.Context, _, dst string) error {
		slugDir := filepath.Join(dst, "explicit-slug")
		if err := os.MkdirAll(slugDir, 0o755); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(slugDir, "manifest.yaml"), fixtureManifest(), 0o644)
	}

	cfg, _ := config.Load(root)
	_, err := Run(context.Background(), Options{
		RepoRoot:    root,
		Config:      cfg,
		State:       &state.State{},
		Runner:      fakeRunner,
		Interactive: false,
		Slug:        "explicit-slug", // explicit → no collision guard
		Now:         fixedNow,
	})
	if err != nil {
		t.Fatalf("Run: expected success (explicit slug overwrite), got %v", err)
	}
}

func TestRun_StackDown_ReturnsHintError(t *testing.T) {
	root := t.TempDir()
	writeFrankenpressTOML(t, root)

	fakeRunner := docker.NewFake()
	fakeRunner.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "exited"},
	}
	cfg, _ := config.Load(root)
	_, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		State:    &state.State{},
		Runner:   fakeRunner,
		Slug:     "any",
		Quick:    true,
		Now:      fixedNow,
	})
	if err == nil {
		t.Fatal("Run: expected stack-down error")
	}
	if !strings.Contains(err.Error(), "make up") {
		t.Errorf("error message missing make-up hint: %v", err)
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"sts-launch", "sts-launch"},
		{"STS Launch", "sts-launch"},
		{"footer image / test!", "footer-image-test"},
		{"---foo---", "foo"},
		{"", ""},
		{"!!", ""},
	}
	for _, tc := range cases {
		got := slugify(tc.in)
		if got != tc.want {
			t.Errorf("slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- helpers ---

func fixedNow() time.Time {
	return time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC)
}

func writeComposer(t *testing.T, root, name string) {
	t.Helper()
	body := []byte(`{"name":"` + name + `"}`)
	if err := os.WriteFile(filepath.Join(root, "composer.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFrankenpressTOML(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureManifest() []byte {
	return []byte(`schema: fp.snapshot/v4
id: test-20260512-100000
created: "2026-05-12T10:00:00Z"
source:
  site_url: "http://localhost:8080"
  wp_version: 6.9.4
  source_theme: twentytwentyfive
author:
  note: test note
adapter: fse
scope:
  post_types_additive: []
  post_types_owned:
  - wp_template
  - wp_template_part
contents:
  wxr_post_count: 0
  templates_count: 5
  options_count: 8
  attachments_count: 2
  binaries_file_count: 10
  binaries_total_bytes: 144507
  uploads_file_count: 16
  uploads_total_bytes: 1000505
`)
}

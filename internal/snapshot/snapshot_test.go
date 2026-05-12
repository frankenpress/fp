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

func TestDefaultSlug_PrefersStateLastSlug(t *testing.T) {
	root := t.TempDir()
	writeComposer(t, root, "frankenpress/sts")
	opts := Options{
		RepoRoot: root,
		State:    &state.State{LastSlug: "sts-launch"},
		Now:      fixedNow,
	}
	got := DefaultSlug(opts)
	if got != "sts-launch" {
		t.Errorf("DefaultSlug = %q, want sts-launch", got)
	}
}

func TestDefaultSlug_FallsBackToComposerWhenNoState(t *testing.T) {
	root := t.TempDir()
	writeComposer(t, root, "frankenpress/sts")
	opts := Options{
		RepoRoot: root,
		State:    &state.State{},
		Now:      fixedNow,
	}
	got := DefaultSlug(opts)
	if got != "sts-launch" {
		t.Errorf("DefaultSlug = %q, want sts-launch (composer fallback)", got)
	}
}

func TestDefaultSlug_TimestampedWhenNothingElse(t *testing.T) {
	root := t.TempDir()
	opts := Options{
		RepoRoot: root,
		State:    &state.State{},
		Now:      fixedNow,
	}
	got := DefaultSlug(opts)
	want := "snapshot-20260512-100000"
	if got != want {
		t.Errorf("DefaultSlug = %q, want %q", got, want)
	}
}

func TestTimestampedSlug(t *testing.T) {
	got := TimestampedSlug(time.Date(2026, 5, 12, 10, 0, 0, 0, time.UTC))
	want := "snapshot-20260512-100000"
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
		slugDir := filepath.Join(dst, "snapshot-20260512-100000")
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
	if err := Run(context.Background(), opts); err != nil {
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
				if a != "--slug=snapshot-20260512-100000" {
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
	if err := Run(context.Background(), opts); err != nil {
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

func TestRun_StackDown_ReturnsHintError(t *testing.T) {
	root := t.TempDir()
	writeFrankenpressTOML(t, root)

	fakeRunner := docker.NewFake()
	fakeRunner.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "exited"},
	}
	cfg, _ := config.Load(root)
	err := Run(context.Background(), Options{
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

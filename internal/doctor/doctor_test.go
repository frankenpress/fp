package doctor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/gh"
)

func TestRun_AllGreen_RendersNominal(t *testing.T) {
	root := setupSite(t, "FP_S3_DISABLED=0\n")
	writeSnap(t, root, "2026-05-14T09-18-00Z", "2026-05-14T09:18:00Z")

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = []docker.Container{
		{Name: "mysite-site-1", Service: "site", State: "running"},
	}
	g := gh.NewFake()
	g.AuthLoggedIn = true
	g.AuthSummary = "MatthewKennedy@github.com"

	var buf bytes.Buffer
	err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:48:00Z") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "all systems nominal") {
		t.Errorf("expected 'all systems nominal', got:\n%s", out)
	}
	if !strings.Contains(out, "fp version") {
		t.Errorf("missing fp version line: %s", out)
	}
	if !strings.Contains(out, "v2.31.0") {
		t.Errorf("missing docker compose version: %s", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("missing running status: %s", out)
	}
	if !strings.Contains(out, "30 minute(s) ago") {
		t.Errorf("missing snapshot age formatted as relative time, got:\n%s", out)
	}
	if !strings.Contains(out, "MatthewKennedy@github.com") {
		t.Errorf("missing gh auth user, got: %s", out)
	}
}

func TestRun_ServiceNotRunning_ReportsHint(t *testing.T) {
	root := setupSite(t, "FP_S3_DISABLED=0\n")
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = nil // empty → StatusProjectAbsent
	g := gh.NewFake()
	g.AuthLoggedIn = true
	g.AuthSummary = "user@github.com"

	var buf bytes.Buffer
	_ = Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})

	out := buf.String()
	if !strings.Contains(out, "not running") {
		t.Errorf("expected 'not running', got:\n%s", out)
	}
	if !strings.Contains(out, "issue(s) detected") {
		t.Errorf("expected issue tally, got:\n%s", out)
	}
	if !strings.Contains(out, "fp up") {
		t.Errorf("expected recovery hint pointing at fp up, got:\n%s", out)
	}
}

func TestRun_NoSnapshots_ReportsHint(t *testing.T) {
	root := setupSite(t, "FP_S3_DISABLED=0\n")
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = []docker.Container{
		{Name: "x-site-1", Service: "site", State: "running"},
	}
	g := gh.NewFake()
	g.AuthLoggedIn = true
	g.AuthSummary = "user@github.com"

	var buf bytes.Buffer
	_ = Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})

	out := buf.String()
	if !strings.Contains(out, "latest snapshot") || !strings.Contains(out, "none") {
		t.Errorf("expected 'latest snapshot: none', got:\n%s", out)
	}
	if !strings.Contains(out, "fp snapshot") {
		t.Errorf("expected hint pointing at fp snapshot, got:\n%s", out)
	}
}

func TestRun_EnvMissing_ReportsHint(t *testing.T) {
	root := t.TempDir()
	// frankenpress.toml exists (so config.Load succeeds) but no .env.
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte("[snapshot]\noutput_dir = \"web/imports\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = []docker.Container{
		{Name: "x-site-1", Service: "site", State: "running"},
	}
	g := gh.NewFake()
	g.AuthLoggedIn = true
	g.AuthSummary = "user@github.com"

	var buf bytes.Buffer
	_ = Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})

	out := buf.String()
	if !strings.Contains(out, ".env missing") {
		t.Errorf("expected '.env missing', got:\n%s", out)
	}
	if !strings.Contains(out, "fp init") {
		t.Errorf("expected hint mentioning fp init, got:\n%s", out)
	}
}

func TestRun_GHNotLoggedIn_ReportsHint(t *testing.T) {
	root := setupSite(t, "FP_S3_DISABLED=0\n")
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = []docker.Container{
		{Name: "x-site-1", Service: "site", State: "running"},
	}
	g := gh.NewFake()
	g.AuthLoggedIn = false
	g.AuthSummary = "not logged in"

	var buf bytes.Buffer
	_ = Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})

	out := buf.String()
	if !strings.Contains(out, "not logged in") {
		t.Errorf("expected 'not logged in' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "gh auth login") {
		t.Errorf("expected hint pointing at gh auth login, got:\n%s", out)
	}
}

func TestRun_GHUnavailable_ReportsHint(t *testing.T) {
	root := setupSite(t, "FP_S3_DISABLED=0\n")
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = []docker.Container{
		{Name: "x-site-1", Service: "site", State: "running"},
	}
	g := gh.NewFake()
	g.AuthErr = errors.New("exec: gh: not found in PATH")

	var buf bytes.Buffer
	_ = Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})

	out := buf.String()
	if !strings.Contains(out, "gh CLI unavailable") {
		t.Errorf("expected 'gh CLI unavailable', got:\n%s", out)
	}
}

func TestRun_S3Disabled_ShowsAlternateValue(t *testing.T) {
	root := setupSite(t, "FP_S3_DISABLED=1\n")
	writeSnap(t, root, "snap-1", "2026-05-14T09:18:00Z")

	d := docker.NewFake()
	d.ComposeVersionString = "v2.31.0"
	d.PSContainers = []docker.Container{
		{Name: "x-site-1", Service: "site", State: "running"},
	}
	g := gh.NewFake()
	g.AuthLoggedIn = true
	g.AuthSummary = "user@github.com"

	var buf bytes.Buffer
	_ = Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &buf,
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})

	out := buf.String()
	if !strings.Contains(out, "FP_S3_DISABLED=1") {
		t.Errorf("expected FP_S3_DISABLED=1 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "local filesystem") {
		t.Errorf("expected 'local filesystem' description, got:\n%s", out)
	}
}

func TestRun_AlwaysReturnsNil(t *testing.T) {
	// Doctor is a report, not a gate. Even with everything broken it
	// returns nil.
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	d := docker.NewFake()
	d.ComposeVersionErr = errors.New("docker not installed")
	d.PSErr = errors.New("docker not installed")
	g := gh.NewFake()
	g.AuthErr = errors.New("gh not installed")

	err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   loadCfg(t, root),
		Docker:   d,
		GH:       g,
		Stdout:   &bytes.Buffer{},
		Now:      func() time.Time { return mustTime("2026-05-14T09:30:00Z") },
	})
	if err != nil {
		t.Errorf("doctor must return nil even when checks fail, got: %v", err)
	}
}

func TestSnapshotAge(t *testing.T) {
	now := mustTime("2026-05-14T12:00:00Z")
	cases := []struct {
		in, want string
	}{
		{"", "no created field"},
		{"not-a-timestamp", "unparseable created"},
		{"2026-05-14T11:59:30Z", "just now"},
		{"2026-05-14T11:30:00Z", "30 minute(s) ago"},
		{"2026-05-14T08:00:00Z", "4 hour(s) ago"},
		{"2026-05-12T12:00:00Z", "2 day(s) ago"},
		{"2026-01-01T12:00:00Z", "2026-01-01"},
	}
	for _, c := range cases {
		got := snapshotAge(c.in, now)
		if got != c.want {
			t.Errorf("snapshotAge(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- helpers --------------------------------------------------------

func setupSite(t *testing.T, envContent string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte("[snapshot]\noutput_dir = \"web/imports\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(envContent), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeSnap(t *testing.T, root, slug, created string) {
	t.Helper()
	dir := filepath.Join(root, "web", "imports", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema: fp.snapshot/v5\nid: " + slug + "\ncreated: " + created + "\nadapter: fse\n"
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func loadCfg(t *testing.T, root string) *config.Config {
	t.Helper()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func mustTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

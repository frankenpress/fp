package setup

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
)

// happyPathTenant builds a temp dir that looks like a fresh-cloned
// site-template repo: composer.json, .env.example, frankenpress.toml.
// Nothing else — no .env, no vendor/, no web/wp/. Returns the path.
func happyPathTenant(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{"name":"frankenpress/test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("DB_NAME=wordpress\nWP_HOME=http://localhost:8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// freshRunner returns a Fake configured for a happy path: containers
// healthy after compose up, wp core is-installed exits non-zero (so
// fp init runs the install), wp core install succeeds.
func freshRunner() *docker.Fake {
	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	// is-installed exits 1 on a fresh DB.
	fake.ComposeExecFunc = func(_ context.Context, _, _ string, args []string) ([]byte, []byte, error) {
		if len(args) >= 3 && args[len(args)-2] == "core" && args[len(args)-1] == "is-installed" {
			return nil, nil, &docker.ExecError{ExitCode: 1}
		}
		return nil, nil, nil
	}
	return fake
}

func TestRun_HappyPath_FreshClone_NoApply(t *testing.T) {
	root := happyPathTenant(t)
	fake := freshRunner()
	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &stdout,
		Stderr:   &stderr,
		NoApply:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	// .env was scaffolded.
	if !result.EnvScaffolded {
		t.Error("EnvScaffolded = false, want true (.env was missing)")
	}
	if _, err := os.Stat(filepath.Join(root, ".env")); err != nil {
		t.Errorf(".env not created: %v", err)
	}

	// Designer-mode S3 line landed.
	if !result.S3DesignerMode {
		t.Error("S3DesignerMode = false, want true")
	}
	body, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !strings.Contains(string(body), "FP_S3_DISABLED=0") {
		t.Errorf(".env missing FP_S3_DISABLED=0:\n%s", string(body))
	}

	// Composer install fired (vendor/ was missing).
	if !result.BootstrapRan {
		t.Error("BootstrapRan = false, want true")
	}
	if fake.CallCount("ComposerInstall") != 1 {
		t.Errorf("ComposerInstall calls = %d, want 1", fake.CallCount("ComposerInstall"))
	}

	// ComposeUp fired.
	if !result.StackBrought {
		t.Error("StackBrought = false")
	}
	if fake.CallCount("ComposeUp") != 1 {
		t.Errorf("ComposeUp calls = %d, want 1", fake.CallCount("ComposeUp"))
	}

	// WP install fired (is-installed returned non-zero).
	if !result.WPInstalled {
		t.Error("WPInstalled = false, want true")
	}

	// Resolved URL came from .env.example's WP_HOME.
	if result.SiteURL != "http://localhost:8080" {
		t.Errorf("SiteURL = %q, want http://localhost:8080", result.SiteURL)
	}

	// NoApply path: no apply.* calls observed.
	if result.SnapshotApplied != "" {
		t.Errorf("SnapshotApplied = %q, want empty with --no-apply", result.SnapshotApplied)
	}
}

func TestRun_SkipsBootstrap_WhenVendorExists(t *testing.T) {
	root := happyPathTenant(t)
	// Pre-create vendor/ → bootstrap should skip composer.
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := freshRunner()
	cfg, _ := config.Load(root)

	_, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		NoApply:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.CallCount("ComposerInstall") != 0 {
		t.Errorf("ComposerInstall calls = %d, want 0 when vendor/ exists", fake.CallCount("ComposerInstall"))
	}
}

func TestRun_SkipsBootstrap_WhenSkipSetupFlag(t *testing.T) {
	root := happyPathTenant(t)
	fake := freshRunner()
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot:  root,
		Config:    cfg,
		Runner:    fake,
		Stdout:    io.Discard,
		Stderr:    io.Discard,
		NoApply:   true,
		SkipSetup: true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.CallCount("ComposerInstall") != 0 {
		t.Errorf("ComposerInstall calls = %d, want 0 with --skip-setup", fake.CallCount("ComposerInstall"))
	}
	if result.EnvScaffolded {
		t.Error("EnvScaffolded = true with --skip-setup; .env scaffold should also be skipped")
	}
}

func TestRun_RespectsExplicitFP_S3_DISABLED(t *testing.T) {
	root := happyPathTenant(t)
	// Pre-create .env with explicit FP_S3_DISABLED=1.
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("FP_S3_DISABLED=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := freshRunner()
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		NoApply:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.S3DesignerMode {
		t.Error("S3DesignerMode = true, want false (operator's FP_S3_DISABLED=1 must be preserved)")
	}
	body, _ := os.ReadFile(filepath.Join(root, ".env"))
	if !strings.Contains(string(body), "FP_S3_DISABLED=1") {
		t.Errorf(".env lost the operator's FP_S3_DISABLED=1:\n%s", string(body))
	}
	if strings.Contains(string(body), "FP_S3_DISABLED=0") {
		t.Errorf(".env was overwritten with FP_S3_DISABLED=0:\n%s", string(body))
	}
}

func TestRun_RespectsDisableS3FromTOML(t *testing.T) {
	root := happyPathTenant(t)
	// Operator opted out via [init] disable_s3 = true.
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte("[init]\ndisable_s3 = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := freshRunner()
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		NoApply:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.S3DesignerMode {
		t.Error("S3DesignerMode = true, want false (disable_s3 = true in toml)")
	}
}

func TestRun_SkipsWPInstall_WhenAlreadyInstalled(t *testing.T) {
	root := happyPathTenant(t)
	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	// is-installed returns nil err → already installed.
	fake.ComposeExecFunc = func(_ context.Context, _, _ string, _ []string) ([]byte, []byte, error) {
		return nil, nil, nil
	}
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		NoApply:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.WPInstalled {
		t.Error("WPInstalled = true, want false when wp core is-installed succeeds")
	}
}

func TestRun_ResolvesWPHomeFromDotEnv(t *testing.T) {
	root := happyPathTenant(t)
	// Override .env.example with a custom WP_HOME.
	if err := os.WriteFile(filepath.Join(root, ".env.example"), []byte("WP_HOME=http://custom.test:9090\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := freshRunner()
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		NoApply:  true,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SiteURL != "http://custom.test:9090" {
		t.Errorf("SiteURL = %q, want http://custom.test:9090", result.SiteURL)
	}
}

// writeSnapshot writes a minimal valid manifest.yaml under
// <root>/web/imports/<slug>/ so PickLatest + apply can both consume
// it. created drives the lex-sort order for pick-latest tests.
func writeSnapshot(t *testing.T, root, slug, created string) {
	t.Helper()
	dir := filepath.Join(root, "web", "imports", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("schema: fp.snapshot/v5\nid: " + slug + "\ncreated: \"" + created + "\"\nadapter: fse\nsource:\n  source_theme: twentytwentyfive\n")
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_AppliesLatestSnapshot_WhenNoSlugFlag(t *testing.T) {
	root := happyPathTenant(t)
	writeSnapshot(t, root, "2026-01-01T00-00-00Z", "2026-01-01T00:00:00Z")
	writeSnapshot(t, root, "2026-05-14T09-18-00Z", "2026-05-14T09:18:00Z")
	writeSnapshot(t, root, "2026-03-15T12-00-00Z", "2026-03-15T12:00:00Z")

	fake := freshRunner()
	// Track which slug the apply path docker-cp'd.
	var copiedFrom string
	fake.CopyFunc = func(_ context.Context, src, _ string) error {
		copiedFrom = src
		return nil
	}
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		// NoApply intentionally false → exercise apply path.
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SnapshotApplied != "2026-05-14T09-18-00Z" {
		t.Errorf("SnapshotApplied = %q, want 2026-05-14T09-18-00Z (highest created)", result.SnapshotApplied)
	}
	if !strings.HasSuffix(copiedFrom, "/web/imports/2026-05-14T09-18-00Z") {
		t.Errorf("docker cp src = %q, want suffix /web/imports/2026-05-14T09-18-00Z", copiedFrom)
	}
}

func TestRun_AppliesExplicitSlug_WhenSlugFlagSet(t *testing.T) {
	root := happyPathTenant(t)
	writeSnapshot(t, root, "2026-05-14T09-18-00Z", "2026-05-14T09:18:00Z")
	writeSnapshot(t, root, "milestone-launch", "2026-01-01T00:00:00Z")

	fake := freshRunner()
	var copiedFrom string
	fake.CopyFunc = func(_ context.Context, src, _ string) error {
		copiedFrom = src
		return nil
	}
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		Slug:     "milestone-launch",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.SnapshotApplied != "milestone-launch" {
		t.Errorf("SnapshotApplied = %q, want milestone-launch (--slug override)", result.SnapshotApplied)
	}
	if !strings.HasSuffix(copiedFrom, "/web/imports/milestone-launch") {
		t.Errorf("docker cp src = %q, want milestone-launch", copiedFrom)
	}
}

func TestRun_HandlesEmptyImports_SoftSkip(t *testing.T) {
	root := happyPathTenant(t)
	// Create empty web/imports/ so PickLatest returns the "fp
	// snapshot" hint and runApply does the soft skip rather than
	// hard-failing.
	if err := os.MkdirAll(filepath.Join(root, "web", "imports"), 0o755); err != nil {
		t.Fatal(err)
	}
	fake := freshRunner()
	cfg, _ := config.Load(root)

	result, err := Run(context.Background(), Options{
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
		// No NoApply → exercise apply path.
	})
	if err != nil {
		t.Fatalf("Run: %v (expected soft skip, not error)", err)
	}
	if !result.SnapshotsAbsent {
		t.Error("SnapshotsAbsent = false, want true")
	}
	if result.SnapshotApplied != "" {
		t.Errorf("SnapshotApplied = %q, want empty", result.SnapshotApplied)
	}
}

func TestRun_RequiresConfig(t *testing.T) {
	_, err := Run(context.Background(), Options{Runner: docker.NewFake()})
	if err == nil {
		t.Fatal("expected error when Config is nil")
	}
}

func TestRun_RequiresRunner(t *testing.T) {
	_, err := Run(context.Background(), Options{Config: &config.Config{}})
	if err == nil {
		t.Fatal("expected error when Runner is nil")
	}
}

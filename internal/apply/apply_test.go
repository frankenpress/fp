package apply

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
)

func TestResolveSnapshotDir_BareSlug(t *testing.T) {
	root := t.TempDir()
	snap := filepath.Join(root, "web", "imports", "sts-launch")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}

	abs, rel, err := resolveSnapshotDir(root, "web/imports", "sts-launch")
	if err != nil {
		t.Fatalf("resolveSnapshotDir: %v", err)
	}
	if abs != snap {
		t.Errorf("abs = %q, want %q", abs, snap)
	}
	if filepath.ToSlash(rel) != "web/imports/sts-launch" {
		t.Errorf("rel = %q, want web/imports/sts-launch", rel)
	}
}

func TestResolveSnapshotDir_AbsolutePath(t *testing.T) {
	root := t.TempDir()
	snap := filepath.Join(root, "custom", "loc", "alt-snap")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}

	abs, rel, err := resolveSnapshotDir(root, "web/imports", snap)
	if err != nil {
		t.Fatalf("resolveSnapshotDir: %v", err)
	}
	if abs != snap {
		t.Errorf("abs = %q, want %q", abs, snap)
	}
	if filepath.ToSlash(rel) != "custom/loc/alt-snap" {
		t.Errorf("rel = %q, want custom/loc/alt-snap", rel)
	}
}

func TestResolveSnapshotDir_OutsideRepo(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a different temp dir entirely
	snap := filepath.Join(outside, "alien-snap")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveSnapshotDir(root, "web/imports", snap)
	if err == nil {
		t.Fatal("expected error for path outside repo root")
	}
	if !strings.Contains(err.Error(), "outside the repo root") {
		t.Errorf("error message missing outside-repo hint: %v", err)
	}
}

func TestResolveSnapshotDir_NotFound(t *testing.T) {
	root := t.TempDir()
	_, _, err := resolveSnapshotDir(root, "web/imports", "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing snapshot dir")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error message missing not-found phrase: %v", err)
	}
}

func TestResolveSnapshotDir_PathIsFileNotDir(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "web", "imports", "not-a-dir")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err := resolveSnapshotDir(root, "web/imports", "not-a-dir")
	if err == nil {
		t.Fatal("expected error for file (not directory)")
	}
}

func TestRun_StackDown(t *testing.T) {
	root := t.TempDir()
	writeTomlAndSnap(t, root, "sts-launch")

	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "exited"},
	}
	cfg, _ := config.Load(root)

	err := Run(context.Background(), Options{
		Target:   "sts-launch",
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	if err == nil {
		t.Fatal("expected stack-down error")
	}
	if !strings.Contains(err.Error(), "make up") {
		t.Errorf("error message missing make-up hint: %v", err)
	}
}

func TestRun_MissingManifest(t *testing.T) {
	root := t.TempDir()
	// Create the snap dir but no manifest.yaml.
	if err := os.MkdirAll(filepath.Join(root, "web", "imports", "sts-launch"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := docker.NewFake()
	cfg, _ := config.Load(root)
	err := Run(context.Background(), Options{
		Target:   "sts-launch",
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   io.Discard,
	})
	if err == nil {
		t.Fatal("expected manifest-missing error")
	}
	if !strings.Contains(err.Error(), "manifest") {
		t.Errorf("error message missing 'manifest': %v", err)
	}
}

func TestRun_HappyPath_AppliesAndPrintsSummary(t *testing.T) {
	root := t.TempDir()
	writeTomlAndSnap(t, root, "sts-launch")

	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	fake.StreamingFunc = func(_ context.Context, _, _ string, args []string, stdout, _ io.Writer) error {
		// Verify we're calling wp + the right args.
		if len(args) == 0 || args[0] != "wp" {
			t.Errorf("StreamingFunc args[0] = %v, want wp", args)
		}
		foundSlug := false
		for _, a := range args {
			if strings.HasPrefix(a, "--snapshot-dir=") {
				want := "--snapshot-dir=/app/web/imports/sts-launch"
				if a != want {
					t.Errorf("snapshot-dir arg = %q, want %q", a, want)
				}
				foundSlug = true
			}
		}
		if !foundSlug {
			t.Error("StreamingFunc missing --snapshot-dir flag")
		}
		_, _ = stdout.Write([]byte("Success: apply complete\n"))
		return nil
	}

	cfg, _ := config.Load(root)

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		Target:   "sts-launch",
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &out,
		Stderr:   io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, want := range []string{
		"applying \"sts-launch\"",
		"applied snapshot: sts-launch",
		"templates:    5 upserted",
		"options:      8 updated",
		"attachments:  2 upserted",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q\n%s", want, out.String())
		}
	}
}

func TestRun_SkippedPath_PrintsSkippedSummary(t *testing.T) {
	root := t.TempDir()
	writeTomlAndSnap(t, root, "sts-launch")

	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	fake.StreamingFunc = func(_ context.Context, _, _ string, _ []string, stdout, _ io.Writer) error {
		_, _ = stdout.Write([]byte("snapshot already applied (idempotency markers matched); no-op\nSuccess: apply skipped\n"))
		return nil
	}

	cfg, _ := config.Load(root)

	var out bytes.Buffer
	err := Run(context.Background(), Options{
		Target:   "sts-launch",
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &out,
		Stderr:   io.Discard,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "snapshot already applied: sts-launch") {
		t.Errorf("missing skipped-summary line\n%s", out.String())
	}
	// Make sure we didn't ALSO print the "applied" line.
	if strings.Contains(out.String(), "applied snapshot:") {
		t.Errorf("printed both applied + skipped lines\n%s", out.String())
	}
}

func TestRun_WPCLIFailure_SurfacesExitCode(t *testing.T) {
	root := t.TempDir()
	writeTomlAndSnap(t, root, "sts-launch")

	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "test-site-1", Service: "site", State: "running"},
	}
	fake.StreamingErr = &docker.ExecError{ExitCode: 1}

	cfg, _ := config.Load(root)
	var stderr bytes.Buffer
	err := Run(context.Background(), Options{
		Target:   "sts-launch",
		RepoRoot: root,
		Config:   cfg,
		Runner:   fake,
		Stdout:   io.Discard,
		Stderr:   &stderr,
	})
	if err == nil {
		t.Fatal("expected wp apply failure")
	}
	if !strings.Contains(stderr.String(), "wp fp apply exited 1") {
		t.Errorf("stderr missing exit-code framing: %s", stderr.String())
	}
}

func TestRun_NoTarget(t *testing.T) {
	err := Run(context.Background(), Options{})
	if err == nil {
		t.Fatal("expected error for missing target")
	}
	if !errors.Is(err, err) || !strings.Contains(err.Error(), "snapshot dir or slug") {
		t.Errorf("error message missing usage hint: %v", err)
	}
}

func TestCaptureWriter_DetectsSkipped(t *testing.T) {
	var out bytes.Buffer
	cw := &captureWriter{w: &out}
	_, _ = cw.Write([]byte("Success: apply skipped\n"))
	if !cw.skipped {
		t.Error("captureWriter.skipped is false; expected true after seeing 'apply skipped'")
	}
}

// --- helpers ---

func writeTomlAndSnap(t *testing.T, root, slug string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := filepath.Join(root, "web", "imports", slug)
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snap, "manifest.yaml"), fixtureManifest(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureManifest() []byte {
	return []byte(`schema: fp.snapshot/v4
id: test-20260513
created: "2026-05-13T10:00:00Z"
source:
  source_theme: twentytwentyfive
author:
  note: test
adapter: fse
scope:
  post_types_owned:
  - wp_template
  - wp_template_part
contents:
  templates_count: 5
  options_count: 8
  attachments_count: 2
`)
}

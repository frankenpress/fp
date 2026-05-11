package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, Filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadCompleteConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
[site]
tenant = "EightOEight"
name   = "sts"
repo   = "EightOEight/sts"

[snapshots]
bucket = "sts-snapshots"

[gitops]
repo           = "aypex-io/gitops-fp"
applicationset = "apps/applicationset.yaml"
site_key       = "sts"

[signers]
identities = ["m.kennedy@aypex.io"]
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Site.Name != "sts" {
		t.Errorf("Site.Name = %q", cfg.Site.Name)
	}
	if cfg.Snapshots.Bucket != "sts-snapshots" {
		t.Errorf("Snapshots.Bucket = %q", cfg.Snapshots.Bucket)
	}
	if cfg.Gitops.Repo != "aypex-io/gitops-fp" {
		t.Errorf("Gitops.Repo = %q", cfg.Gitops.Repo)
	}
	if len(cfg.Signers.Identities) != 1 || cfg.Signers.Identities[0] != "m.kennedy@aypex.io" {
		t.Errorf("Signers.Identities = %v", cfg.Signers.Identities)
	}

	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate on complete config: %v", err)
	}
}

func TestLoadWalksUpToParent(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `
[site]
name = "x"
repo = "o/x"
[snapshots]
bucket = "b"
[gitops]
repo = "o/g"
applicationset = "a.yaml"
site_key = "x"
`)

	nested := filepath.Join(root, "deeply", "nested", "subdir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(nested)
	if err != nil {
		t.Fatalf("Load from nested: %v", err)
	}
	if cfg.Path != filepath.Join(root, Filename) {
		t.Errorf("Path = %q, want %q", cfg.Path, filepath.Join(root, Filename))
	}
}

func TestLoadNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected errors.Is ErrNotFound; got %v", err)
	}
}

func TestLoadRejectsUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
[site]
name = "x"
unexpected_field = "typo"
`)

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for unknown key, got nil")
	}
	if !strings.Contains(err.Error(), "unknown keys") {
		t.Errorf("error should mention unknown keys; got %v", err)
	}
}

func TestValidateReportsMissingFields(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `
[site]
name = "x"
`)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	verr := cfg.Validate()
	if verr == nil {
		t.Fatal("expected validation error")
	}
	msg := verr.Error()
	for _, key := range []string{"site.repo", "snapshots.bucket", "gitops.repo", "gitops.applicationset", "gitops.site_key"} {
		if !strings.Contains(msg, key) {
			t.Errorf("error should mention %q; got %v", key, msg)
		}
	}
}

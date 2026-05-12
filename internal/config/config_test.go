package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_DefaultsWhenTOMLMissingButComposerPresent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoRoot != root {
		t.Errorf("RepoRoot = %q, want %q", cfg.RepoRoot, root)
	}
	if cfg.Path != "" {
		t.Errorf("Path = %q, want empty", cfg.Path)
	}
	want := Defaults()
	if cfg.Snapshot != want {
		t.Errorf("Snapshot = %+v, want %+v", cfg.Snapshot, want)
	}
}

func TestLoad_TOMLOverridesDefaults(t *testing.T) {
	root := t.TempDir()
	toml := `
[site]
tenant = "EightOEight"
name   = "sts"

[snapshot]
project = "custom-project"
service = "wordpress"
output_dir = "design/imports"
container_output_dir = "/var/www/imports"
`
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Snapshot.Project != "custom-project" {
		t.Errorf("Project = %q, want custom-project", cfg.Snapshot.Project)
	}
	if cfg.Snapshot.Service != "wordpress" {
		t.Errorf("Service = %q, want wordpress", cfg.Snapshot.Service)
	}
	if cfg.Snapshot.OutputDir != "design/imports" {
		t.Errorf("OutputDir = %q, want design/imports", cfg.Snapshot.OutputDir)
	}
	if cfg.Snapshot.ContainerOutputDir != "/var/www/imports" {
		t.Errorf("ContainerOutputDir = %q, want /var/www/imports", cfg.Snapshot.ContainerOutputDir)
	}
}

func TestLoad_EmptyTOMLKeepsDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Snapshot != Defaults() {
		t.Errorf("Snapshot = %+v, want %+v", cfg.Snapshot, Defaults())
	}
}

func TestLoad_WalksUpFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "web", "imports")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(sub)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.RepoRoot != root {
		t.Errorf("RepoRoot = %q, want %q", cfg.RepoRoot, root)
	}
}

func TestLoad_InvalidTOMLReturnsParseError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "frankenpress.toml"), []byte("[snapshot\nfoo = bar"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load: expected parse error, got nil")
	}
	var parseErr *ErrTOMLParse
	if !errors.As(err, &parseErr) {
		t.Fatalf("Load: expected *ErrTOMLParse, got %T (%v)", err, err)
	}
}

func TestLoad_NoMarkerReturnsErrRepoRootNotFound(t *testing.T) {
	root := t.TempDir()
	_, err := Load(root)
	if !errors.Is(err, ErrRepoRootNotFound) {
		t.Fatalf("Load: expected ErrRepoRootNotFound, got %v", err)
	}
}

func TestDefaults(t *testing.T) {
	d := Defaults()
	if d.Service != "site" {
		t.Errorf("Service = %q, want site", d.Service)
	}
	if d.OutputDir != "web/imports" {
		t.Errorf("OutputDir = %q, want web/imports", d.OutputDir)
	}
	if d.ContainerOutputDir != "/app/web/imports" {
		t.Errorf("ContainerOutputDir = %q, want /app/web/imports", d.ContainerOutputDir)
	}
	if d.Project != "" {
		t.Errorf("Project = %q, want empty (auto-detected later)", d.Project)
	}
}

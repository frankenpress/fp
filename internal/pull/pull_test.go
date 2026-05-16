package pull

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/aws"
	"github.com/frankenpress/fp/internal/config"
)

func mkConfig(repoRoot, bucket string) *config.Config {
	return &config.Config{
		RepoRoot: repoRoot,
		Pull:     config.PullConfig{Bucket: bucket, Profile: "mkennedy", Region: "eu-west-2"},
	}
}

func TestRun_RefusesWhenBucketUnset(t *testing.T) {
	tmp := t.TempDir()
	cfg := &config.Config{RepoRoot: tmp}
	fake := aws.NewFake()

	err := Run(context.Background(), Options{
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error when [pull].bucket is unset")
	}
	if !strings.Contains(err.Error(), "[pull].bucket") {
		t.Errorf("error should mention [pull].bucket: %v", err)
	}
	if fake.CallCount("ListSnapshotPrefixes") != 0 {
		t.Errorf("should not have called aws ls when bucket unset")
	}
}

func TestRun_PickLatestByLex(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	fake.Prefixes = []string{
		"prod-2026-05-14T00-00-00Z",
		"prod-2026-05-15T00-00-00Z",
		"prod-2026-05-16T00-00-00Z",
	}
	fake.SyncFiles = map[string]map[string][]byte{
		"prod-2026-05-16T00-00-00Z": {
			"manifest.yaml": []byte("schema: fp.snapshot/v5\n"),
		},
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify the latest slug landed under .fp/prod-snapshots/
	destManifest := filepath.Join(tmp, ".fp", "prod-snapshots", "prod-2026-05-16T00-00-00Z", "manifest.yaml")
	if _, err := os.Stat(destManifest); err != nil {
		t.Errorf("expected manifest at %s: %v", destManifest, err)
	}

	// And that we picked the latest (lex-highest) slug.
	if !strings.Contains(stdout.String(), "prod-2026-05-16T00-00-00Z") {
		t.Errorf("stdout missing latest slug: %s", stdout.String())
	}

	// Profile + region threaded through.
	for _, c := range fake.Calls {
		if c.Profile != "mkennedy" {
			t.Errorf("profile not threaded: %v", c)
		}
		if c.Region != "eu-west-2" {
			t.Errorf("region not threaded: %v", c)
		}
	}
}

func TestRun_SlugFlagOverridesAutoPick(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	fake.Prefixes = []string{
		"prod-2026-05-14T00-00-00Z",
		"prod-2026-05-15T00-00-00Z",
		"prod-2026-05-16T00-00-00Z",
	}
	fake.SyncFiles = map[string]map[string][]byte{
		"prod-2026-05-15T00-00-00Z": {"manifest.yaml": []byte("x")},
	}

	err := Run(context.Background(), Options{
		Slug:     "prod-2026-05-15T00-00-00Z",
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := fake.Calls[len(fake.Calls)-1].Slug
	if got != "prod-2026-05-15T00-00-00Z" {
		t.Errorf("SyncDown slug = %q, want prod-2026-05-15T00-00-00Z", got)
	}
}

func TestRun_SlugFlagWithUnknownSlugErrors(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	fake.Prefixes = []string{"prod-2026-05-14T00-00-00Z"}

	err := Run(context.Background(), Options{
		Slug:     "prod-2099-12-31T00-00-00Z",
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error for unknown slug")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should say 'not found': %v", err)
	}
	if fake.CallCount("SyncDown") != 0 {
		t.Errorf("should not SyncDown when slug unknown")
	}
}

func TestRun_ListOnlySkipsDownload(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	fake.Prefixes = []string{
		"prod-2026-05-14T00-00-00Z",
		"prod-2026-05-15T00-00-00Z",
	}

	var stdout bytes.Buffer
	err := Run(context.Background(), Options{
		ListOnly: true,
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &stdout,
		Stderr:   &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if fake.CallCount("SyncDown") != 0 {
		t.Errorf("--list should not SyncDown; got %d calls", fake.CallCount("SyncDown"))
	}
	if !strings.Contains(stdout.String(), "prod-2026-05-15T00-00-00Z") {
		t.Errorf("stdout should list available slugs: %s", stdout.String())
	}
}

func TestRun_EmptyBucketAutopickErrors(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	// fake.Prefixes intentionally empty.

	err := Run(context.Background(), Options{
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error for empty bucket on autopick")
	}
	if !strings.Contains(err.Error(), "no snapshots") {
		t.Errorf("error should say 'no snapshots': %v", err)
	}
}

func TestRun_AwsErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	fake.ListErr = errors.New("aws ls boom")

	err := Run(context.Background(), Options{
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	if err == nil {
		t.Fatal("expected error when aws ls fails")
	}
	if !strings.Contains(err.Error(), "aws ls boom") {
		t.Errorf("error should wrap aws ls error: %v", err)
	}
}

func TestRun_DropsGitignoreStub(t *testing.T) {
	tmp := t.TempDir()
	cfg := mkConfig(tmp, "test-bucket")
	fake := aws.NewFake()
	fake.Prefixes = []string{"prod-2026-05-16T00-00-00Z"}
	fake.SyncFiles = map[string]map[string][]byte{
		"prod-2026-05-16T00-00-00Z": {"manifest.yaml": []byte("x")},
	}

	if err := Run(context.Background(), Options{
		RepoRoot: tmp,
		Config:   cfg,
		Runner:   fake,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	gi := filepath.Join(tmp, ".fp", "prod-snapshots", ".gitignore")
	content, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("expected .gitignore stub at %s: %v", gi, err)
	}
	if !strings.Contains(string(content), "*") {
		t.Errorf(".gitignore should contain '*': %q", string(content))
	}
}

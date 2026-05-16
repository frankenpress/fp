// Package pull wires the fp pull subcommand end-to-end.
//
// Downloads a snapshot bundle from the per-tenant S3 snapshot bucket
// (created out-of-band by tg_frankenpress) into
// `.fp/prod-snapshots/<slug>/` so designers can `fp apply` it locally
// for theme work against real-volume content.
//
// Same Runner-shaped seam as the rest of fp — the orchestrator takes
// an aws.Runner, picks the latest (or a specified) slug, syncs the
// prefix down, prints a summary.
//
// Capture/transport split: this CLI does not capture. The mu-plugin's
// SnapshotExporter component (cluster side) handles capture + upload
// on a daily wp-cron event or admin-button trigger.
package pull

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/frankenpress/fp/internal/aws"
	"github.com/frankenpress/fp/internal/config"
)

// pulledDir is where downloaded snapshots land, relative to the
// repo root. Gitignored alongside `.fp/state.json` — these are
// ephemeral working copies, not committed history.
const pulledDir = ".fp/prod-snapshots"

// Options carries every input fp pull needs.
type Options struct {
	// Slug overrides the auto-pick-latest behaviour. Empty → pick
	// latest by lex order (snapshot slugs are ISO timestamps).
	Slug string

	// ListOnly skips the download — just lists available slugs.
	ListOnly bool

	RepoRoot string
	Config   *config.Config
	Runner   aws.Runner

	Stdout io.Writer
	Stderr io.Writer
}

// Run executes the pull pipeline.
func Run(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Config == nil {
		return fmt.Errorf("pull: nil config")
	}
	if opts.Runner == nil {
		return fmt.Errorf("pull: nil aws runner")
	}

	bucket := opts.Config.Pull.Bucket
	if bucket == "" {
		return fmt.Errorf(
			"[pull].bucket is not set in frankenpress.toml — add it with the name of your tenant's snapshot bucket (e.g. \"sts-production-snapshots-eu-west-2-533158516642\")",
		)
	}
	profile := opts.Config.Pull.Profile
	region := opts.Config.Pull.Region

	if opts.ListOnly {
		return runList(ctx, opts, bucket, profile, region)
	}

	slug, err := resolveSlug(ctx, opts, bucket, profile, region)
	if err != nil {
		return err
	}

	destDir := filepath.Join(opts.RepoRoot, pulledDir, slug)
	if err := os.MkdirAll(filepath.Dir(destDir), 0o755); err != nil {
		return fmt.Errorf("create pulled-snapshots parent dir: %w", err)
	}

	fmt.Fprintf(opts.Stdout, "[fp] pulling s3://%s/%s/ → %s/\n", bucket, slug, filepath.Join(pulledDir, slug))
	if err := opts.Runner.SyncDown(ctx, bucket, slug, destDir, profile, region); err != nil {
		return fmt.Errorf("aws s3 sync failed: %w", err)
	}

	// Best-effort: drop a .gitignore stub inside .fp/prod-snapshots/
	// so designers who haven't already gitignored .fp/ don't
	// accidentally commit pulled content.
	writeGitignoreStub(filepath.Join(opts.RepoRoot, pulledDir))

	fmt.Fprintf(opts.Stdout, "pulled snapshot: %s\n", slug)
	fmt.Fprintf(opts.Stdout, "  apply with:    fp apply %s\n", slug)
	return nil
}

func runList(ctx context.Context, opts Options, bucket, profile, region string) error {
	slugs, err := opts.Runner.ListSnapshotPrefixes(ctx, bucket, profile, region)
	if err != nil {
		return fmt.Errorf("aws s3 ls failed: %w", err)
	}
	if len(slugs) == 0 {
		fmt.Fprintf(opts.Stdout, "no snapshots in s3://%s/ — the SnapshotExporter daily cron may not have fired yet, or `snapshotExport.enabled` may not be set on the tenant chart\n", bucket)
		return nil
	}
	fmt.Fprintf(opts.Stdout, "snapshots in s3://%s/:\n", bucket)
	// Reverse-sort so the newest prints first (slugs are ISO
	// timestamps ascending from aws ls; designers want newest at top).
	for i := len(slugs) - 1; i >= 0; i-- {
		fmt.Fprintf(opts.Stdout, "  %s\n", slugs[i])
	}
	return nil
}

// resolveSlug returns the slug to pull. With --slug, validates it
// exists in the bucket (catches typos before paying the sync cost).
// Without, picks the highest lex slug from the listing (== newest
// for ISO timestamps).
func resolveSlug(ctx context.Context, opts Options, bucket, profile, region string) (string, error) {
	slugs, err := opts.Runner.ListSnapshotPrefixes(ctx, bucket, profile, region)
	if err != nil {
		return "", fmt.Errorf("aws s3 ls failed: %w", err)
	}
	if len(slugs) == 0 {
		return "", fmt.Errorf("no snapshots in s3://%s/ — try `fp pull --list` to confirm, or check that the SnapshotExporter daily cron has fired on the tenant", bucket)
	}
	if opts.Slug != "" {
		for _, s := range slugs {
			if s == opts.Slug {
				return s, nil
			}
		}
		return "", fmt.Errorf("snapshot %q not found in s3://%s/. available: try `fp pull --list`", opts.Slug, bucket)
	}
	// Pick the highest lex slug (slugs ascending → last is newest).
	return slugs[len(slugs)-1], nil
}

// writeGitignoreStub drops a `*` gitignore inside .fp/prod-snapshots/
// so the pulled content can't be accidentally committed. Best-effort —
// silently skips if the file exists or can't be written.
func writeGitignoreStub(dir string) {
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); err == nil {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(gi, []byte("*\n!.gitignore\n"), 0o644)
}

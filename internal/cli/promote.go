package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/frankenpress/fp/internal/promote"
	"github.com/frankenpress/fp/pkg/config"
	"github.com/spf13/cobra"
)

func newPromoteCmd() *cobra.Command {
	var (
		snapshotDir string
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "promote {stg|prd} [--snapshot-dir=<path>]",
		Short: "Upload a snapshot's blobs to S3 and open a gitops PR for the values bump",
		Long: `Promotes a snapshot directory to staging or production.

Reads frankenpress.toml at the site repo root (or any ancestor of cwd) to
find the snapshots bucket + gitops repo configuration. Uploads the
snapshot's artefacts (manifest.yaml, manifest.json, composer-patch.json,
uploads-manifest.txt, db.sql.gz) to s3://<snapshots.bucket>/snapshots/<id>/,
then clones the gitops repo, edits the applicationset.yaml matrix entry
matching gitops.site_key, commits the snapshot bump, pushes a branch,
and opens a PR for engineer review.

If --snapshot-dir isn't passed, the most recent fp-snapshots/<slug>-* dir
is used.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			env := promote.Env(args[0])
			if !env.Valid() {
				return fmt.Errorf("invalid env %q (must be stg or prd)", args[0])
			}

			cfg, err := config.Load("")
			if err != nil {
				if errors.Is(err, config.ErrNotFound) {
					return fmt.Errorf("frankenpress.toml not found — run `fp promote` from inside a site repo, or pass --snapshot-dir from one")
				}
				return err
			}
			if err := cfg.Validate(); err != nil {
				return err
			}

			dir, err := resolveSnapshotDir(snapshotDir)
			if err != nil {
				return err
			}

			m, err := promote.LoadManifest(dir)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "[fp] promoting snapshot id=%s env=%s\n", m.ID, env)
			fmt.Fprintf(out, "[fp] snapshot dir: %s\n", dir)
			fmt.Fprintf(out, "[fp] gitops repo: %s (%s)\n", cfg.Gitops.Repo, cfg.Gitops.Applicationset)

			if dryRun {
				fmt.Fprintln(out, "\n[dry-run] no S3 upload, no gitops PR. Re-run without --dry-run when ready.")
				return nil
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Minute)
			defer cancel()

			uploader := promote.Uploader{
				Bucket: cfg.Snapshots.Bucket,
				Stdout: out,
				Stderr: cmd.ErrOrStderr(),
			}
			s3Key, err := uploader.Upload(ctx, dir, m.ID)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "[fp] uploaded to s3://%s/%s\n", cfg.Snapshots.Bucket, s3Key)

			opener := promote.GitopsPR{
				GitopsRepo:     cfg.Gitops.Repo,
				Applicationset: cfg.Gitops.Applicationset,
				SiteKey:        cfg.Gitops.SiteKey,
				Bucket:         cfg.Snapshots.Bucket,
				Stdout:         out,
				Stderr:         cmd.ErrOrStderr(),
			}
			url, err := opener.Open(ctx, env, m.ID, s3Key)
			if err != nil {
				return err
			}

			fmt.Fprintln(out)
			fmt.Fprintf(out, "promote PR opened: %s\n", url)
			fmt.Fprintln(out, "next: engineer reviews the PR (composer.json deltas + applicationset.yaml bump), merges → ArgoCD reconciles → install Job's `wp fp apply` runs.")
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotDir, "snapshot-dir", "", "Snapshot directory to promote. Defaults to the most recent fp-snapshots/* entry under cwd.")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Read config + manifest, print the planned actions; no S3 upload, no gh pr create.")

	return cmd
}

// resolveSnapshotDir returns the explicit override or finds the
// newest dir under ./fp-snapshots/.
func resolveSnapshotDir(override string) (string, error) {
	if override != "" {
		if info, err := os.Stat(override); err != nil || !info.IsDir() {
			return "", fmt.Errorf("--snapshot-dir %q is not a directory", override)
		}
		return override, nil
	}

	entries, err := os.ReadDir("fp-snapshots")
	if err != nil {
		return "", fmt.Errorf("no fp-snapshots/ dir in cwd; run `fp snapshot` first or pass --snapshot-dir")
	}

	var newest os.DirEntry
	var newestTime time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = e
		}
	}
	if newest == nil {
		return "", fmt.Errorf("no snapshot dirs found under fp-snapshots/; run `fp snapshot` first or pass --snapshot-dir")
	}

	return filepath.Join("fp-snapshots", newest.Name()), nil
}

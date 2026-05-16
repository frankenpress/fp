package cli

import (
	"errors"

	"github.com/frankenpress/fp/internal/aws"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/pull"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	var (
		listOnly bool
		slug     string
	)
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Download a prod snapshot from the per-tenant S3 snapshot bucket",
		Long: `Download the latest (or a specified) prod snapshot from the per-tenant S3
snapshot bucket into .fp/prod-snapshots/<slug>/. Designers use the
pulled bundle as a working corpus for theme-against-real-content work;
it's separate from committed designer captures in web/imports/.

The cluster-side capture is handled by the mu-plugin's SnapshotExporter
component (gated on FP_SNAPSHOT_BUCKET on the tenant pod), which
publishes a bundle daily at site-local midnight or on admin-button
demand. fp pull downloads what's already up there — it does NOT
capture or upload.

Configure once in frankenpress.toml:

  [pull]
  bucket  = "sts-production-snapshots-eu-west-2-533158516642"
  profile = "mkennedy"   # optional aws --profile
  # region = "eu-west-2" # optional; aws CLI resolves otherwise

AWS credentials come from your shell (aws-vault exec / AWS_PROFILE /
~/.aws/credentials) — fp does not link the AWS SDK. The Admin role
in the prod account is the canonical access path; the snapshot bucket
inherits its grant from there.

Once pulled, apply the snapshot like any other:

  fp apply prod-2026-05-16T00-00-00Z`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load("")
			if err != nil {
				if errors.Is(err, config.ErrRepoRootNotFound) {
					return errors.New("not inside a FrankenPress site repo (no frankenpress.toml or composer.json found above cwd)")
				}
				return err
			}

			return pull.Run(cmd.Context(), pull.Options{
				Slug:     slug,
				ListOnly: listOnly,
				RepoRoot: cfg.RepoRoot,
				Config:   cfg,
				Runner:   aws.NewReal(),
				Stdout:   cmd.OutOrStdout(),
				Stderr:   cmd.ErrOrStderr(),
			})
		},
	}

	cmd.Flags().BoolVar(&listOnly, "list", false, "List available snapshots without downloading")
	cmd.Flags().StringVar(&slug, "slug", "", "Download a specific snapshot by slug instead of the latest")

	cmd.MarkFlagsMutuallyExclusive("list", "slug")

	return cmd
}

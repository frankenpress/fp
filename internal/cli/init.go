package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/setup"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var (
		slug        string
		skipSetup   bool
		noApply     bool
		reinstallWP bool
		service     string
		project     string
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "One-command designer onboarding: bootstrap, up, install WP, apply latest snapshot",
		Long: `Bootstraps a fresh clone or recovers a wiped local stack in one command:

  1. Scaffold .env from .env.example if missing
  2. Run composer install via docker (no PHP needed on the host)
  3. Write FP_S3_DISABLED=0 to .env (designer-mode S3 → MinIO),
     unless the operator has explicitly set FP_S3_DISABLED already
  4. docker compose up -d --wait
  5. wp core install (if WP isn't installed yet)
  6. Apply the latest snapshot (highest manifest.created) — same
     pick-latest logic the chart's install Job uses on cluster deploy

Designed for two scenarios:

  - First-time onboarding: clone the repo, run "fp init", be ready
    to design within a couple of minutes.
  - Recovery after "docker compose down -v": run "fp init" again
    and you're back to the state captured by the latest snapshot,
    assets and all.

Configuration lives in frankenpress.toml's [init] section (all
optional):

  [init]
  site_title     = "FrankenPress site"
  admin_user     = "admin"
  admin_password = "admin"
  admin_email    = "admin@example.test"
  disable_s3     = false    # true to keep FP_S3_DISABLED=1 locally`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load("")
			if err != nil {
				if errors.Is(err, config.ErrRepoRootNotFound) {
					return errors.New(
						"not inside a FrankenPress site repo. fp expects a frankenpress.toml or a composer.json with a frankenpress/* dep at or above cwd",
					)
				}
				return err
			}

			opts := setup.Options{
				RepoRoot:    cfg.RepoRoot,
				Config:      cfg,
				Runner:      docker.NewReal(),
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Slug:        slug,
				SkipSetup:   skipSetup,
				NoApply:     noApply,
				ReinstallWP: reinstallWP,
				Service:     service,
				Project:     project,
			}
			result, err := setup.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}
			printSummary(cmd.OutOrStdout(), result, cfg)
			return nil
		},
	}

	cmd.Flags().StringVar(&slug, "slug", "", "Override the most-recent-snapshot pick with this slug.")
	cmd.Flags().BoolVar(&skipSetup, "skip-setup", false, "Skip composer install + .env scaffolding. Use for CI / scripted setups.")
	cmd.Flags().BoolVar(&noApply, "no-apply", false, "Bring the stack up + install WP, but don't apply any snapshot.")
	cmd.Flags().BoolVar(&reinstallWP, "reinstall-wp", false, "Drop existing WP install and re-run wp core install. Default off.")
	cmd.Flags().StringVar(&service, "service", "", "Override [snapshot].service for this run.")
	cmd.Flags().StringVar(&project, "project", "", "Override [snapshot].project for this run.")

	return cmd
}

// printSummary renders the post-init "ready" message.
func printSummary(w io.Writer, r *setup.Result, cfg *config.Config) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "fp init complete.")
	if r.SiteURL != "" {
		fmt.Fprintf(w, "  url:        %s\n", r.SiteURL)
	}
	if r.WPInstalled {
		fmt.Fprintf(w, "  admin:      %s / %s\n", cfg.Init.AdminUser, cfg.Init.AdminPassword)
	}
	if r.SnapshotApplied != "" {
		fmt.Fprintf(w, "  snapshot:   %s\n", r.SnapshotApplied)
	} else if r.SnapshotsAbsent {
		fmt.Fprintln(w, "  snapshot:   none yet — capture one with `fp snapshot`")
	}
	if r.S3DesignerMode {
		fmt.Fprintln(w, "  note:       set FP_S3_DISABLED=0 in .env (designer-mode S3, MinIO)")
		fmt.Fprintln(w, "              set to 1 manually if you need wp-admin plugin/theme zip installs")
	}
}

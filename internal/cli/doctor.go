package cli

import (
	"errors"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/doctor"
	"github.com/frankenpress/fp/internal/gh"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Read-only health check of the local FrankenPress stack",
		Long: `Reports on every moving part that fp's other subcommands depend on:
fp + docker compose versions, compose stack status, latest snapshot
under web/imports/, designer-mode S3 toggle, current git branch and
dirty state under web/imports/, and gh auth state.

Always exits 0 — doctor is a report, not a gate. Each "problem" check
prints a one-line hint with the suggested recovery command; act on
those yourself and re-run.`,
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
			return doctor.Run(cmd.Context(), doctor.Options{
				RepoRoot: cfg.RepoRoot,
				Config:   cfg,
				Docker:   docker.NewReal(),
				GH:       gh.NewReal(),
				Stdout:   cmd.OutOrStdout(),
			})
		},
	}
}

package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/prompt"
	"github.com/frankenpress/fp/internal/snapshot"
	"github.com/frankenpress/fp/internal/state"
	"github.com/spf13/cobra"
)

func newSnapshotCmd() *cobra.Command {
	var (
		slug      string
		note      string
		noteFile  string
		quick     bool
		outputDir string
		service   string
		project   string
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Capture local site state into web/imports/<slug>/",
		Long: `Wraps wp fp snapshot inside the running site container, then docker-cp's
the result out into the site repo. Slug + note default to interactive prompts;
override with --slug / --note for non-interactive use.

Common case: from the site repo root, run "fp snapshot" — accept the slug
default (Enter), write a note, done.

Run with --quick for an ad-hoc capture with a timestamped slug and no prompts;
.fp/state.json is not updated.`,
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
			st, err := state.Load(cfg.RepoRoot)
			if err != nil {
				return fmt.Errorf("load .fp/state.json: %w", err)
			}

			opts := snapshot.Options{
				RepoRoot:    cfg.RepoRoot,
				Config:      cfg,
				State:       st,
				Runner:      docker.NewReal(),
				Stdin:       os.Stdin,
				Stdout:      cmd.OutOrStdout(),
				Stderr:      cmd.ErrOrStderr(),
				Interactive: prompt.IsTerminal(os.Stdin) && prompt.IsTerminal(os.Stdout),
				Slug:        slug,
				Note:        note,
				NoteFile:    noteFile,
				Quick:       quick,
				OutputDir:   outputDir,
				Service:     service,
				Project:     project,
			}

			return snapshot.Run(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&slug, "slug", "", "Snapshot slug. Skips the slug prompt.")
	cmd.Flags().StringVar(&note, "note", "", "Designer note. Skips the note prompt. Mutually exclusive with --note-file.")
	cmd.Flags().StringVar(&noteFile, "note-file", "", "Read the designer note from a file. Skips the note prompt.")
	cmd.Flags().BoolVar(&quick, "quick", false, "Skip prompts AND safety guards; force a timestamped slug; do not update .fp/state.json.")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Override [snapshot].output_dir for this run.")
	cmd.Flags().StringVar(&service, "service", "", "Override [snapshot].service for this run.")
	cmd.Flags().StringVar(&project, "project", "", "Override [snapshot].project for this run.")

	return cmd
}

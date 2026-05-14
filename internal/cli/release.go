package cli

import (
	"errors"
	"os"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/gh"
	"github.com/frankenpress/fp/internal/git"
	"github.com/frankenpress/fp/internal/prompt"
	"github.com/frankenpress/fp/internal/release"
	"github.com/frankenpress/fp/internal/state"
	"github.com/spf13/cobra"
)

func newReleaseCmd() *cobra.Command {
	var (
		slug     string
		note     string
		noteFile string
		branch   string
		noPR     bool
		draft    bool
		yes      bool
	)

	cmd := &cobra.Command{
		Use:   "release",
		Short: "Capture + commit + push + open PR in one shot",
		Long: `One-shot designer release flow: runs fp snapshot, then commits
web/imports/<slug>/, pushes to origin, and opens a PR.

Branch policy:
  - If your current branch is main / master / trunk, fp release auto-
    creates feat/snapshot-<slug> and switches to it before committing.
  - Otherwise it stays on the current branch.
  - Override with --branch.

Commit shape:
  - Author: your local git config (designer's own identity, not a bot).
  - Subject: "snapshot: <slug>".
  - Body: the snapshot note.

Pass --yes to skip the commit-confirm prompt (useful in scripts).
Pass --no-pr to commit and push without opening a PR.
Pass --draft to open the PR as a draft (mutually exclusive with --no-pr).

Designed for the canonical "I'm done iterating, ship it" path. For
ad-hoc captures use fp snapshot directly.`,
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
				return err
			}

			opts := release.Options{
				Slug:         slug,
				Note:         note,
				NoteFile:     noteFile,
				Branch:       branch,
				NoPR:         noPR,
				Draft:        draft,
				Yes:          yes,
				RepoRoot:     cfg.RepoRoot,
				Config:       cfg,
				State:        st,
				DockerRunner: docker.NewReal(),
				GitRunner:    git.NewReal(),
				GHRunner:     gh.NewReal(),
				Stdin:        os.Stdin,
				Stdout:       cmd.OutOrStdout(),
				Stderr:       cmd.ErrOrStderr(),
				Interactive:  prompt.IsTerminal(os.Stdin) && prompt.IsTerminal(os.Stdout),
			}
			return release.Run(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&slug, "slug", "", "Snapshot slug. Skips the slug prompt.")
	cmd.Flags().StringVar(&note, "note", "", "Designer note. Skips the note prompt. Mutually exclusive with --note-file.")
	cmd.Flags().StringVar(&noteFile, "note-file", "", "Read the designer note from a file.")
	cmd.Flags().StringVar(&branch, "branch", "", "Target branch (default: current, or feat/snapshot-<slug> from a protected branch).")
	cmd.Flags().BoolVar(&noPR, "no-pr", false, "Skip the gh pr create step.")
	cmd.Flags().BoolVar(&draft, "draft", false, "Open the PR as draft (gh pr create --draft).")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip the commit-confirm prompt.")
	cmd.MarkFlagsMutuallyExclusive("no-pr", "draft")

	return cmd
}

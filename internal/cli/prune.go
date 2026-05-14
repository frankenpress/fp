package cli

import (
	"errors"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/prune"
	"github.com/spf13/cobra"
)

func newPruneCmd() *cobra.Command {
	var (
		keep  int
		apply bool
		quick bool
	)

	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Keep the newest N snapshots, remove the rest",
		Long: `Walks [snapshot].output_dir, keeps the newest --keep N snapshots by
manifest.created, and removes everything older.

Dry-run by default: prints what would go and exits without touching
disk. Pass --apply to actually delete.

When --apply is set, refuses if any candidate has uncommitted git
changes. Pass --quick to override — the project's single safety
bypass.

  fp prune --keep 5             # preview what would be removed
  fp prune --keep 5 --apply     # actually do it
  fp prune --keep 0 --apply     # remove every snapshot (rare)`,
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
			if !cmd.Flags().Changed("keep") {
				return errors.New("prune: --keep is required (use 0 to remove everything)")
			}

			return prune.Prune(prune.PruneOptions{
				Keep:      keep,
				Apply:     apply,
				RepoRoot:  cfg.RepoRoot,
				OutputDir: cfg.Snapshot.OutputDir,
				Quick:     quick,
				Stdout:    cmd.OutOrStdout(),
				Stderr:    cmd.ErrOrStderr(),
			})
		},
	}

	cmd.Flags().IntVar(&keep, "keep", -1, "Number of newest snapshots to retain. Required. 0 = remove all.")
	cmd.Flags().BoolVar(&apply, "apply", false, "Actually delete. Without this flag, prune is a dry-run.")
	cmd.Flags().BoolVar(&quick, "quick", false, "Skip the uncommitted-changes guard. The single safety-bypass flag.")

	return cmd
}

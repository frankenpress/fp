package cli

import (
	"errors"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/prune"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var quick bool

	cmd := &cobra.Command{
		Use:     "delete <snapshot-dir-or-slug>",
		Aliases: []string{"rm"},
		Short:   "Delete a single local snapshot directory",
		Long: `Removes one snapshot directory from disk. The positional argument is
interpreted the same way fp apply / fp diff interpret it:

  - a bare slug (e.g. "sts-launch") -> resolved against [snapshot].output_dir
  - a relative path with separators -> relative to your current directory
  - an absolute path

The target must contain a manifest.yaml (the "is this even a snapshot"
safety check) and must resolve to a path inside the repo root. fp
delete only operates on snapshots inside the site repo; it will not
blindly rm anything outside.

Refuses by default when the target has uncommitted git changes. Pass
--quick to override — the project's single safety-bypass flag.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				if errors.Is(err, config.ErrRepoRootNotFound) {
					return errors.New(
						"not inside a FrankenPress site repo. fp expects a frankenpress.toml or a composer.json with a frankenpress/* dep at or above cwd",
					)
				}
				return err
			}

			return prune.Delete(prune.DeleteOptions{
				Target:    args[0],
				RepoRoot:  cfg.RepoRoot,
				OutputDir: cfg.Snapshot.OutputDir,
				Quick:     quick,
				Stdout:    cmd.OutOrStdout(),
				Stderr:    cmd.ErrOrStderr(),
			})
		},
	}

	cmd.Flags().BoolVar(&quick, "quick", false, "Skip the uncommitted-changes guard. The single safety-bypass flag.")

	return cmd
}

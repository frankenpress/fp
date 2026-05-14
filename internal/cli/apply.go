package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/frankenpress/fp/internal/apply"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var (
		service string
		project string
	)

	cmd := &cobra.Command{
		Use:   "apply [<snapshot-dir-or-slug>]",
		Short: "Apply a snapshot back into the local stack",
		Long: `Stages a snapshot directory into the running site container and runs
wp fp apply against it. Useful for round-trip iteration on the local
docker-compose stack: capture, edit, apply, repeat.

With no positional argument, fp picks the snapshot with the highest
manifest.created under [snapshot].output_dir — the same logic the
charts install Job uses at deploy time. Run "fp apply" alone after a
fresh "fp snapshot" to re-apply your most recent capture.

With a positional argument, it is interpreted as:
  - a bare slug (e.g. "sts-launch") -> resolved against [snapshot].output_dir
  - a relative path with separators -> relative to your current directory
  - an absolute path

Either way the path must resolve to a directory inside the site repo,
since fp stages it into the container's mirror at /app/<rel-to-repo>.

The mu-plugin's apply is idempotent — markers short-circuit re-applies.
fp surfaces "snapshot already applied" cleanly without erroring.`,
		Args: cobra.MaximumNArgs(1),
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

			var target string
			if len(args) > 0 {
				target = args[0]
			}

			opts := apply.Options{
				Target:   target,
				RepoRoot: cfg.RepoRoot,
				Config:   cfg,
				Runner:   docker.NewReal(),
				Stdout:   cmd.OutOrStdout(),
				Stderr:   cmd.ErrOrStderr(),
				Service:  service,
				Project:  project,
			}

			if err := apply.Run(cmd.Context(), opts); err != nil {
				return err
			}
			_, _ = fmt.Fprintln(os.Stderr) // padding for the trailing prompt
			return nil
		},
	}

	cmd.Flags().StringVar(&service, "service", "", "Override [snapshot].service for this run.")
	cmd.Flags().StringVar(&project, "project", "", "Override [snapshot].project for this run.")

	return cmd
}

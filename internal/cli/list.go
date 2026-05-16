package cli

import (
	"errors"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/list"
	"github.com/frankenpress/fp/internal/pull"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var (
		jsonOut bool
		limit   int
	)

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List local snapshots with manifest metadata",
		Long: `Walks the configured snapshot output dir (default web/imports/) and
prints one row per snapshot with its created timestamp, content counts,
and the first line of the designer note.

Output is sorted newest-first by manifest.created. Snapshots whose
manifest is missing a created field are still listed (with "—" in the
created column) so broken captures don't disappear.

Pass --json for a machine-readable array — useful when scripting
"keep the latest N captures" or piping into jq.`,
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

			outputDir := cfg.Snapshot.OutputDir
			if outputDir == "" {
				outputDir = "web/imports"
			}

			format := "table"
			if jsonOut {
				format = "json"
			}

			return list.Run(list.Options{
				RepoRoot:  cfg.RepoRoot,
				OutputDir: outputDir,
				PullDir:   pull.PulledDir,
				Limit:     limit,
				Format:    format,
				Stdout:    cmd.OutOrStdout(),
			})
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Emit a JSON array instead of the human table.")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap the output to the most recent N snapshots. 0 = no cap.")

	return cmd
}

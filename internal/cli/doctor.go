package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/frankenpress/fp/internal/doctor"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Pre-flight environment checks",
		Long: `Verifies the tools fp needs are installed and reachable.

  - Required: docker, docker compose, git, gh
  - Optional: aws (needed for fp promote), jq (used by Makefile recipes)

Exits non-zero if any required tool is missing. Warnings (missing
optional tools) do not affect the exit code — fp snapshot still works
without aws / jq.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()

			results := doctor.Run(ctx)

			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			for _, r := range results {
				fmt.Fprintf(tw, "%s\t%s\t%s\n", r.Status, r.Name, r.Summary)
			}
			_ = tw.Flush()

			if doctor.HasFailure(results) {
				return fmt.Errorf("one or more required tools are missing")
			}
			return nil
		},
	}
}

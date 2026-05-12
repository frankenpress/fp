package cli

import "github.com/spf13/cobra"

func newApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <snapshot-dir>",
		Short: "Apply a snapshot back into the local stack (Phase 2)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return errNotImplemented("apply")
		},
	}
}

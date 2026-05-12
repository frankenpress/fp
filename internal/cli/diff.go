package cli

import "github.com/spf13/cobra"

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff <slug>",
		Short: "Diff current site state against a committed snapshot (later)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return errNotImplemented("diff")
		},
	}
}

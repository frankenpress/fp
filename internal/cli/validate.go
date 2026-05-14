package cli

import "github.com/spf13/cobra"

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "validate <snapshot-dir>",
		Short:  "Validate a snapshot's manifest schema (later)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errNotImplemented("validate")
		},
	}
}

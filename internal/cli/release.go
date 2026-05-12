package cli

import "github.com/spf13/cobra"

func newReleaseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "release",
		Short: "Capture + commit + push + open PR in one shot (later)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return errNotImplemented("release")
		},
	}
}

// Package cli wires the Cobra command tree for fp.
//
// The root command lives here; each subcommand has its own file so
// that adding a new verb is one file + one NewRoot() registration.
package cli

import (
	"github.com/frankenpress/fp/internal/version"
	"github.com/spf13/cobra"
)

// NewRoot builds the top-level `fp` command with all v1 subcommands
// registered. Splitting this out as a function (vs. an init-time
// var) keeps tests free of global state — each test that exercises
// the CLI builds a fresh tree.
func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fp",
		Short: "FrankenPress designer-promotion CLI",
		Long: `fp captures a designer's local FrankenPress site state and promotes it
through gitops to staging / production. Pairs with the wp fp snapshot
WP-CLI subcommand (provided by frankenpress/mu-plugin v0.7.0+).

See https://docs.frankenpress.com/components/fp for the full v1 design.`,
		Version:           version.String(),
		SilenceUsage:      true,
		SilenceErrors:     false,
		DisableAutoGenTag: true,
	}

	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newDoctorCmd())
	cmd.AddCommand(newSnapshotCmd())
	cmd.AddCommand(newPromoteCmd())

	return cmd
}

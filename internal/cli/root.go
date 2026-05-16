// Package cli wires the cobra command tree for the fp binary.
//
// The root command has zero behaviour beyond printing help; every
// real verb is a subcommand registered here. Adding a verb is one
// new file in this package + one cmd.AddCommand line below.
package cli

import (
	"fmt"

	"github.com/frankenpress/fp/internal/version"
	"github.com/spf13/cobra"
)

// Root wraps the cobra root command so cmd/fp/main.go has a stable
// surface to call (Run + exit code) without leaking cobra types.
type Root struct {
	cmd *cobra.Command
}

// NewRoot builds the full fp command tree.
func NewRoot() *Root {
	cmd := &cobra.Command{
		Use:           "fp",
		Short:         "FrankenPress designer-promotion CLI",
		Long:          rootLong,
		Version:       version.String(),
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	cmd.AddCommand(
		newInitCmd(),
		newUpCmd(),
		newDownCmd(),
		newRestartCmd(),
		newLogsCmd(),
		newSnapshotCmd(),
		newApplyCmd(),
		newListCmd(),
		newDiffCmd(),
		newDeleteCmd(),
		newPruneCmd(),
		newPullCmd(),
		newDoctorCmd(),
		newWPCmd(),
		newValidateCmd(),
		newReleaseCmd(),
		newVersionCmd(),
	)

	return &Root{cmd: cmd}
}

// Run executes the command tree with the supplied args. Returns the
// process exit code; cobra prints any error to stderr before this
// returns, so the caller just needs to forward the code to os.Exit.
func (r *Root) Run(args []string) int {
	r.cmd.SetArgs(args)
	err := r.cmd.Execute()
	if err == nil {
		return 0
	}
	// Subcommands that want a specific exit code (e.g. the "not
	// implemented yet" stubs returning 2) wrap their error in
	// exitCodeError. Anything else is exit 1.
	if ec, ok := err.(exitCodeError); ok {
		return ec.code
	}
	return 1
}

// CobraCmd exposes the underlying cobra command for tests that need
// to inspect flag wiring or invoke subcommands directly.
func (r *Root) CobraCmd() *cobra.Command { return r.cmd }

// exitCodeError lets a subcommand request a specific non-1 exit code.
type exitCodeError struct {
	code int
	msg  string
}

func (e exitCodeError) Error() string { return e.msg }

// errNotImplemented is returned by Phase-1 stub subcommands. It
// produces an exit code of 2 (per the plan's command-surface table)
// so callers can distinguish "not built yet" from a real failure.
func errNotImplemented(verb string) error {
	return exitCodeError{
		code: 2,
		msg:  fmt.Sprintf("fp %s is not implemented yet — see https://github.com/frankenpress/fp for the roadmap", verb),
	}
}

const rootLong = `fp wraps the FrankenPress designer-promotion lifecycle: bootstrap a fresh
clone, capture local site state, apply snapshots back for round-trip
iteration, and ship the result via PR. It is a thin host-side ergonomics
layer over the wp fp WP-CLI subcommands provided by frankenpress/mu-plugin.

  fp init                  one-command onboarding: bootstrap + up + apply latest
  fp up [args...]          bring the local stack up (-d --wait auto-applied)
  fp down [args...]        tear the local stack down
  fp restart [args...]     restart services in the local stack
  fp logs [args...]        print logs from the local stack
  fp snapshot              capture local site state into web/imports/<slug>/
  fp apply [dir-or-slug]   apply a snapshot back into the local stack
  fp list                  list local snapshots with manifest metadata
  fp diff <a> <b>          structural delta between two committed snapshots
  fp delete <dir-or-slug>  remove a single local snapshot
  fp prune --keep N        keep the newest N snapshots, remove the rest
  fp pull                  download a prod snapshot from S3 into .fp/prod-snapshots/
  fp doctor                read-only health check of the local stack
  fp wp <args...>          run wp-cli inside the running site container
  fp release               one-shot capture + commit + push + open PR
  fp version               print the binary version

fp init brings a fresh clone (or a post-down -v stack) to "ready to
design" in one command. fp up / down / restart / logs are thin
passthroughs to docker compose for the daily dev loop. snapshot /
apply / release expect docker-compose to already be running and shell
out to docker compose exec. list / diff / delete / prune / doctor are
pure host-side or read-only — they don't mutate the running stack.`

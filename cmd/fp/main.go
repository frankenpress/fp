// Command fp is the FrankenPress designer-promotion CLI.
//
// fp captures a designer's local WordPress site state (DB, plugin
// diff, premium-theme adapter state) into a portable snapshot bundle
// and promotes it through gitops to staging/production. See
// https://github.com/frankenpress/fp for the v1 design.
//
// Phase 1 surface (v0.1.x):
//
//	fp version    show binary + manifest schema versions
//	fp doctor     pre-flight environment checks
//	fp snapshot   capture local site state (wraps `wp fp snapshot`
//	              inside the running site container)
//
// Phase 2+ adds promote / restore / diff / adapters subcommands; see
// the plan in `.aidocs/fp-cli-design.md`.
package main

import (
	"fmt"
	"os"

	"github.com/frankenpress/fp/internal/cli"
)

func main() {
	if err := cli.NewRoot().Execute(); err != nil {
		// Cobra's Execute() already prints the error to stderr; we
		// only need to set the exit code so `fp foo && next` short-
		// circuits correctly in shell pipelines.
		fmt.Fprintln(os.Stderr) //nolint:errcheck
		os.Exit(1)
	}
}

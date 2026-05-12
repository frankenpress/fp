// Command fp is the FrankenPress designer-promotion CLI.
//
// fp wraps the snapshot/apply lifecycle implemented by frankenpress/mu-plugin's
// `wp fp` WP-CLI subcommands. It captures local site state into a portable
// snapshot directory under the site repo's web/imports/<slug>/ and, in later
// phases, applies snapshots back into a local stack for round-trip iteration.
//
// See https://github.com/frankenpress/fp and the design doc at
// frankenpress/.aidocs/fp-go-cli.md for the full surface.
package main

import (
	"os"

	"github.com/frankenpress/fp/internal/cli"
)

func main() {
	os.Exit(cli.NewRoot().Run(os.Args[1:]))
}

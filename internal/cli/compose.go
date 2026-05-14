// compose.go bundles the thin docker-compose verb wrappers
// (fp up / down / logs / restart). They share the same shape: flag
// parsing is disabled so docker compose's own flags pass through
// untouched, project name is resolved from frankenpress.toml's
// [snapshot] block (or basename(repoRoot)), and the exit code is
// forwarded so scripts can treat the wrappers like aliases.
//
// fp up is the only verb with auto-prepended flags: it always passes
// -d --wait so designers get the daily-driver "bring it up and gate
// on healthchecks" behaviour without typing the flags every time.
// The other three are pure passthroughs.

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/frankenpress/fp/internal/compose"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/spf13/cobra"
)

func newUpCmd() *cobra.Command {
	return newComposeVerbCmd(composeVerbSpec{
		verb: "up",
		// Always run detached with healthcheck-gating. Designers who
		// want a foreground stack can fall back to `docker compose up`.
		prepend: []string{"-d", "--wait"},
		short:   "Bring the local stack up (-d --wait)",
		long: `Wraps docker compose up with -d --wait pre-applied so the daily-
driver "bring it up, gate on healthchecks, return when ready" path
is one command. Any extra flags pass through to docker compose
untouched.

Examples:
  fp up                       # detached, wait for healthchecks
  fp up --build               # rebuild images first
  fp up --force-recreate
  fp up site                  # just one service

For a foreground stack (interactive logs), fall back to
docker compose up directly — fp up is the convenient default,
not a wrapper around every compose-up shape.`,
	})
}

func newDownCmd() *cobra.Command {
	return newComposeVerbCmd(composeVerbSpec{
		verb:  "down",
		short: "Tear the local stack down",
		long: `Pure passthrough to docker compose down. No auto-prepended flags;
designer passes -v to nuke volumes, --rmi to drop images, etc.

Examples:
  fp down
  fp down -v                  # also delete named volumes (full reset)
  fp down --rmi local`,
	})
}

func newLogsCmd() *cobra.Command {
	return newComposeVerbCmd(composeVerbSpec{
		verb:  "logs",
		short: "Print logs from the local stack",
		long: `Pure passthrough to docker compose logs. No auto-prepended flags;
designer passes -f to follow, --tail N to cap, etc.

Examples:
  fp logs                     # all services, existing logs, exit
  fp logs -f                  # follow
  fp logs -f site             # follow one service
  fp logs --tail 200 site`,
	})
}

func newRestartCmd() *cobra.Command {
	return newComposeVerbCmd(composeVerbSpec{
		verb:  "restart",
		short: "Restart services in the local stack",
		long: `Pure passthrough to docker compose restart. No auto-prepended flags.

Examples:
  fp restart                  # all services
  fp restart site             # just one service
  fp restart --no-deps site   # without restarting dependencies`,
	})
}

// composeVerbSpec captures the per-verb differences between the four
// wrappers. Everything else (flag parsing, project resolution, exit
// code forwarding) is identical and lives in newComposeVerbCmd.
type composeVerbSpec struct {
	verb    string
	prepend []string // flags auto-prepended after the verb (fp up uses this; others empty)
	short   string
	long    string
}

func newComposeVerbCmd(spec composeVerbSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:                spec.verb + " [args...]",
		Short:              spec.short,
		Long:               spec.long,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, a := range args {
				if a == "--help" || a == "-h" {
					return cmd.Help()
				}
			}

			cfg, err := config.Load("")
			if err != nil {
				if errors.Is(err, config.ErrRepoRootNotFound) {
					fmt.Fprintln(cmd.ErrOrStderr(), "error: not inside a FrankenPress site repo. fp expects a frankenpress.toml or a composer.json with a frankenpress/* dep at or above cwd")
					return exitCodeError{code: 1}
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				return exitCodeError{code: 1}
			}

			project := firstNonEmpty(cfg.Snapshot.Project, compose.DefaultProject(cfg.RepoRoot))
			full := append([]string{spec.verb}, spec.prepend...)
			full = append(full, args...)

			return runCompose(cmd.Context(), docker.NewReal(), project, full, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	return cmd
}

// runCompose verifies nothing — these wrappers exist to let docker
// compose's own error messages speak. It streams stdout/stderr and
// forwards the exit code via exitCodeError.
func runCompose(ctx context.Context, runner docker.Runner, project string, args []string, stdout, stderr io.Writer) error {
	execErr := runner.ComposeRun(ctx, project, args, stdout, stderr)
	if execErr == nil {
		return nil
	}
	var ee *docker.ExecError
	if errors.As(execErr, &ee) && ee.ExitCode > 0 {
		return exitCodeError{code: ee.ExitCode}
	}
	fmt.Fprintf(stderr, "error: %v\n", execErr)
	return exitCodeError{code: 1}
}

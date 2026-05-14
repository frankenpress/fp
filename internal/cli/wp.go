package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/frankenpress/fp/internal/compose"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/spf13/cobra"
)

func newWPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wp <args...>",
		Short: "Run wp-cli inside the running site container",
		Long: `Passthrough to wp-cli inside the running site container. Prefixes the
call with --allow-root --path=/app/web/wp so designers don't need to
remember either flag, and resolves project + service from
frankenpress.toml's [snapshot] block.

Stdout / stderr stream verbatim; wp-cli's exit code is forwarded so
scripts can treat fp wp like a thin alias.

Examples:
  fp wp option get blogname
  fp wp plugin list --status=active
  fp wp post list --format=json
  fp wp help post                # wp-cli's own help

To override service / project for a single invocation, prefix them
before any wp-cli arguments:
  fp wp --service custom -- post list
  fp wp --project=mysite-stack post list

The "--" separator is optional but encouraged — anything after it is
passed to wp-cli untouched, no matter what it looks like.`,
		Args:               cobra.MinimumNArgs(1),
		DisableFlagParsing: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, a := range args {
				if a == "--help" || a == "-h" {
					return cmd.Help()
				}
			}

			serviceOverride, projectOverride, wpArgs, err := parseWPFlags(args)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				return exitCodeError{code: 1}
			}
			if len(wpArgs) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "error: fp wp needs at least one wp-cli argument")
				return exitCodeError{code: 1}
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

			service := firstNonEmpty(serviceOverride, cfg.Snapshot.Service, "site")
			project := firstNonEmpty(projectOverride, cfg.Snapshot.Project, compose.DefaultProject(cfg.RepoRoot))

			return runWP(cmd.Context(), docker.NewReal(), project, service, wpArgs, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	return cmd
}

// runWP verifies the stack is up, then streams `wp <args...>` through
// docker compose exec. Extracted from newWPCmd so unit tests can drive
// it with a docker.Fake.
func runWP(ctx context.Context, runner docker.Runner, project, service string, args []string, stdout, stderr io.Writer) error {
	status, _, err := compose.Check(ctx, runner, project, service)
	if err != nil {
		fmt.Fprintf(stderr, "error: docker compose ps failed: %v\n", err)
		return exitCodeError{code: 1}
	}
	if status != compose.StatusServiceRunning {
		fmt.Fprintln(stderr, "error: "+compose.FormatNotRunning(status, project, service))
		return exitCodeError{code: 1}
	}

	wpArgs := append([]string{"wp", "--allow-root", "--path=/app/web/wp"}, args...)
	execErr := runner.ComposeExecStreaming(ctx, project, service, wpArgs, stdout, stderr)
	if execErr != nil {
		var ee *docker.ExecError
		if errors.As(execErr, &ee) && ee.ExitCode > 0 {
			return exitCodeError{code: ee.ExitCode}
		}
		fmt.Fprintf(stderr, "error: %v\n", execErr)
		return exitCodeError{code: 1}
	}
	return nil
}

// parseWPFlags consumes leading --service / --project options (in
// either "--key val" or "--key=val" form) from args, then returns
// everything else as the wp-cli command. An explicit "--" terminator
// stops the scan even if more flags would otherwise match.
func parseWPFlags(args []string) (service, project string, remainder []string, err error) {
	remainder = args
	for len(remainder) > 0 {
		arg := remainder[0]
		switch {
		case arg == "--":
			remainder = remainder[1:]
			return service, project, remainder, nil
		case arg == "--service":
			if len(remainder) < 2 {
				return "", "", nil, errors.New("--service needs a value")
			}
			service = remainder[1]
			remainder = remainder[2:]
		case strings.HasPrefix(arg, "--service="):
			service = strings.TrimPrefix(arg, "--service=")
			remainder = remainder[1:]
		case arg == "--project":
			if len(remainder) < 2 {
				return "", "", nil, errors.New("--project needs a value")
			}
			project = remainder[1]
			remainder = remainder[2:]
		case strings.HasPrefix(arg, "--project="):
			project = strings.TrimPrefix(arg, "--project=")
			remainder = remainder[1:]
		default:
			return service, project, remainder, nil
		}
	}
	return service, project, remainder, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

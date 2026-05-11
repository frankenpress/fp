// Package wpcli wraps docker-compose-based invocations of wp-cli
// running inside a designer's local FrankenPress site container.
//
// Phase 1 surface: just enough to run `wp fp snapshot` and stream its
// output to the user. Phase 2 expands this to parse JSON output, run
// apply on remote stacks via kubectl exec, etc.
package wpcli

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// Runner executes wp-cli commands against a running site container.
type Runner struct {
	// ComposeService is the docker-compose service name running the
	// site (e.g. "site" in the FrankenPress site-template). Defaults
	// to "site" when zero-valued.
	ComposeService string

	// WordPressPath is the in-container path to the WordPress install
	// (the --path argument to wp-cli). Defaults to "/app/web/wp" when
	// zero-valued; that's the Bedrock layout the FrankenPress runtime
	// image ships.
	WordPressPath string

	// WorkingDir is the host-side directory `docker compose exec` is
	// invoked from. Must contain a docker-compose.yml resolvable to
	// the site service. Defaults to the process cwd when empty.
	WorkingDir string
}

// Run executes a wp-cli command inside the site container with stdout
// and stderr forwarded to the supplied writers. Returns a wrapped
// error on non-zero exit; stderr content is included in the message
// for easier debugging from `fp <verb>` callers.
func (r Runner) Run(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	service := r.ComposeService
	if service == "" {
		service = "site"
	}
	wpPath := r.WordPressPath
	if wpPath == "" {
		wpPath = "/app/web/wp"
	}

	full := append(
		[]string{
			"compose", "exec", "-T", service,
			"wp", "--allow-root", "--path=" + wpPath,
		},
		args...,
	)

	cmd := exec.CommandContext(ctx, "docker", full...)
	if r.WorkingDir != "" {
		cmd.Dir = r.WorkingDir
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("wp %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// EnsureSiteIsUp returns nil when the site service appears healthy.
// Uses `docker compose ps --filter "status=running"` rather than
// hitting /healthz over HTTP because the latter would tie us to a
// specific port mapping (which sites may override).
func (r Runner) EnsureSiteIsUp(ctx context.Context) error {
	service := r.ComposeService
	if service == "" {
		service = "site"
	}

	cmd := exec.CommandContext(ctx, "docker", "compose", "ps", "--status", "running", "--services")
	if r.WorkingDir != "" {
		cmd.Dir = r.WorkingDir
	}
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("`docker compose ps` failed: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == service {
			return nil
		}
	}
	return fmt.Errorf("site service %q is not running; run `make up` first", service)
}

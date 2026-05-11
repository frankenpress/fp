// Package doctor performs pre-flight environment checks for fp.
//
// Each Check returns whether it passed and a one-line summary suitable
// for human display. Checks are pure data structures — no terminal
// formatting — so the cli/doctor.go wrapper can render them however
// it likes (tabular, JSON, etc.).
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Status is the result of a single environment check.
type Status int

const (
	// StatusOK means the check passed.
	StatusOK Status = iota
	// StatusWarn means the check found something the designer should
	// know about, but doesn't block subsequent fp commands. E.g. a
	// missing optional tool like awscli — promote() will fail, but
	// snapshot() works fine.
	StatusWarn
	// StatusFail means the check failed and the corresponding fp
	// subcommands will not work. The overall `fp doctor` exit code
	// reflects whether any StatusFail was emitted.
	StatusFail
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusWarn:
		return "warn"
	case StatusFail:
		return "fail"
	default:
		return "?"
	}
}

// Result captures the outcome of one check.
type Result struct {
	Name    string // Short, lowercase identifier (e.g. "docker", "docker compose plugin").
	Status  Status
	Summary string // One-line description shown to the user.
}

// Run executes every registered check and returns the results in
// declaration order. The context is propagated to each check for
// cancellation / timeout, but most checks complete in <100ms.
func Run(ctx context.Context) []Result {
	checks := []checkFn{
		checkCommand("docker", true, "needed to run the site stack locally"),
		checkDockerCompose,
		checkCommand("gh", true, "needed to open PRs for promote (Phase 2)"),
		checkCommand("git", true, "needed for snapshot manifest source refs"),
		checkCommand("aws", false, "only required for fp promote (Phase 2); snapshot works without it"),
		checkCommand("jq", false, "only used by the sts Makefile's make promote target"),
	}

	out := make([]Result, 0, len(checks))
	for _, c := range checks {
		out = append(out, c(ctx))
	}
	return out
}

// HasFailure reports whether any check returned StatusFail.
func HasFailure(results []Result) bool {
	for _, r := range results {
		if r.Status == StatusFail {
			return true
		}
	}
	return false
}

type checkFn func(context.Context) Result

// checkCommand returns a check that verifies a CLI is on the PATH.
// If required is true, missing → StatusFail; otherwise StatusWarn.
func checkCommand(name string, required bool, hint string) checkFn {
	return func(ctx context.Context) Result {
		path, err := exec.LookPath(name)
		if err != nil {
			status := StatusFail
			if !required {
				status = StatusWarn
			}
			return Result{
				Name:    name,
				Status:  status,
				Summary: fmt.Sprintf("not on PATH (%s)", hint),
			}
		}
		v := commandVersion(ctx, name)
		return Result{
			Name:    name,
			Status:  StatusOK,
			Summary: fmt.Sprintf("%s — %s", path, v),
		}
	}
}

// checkDockerCompose specifically validates the v2 docker-compose
// plugin (invoked as `docker compose`, not the deprecated standalone
// `docker-compose` binary). FrankenPress site Makefiles use the v2
// form throughout.
func checkDockerCompose(ctx context.Context) Result {
	cmd := exec.CommandContext(ctx, "docker", "compose", "version")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return Result{
				Name:    "docker compose",
				Status:  StatusFail,
				Summary: fmt.Sprintf("`docker compose version` failed: %s", strings.TrimSpace(string(exitErr.Stderr))),
			}
		}
		return Result{
			Name:    "docker compose",
			Status:  StatusFail,
			Summary: fmt.Sprintf("`docker compose version` could not run: %s", err.Error()),
		}
	}
	return Result{
		Name:    "docker compose",
		Status:  StatusOK,
		Summary: strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]),
	}
}

// commandVersion shells out to `<name> --version` for a one-line
// summary. Best-effort: returns "(version probe failed)" rather than
// erroring — the LookPath result already proved the binary exists.
func commandVersion(ctx context.Context, name string) string {
	cmd := exec.CommandContext(ctx, name, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "(version probe failed)"
	}
	first := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if first == "" {
		return "(version output empty)"
	}
	return first
}

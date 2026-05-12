// Package gh wraps the GitHub CLI for the operations fp release
// needs (PR creation + lookup). Same Runner pattern as docker / git
// — auth + remote discovery is whatever `gh` does locally, fp does
// not own any of it.
package gh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner abstracts the gh operations fp release needs.
type Runner interface {
	// PRCreate runs `gh pr create --title <t> --body <b>` and returns
	// the URL gh prints on success.
	PRCreate(ctx context.Context, repoRoot, title, body string) (url string, err error)
	// PRView looks up a PR for the named branch. Returns "" if no PR
	// exists (gh prints a "no pull requests found" error in that case
	// — we treat it as a soft signal, not a hard error).
	PRView(ctx context.Context, repoRoot, branch string) (url string, err error)
}

// ExecError carries stderr from a failed gh invocation.
type ExecError struct {
	Cmd      string
	Args     []string
	ExitCode int
	Stderr   []byte
}

func (e *ExecError) Error() string {
	cmd := e.Cmd
	if len(e.Args) > 0 {
		cmd = cmd + " " + strings.Join(e.Args, " ")
	}
	return fmt.Sprintf("%s exited %d", cmd, e.ExitCode)
}

type realRunner struct{}

// NewReal returns the production gh Runner.
func NewReal() Runner { return &realRunner{} }

func (r *realRunner) run(ctx context.Context, repoRoot string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = repoRoot
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if rerr := cmd.Run(); rerr != nil {
		ee := &ExecError{
			Cmd:    "gh " + strings.Join(args, " "),
			Stderr: errBuf.Bytes(),
		}
		if exitErr, ok := rerr.(*exec.ExitError); ok {
			ee.ExitCode = exitErr.ExitCode()
		} else {
			ee.ExitCode = -1
		}
		return out.Bytes(), errBuf.Bytes(), ee
	}
	return out.Bytes(), errBuf.Bytes(), nil
}

func (r *realRunner) PRCreate(ctx context.Context, repoRoot, title, body string) (string, error) {
	out, _, err := r.run(ctx, repoRoot, "pr", "create", "--title", title, "--body", body)
	if err != nil {
		return "", err
	}
	// gh prints the URL as the last non-empty line of stdout.
	return lastNonEmptyLine(out), nil
}

func (r *realRunner) PRView(ctx context.Context, repoRoot, branch string) (string, error) {
	out, _, err := r.run(ctx, repoRoot, "pr", "view", branch, "--json", "url", "--jq", ".url")
	if err != nil {
		// gh exits non-zero when no PR is found. Treat as no-PR rather
		// than a hard error; callers fall back to a manual hint.
		var ee *ExecError
		if errors.As(err, &ee) && bytes.Contains(ee.Stderr, []byte("no pull requests found")) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func lastNonEmptyLine(b []byte) string {
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l != "" {
			return l
		}
	}
	return ""
}

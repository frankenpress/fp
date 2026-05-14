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
	"regexp"
	"strings"
)

// Runner abstracts the gh operations fp release + fp doctor need.
type Runner interface {
	// PRCreate runs `gh pr create --title <t> --body <b>` and returns
	// the URL gh prints on success.
	PRCreate(ctx context.Context, repoRoot, title, body string) (url string, err error)
	// PRView looks up a PR for the named branch. Returns "" if no PR
	// exists (gh prints a "no pull requests found" error in that case
	// — we treat it as a soft signal, not a hard error).
	PRView(ctx context.Context, repoRoot, branch string) (url string, err error)
	// AuthStatus reports whether the user has an authenticated gh
	// session. loggedIn is the headline signal; summary is a one-line
	// human-readable form for `fp doctor` output ("user@host" when
	// the parse succeeds, "logged in" / "not logged in" as fallback).
	// A non-nil error only fires when the gh CLI itself is missing
	// or unreachable — "not logged in" is loggedIn=false + nil err.
	AuthStatus(ctx context.Context) (loggedIn bool, summary string, err error)
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

func (r *realRunner) AuthStatus(ctx context.Context) (bool, string, error) {
	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	rerr := cmd.Run()
	// gh writes the status block to stderr in most versions; merge.
	combined := append(out.Bytes(), errBuf.Bytes()...)
	if rerr != nil {
		if _, ok := rerr.(*exec.ExitError); ok {
			// Non-zero exit = not logged in to any host. Not an error
			// the doctor surface — surface it as the signal.
			return false, "not logged in", nil
		}
		// gh missing from PATH / fork failure — real error.
		return false, "", rerr
	}
	if summary := parseAuthStatus(combined); summary != "" {
		return true, summary, nil
	}
	return true, "logged in", nil
}

// authStatusRE matches gh's "Logged in to <host> account <user>" line
// across versions. Tolerant of the leading bullet / checkmark.
var authStatusRE = regexp.MustCompile(`Logged in to (\S+) (?:as|account) (\S+)`)

func parseAuthStatus(b []byte) string {
	m := authStatusRE.FindStringSubmatch(string(b))
	if len(m) != 3 {
		return ""
	}
	return m[2] + "@" + m[1]
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

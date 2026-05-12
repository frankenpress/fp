// Package git is the seam between fp and the user's git CLI.
//
// fp release shells out to `git` for branch + add + commit + push.
// All git invocations route through the Runner interface so tests can
// substitute a recording fake.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Runner abstracts the git operations fp release needs.
type Runner interface {
	CurrentBranch(ctx context.Context, repoRoot string) (string, error)
	BranchExists(ctx context.Context, repoRoot, branch string) (bool, error)
	Checkout(ctx context.Context, repoRoot, branch string, create bool) error
	Add(ctx context.Context, repoRoot string, paths []string) error
	Commit(ctx context.Context, repoRoot, message string) error
	Push(ctx context.Context, repoRoot, remote, branch string, setUpstream bool) error
}

// ExecError is returned when a git command exits non-zero. It carries
// stderr so callers (the release orchestrator) can surface it.
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

// NewReal returns the production git Runner.
func NewReal() Runner { return &realRunner{} }

func (r *realRunner) run(ctx context.Context, repoRoot string, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoRoot
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if rerr := cmd.Run(); rerr != nil {
		ee := &ExecError{
			Cmd:    "git " + strings.Join(args, " "),
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

func (r *realRunner) CurrentBranch(ctx context.Context, repoRoot string) (string, error) {
	out, _, err := r.run(ctx, repoRoot, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *realRunner) BranchExists(ctx context.Context, repoRoot, branch string) (bool, error) {
	_, _, err := r.run(ctx, repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	var ee *ExecError
	if errors.As(err, &ee) && ee.ExitCode == 1 {
		// rev-parse --verify --quiet exits 1 when the ref is missing.
		return false, nil
	}
	return false, err
}

func (r *realRunner) Checkout(ctx context.Context, repoRoot, branch string, create bool) error {
	args := []string{"checkout"}
	if create {
		args = append(args, "-b")
	}
	args = append(args, branch)
	_, _, err := r.run(ctx, repoRoot, args...)
	return err
}

func (r *realRunner) Add(ctx context.Context, repoRoot string, paths []string) error {
	if len(paths) == 0 {
		return errors.New("git.Add: no paths")
	}
	args := append([]string{"add", "--"}, paths...)
	_, _, err := r.run(ctx, repoRoot, args...)
	return err
}

func (r *realRunner) Commit(ctx context.Context, repoRoot, message string) error {
	if strings.TrimSpace(message) == "" {
		return errors.New("git.Commit: empty message")
	}
	_, _, err := r.run(ctx, repoRoot, "commit", "-m", message)
	return err
}

func (r *realRunner) Push(ctx context.Context, repoRoot, remote, branch string, setUpstream bool) error {
	args := []string{"push"}
	if setUpstream {
		args = append(args, "-u")
	}
	args = append(args, remote, branch)
	_, _, err := r.run(ctx, repoRoot, args...)
	return err
}

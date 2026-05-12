// Package docker is the seam between fp and the user's docker CLI.
//
// fp never links the Docker SDK; it shells out to `docker compose` and
// `docker cp` exactly the way the designer would in their terminal.
// That keeps auth (DOCKER_HOST, contexts, credential helpers, rootless,
// colima, orbstack) entirely the user's problem and out of our code.
//
// All docker invocations route through the Runner interface so tests
// can substitute a recording fake (see fake.go). The real impl is
// exec.Command-based and intentionally thin.
package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner abstracts the docker / docker-compose calls fp needs. Each
// method's contract is documented inline; implementations must not
// mutate the slices they're handed.
type Runner interface {
	// ComposeExec runs `docker compose --project-name <project>
	// exec <service> <args...>` and returns its stdout + stderr.
	// Exit status non-zero produces a non-nil error AND populated
	// stdout/stderr — callers (the snapshot orchestrator) need the
	// stderr verbatim for the Error-UX (b) and (d) cases.
	ComposeExec(ctx context.Context, project, service string, args []string) (stdout, stderr []byte, err error)

	// ComposeExecStreaming runs the same command but pipes the
	// container's stdout/stderr directly to the caller's writers
	// instead of buffering. Used for `wp fp snapshot` so designers
	// see WP-CLI log output line-by-line as it happens.
	ComposeExecStreaming(ctx context.Context, project, service string, args []string, stdout, stderr io.Writer) error

	// Copy wraps `docker cp <src> <dst>`. Either side may be
	// "<container>:<path>" — the docker CLI handles direction.
	Copy(ctx context.Context, src, dst string) error

	// PS lists containers in the named compose project. Returns the
	// running + stopped containers so the snapshot orchestrator can
	// distinguish "stack is down" from "wrong project name".
	PS(ctx context.Context, project string) ([]Container, error)
}

// Container is a single entry from `docker compose ps --format json`.
// We pull the minimal subset fp needs; future fields can be added
// without breaking callers.
type Container struct {
	Name    string `json:"Name"`
	Service string `json:"Service"`
	State   string `json:"State"`
	Status  string `json:"Status"`
}

// ExecError is returned by ComposeExec / Copy when the underlying
// command exits non-zero. It carries the stderr text so callers can
// surface it verbatim in the designer's terminal.
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

// realRunner shells out to the user's docker CLI.
type realRunner struct{}

// NewReal returns the production Runner. There is no constructor
// argument — auth + endpoint discovery is whatever the user's docker
// CLI does, by design.
func NewReal() Runner { return &realRunner{} }

func (r *realRunner) ComposeExec(ctx context.Context, project, service string, args []string) ([]byte, []byte, error) {
	full := composeExecArgs(project, service, args)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Env = os.Environ()
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if err != nil {
		ee := &ExecError{
			Cmd:    "docker " + strings.Join(full, " "),
			Stderr: errBuf.Bytes(),
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			ee.ExitCode = exitErr.ExitCode()
		} else {
			ee.ExitCode = -1
		}
		return outBuf.Bytes(), errBuf.Bytes(), ee
	}
	return outBuf.Bytes(), errBuf.Bytes(), nil
}

func (r *realRunner) ComposeExecStreaming(ctx context.Context, project, service string, args []string, stdout, stderr io.Writer) error {
	full := composeExecArgs(project, service, args)
	cmd := exec.CommandContext(ctx, "docker", full...)
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExecError{
				Cmd:      "docker " + strings.Join(full, " "),
				ExitCode: exitErr.ExitCode(),
			}
		}
		return err
	}
	return nil
}

func (r *realRunner) Copy(ctx context.Context, src, dst string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", src, dst)
	cmd.Env = os.Environ()
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		ee := &ExecError{
			Cmd:    fmt.Sprintf("docker cp %s %s", src, dst),
			Stderr: errBuf.Bytes(),
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			ee.ExitCode = exitErr.ExitCode()
		} else {
			ee.ExitCode = -1
		}
		return ee
	}
	return nil
}

func (r *realRunner) PS(ctx context.Context, project string) ([]Container, error) {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "--project-name", project)
	}
	args = append(args, "ps", "--all", "--format", "json")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		ee := &ExecError{
			Cmd:    "docker " + strings.Join(args, " "),
			Stderr: errBuf.Bytes(),
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			ee.ExitCode = exitErr.ExitCode()
		} else {
			ee.ExitCode = -1
		}
		return nil, ee
	}
	return parsePS(outBuf.Bytes())
}

func composeExecArgs(project, service string, args []string) []string {
	full := []string{"compose"}
	if project != "" {
		full = append(full, "--project-name", project)
	}
	full = append(full, "exec", "-T", service)
	full = append(full, args...)
	return full
}

// parsePS handles both docker compose's "newline-delimited JSON
// objects" and the "single JSON array" formats. v2.20+ ships NDJSON;
// older v2 versions ship the array form.
func parsePS(data []byte) ([]Container, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var out []Container
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return nil, fmt.Errorf("docker: parse ps array: %w", err)
		}
		return out, nil
	}
	var out []Container
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	for {
		var c Container
		if err := dec.Decode(&c); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("docker: parse ps ndjson: %w", err)
		}
		out = append(out, c)
	}
	return out, nil
}

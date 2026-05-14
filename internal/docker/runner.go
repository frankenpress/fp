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

	// ComposeUp runs `docker compose --project-name <project>
	// up -d --wait`. Streams build output, container creation, and
	// healthcheck-wait progress to the caller's writers so designers
	// see the stack come alive in real time. The --wait flag gates
	// on every service's healthcheck (or readiness, when no
	// healthcheck is defined) before returning. Used by `fp init`
	// to bring a fresh stack from cold to ready in one command.
	ComposeUp(ctx context.Context, project string, stdout, stderr io.Writer) error

	// ComposeBuild runs `docker compose --project-name <project>
	// build <service>`. Streams the BuildKit output to the caller's
	// writers (builds are verbose; designers expect to see layer
	// progress). Used by `fp init` when the site image hasn't been
	// built yet for the local stack.
	ComposeBuild(ctx context.Context, project, service string, stdout, stderr io.Writer) error

	// ComposerInstall runs `docker run --rm -v <repoRoot>:/app
	// -w /app composer:2 install --prefer-dist --no-interaction
	// --no-progress`. Mirrors site-template's Makefile `setup`
	// target — composer is invoked in a container so designers
	// don't need PHP / composer on the host. repoRoot is mounted
	// at /app; vendor/ lands on the host filesystem.
	ComposerInstall(ctx context.Context, repoRoot string, stdout, stderr io.Writer) error

	// ComposeVersion runs `docker compose version --short` and
	// returns the trimmed version string (e.g. "v2.31.0"). Used
	// purely as diagnostic context by `fp doctor`. A non-nil error
	// almost always means the docker CLI isn't installed or isn't
	// on PATH.
	ComposeVersion(ctx context.Context) (string, error)

	// ComposeRun runs `docker compose --project-name <project>
	// <args...>` with the caller's stdout/stderr piped through.
	// Generic shape for the thin compose-verb wrappers (fp up /
	// down / logs / restart) that pass user flags through to
	// docker compose untouched. Non-zero exit produces an
	// *ExecError so callers can forward the exit code.
	ComposeRun(ctx context.Context, project string, args []string, stdout, stderr io.Writer) error
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

func (r *realRunner) ComposeUp(ctx context.Context, project string, stdout, stderr io.Writer) error {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "--project-name", project)
	}
	args = append(args, "up", "-d", "--wait")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExecError{
				Cmd:      "docker " + strings.Join(args, " "),
				ExitCode: exitErr.ExitCode(),
			}
		}
		return err
	}
	return nil
}

func (r *realRunner) ComposeBuild(ctx context.Context, project, service string, stdout, stderr io.Writer) error {
	args := []string{"compose"}
	if project != "" {
		args = append(args, "--project-name", project)
	}
	args = append(args, "build", service)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExecError{
				Cmd:      "docker " + strings.Join(args, " "),
				ExitCode: exitErr.ExitCode(),
			}
		}
		return err
	}
	return nil
}

func (r *realRunner) ComposerInstall(ctx context.Context, repoRoot string, stdout, stderr io.Writer) error {
	// Matches site-template's Makefile setup target verbatim so
	// designers see the same behaviour whether they `make setup` or
	// `fp init`.
	args := []string{
		"run", "--rm",
		"-v", repoRoot + ":/app",
		"-w", "/app",
		"composer:2",
		"install", "--prefer-dist", "--no-interaction", "--no-progress",
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = os.Environ()
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return &ExecError{
				Cmd:      "docker " + strings.Join(args, " "),
				ExitCode: exitErr.ExitCode(),
			}
		}
		return err
	}
	return nil
}

func (r *realRunner) ComposeRun(ctx context.Context, project string, args []string, stdout, stderr io.Writer) error {
	full := []string{"compose"}
	if project != "" {
		full = append(full, "--project-name", project)
	}
	full = append(full, args...)
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

func (r *realRunner) ComposeVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "version", "--short")
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", &ExecError{
				Cmd:      "docker compose version --short",
				ExitCode: exitErr.ExitCode(),
				Stderr:   exitErr.Stderr,
			}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
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

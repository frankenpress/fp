package docker

import (
	"context"
	"errors"
	"io"
)

// Fake is a recording Runner for unit tests. Configure canned responses
// on the *Result fields; every method call appends to Calls so tests
// can assert what fp actually invoked.
//
// Field design: each method has both a "default" response (used when
// no matcher fits) and an optional Func override. The Func form is
// the escape hatch when a test needs branching behaviour.
type Fake struct {
	Calls []Call

	// Default responses.
	ComposeExecStdout []byte
	ComposeExecStderr []byte
	ComposeExecErr    error

	StreamingStdout []byte
	StreamingStderr []byte
	StreamingErr    error

	CopyErr error

	PSContainers []Container
	PSErr        error

	ComposeUpErr       error
	ComposeBuildErr    error
	ComposerInstallErr error

	// Optional per-method overrides.
	ComposeExecFunc     func(ctx context.Context, project, service string, args []string) (stdout, stderr []byte, err error)
	StreamingFunc       func(ctx context.Context, project, service string, args []string, stdout, stderr io.Writer) error
	CopyFunc            func(ctx context.Context, src, dst string) error
	PSFunc              func(ctx context.Context, project string) ([]Container, error)
	ComposeUpFunc       func(ctx context.Context, project string, stdout, stderr io.Writer) error
	ComposeBuildFunc    func(ctx context.Context, project, service string, stdout, stderr io.Writer) error
	ComposerInstallFunc func(ctx context.Context, repoRoot string, stdout, stderr io.Writer) error
}

// Call records a single Runner invocation.
type Call struct {
	Method   string
	Project  string
	Service  string
	Args     []string
	Src      string
	Dst      string
	RepoRoot string // populated for ComposerInstall
}

// NewFake returns a Fake with empty Calls (so tests don't have to nil-check).
func NewFake() *Fake { return &Fake{} }

func (f *Fake) ComposeExec(ctx context.Context, project, service string, args []string) ([]byte, []byte, error) {
	f.Calls = append(f.Calls, Call{
		Method:  "ComposeExec",
		Project: project,
		Service: service,
		Args:    append([]string(nil), args...),
	})
	if f.ComposeExecFunc != nil {
		return f.ComposeExecFunc(ctx, project, service, args)
	}
	return f.ComposeExecStdout, f.ComposeExecStderr, f.ComposeExecErr
}

func (f *Fake) ComposeExecStreaming(ctx context.Context, project, service string, args []string, stdout, stderr io.Writer) error {
	f.Calls = append(f.Calls, Call{
		Method:  "ComposeExecStreaming",
		Project: project,
		Service: service,
		Args:    append([]string(nil), args...),
	})
	if f.StreamingFunc != nil {
		return f.StreamingFunc(ctx, project, service, args, stdout, stderr)
	}
	if len(f.StreamingStdout) > 0 && stdout != nil {
		_, _ = stdout.Write(f.StreamingStdout)
	}
	if len(f.StreamingStderr) > 0 && stderr != nil {
		_, _ = stderr.Write(f.StreamingStderr)
	}
	return f.StreamingErr
}

func (f *Fake) Copy(ctx context.Context, src, dst string) error {
	f.Calls = append(f.Calls, Call{
		Method: "Copy",
		Src:    src,
		Dst:    dst,
	})
	if f.CopyFunc != nil {
		return f.CopyFunc(ctx, src, dst)
	}
	return f.CopyErr
}

func (f *Fake) PS(ctx context.Context, project string) ([]Container, error) {
	f.Calls = append(f.Calls, Call{
		Method:  "PS",
		Project: project,
	})
	if f.PSFunc != nil {
		return f.PSFunc(ctx, project)
	}
	return f.PSContainers, f.PSErr
}

func (f *Fake) ComposeUp(ctx context.Context, project string, stdout, stderr io.Writer) error {
	f.Calls = append(f.Calls, Call{
		Method:  "ComposeUp",
		Project: project,
	})
	if f.ComposeUpFunc != nil {
		return f.ComposeUpFunc(ctx, project, stdout, stderr)
	}
	return f.ComposeUpErr
}

func (f *Fake) ComposeBuild(ctx context.Context, project, service string, stdout, stderr io.Writer) error {
	f.Calls = append(f.Calls, Call{
		Method:  "ComposeBuild",
		Project: project,
		Service: service,
	})
	if f.ComposeBuildFunc != nil {
		return f.ComposeBuildFunc(ctx, project, service, stdout, stderr)
	}
	return f.ComposeBuildErr
}

func (f *Fake) ComposerInstall(ctx context.Context, repoRoot string, stdout, stderr io.Writer) error {
	f.Calls = append(f.Calls, Call{
		Method:   "ComposerInstall",
		RepoRoot: repoRoot,
	})
	if f.ComposerInstallFunc != nil {
		return f.ComposerInstallFunc(ctx, repoRoot, stdout, stderr)
	}
	return f.ComposerInstallErr
}

// CallCount returns how many times the named method was invoked.
// Convenience helper so tests don't write loops everywhere.
func (f *Fake) CallCount(method string) int {
	n := 0
	for _, c := range f.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// ErrFake is a sentinel test runners can use when they want a generic
// "this would have failed in production" error without crafting one.
var ErrFake = errors.New("fake docker error")

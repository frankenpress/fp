package git

import (
	"context"
	"errors"
	"fmt"
)

// Fake is a recording Runner for tests. Configure canned responses
// on the *Result / *Err fields; every method invocation appends to
// Calls so tests can assert sequencing.
type Fake struct {
	Calls []Call

	Branch             string // returned by CurrentBranch
	CurrentBranchErr   error
	ExistingBranches   map[string]bool
	BranchExistsErrFor map[string]error
	CheckoutErrFor     map[string]error // keyed by branch
	AddErr             error
	CommitErr          error
	PushErr            error
}

// Call records a single Runner invocation.
type Call struct {
	Method   string
	Branch   string
	Paths    []string
	Message  string
	Remote   string
	Create   bool
	Upstream bool
}

// NewFake returns an initialised Fake.
func NewFake() *Fake {
	return &Fake{
		ExistingBranches:   map[string]bool{},
		BranchExistsErrFor: map[string]error{},
		CheckoutErrFor:     map[string]error{},
	}
}

func (f *Fake) CurrentBranch(_ context.Context, _ string) (string, error) {
	f.Calls = append(f.Calls, Call{Method: "CurrentBranch"})
	return f.Branch, f.CurrentBranchErr
}

func (f *Fake) BranchExists(_ context.Context, _, branch string) (bool, error) {
	f.Calls = append(f.Calls, Call{Method: "BranchExists", Branch: branch})
	if err, ok := f.BranchExistsErrFor[branch]; ok {
		return false, err
	}
	return f.ExistingBranches[branch], nil
}

func (f *Fake) Checkout(_ context.Context, _, branch string, create bool) error {
	f.Calls = append(f.Calls, Call{Method: "Checkout", Branch: branch, Create: create})
	if err, ok := f.CheckoutErrFor[branch]; ok {
		return err
	}
	// Switching branches updates Branch state.
	f.Branch = branch
	if create {
		f.ExistingBranches[branch] = true
	}
	return nil
}

func (f *Fake) Add(_ context.Context, _ string, paths []string) error {
	f.Calls = append(f.Calls, Call{Method: "Add", Paths: append([]string(nil), paths...)})
	return f.AddErr
}

func (f *Fake) Commit(_ context.Context, _, message string) error {
	f.Calls = append(f.Calls, Call{Method: "Commit", Message: message})
	return f.CommitErr
}

func (f *Fake) Push(_ context.Context, _, remote, branch string, setUpstream bool) error {
	f.Calls = append(f.Calls, Call{Method: "Push", Remote: remote, Branch: branch, Upstream: setUpstream})
	return f.PushErr
}

// CallCount returns how many times the named method was invoked.
func (f *Fake) CallCount(method string) int {
	n := 0
	for _, c := range f.Calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// ErrFake is a sentinel for tests that want a generic git failure.
var ErrFake = errors.New("fake git error")

// MakeExecError fakes a real-impl-shaped error for testing the error path.
func MakeExecError(args []string, exitCode int, stderr string) error {
	return &ExecError{
		Cmd:      "git " + fmt.Sprintf("%v", args),
		ExitCode: exitCode,
		Stderr:   []byte(stderr),
	}
}

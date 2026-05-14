package gh

import "context"

// Fake is a recording gh Runner for tests.
type Fake struct {
	Calls []Call

	PRCreateURL string
	PRCreateErr error

	PRViewURL string
	PRViewErr error

	AuthLoggedIn bool
	AuthSummary  string
	AuthErr      error
}

// Call records a single Runner invocation.
type Call struct {
	Method string
	Title  string
	Body   string
	Branch string
}

// NewFake returns an empty Fake.
func NewFake() *Fake { return &Fake{} }

func (f *Fake) PRCreate(_ context.Context, _, title, body string) (string, error) {
	f.Calls = append(f.Calls, Call{Method: "PRCreate", Title: title, Body: body})
	return f.PRCreateURL, f.PRCreateErr
}

func (f *Fake) PRView(_ context.Context, _, branch string) (string, error) {
	f.Calls = append(f.Calls, Call{Method: "PRView", Branch: branch})
	return f.PRViewURL, f.PRViewErr
}

func (f *Fake) AuthStatus(_ context.Context) (bool, string, error) {
	f.Calls = append(f.Calls, Call{Method: "AuthStatus"})
	return f.AuthLoggedIn, f.AuthSummary, f.AuthErr
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

package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/frankenpress/fp/internal/docker"
)

func TestParseWPFlags(t *testing.T) {
	cases := []struct {
		name             string
		in               []string
		wantService      string
		wantProject      string
		wantRemainder    []string
		wantErrSubstring string
	}{
		{
			name:          "no flags",
			in:            []string{"option", "get", "blogname"},
			wantRemainder: []string{"option", "get", "blogname"},
		},
		{
			name:          "service two-arg form",
			in:            []string{"--service", "custom", "post", "list"},
			wantService:   "custom",
			wantRemainder: []string{"post", "list"},
		},
		{
			name:          "service equals form",
			in:            []string{"--service=custom", "post", "list"},
			wantService:   "custom",
			wantRemainder: []string{"post", "list"},
		},
		{
			name:          "project two-arg form",
			in:            []string{"--project", "mysite", "post", "list"},
			wantProject:   "mysite",
			wantRemainder: []string{"post", "list"},
		},
		{
			name:          "both overrides",
			in:            []string{"--service=custom", "--project=mysite", "post", "list"},
			wantService:   "custom",
			wantProject:   "mysite",
			wantRemainder: []string{"post", "list"},
		},
		{
			name:          "double-dash terminator",
			in:            []string{"--service", "custom", "--", "--service", "X"},
			wantService:   "custom",
			wantRemainder: []string{"--service", "X"},
		},
		{
			name:          "trailing flag-shaped wp arg is left alone",
			in:            []string{"post", "list", "--format=json"},
			wantRemainder: []string{"post", "list", "--format=json"},
		},
		{
			name:             "service missing value",
			in:               []string{"--service"},
			wantErrSubstring: "--service needs a value",
		},
		{
			name:             "project missing value",
			in:               []string{"--project"},
			wantErrSubstring: "--project needs a value",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, proj, rem, err := parseWPFlags(c.in)
			if c.wantErrSubstring != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErrSubstring) {
					t.Errorf("err = %v, want substring %q", err, c.wantErrSubstring)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if svc != c.wantService {
				t.Errorf("service = %q, want %q", svc, c.wantService)
			}
			if proj != c.wantProject {
				t.Errorf("project = %q, want %q", proj, c.wantProject)
			}
			if !reflect.DeepEqual(rem, c.wantRemainder) {
				t.Errorf("remainder = %v, want %v", rem, c.wantRemainder)
			}
		})
	}
}

func TestRunWP_HappyPath_StreamsThroughComposeExec(t *testing.T) {
	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "fp-site-1", Service: "site", State: "running"},
	}

	var stdout, stderr bytes.Buffer
	err := runWP(context.Background(), fake, "fp", "site", []string{"option", "get", "blogname"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runWP: %v", err)
	}

	// Verify the streaming call was made with the standard wp-cli prefix.
	var streaming *docker.Call
	for i := range fake.Calls {
		if fake.Calls[i].Method == "ComposeExecStreaming" {
			streaming = &fake.Calls[i]
			break
		}
	}
	if streaming == nil {
		t.Fatalf("expected one ComposeExecStreaming call, got %+v", fake.Calls)
	}
	wantArgs := []string{"wp", "--allow-root", "--path=/app/web/wp", "option", "get", "blogname"}
	if !reflect.DeepEqual(streaming.Args, wantArgs) {
		t.Errorf("streaming args = %v, want %v", streaming.Args, wantArgs)
	}
	if streaming.Project != "fp" || streaming.Service != "site" {
		t.Errorf("project/service = %q/%q, want fp/site", streaming.Project, streaming.Service)
	}
}

func TestRunWP_StackDown_ReturnsExitCode1AndFriendlyError(t *testing.T) {
	fake := docker.NewFake()
	// PSContainers empty → StatusProjectMissing.

	var stdout, stderr bytes.Buffer
	err := runWP(context.Background(), fake, "fp", "site", []string{"option", "get", "blogname"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when stack is down")
	}
	ec, ok := err.(exitCodeError)
	if !ok {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if ec.code != 1 {
		t.Errorf("exit code = %d, want 1", ec.code)
	}
	if !strings.Contains(stderr.String(), "no docker-compose project named") {
		t.Errorf("stderr missing stack-down hint:\n%s", stderr.String())
	}
}

func TestRunWP_WPExitCodePassedThrough(t *testing.T) {
	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "fp-site-1", Service: "site", State: "running"},
	}
	fake.StreamingErr = &docker.ExecError{
		Cmd:      "docker compose exec ...",
		ExitCode: 7,
	}

	var stdout, stderr bytes.Buffer
	err := runWP(context.Background(), fake, "fp", "site", []string{"option", "get", "doesnotexist"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error when wp exits non-zero")
	}
	ec, ok := err.(exitCodeError)
	if !ok {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if ec.code != 7 {
		t.Errorf("exit code = %d, want 7 (mirroring wp-cli's exit)", ec.code)
	}
}

func TestRunWP_NonExecErrorSurfacesAsExit1(t *testing.T) {
	fake := docker.NewFake()
	fake.PSContainers = []docker.Container{
		{Name: "fp-site-1", Service: "site", State: "running"},
	}
	fake.StreamingErr = errors.New("fork: out of pids")

	var stdout, stderr bytes.Buffer
	err := runWP(context.Background(), fake, "fp", "site", []string{"option", "get", "blogname"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	ec, ok := err.(exitCodeError)
	if !ok {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if ec.code != 1 {
		t.Errorf("exit code = %d, want 1 for non-exec errors", ec.code)
	}
	if !strings.Contains(stderr.String(), "fork: out of pids") {
		t.Errorf("stderr missing underlying error:\n%s", stderr.String())
	}
}

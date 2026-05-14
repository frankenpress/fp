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

func TestRunCompose_HappyPath_DelegatesToRunner(t *testing.T) {
	fake := docker.NewFake()

	var stdout, stderr bytes.Buffer
	err := runCompose(context.Background(), fake, "fp", []string{"up", "-d", "--wait"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("runCompose: %v", err)
	}

	if got := fake.CallCount("ComposeRun"); got != 1 {
		t.Errorf("ComposeRun call count = %d, want 1", got)
	}
	call := fake.Calls[0]
	if call.Project != "fp" {
		t.Errorf("project = %q, want fp", call.Project)
	}
	wantArgs := []string{"up", "-d", "--wait"}
	if !reflect.DeepEqual(call.Args, wantArgs) {
		t.Errorf("args = %v, want %v", call.Args, wantArgs)
	}
}

func TestRunCompose_NonZeroExitForwarded(t *testing.T) {
	fake := docker.NewFake()
	fake.ComposeRunErr = &docker.ExecError{
		Cmd:      "docker compose ...",
		ExitCode: 17,
	}

	var stdout, stderr bytes.Buffer
	err := runCompose(context.Background(), fake, "fp", []string{"up"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	ec, ok := err.(exitCodeError)
	if !ok {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if ec.code != 17 {
		t.Errorf("exit code = %d, want 17 (forwarded from docker compose)", ec.code)
	}
}

func TestRunCompose_GenericErrorAsExit1(t *testing.T) {
	fake := docker.NewFake()
	fake.ComposeRunErr = errors.New("docker daemon not reachable")

	var stdout, stderr bytes.Buffer
	err := runCompose(context.Background(), fake, "fp", []string{"down"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected error")
	}
	ec, ok := err.(exitCodeError)
	if !ok {
		t.Fatalf("expected exitCodeError, got %T: %v", err, err)
	}
	if ec.code != 1 {
		t.Errorf("exit code = %d, want 1", ec.code)
	}
	if !strings.Contains(stderr.String(), "docker daemon not reachable") {
		t.Errorf("stderr missing underlying error:\n%s", stderr.String())
	}
}

func TestNewUpCmd_AutoPrependsDetachedAndWait(t *testing.T) {
	// The up verb's spec.prepend is the only verb-specific behaviour
	// worth a direct check — guard against an accidental drop.
	cmd := newUpCmd()
	if cmd.Use != "up [args...]" {
		t.Errorf("up.Use = %q", cmd.Use)
	}
	if !strings.Contains(cmd.Long, "-d --wait") {
		t.Errorf("up.Long should mention the auto-prepended -d --wait flags:\n%s", cmd.Long)
	}
}

func TestComposeVerbSpec_VerbsAreDistinct(t *testing.T) {
	// Cheap structural check that the four constructors return four
	// different cobra commands with the expected verbs. Catches
	// accidental copy-paste between the constructors.
	cases := map[string]*struct {
		cmd  func() any
		verb string
	}{
		"up":      {cmd: func() any { return newUpCmd() }, verb: "up"},
		"down":    {cmd: func() any { return newDownCmd() }, verb: "down"},
		"logs":    {cmd: func() any { return newLogsCmd() }, verb: "logs"},
		"restart": {cmd: func() any { return newRestartCmd() }, verb: "restart"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got := c.cmd()
			use := got.(interface{ UseLine() string }).UseLine()
			if !strings.HasPrefix(use, c.verb+" ") {
				t.Errorf("UseLine = %q, want prefix %q", use, c.verb+" ")
			}
		})
	}
}

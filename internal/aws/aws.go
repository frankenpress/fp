// Package aws is the seam between fp and the user's aws CLI.
//
// fp pull shells out to `aws s3` for listing snapshot prefixes and
// syncing them down. All aws invocations route through the Runner
// interface so tests can substitute a recording fake.
//
// fp does not link the AWS SDK by design — credential discovery,
// profile resolution, and region defaults are the user's aws CLI's
// job (typically via `aws-vault exec`, `AWS_PROFILE`, or
// `~/.aws/credentials`). Same shape as docker/git/gh.
package aws

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Runner abstracts the aws-s3 operations fp pull needs.
type Runner interface {
	// ListSnapshotPrefixes returns the slugs (without trailing slash)
	// of every "directory" under s3://bucket/. The aws s3 ls output
	// shape is "                           PRE <slug>/" per line for
	// common-prefix entries.
	//
	// profile and region are passed verbatim to `aws --profile X
	// --region Y`; empty strings omit the flags so aws's own
	// resolution (env, ~/.aws/credentials, etc.) wins.
	//
	// Returns a slice sorted ascending (lex == chronological because
	// snapshot slugs are ISO-8601 UTC timestamps like
	// "prod-2026-05-16T00-00-00Z").
	ListSnapshotPrefixes(ctx context.Context, bucket, profile, region string) ([]string, error)

	// SyncDown runs `aws s3 sync s3://bucket/slug/ destDir/` to mirror
	// a prefix locally. destDir is created by aws s3 sync if missing.
	// --delete is NOT passed — fp's caller has already decided whether
	// destDir is fresh.
	SyncDown(ctx context.Context, bucket, slug, destDir, profile, region string) error
}

// ExecError is returned when an aws command exits non-zero. Carries
// stderr so callers can surface the underlying message.
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

// NewReal returns the production aws Runner.
func NewReal() Runner { return &realRunner{} }

func (r *realRunner) run(ctx context.Context, args ...string) (stdout, stderr []byte, err error) {
	cmd := exec.CommandContext(ctx, "aws", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if rerr := cmd.Run(); rerr != nil {
		ee := &ExecError{
			Cmd:    "aws " + strings.Join(args, " "),
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

// commonArgs prepends `--profile X --region Y` when set. aws's own
// resolution takes over when either is empty.
func commonArgs(profile, region string) []string {
	args := []string{}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	return args
}

func (r *realRunner) ListSnapshotPrefixes(ctx context.Context, bucket, profile, region string) ([]string, error) {
	if bucket == "" {
		return nil, fmt.Errorf("aws.ListSnapshotPrefixes: empty bucket")
	}
	args := append(commonArgs(profile, region), "s3", "ls", "s3://"+bucket+"/")
	out, _, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parsePrefixList(out), nil
}

func (r *realRunner) SyncDown(ctx context.Context, bucket, slug, destDir, profile, region string) error {
	if bucket == "" {
		return fmt.Errorf("aws.SyncDown: empty bucket")
	}
	if slug == "" {
		return fmt.Errorf("aws.SyncDown: empty slug")
	}
	if destDir == "" {
		return fmt.Errorf("aws.SyncDown: empty destDir")
	}
	args := append(commonArgs(profile, region), "s3", "sync", "s3://"+bucket+"/"+slug+"/", destDir+"/")
	_, _, err := r.run(ctx, args...)
	return err
}

// parsePrefixList extracts the slug names from `aws s3 ls` output.
// Each common-prefix entry looks like:
//
//	"                           PRE prod-2026-05-16T00-00-00Z/"
//
// Returns the slugs ascending-sorted (lex == chronological for ISO-
// timestamp slugs). Object entries (which start with a date) are
// ignored — we only care about top-level prefixes.
func parsePrefixList(raw []byte) []string {
	out := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "PRE ") {
			continue
		}
		name := strings.TrimPrefix(line, "PRE ")
		name = strings.TrimSuffix(name, "/")
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

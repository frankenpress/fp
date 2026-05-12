// Package version exposes build-time metadata baked in via -ldflags.
//
// goreleaser sets Version and Commit on every release build. Local
// `go build ./cmd/fp` invocations leave them at their zero values;
// String() falls back to a sensible "dev" indicator so `fp version`
// is always meaningful.
package version

import (
	"fmt"
	"runtime/debug"
)

// Set via -ldflags at release time. Keep names stable — the
// .goreleaser.yaml ldflags section references them by import path.
var (
	Version = ""
	Commit  = ""
)

// String returns a one-line "<version> (<commit>)" summary suitable
// for `fp version` and `fp --version`. Falls back to vcs.revision from
// the build-embedded module info for local builds.
func String() string {
	v := Version
	if v == "" {
		v = "dev"
	}
	c := Commit
	if c == "" {
		c = readVCSRevision()
	}
	if c == "" {
		return v
	}
	return fmt.Sprintf("%s (%s)", v, shortCommit(c))
}

func readVCSRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

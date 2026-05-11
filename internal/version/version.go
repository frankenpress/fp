// Package version exposes build-time metadata for the fp binary.
//
// Values are wired in via goreleaser ldflags during release builds.
// Local `go build` invocations get the zero values (`Version = "dev"`),
// which the version subcommand surfaces as "dev (no commit info)".
package version

import (
	"fmt"
	"runtime/debug"
)

// Version is the semantic version of the binary. Set via -ldflags on
// release builds; defaults to "dev" for local `go build`.
var Version = "dev"

// Commit is the git SHA the binary was built from. Set via -ldflags
// during release; falls back to debug.ReadBuildInfo's vcs.revision
// for `go install` builds.
var Commit = ""

// BuildDate is an ISO-8601 string for when the binary was built. Set
// via -ldflags during release; empty otherwise.
var BuildDate = ""

// ManifestSchema is the fp.snapshot manifest schema the binary
// accepts. Bumped only when the schema's compatibility envelope
// changes; the schema content itself lives in pkg/manifest/schema.json.
const ManifestSchema = "fp.snapshot/v1"

// String returns a human-readable single-line version string suitable
// for `fp --version` and `fp version` output.
func String() string {
	commit := Commit
	if commit == "" {
		commit = resolveBuildInfoRevision()
	}
	if commit == "" {
		commit = "unknown"
	}

	date := BuildDate
	if date == "" {
		date = "unknown"
	}

	return fmt.Sprintf("fp %s (commit %s, built %s, manifest schema %s)", Version, commit, date, ManifestSchema)
}

func resolveBuildInfoRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) >= 7 {
				return s.Value[:7]
			}
			return s.Value
		}
	}
	return ""
}

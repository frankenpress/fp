package apply

import (
	"fmt"
	"path/filepath"

	"github.com/frankenpress/fp/internal/summary"
)

// PickLatest walks <repoRoot>/<outputDir>/ for snapshot directories
// and returns the slug + absolute host path of the one whose
// manifest.yaml carries the highest `created` value (UTC ISO 8601,
// lex-sortable).
//
// Used by `fp apply` when invoked with no positional arg, and by
// `fp init` (the designer-onboarding command). Both want the same
// "what's the latest committed snapshot" answer — the underlying walk
// lives in summary.Walk() so list/prune share the same source of truth.
//
// Returns an error when:
//   - <repoRoot>/<outputDir>/ does not exist or is not readable
//   - no subdirectory contains a manifest.yaml that parses
//   - no parsed manifest carries a non-empty `created` field
//
// Subdirectories without a manifest.yaml are silently skipped — they
// might be .gitkeep stubs, half-deleted dirs, or other detritus.
// Manifests without a `created` field are walked (so `fp list` can
// surface them) but skipped here.
func PickLatest(repoRoot, outputDir string) (slug, hostDir string, err error) {
	entries, err := summary.Walk(repoRoot, outputDir)
	if err != nil {
		return "", "", err
	}
	// summary.Walk sorts by Created desc with empty-Created last, so
	// the first non-empty-Created entry is the latest.
	for _, e := range entries {
		if e.Manifest.Created == "" {
			continue
		}
		return e.Slug, e.HostDir, nil
	}
	return "", "", fmt.Errorf(
		"no snapshot dir with a parseable manifest.yaml (and a `created` field) found under %s. capture one with `fp snapshot` first",
		filepath.Join(repoRoot, outputDir),
	)
}

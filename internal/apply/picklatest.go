package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/frankenpress/fp/internal/summary"
)

// PickLatest walks <repoRoot>/<outputDir>/ for snapshot directories
// and returns the slug + absolute host path of the one whose
// manifest.yaml carries the highest `created` value (UTC ISO 8601,
// lex-sortable).
//
// Used by `fp apply` when invoked with no positional arg, and by
// future `fp init` (the designer-onboarding command). Both want the
// same "what's the latest committed snapshot" answer — keeping it
// in one place stops the two callers from drifting.
//
// Returns an error when:
//   - <repoRoot>/<outputDir>/ does not exist or is not readable
//   - no subdirectory contains a manifest.yaml that parses
//   - no parsed manifest carries a non-empty `created` field
//
// Subdirectories without a manifest.yaml are silently skipped — they
// might be .gitkeep stubs, half-deleted dirs, or other detritus.
// Subdirectories whose manifest fails to parse OR is missing `created`
// are also skipped (the chart's install Job applies the same
// tolerance).
func PickLatest(repoRoot, outputDir string) (slug, hostDir string, err error) {
	dir := filepath.Join(repoRoot, outputDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", dir, err)
	}

	var bestSlug, bestDir, bestCreated string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, e.Name(), "manifest.yaml")
		m, err := summary.Read(manifestPath)
		if err != nil {
			continue
		}
		if m.Created == "" {
			continue
		}
		if bestCreated == "" || m.Created > bestCreated {
			bestCreated = m.Created
			bestSlug = e.Name()
			bestDir = filepath.Join(dir, e.Name())
		}
	}

	if bestSlug == "" {
		return "", "", fmt.Errorf(
			"no snapshot dir with a parseable manifest.yaml (and a `created` field) found under %s. capture one with `fp snapshot` first",
			dir,
		)
	}
	return bestSlug, bestDir, nil
}

package apply

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

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

// PickLatestFromDirs walks every (repoRoot, dir) pair and returns the
// slug + host path of the snapshot with the highest manifest.created
// across all dirs. Slugs that appear in more than one dir produce a
// hard error — fp does not silently pick one over the other because
// the design separation (committed `web/imports/` vs pulled
// `.fp/prod-snapshots/`) is load-bearing for the round-trip flow.
//
// Used by `fp apply` to honour both `[snapshot].output_dir` (committed
// captures) and `.fp/prod-snapshots/` (pulled prod captures) in the
// no-positional case.
//
// Returns the standard PickLatest error when no dir contributes any
// snapshot with a non-empty `created` field.
func PickLatestFromDirs(repoRoot string, dirs []string) (slug, hostDir string, err error) {
	// slug → host paths it was seen at (for collision diagnostics).
	seen := map[string][]string{}
	all := []summary.Entry{}
	for _, dir := range dirs {
		entries, werr := summary.Walk(repoRoot, dir)
		if werr != nil {
			// Missing dir is fine — pulled-snapshots dir won't exist
			// until first `fp pull`. Propagate non-not-exist errors.
			if errors.Is(werr, fs.ErrNotExist) {
				continue
			}
			return "", "", werr
		}
		for _, e := range entries {
			seen[e.Slug] = append(seen[e.Slug], e.HostDir)
			all = append(all, e)
		}
	}

	// Collision check — same slug in more than one dir.
	for s, paths := range seen {
		if len(paths) > 1 {
			sort.Strings(paths)
			return "", "", fmt.Errorf(
				"snapshot slug %q exists in multiple dirs (%s). remove one or pass an explicit path to `fp apply`",
				s,
				strings.Join(paths, ", "),
			)
		}
	}

	// Sort across dirs by Created desc, empty Created last.
	sort.SliceStable(all, func(i, j int) bool {
		ci, cj := all[i].Manifest.Created, all[j].Manifest.Created
		if ci == "" && cj != "" {
			return false
		}
		if ci != "" && cj == "" {
			return true
		}
		return ci > cj
	})

	for _, e := range all {
		if e.Manifest.Created == "" {
			continue
		}
		return e.Slug, e.HostDir, nil
	}

	checked := make([]string, 0, len(dirs))
	for _, d := range dirs {
		checked = append(checked, filepath.Join(repoRoot, d))
	}
	return "", "", fmt.Errorf(
		"no snapshot dir with a parseable manifest.yaml (and a `created` field) found under %s. capture one with `fp snapshot` or pull one with `fp pull`",
		strings.Join(checked, " or "),
	)
}

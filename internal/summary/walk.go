package summary

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Entry is one snapshot directory under <repoRoot>/<outputDir>/ that
// contains a parseable manifest.yaml.
type Entry struct {
	// Slug is the directory's basename (matches manifest.id when the
	// snapshot was produced by mu-plugin's wp fp snapshot).
	Slug string
	// HostDir is the absolute path to the snapshot directory on the host.
	HostDir string
	// Manifest is the parsed manifest.yaml. Never nil for entries
	// returned from Walk; fields may be empty when the underlying YAML
	// omitted them.
	Manifest *Manifest
}

// Walk returns one Entry per direct subdirectory of
// <repoRoot>/<outputDir>/ that contains a parseable manifest.yaml.
// Subdirectories without a manifest.yaml (or whose manifest fails to
// parse) are silently skipped — same tolerance as the chart's install
// Job, so `web/imports/.gitkeep` and half-deleted captures don't trip
// the walk.
//
// Order: by Manifest.Created descending. Entries with an empty Created
// (broken or pre-v4 manifests) sort last among themselves, stable by
// slug, so `fp list` still surfaces them.
//
// Returns an OS-level error when the output dir is unreadable; an
// empty result + nil error means the dir exists but has no snapshots.
// Callers that need "at least one snapshot with Created" (PickLatest)
// enforce that themselves.
func Walk(repoRoot, outputDir string) ([]Entry, error) {
	dir := filepath.Join(repoRoot, outputDir)
	dirents, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}

	var entries []Entry
	for _, d := range dirents {
		if !d.IsDir() {
			continue
		}
		manifestPath := filepath.Join(dir, d.Name(), "manifest.yaml")
		m, err := Read(manifestPath)
		if err != nil {
			continue
		}
		entries = append(entries, Entry{
			Slug:     d.Name(),
			HostDir:  filepath.Join(dir, d.Name()),
			Manifest: m,
		})
	}

	sort.SliceStable(entries, func(i, j int) bool {
		ci, cj := entries[i].Manifest.Created, entries[j].Manifest.Created
		// Empty Created sorts after non-empty.
		if ci == "" && cj != "" {
			return false
		}
		if cj == "" && ci != "" {
			return true
		}
		if ci == cj {
			return entries[i].Slug < entries[j].Slug
		}
		return ci > cj
	})

	return entries, nil
}

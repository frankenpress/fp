// Package prune backs the fp delete and fp prune subcommands. Both
// resolve a list of snapshot directories to remove and funnel through
// a shared remove() helper that honours the uncommitted-changes guard
// and prints one line per removed entry.
//
// Why one package for two verbs: delete is "remove this one"; prune is
// "remove all but the newest N". They share target validation (must
// contain manifest.yaml), the git uncommitted guard, the actual
// os.RemoveAll, and the audit-trail print. Splitting into two
// packages would force either duplication or a third "removal-core"
// package.
package prune

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/frankenpress/fp/internal/repo"
	"github.com/frankenpress/fp/internal/summary"
)

// DeleteOptions drives Delete (the fp delete <target> path).
type DeleteOptions struct {
	// Target is a bare slug, a relative path with separators, or an
	// absolute path — same resolution as fp apply / fp diff.
	Target string

	RepoRoot  string
	OutputDir string

	// Quick bypasses the uncommitted-changes guard. Mirrors fp
	// snapshot --quick semantics — the project's single safety bypass.
	Quick bool

	Stdout io.Writer
	Stderr io.Writer
}

// PruneOptions drives Prune (the fp prune --keep N path).
type PruneOptions struct {
	// Keep is the number of newest snapshots to retain (by
	// Manifest.Created desc). 0 = delete all; negative = error.
	Keep int

	// Apply == false (default) is dry-run: print what would go and
	// exit 0. Apply == true performs the deletions.
	Apply bool

	RepoRoot  string
	OutputDir string

	// Quick bypasses the uncommitted-changes guard. Only consulted
	// when Apply is true.
	Quick bool

	Stdout io.Writer
	Stderr io.Writer
}

// candidate is one snapshot dir queued for removal. Built from either
// target resolution (Delete) or summary.Walk filtering (Prune).
type candidate struct {
	Slug      string
	HostDir   string
	RelToRoot string // path relative to RepoRoot, for git status
	Created   string // raw manifest.created, used in the summary line
}

// Delete removes a single snapshot directory.
func Delete(opts DeleteOptions) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Target == "" {
		return errors.New("delete: missing target slug or path")
	}

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "web/imports"
	}

	hostDir, relToRoot, err := resolveTarget(opts.RepoRoot, outputDir, opts.Target)
	if err != nil {
		return err
	}
	if err := assertSnapshotDir(hostDir); err != nil {
		return err
	}

	c := candidate{
		Slug:      filepath.Base(hostDir),
		HostDir:   hostDir,
		RelToRoot: relToRoot,
	}
	// Read the manifest opportunistically for the summary line. A
	// dir that passes assertSnapshotDir has a parseable manifest;
	// failures here are unusual but non-fatal.
	if m, mErr := summary.Read(filepath.Join(hostDir, "manifest.yaml")); mErr == nil {
		c.Created = m.Created
	}

	return remove(opts.RepoRoot, []candidate{c}, opts.Quick, false, opts.Stdout)
}

// Prune walks the snapshot output dir, keeps the newest Keep entries
// by Manifest.Created, and (when Apply is true) removes the rest.
func Prune(opts PruneOptions) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Keep < 0 {
		return fmt.Errorf("prune: --keep must be >= 0, got %d", opts.Keep)
	}

	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "web/imports"
	}

	entries, err := summary.Walk(opts.RepoRoot, outputDir)
	if err != nil {
		return err
	}

	if len(entries) <= opts.Keep {
		fmt.Fprintf(opts.Stdout,
			"nothing to prune: %d snapshot(s) on disk, keeping the newest %d.\n",
			len(entries), opts.Keep,
		)
		return nil
	}

	// summary.Walk sorts newest-first; everything past index Keep goes.
	doomed := entries[opts.Keep:]
	cands := make([]candidate, 0, len(doomed))
	for _, e := range doomed {
		rel, err := filepath.Rel(opts.RepoRoot, e.HostDir)
		if err != nil {
			return fmt.Errorf("compute rel path for %s: %w", e.HostDir, err)
		}
		cands = append(cands, candidate{
			Slug:      e.Slug,
			HostDir:   e.HostDir,
			RelToRoot: rel,
			Created:   e.Manifest.Created,
		})
	}

	return remove(opts.RepoRoot, cands, opts.Quick, !opts.Apply, opts.Stdout)
}

// remove performs (or previews) the os.RemoveAll on each candidate.
// In dry mode it lists what would go and exits without touching disk.
// Otherwise it runs the uncommitted-changes guard once (over the full
// candidate set, refusing if any has dirty git state without quick)
// then deletes each in order.
func remove(repoRoot string, cands []candidate, quick, dry bool, w io.Writer) error {
	if len(cands) == 0 {
		return nil
	}

	if dry {
		fmt.Fprintf(w, "would remove %d snapshot(s) (dry run; pass --apply to actually delete):\n", len(cands))
		for _, c := range cands {
			fmt.Fprintf(w, "  %s\n", formatLine(c))
		}
		return nil
	}

	// Uncommitted-changes guard — skipped when quick, skipped when
	// repoRoot isn't a git working tree.
	if !quick && repo.IsGitRepo(repoRoot) {
		var dirty []string
		for _, c := range cands {
			d, err := repo.HasUncommittedChanges(repoRoot, c.RelToRoot)
			if err != nil {
				return fmt.Errorf("git status check for %s: %w", c.RelToRoot, err)
			}
			if d {
				dirty = append(dirty, c.RelToRoot)
			}
		}
		if len(dirty) > 0 {
			return fmt.Errorf(
				"refusing to delete: uncommitted changes under %s. commit/stash first, or pass --quick to override",
				strings.Join(dirty, ", "),
			)
		}
	}

	fmt.Fprintf(w, "removing %d snapshot(s):\n", len(cands))
	for _, c := range cands {
		if err := os.RemoveAll(c.HostDir); err != nil {
			return fmt.Errorf("remove %s: %w", c.HostDir, err)
		}
		fmt.Fprintf(w, "  removed %s\n", formatLine(c))
	}
	return nil
}

// resolveTarget mirrors apply.resolveSnapshotDir: bare slug → joined
// against outputDir; path with separators → relative to cwd; absolute
// → as given. Refuses targets outside the repo root since blindly
// rm-ing /tmp/foo is not in scope.
func resolveTarget(repoRoot, outputDir, target string) (hostDir, relToRoot string, err error) {
	var abs string
	switch {
	case filepath.IsAbs(target):
		abs = filepath.Clean(target)
	case strings.ContainsRune(target, filepath.Separator):
		cwd, werr := os.Getwd()
		if werr != nil {
			return "", "", fmt.Errorf("getwd: %w", werr)
		}
		abs = filepath.Clean(filepath.Join(cwd, target))
	default:
		abs = filepath.Join(repoRoot, outputDir, target)
	}

	info, err := os.Stat(abs)
	if err != nil {
		return "", "", fmt.Errorf("snapshot dir not found: %s", abs)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("snapshot path is not a directory: %s", abs)
	}

	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", "", fmt.Errorf("compute path relative to repo root: %w", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf(
			"snapshot dir %s is outside the repo root %s; fp delete only operates on snapshots inside the repo",
			abs, repoRoot,
		)
	}
	return abs, rel, nil
}

// assertSnapshotDir refuses to remove a directory that doesn't look
// like a snapshot. Mirrors the check used elsewhere: presence of
// manifest.yaml is the canonical "is this an fp snapshot dir" test.
func assertSnapshotDir(hostDir string) error {
	manifestPath := filepath.Join(hostDir, "manifest.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return fmt.Errorf(
			"refusing to delete %s: no manifest.yaml — does not look like an fp snapshot dir",
			hostDir,
		)
	}
	return nil
}

// formatLine renders one candidate for the audit-trail output. Falls
// back to just the rel path when created is empty (broken manifest).
func formatLine(c candidate) string {
	if c.Created == "" {
		return c.RelToRoot
	}
	return fmt.Sprintf("%s (created %s)", c.RelToRoot, c.Created)
}

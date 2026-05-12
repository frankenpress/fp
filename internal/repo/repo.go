// Package repo reads slug-default cascade inputs from the surrounding
// git repository: the current branch name (sources 2 of the cascade)
// and the composer.json name field (source 3).
//
// Neither lookup is fatal — failures fall through to the next source
// in the cascade. The snapshot orchestrator combines them with the
// state file (source 1) and the timestamped fallback (source 4) to
// produce the default the slug prompt suggests.
package repo

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BranchName returns the current git branch name (output of
// `git symbolic-ref --short HEAD`), with the leading "feat/" /
// "fix/" / "chore/" group stripped. Returns empty string when the
// branch is main/master/HEAD (detached) or when git is unavailable;
// callers fall through to the next slug source.
func BranchName(repoRoot string) string {
	out, err := runGit(repoRoot, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	branch := strings.TrimSpace(out)
	if branch == "" {
		return ""
	}
	switch branch {
	case "main", "master", "HEAD", "trunk":
		return ""
	}
	// Strip a single conventional-commit-style prefix if present.
	for _, prefix := range []string{"feat/", "fix/", "chore/", "docs/", "refactor/", "test/", "ci/", "build/", "perf/", "style/", "wip/"} {
		if strings.HasPrefix(branch, prefix) {
			return branch[len(prefix):]
		}
	}
	return branch
}

// ComposerName returns the package name from composer.json (the part
// after the slash — "frankenpress/sts" → "sts"). Empty if the file is
// missing, unreadable, or doesn't carry a name field.
func ComposerName(repoRoot string) string {
	data, err := os.ReadFile(filepath.Join(repoRoot, "composer.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return ""
	}
	name := strings.TrimSpace(doc.Name)
	if name == "" {
		return ""
	}
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// HasUncommittedChanges runs `git status --porcelain` against pathspec.
// Returns true if anything is staged, modified, or untracked under
// that path. The plan's Error-UX (e) uses this to gate the
// pre-clean prompt.
func HasUncommittedChanges(repoRoot, pathspec string) (bool, error) {
	if pathspec == "" {
		return false, nil
	}
	out, err := runGit(repoRoot, "status", "--porcelain", "--", pathspec)
	if err != nil {
		// A directory that isn't tracked yet is fine — treat the
		// git error as "no uncommitted changes" rather than failing
		// the whole snapshot. The pre-clean still runs regardless.
		if _, statErr := os.Stat(filepath.Join(repoRoot, pathspec)); os.IsNotExist(statErr) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// IsGitRepo reports whether repoRoot looks like a git working tree.
// Used to decide whether to consult BranchName / HasUncommittedChanges.
func IsGitRepo(repoRoot string) bool {
	_, err := os.Stat(filepath.Join(repoRoot, ".git"))
	return err == nil
}

func runGit(repoRoot string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Package release wires fp release end-to-end: capture, branch
// hygiene, commit, push, PR creation. Composes snapshot.Run + git.Runner
// + gh.Runner + prompt.Confirm.
//
// fp release is the canonical one-shot designer flow:
//
//	fp release           # interactive: prompts for slug + note + commit-confirm
//	fp release --yes     # skip the commit-confirm prompt
//	fp release --no-pr   # commit + push but skip gh pr create
//	fp release --branch feat/foo --yes --note "msg"  # scripted
//
// Failure recovery: every step prints a concrete continuation command
// in its error message. The capture step is the only operation that
// modifies the working tree; git ops are all reversible with a manual
// `git reset` if something goes wrong mid-pipeline.
package release

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/gh"
	"github.com/frankenpress/fp/internal/git"
	"github.com/frankenpress/fp/internal/prompt"
	"github.com/frankenpress/fp/internal/snapshot"
	"github.com/frankenpress/fp/internal/state"
	"github.com/frankenpress/fp/internal/summary"
)

// protectedBranches names branches we refuse to commit a snapshot
// directly to. Anything matched here causes auto-branching to
// `feat/snapshot-<slug>`.
var protectedBranches = map[string]struct{}{
	"main":   {},
	"master": {},
	"trunk":  {},
}

// Options carries every input fp release needs. Built from flags +
// env by internal/cli/release.go.
type Options struct {
	// Snapshot inputs (all pass through to snapshot.Run).
	Slug     string
	Note     string
	NoteFile string

	// Release-specific.
	Branch string // override default branch policy
	NoPR   bool
	Draft  bool // open the PR as draft (gh pr create --draft)
	Yes    bool // skip the commit-confirm prompt

	// Composed dependencies.
	RepoRoot     string
	Config       *config.Config
	State        *state.State
	DockerRunner docker.Runner
	GitRunner    git.Runner
	GHRunner     gh.Runner

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Interactive bool
}

// Run executes the release pipeline.
func Run(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	// 1) Capture via snapshot.Run. snapshot owns slug + note resolution
	//    + the uncommitted-changes guard + the docker-cp extraction +
	//    state persistence. We thread its flags through unchanged.
	snapOpts := snapshot.Options{
		RepoRoot:    opts.RepoRoot,
		Config:      opts.Config,
		State:       opts.State,
		Runner:      opts.DockerRunner,
		Stdin:       opts.Stdin,
		Stdout:      opts.Stdout,
		Stderr:      opts.Stderr,
		Interactive: opts.Interactive,
		Slug:        opts.Slug,
		Note:        opts.Note,
		NoteFile:    opts.NoteFile,
	}
	snapResult, err := snapshot.Run(ctx, snapOpts)
	if err != nil {
		return err
	}
	slug := snapResult.Slug
	note := snapResult.Note

	// 2) Read manifest for the PR body summary.
	m, err := summary.Read(snapResult.ManifestPath)
	if err != nil {
		return fmt.Errorf("read manifest at %s: %w", snapResult.ManifestPath, err)
	}

	// 3) Branch policy.
	currentBranch, err := opts.GitRunner.CurrentBranch(ctx, opts.RepoRoot)
	if err != nil {
		return fmt.Errorf("git: detect current branch: %w", err)
	}
	targetBranch := opts.Branch
	if targetBranch == "" {
		if _, protected := protectedBranches[currentBranch]; protected {
			targetBranch = "feat/snapshot-" + slug
		} else {
			targetBranch = currentBranch
		}
	}

	if targetBranch != currentBranch {
		exists, err := opts.GitRunner.BranchExists(ctx, opts.RepoRoot, targetBranch)
		if err != nil {
			return fmt.Errorf("git: branch existence check failed for %q: %w", targetBranch, err)
		}
		if err := opts.GitRunner.Checkout(ctx, opts.RepoRoot, targetBranch, !exists); err != nil {
			return fmt.Errorf("git: checkout %q (create=%v): %w", targetBranch, !exists, err)
		}
		fmt.Fprintf(opts.Stdout, "[fp] switched to branch %s\n", targetBranch)
	}

	// 4) Confirm before commit+push (unless --yes or non-interactive).
	outputDir := opts.Config.Snapshot.OutputDir
	if outputDir == "" {
		outputDir = "web/imports"
	}
	addPath := filepath.Join(outputDir, slug) + string(filepath.Separator)
	commitMsg := buildCommitMessage(slug, note)

	if !opts.Yes && opts.Interactive {
		fmt.Fprintln(opts.Stdout)
		fmt.Fprintln(opts.Stdout, "about to commit + push:")
		fmt.Fprintf(opts.Stdout, "  branch:  %s\n", targetBranch)
		fmt.Fprintf(opts.Stdout, "  add:     %s\n", addPath)
		fmt.Fprintf(opts.Stdout, "  subject: %s\n", firstLine(commitMsg))
		if !opts.NoPR {
			if opts.Draft {
				fmt.Fprintf(opts.Stdout, "  pr:      will run gh pr create --draft after push\n")
			} else {
				fmt.Fprintf(opts.Stdout, "  pr:      will run gh pr create after push\n")
			}
		}
		ok, err := prompt.Confirm(opts.Stdin, opts.Stdout, "continue?")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("aborted before commit (capture is on disk at " + addPath + ")")
		}
	}

	// 5) git add.
	if err := opts.GitRunner.Add(ctx, opts.RepoRoot, []string{addPath}); err != nil {
		return fmt.Errorf("git add %s: %w\n  hint: capture is on disk; re-stage manually with `git add %s` if you want to recover",
			addPath, err, addPath)
	}

	// 6) git commit.
	if err := opts.GitRunner.Commit(ctx, opts.RepoRoot, commitMsg); err != nil {
		return fmt.Errorf("git commit: %w\n  hint: capture + add succeeded; commit failed (hook? empty tree?). retry with `git commit -m %q`",
			err, firstLine(commitMsg))
	}
	fmt.Fprintf(opts.Stdout, "[fp] committed: %s\n", firstLine(commitMsg))

	// 7) git push.
	if err := opts.GitRunner.Push(ctx, opts.RepoRoot, "origin", targetBranch, true); err != nil {
		return fmt.Errorf("git push -u origin %s: %w\n  hint: commit landed locally; retry the push manually, then `gh pr create` if needed",
			targetBranch, err)
	}
	fmt.Fprintf(opts.Stdout, "[fp] pushed %s to origin\n", targetBranch)

	// 8) gh pr create (unless --no-pr).
	if !opts.NoPR {
		title := fmt.Sprintf("snapshot: %s", slug)
		body := buildPRBody(slug, note, m)
		url, err := opts.GHRunner.PRCreate(ctx, opts.RepoRoot, title, body, opts.Draft)
		if err != nil {
			// PR creation failure usually means a PR already exists.
			// Try to look it up and print the URL so the designer
			// doesn't have to switch tools to find it.
			existing, viewErr := opts.GHRunner.PRView(ctx, opts.RepoRoot, targetBranch)
			if viewErr == nil && existing != "" {
				fmt.Fprintf(opts.Stdout, "[fp] existing PR for %s: %s\n", targetBranch, existing)
				return nil
			}
			retryCmd := "gh pr create --title " + fmt.Sprintf("%q", title)
			if opts.Draft {
				retryCmd += " --draft"
			}
			return fmt.Errorf("gh pr create: %w\n  hint: commit + push succeeded; create the PR manually with `%s`",
				err, retryCmd)
		}
		if opts.Draft {
			fmt.Fprintf(opts.Stdout, "[fp] opened draft PR: %s\n", url)
		} else {
			fmt.Fprintf(opts.Stdout, "[fp] opened PR: %s\n", url)
		}
	} else {
		fmt.Fprintln(opts.Stdout, "[fp] --no-pr — skipping gh pr create. push it manually when ready.")
	}

	return nil
}

// buildCommitMessage returns the subject + body string used for both
// the git commit and as the seed for the PR body.
func buildCommitMessage(slug, note string) string {
	subject := "snapshot: " + slug
	if note == "" {
		return subject
	}
	return subject + "\n\n" + note
}

// buildPRBody renders the markdown body fp release uses for the PR.
func buildPRBody(slug, note string, m *summary.Manifest) string {
	var b strings.Builder
	fmt.Fprintln(&b, "## Snapshot")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Field | Value |")
	fmt.Fprintln(&b, "|---|---|")
	fmt.Fprintf(&b, "| Slug | `%s` |\n", slug)
	if m != nil {
		if m.Schema != "" {
			fmt.Fprintf(&b, "| Schema | `%s` |\n", m.Schema)
		}
		if m.Adapter != "" {
			fmt.Fprintf(&b, "| Adapter | `%s` |\n", m.Adapter)
		}
		if m.Source.SourceTheme != "" {
			fmt.Fprintf(&b, "| Source theme | `%s` |\n", m.Source.SourceTheme)
		}
		if m.Contents.TemplatesCount > 0 {
			fmt.Fprintf(&b, "| Templates | %d |\n", m.Contents.TemplatesCount)
		}
		if m.Contents.OptionsCount > 0 {
			fmt.Fprintf(&b, "| Options | %d |\n", m.Contents.OptionsCount)
		}
		if m.Contents.AttachmentsCount > 0 {
			fmt.Fprintf(&b, "| Attachments | %d |\n", m.Contents.AttachmentsCount)
		}
		if m.Contents.UploadsFileCount > 0 {
			fmt.Fprintf(&b, "| Uploads | %d files |\n", m.Contents.UploadsFileCount)
		}
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Designer note")
	fmt.Fprintln(&b)
	if strings.TrimSpace(note) == "" {
		fmt.Fprintln(&b, "(none)")
	} else {
		fmt.Fprintln(&b, note)
	}

	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Apply path")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "Merge → image rebuilds → ArgoCD reconciles → chart's install Job runs `wp fp apply --snapshot-dir=/app/web/imports/%s` on every replica. Idempotent — markers short-circuit re-applies.\n", slug)

	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

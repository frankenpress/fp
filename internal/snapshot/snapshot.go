// Package snapshot wires the fp snapshot subcommand end-to-end:
// resolve slug + note + paths from config/state/flags/prompts, verify
// the docker-compose stack is up, run wp fp snapshot inside the
// container, docker-cp the result out, parse the manifest, print
// the summary, and (in normal mode) persist state.
package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frankenpress/fp/internal/compose"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/prompt"
	"github.com/frankenpress/fp/internal/repo"
	"github.com/frankenpress/fp/internal/state"
	"github.com/frankenpress/fp/internal/summary"
)

// Options carries every input fp snapshot needs. Build it from
// flags + env in internal/cli/snapshot.go; the orchestrator below
// takes no flag-parsing responsibility.
type Options struct {
	// Filesystem + IO.
	RepoRoot string
	Config   *config.Config
	State    *state.State
	Runner   docker.Runner

	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// True when stdin / stdout are TTYs and we should run the
	// interactive prompts. False in CI / when piped.
	Interactive bool

	// Flag values (zero means "not set").
	Slug      string
	Note      string
	NoteFile  string
	Quick     bool
	OutputDir string // overrides config.Snapshot.OutputDir
	Service   string // overrides config.Snapshot.Service
	Project   string // overrides config.Snapshot.Project

	// Time source — tests override to make the timestamped fallback
	// deterministic.
	Now func() time.Time
}

// Result carries the resolved slug + note + manifest path after a
// successful capture so composing callers (fp release) can reference
// them without re-deriving.
type Result struct {
	Slug         string
	Note         string
	ManifestPath string // absolute host path to manifest.yaml
}

// Run executes the snapshot pipeline. Returns the resolved slug + note
// + manifest path on success; on failure returns a non-nil error with
// a partially-populated *Result (or nil) and an error designed to be
// readable in a terminal (see Error UX in the plan).
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	// Resolve effective config values (flags > config > defaults).
	service := firstNonEmpty(opts.Service, opts.Config.Snapshot.Service, "site")
	project := firstNonEmpty(opts.Project, opts.Config.Snapshot.Project, compose.DefaultProject(opts.RepoRoot))
	outputDir := firstNonEmpty(opts.OutputDir, opts.Config.Snapshot.OutputDir, "web/imports")
	containerOutputDir := firstNonEmpty(opts.Config.Snapshot.ContainerOutputDir, "/app/web/imports")

	// Slug resolution — cascade + prompt (unless --quick or --slug).
	slug, err := resolveSlug(opts)
	if err != nil {
		return nil, err
	}

	// Note resolution.
	note, err := resolveNote(opts, slug)
	if err != nil {
		return nil, err
	}

	hostTargetDir := filepath.Join(opts.RepoRoot, outputDir, slug)
	hostOutputParent := filepath.Join(opts.RepoRoot, outputDir)
	containerTargetDir := strings.TrimRight(containerOutputDir, "/") + "/" + slug

	// Uncommitted-changes guard (skipped in --quick mode).
	if !opts.Quick && repo.IsGitRepo(opts.RepoRoot) {
		rel := filepath.Join(outputDir, slug)
		dirty, err := repo.HasUncommittedChanges(opts.RepoRoot, rel)
		if err != nil {
			return nil, fmt.Errorf("git status check failed: %w", err)
		}
		if dirty {
			if !opts.Interactive {
				return nil, fmt.Errorf(
					"%s has uncommitted changes; refusing to overwrite. commit/stash first, or pass --quick",
					rel,
				)
			}
			ok, err := prompt.Confirm(
				opts.Stdin, opts.Stdout,
				fmt.Sprintf("%s/ has uncommitted changes. overwriting will lose them. continue?", rel),
			)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, errors.New("aborted")
			}
		}
	}

	// Stack up-check.
	status, container, err := compose.Check(ctx, opts.Runner, project, service)
	if err != nil {
		return nil, fmt.Errorf("docker compose ps failed: %w", err)
	}
	if status != compose.StatusServiceRunning {
		return nil, errors.New(compose.FormatNotRunning(status, project, service))
	}
	if container == nil {
		return nil, fmt.Errorf("docker compose reported %q running but returned no container metadata", service)
	}

	// Pre-clean host target — designers rely on the committed dir
	// being a clean reflection of the latest capture.
	if err := os.RemoveAll(hostTargetDir); err != nil {
		return nil, fmt.Errorf("pre-clean %s: %w", hostTargetDir, err)
	}
	if err := os.MkdirAll(hostOutputParent, 0o755); err != nil {
		return nil, fmt.Errorf("ensure %s: %w", hostOutputParent, err)
	}

	// Run wp fp snapshot. Streaming stdout+stderr so designers see
	// WP-CLI progress live (snapshots can take a few seconds for
	// larger uploads).
	wpArgs := []string{"wp", "--allow-root", "--path=/app/web/wp", "fp", "snapshot",
		"--slug=" + slug,
		"--output-dir=" + containerTargetDir,
	}
	if note != "" {
		wpArgs = append(wpArgs, "--note="+note)
	}

	fmt.Fprintf(opts.Stdout, "[fp] capturing %q via %s/%s -> %s\n", slug, project, service, filepath.Join(outputDir, slug))
	fmt.Fprintln(opts.Stdout, "--- wp-cli output ---")
	execErr := opts.Runner.ComposeExecStreaming(ctx, project, service, wpArgs, opts.Stdout, opts.Stderr)
	fmt.Fprintln(opts.Stdout, "--- end wp-cli output ---")
	if execErr != nil {
		var ee *docker.ExecError
		exitCode := -1
		if errors.As(execErr, &ee) {
			exitCode = ee.ExitCode
		}
		// The "no adapter detected" message is the only one with a
		// specific actionable hint; everything else is a real bug
		// the designer needs the verbatim stderr (already printed)
		// to diagnose.
		fmt.Fprintf(opts.Stderr, "error: wp fp snapshot exited %d. See wp-cli output above for the verbatim message.\n", exitCode)
		fmt.Fprintln(opts.Stderr, "hint: if the error is \"no snapshot adapter detected\", activate an FSE block theme in WP admin then retry.")
		return nil, errors.New("wp fp snapshot failed")
	}

	// Extract the snapshot out of the container.
	src := container.Name + ":" + containerTargetDir
	if err := opts.Runner.Copy(ctx, src, hostOutputParent); err != nil {
		fmt.Fprintf(opts.Stderr, "error: docker cp %s %s failed: %v\n", src, hostOutputParent, err)
		fmt.Fprintf(opts.Stderr, "hint: snapshot was written inside the container at %s; you can copy it manually with:\n", containerTargetDir)
		fmt.Fprintf(opts.Stderr, "      docker cp %s %s\n", src, hostOutputParent)
		return nil, errors.New("docker cp failed")
	}

	// Print the post-capture summary.
	manifestPath := filepath.Join(hostTargetDir, "manifest.yaml")
	m, err := summary.Read(manifestPath)
	if err != nil {
		// Hard fail — if the manifest isn't there, the snapshot
		// effectively didn't land.
		return nil, fmt.Errorf("read manifest at %s: %w", manifestPath, err)
	}
	fmt.Fprintln(opts.Stdout)
	summary.Print(opts.Stdout, m, slug, filepath.Join(outputDir, slug))

	// Persist state unless --quick.
	if !opts.Quick {
		opts.State.LastSlug = slug
		opts.State.LastNoteUsed = note
		opts.State.LastCaptureAt = opts.Now()
		if err := state.Save(opts.RepoRoot, opts.State); err != nil {
			// Saving state is non-load-bearing — warn but don't
			// kill the success of a captured snapshot.
			fmt.Fprintf(opts.Stderr, "warn: could not persist .fp/state.json: %v\n", err)
		}
	}

	return &Result{
		Slug:         slug,
		Note:         note,
		ManifestPath: manifestPath,
	}, nil
}

// resolveSlug applies the cascade documented in the plan and, when
// interactive + neither --quick nor --slug is set, prompts the
// designer to accept or override the default.
func resolveSlug(opts Options) (string, error) {
	// --slug short-circuits everything.
	if opts.Slug != "" {
		s := slugify(opts.Slug)
		if s == "" {
			return "", fmt.Errorf("--slug %q sanitises to empty; pick a value with at least one alphanumeric character", opts.Slug)
		}
		return s, nil
	}

	// --quick uses a timestamped slug unconditionally.
	if opts.Quick {
		return TimestampedSlug(opts.Now()), nil
	}

	def := DefaultSlug(opts)

	if !opts.Interactive {
		if def == "" {
			return "", errors.New("no --slug provided and no default could be inferred (non-interactive)")
		}
		return def, nil
	}

	chosen, err := prompt.AskSlug(opts.Stdin, opts.Stdout, def)
	if err != nil {
		return "", err
	}
	s := slugify(chosen)
	if s == "" {
		return "", errors.New("slug sanitised to empty; pick a value with at least one alphanumeric character")
	}
	return s, nil
}

// DefaultSlug computes the cascade default without prompting. Exposed
// for tests + for the slug prompt's "[default]: " suggestion.
func DefaultSlug(opts Options) string {
	if opts.State != nil && opts.State.LastSlug != "" {
		if s := slugify(opts.State.LastSlug); s != "" {
			return s
		}
	}
	if repo.IsGitRepo(opts.RepoRoot) {
		if b := repo.BranchName(opts.RepoRoot); b != "" {
			if s := slugify(b); s != "" {
				return s
			}
		}
	}
	if name := repo.ComposerName(opts.RepoRoot); name != "" {
		if s := slugify(name + "-launch"); s != "" {
			return s
		}
	}
	return TimestampedSlug(opts.Now())
}

// TimestampedSlug returns "snapshot-YYYYMMDD-HHMMSS" in UTC. The
// --quick mode's unconditional fallback.
func TimestampedSlug(t time.Time) string {
	return "snapshot-" + t.UTC().Format("20060102-150405")
}

// resolveNote returns the note text, honouring --note / --note-file
// when set, then deferring to the prompt (editor or readline) when
// interactive, and to "" when --quick.
func resolveNote(opts Options, slug string) (string, error) {
	if opts.Note != "" && opts.NoteFile != "" {
		return "", errors.New("--note and --note-file are mutually exclusive")
	}
	if opts.NoteFile != "" {
		data, err := os.ReadFile(opts.NoteFile)
		if err != nil {
			return "", fmt.Errorf("read --note-file %s: %w", opts.NoteFile, err)
		}
		return strings.TrimRight(string(data), "\n"), nil
	}
	if opts.Note != "" {
		return opts.Note, nil
	}
	if opts.Quick {
		return "", nil
	}
	if !opts.Interactive {
		return "", nil
	}
	useEditor := false
	if stdinFile, ok := opts.Stdin.(*os.File); ok {
		useEditor = prompt.IsTerminal(stdinFile)
	}
	return prompt.AskNote(opts.Stdin, opts.Stdout, slug, useEditor)
}

// slugify lower-cases the string, replaces any non-[a-z0-9-] run
// with a single dash, and trims leading/trailing dashes. Matches the
// mu-plugin's safe_slug behaviour so dir names look identical
// regardless of which side produced them.
func slugify(s string) string {
	out := make([]byte, 0, len(s))
	prevDash := true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c+32)
			prevDash = false
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
			prevDash = false
		default:
			if !prevDash {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	return strings.Trim(string(out), "-")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

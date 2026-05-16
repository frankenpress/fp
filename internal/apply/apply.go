// Package apply wires the fp apply subcommand end-to-end.
//
// Same Runner-shaped seam as snapshot — the orchestrator takes a
// docker.Runner, resolves the host-side snapshot dir, docker-cp's it
// into the container at /app/<rel-to-repo-root>, then streams
// `wp fp apply --snapshot-dir=...` through. The mu-plugin's apply is
// idempotent (markers short-circuit re-applies); fp surfaces the
// outcome cleanly.
package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/frankenpress/fp/internal/compose"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/summary"
)

// Options carries every input fp apply needs. Built from flags + env
// in internal/cli/apply.go.
type Options struct {
	// Positional arg from the CLI — slug or path. Slug is interpreted
	// against config.Snapshot.OutputDir (defaults to web/imports);
	// path is interpreted relative to cwd, falling back to absolute.
	// See resolveSnapshotDir for the precise semantics.
	Target string

	RepoRoot string
	Config   *config.Config
	Runner   docker.Runner

	Stdout io.Writer
	Stderr io.Writer

	Service string // overrides config.Snapshot.Service
	Project string // overrides config.Snapshot.Project
}

// Run executes the apply pipeline.
func Run(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}

	service := firstNonEmpty(opts.Service, opts.Config.Snapshot.Service, "site")
	project := firstNonEmpty(opts.Project, opts.Config.Snapshot.Project, compose.DefaultProject(opts.RepoRoot))
	outputDir := firstNonEmpty(opts.Config.Snapshot.OutputDir, "web/imports")

	// No positional → pick the latest snapshot by manifest.created
	// across BOTH the committed output dir AND the pulled-from-prod
	// dir (.fp/prod-snapshots/). Same pick-latest semantics the charts
	// install Job uses, so `fp apply` (locally) and the in-cluster
	// apply target the same snapshot when applied against the same
	// source. Slug collisions across dirs hard-error.
	const pullDir = ".fp/prod-snapshots"
	var hostSnapshotDir, relToRoot string
	var err error
	if opts.Target == "" {
		_, dir, perr := PickLatestFromDirs(opts.RepoRoot, []string{outputDir, pullDir})
		if perr != nil {
			return perr
		}
		hostSnapshotDir = dir
		relToRoot, err = filepath.Rel(opts.RepoRoot, dir)
		if err != nil {
			return fmt.Errorf("compute relative path: %w", err)
		}
	} else {
		hostSnapshotDir, relToRoot, err = resolveSnapshotDir(opts.RepoRoot, outputDir, pullDir, opts.Target)
		if err != nil {
			return err
		}
	}

	manifestPath := filepath.Join(hostSnapshotDir, "manifest.yaml")
	manifest, err := summary.Read(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest at %s: %w", manifestPath, err)
	}
	slug := filepath.Base(hostSnapshotDir)

	// Compose stack must be up.
	status, container, err := compose.Check(ctx, opts.Runner, project, service)
	if err != nil {
		return fmt.Errorf("docker compose ps failed: %w", err)
	}
	if status != compose.StatusServiceRunning {
		return errors.New(compose.FormatNotRunning(status, project, service))
	}
	if container == nil {
		return fmt.Errorf("docker compose reported %q running but returned no container metadata", service)
	}

	containerSnapshotDir := "/app/" + filepath.ToSlash(relToRoot)
	containerParent := "/app/" + filepath.ToSlash(filepath.Dir(relToRoot))
	if containerParent == "/app/." {
		containerParent = "/app"
	}

	fmt.Fprintf(opts.Stdout, "[fp] applying %q via %s/%s\n", slug, project, service)
	fmt.Fprintf(opts.Stdout, "  manifest: %s\n", filepath.Join(relToRoot, "manifest.yaml"))
	if manifest.Adapter != "" {
		fmt.Fprintf(opts.Stdout, "  adapter:  %s\n", manifest.Adapter)
	}

	// Ensure the parent container dir exists. mkdir -p is cheap;
	// running it unconditionally avoids docker cp's "no such file or
	// directory" when the path doesn't exist yet in the live image.
	if _, _, err := opts.Runner.ComposeExec(ctx, project, service, []string{"mkdir", "-p", containerParent}); err != nil {
		return fmt.Errorf("ensure container dir %s: %w", containerParent, err)
	}

	// Stage the snapshot dir into the container. docker cp creates
	// the basename inside the destination dir, so cp <host>/<slug>
	// → <container>:<parent>/ lands at <parent>/<slug>.
	dst := container.Name + ":" + containerParent
	if err := opts.Runner.Copy(ctx, hostSnapshotDir, dst); err != nil {
		fmt.Fprintf(opts.Stderr, "error: docker cp %s %s failed: %v\n", hostSnapshotDir, dst, err)
		return errors.New("docker cp into container failed")
	}

	// Run wp fp apply. Stream output so designers see WP-CLI progress
	// + the canonical "apply complete" / "apply skipped" trailer.
	wpArgs := []string{"wp", "--allow-root", "--path=/app/web/wp", "fp", "apply",
		"--snapshot-dir=" + containerSnapshotDir,
	}

	fmt.Fprintln(opts.Stdout, "--- wp-cli output ---")
	captured := &captureWriter{w: opts.Stdout}
	execErr := opts.Runner.ComposeExecStreaming(ctx, project, service, wpArgs, captured, opts.Stderr)
	fmt.Fprintln(opts.Stdout, "--- end wp-cli output ---")
	if execErr != nil {
		var ee *docker.ExecError
		exitCode := -1
		if errors.As(execErr, &ee) {
			exitCode = ee.ExitCode
		}
		fmt.Fprintf(opts.Stderr, "error: wp fp apply exited %d. See wp-cli output above for the verbatim message.\n", exitCode)
		return errors.New("wp fp apply failed")
	}

	fmt.Fprintln(opts.Stdout)
	if captured.skipped {
		fmt.Fprintf(opts.Stdout, "snapshot already applied: %s (idempotency markers matched; no-op)\n", slug)
	} else {
		fmt.Fprintf(opts.Stdout, "applied snapshot: %s\n", slug)
		if manifest.Source.SourceTheme != "" {
			fmt.Fprintf(opts.Stdout, "  source theme: %s\n", manifest.Source.SourceTheme)
		}
		if manifest.Contents.TemplatesCount > 0 {
			fmt.Fprintf(opts.Stdout, "  templates:    %d upserted\n", manifest.Contents.TemplatesCount)
		}
		if manifest.Contents.OptionsCount > 0 {
			fmt.Fprintf(opts.Stdout, "  options:      %d updated\n", manifest.Contents.OptionsCount)
		}
		if manifest.Contents.AttachmentsCount > 0 {
			fmt.Fprintf(opts.Stdout, "  attachments:  %d upserted\n", manifest.Contents.AttachmentsCount)
		}
	}

	return nil
}

// resolveSnapshotDir interprets target as either:
//   - a bare slug → <repoRoot>/<outputDir>/<target> first, then
//     <repoRoot>/<pullDir>/<target> (committed captures preferred over
//     pulled). Errors if the slug exists in both dirs.
//   - a relative path with separators → <cwd>/<target>, normalised
//   - an absolute path → as given
//
// Returns the absolute host path and the path relative to repoRoot
// (used to compute the container-side /app/<rel> mirror).
func resolveSnapshotDir(repoRoot, outputDir, pullDir, target string) (hostDir, relToRoot string, err error) {
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
		// Bare slug — try the committed dir first, then the pulled
		// dir. Error if both exist.
		committed := filepath.Join(repoRoot, outputDir, target)
		pulled := filepath.Join(repoRoot, pullDir, target)
		committedExists := isDir(committed)
		pulledExists := isDir(pulled)
		switch {
		case committedExists && pulledExists:
			return "", "", fmt.Errorf(
				"snapshot slug %q exists in both %s and %s. remove one or pass an explicit path",
				target, committed, pulled,
			)
		case committedExists:
			abs = committed
		case pulledExists:
			abs = pulled
		default:
			return "", "", fmt.Errorf(
				"snapshot dir not found: tried %s and %s",
				committed, pulled,
			)
		}
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
		return "", "", fmt.Errorf("snapshot dir %s is outside the repo root %s; the container can only see paths under the repo", abs, repoRoot)
	}

	return abs, rel, nil
}

// isDir is a tiny helper for resolveSnapshotDir's two-dir collision
// check. Returns false on any stat error.
func isDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// captureWriter tees output to the underlying writer and notes
// whether mu-plugin's "apply skipped" message appeared. WP_CLI emits
// the trailing `Success: apply skipped` / `Success: apply complete`
// line at the end; we sniff for the skipped case so the summary
// printer can word the result accurately.
type captureWriter struct {
	w       io.Writer
	skipped bool
	buf     bytes.Buffer
}

func (c *captureWriter) Write(p []byte) (int, error) {
	c.buf.Write(p)
	if !c.skipped && bytes.Contains(c.buf.Bytes(), []byte("apply skipped")) {
		c.skipped = true
	}
	return c.w.Write(p)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

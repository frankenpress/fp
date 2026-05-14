package setup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/frankenpress/fp/internal/apply"
	"github.com/frankenpress/fp/internal/compose"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
)

// Options carries every input fp init needs. Built from flags + env
// in internal/cli/init.go.
type Options struct {
	RepoRoot string
	Config   *config.Config
	Runner   docker.Runner

	Stdout io.Writer
	Stderr io.Writer

	// Flag values (zero means "not set").
	Slug        string // explicit snapshot slug for apply (Phase 3); empty → PickLatest
	SkipSetup   bool   // skip composer install + .env scaffolding
	NoApply     bool   // bring stack up + install WP but skip apply
	ReinstallWP bool   // drop existing WP install and re-install (Phase 3)
	Service     string // overrides config.Snapshot.Service
	Project     string // overrides config.Snapshot.Project
}

// Result carries what fp init did, for the post-run summary.
type Result struct {
	BootstrapRan    bool   // composer install fired
	EnvScaffolded   bool   // .env created from .env.example
	S3DesignerMode  bool   // FP_S3_DISABLED=0 was written into .env
	StackBrought    bool   // docker compose up fired (always true on success)
	WPInstalled     bool   // wp core install fired (false if already installed)
	SnapshotApplied string // slug that was applied (Phase 3); empty if no apply
	SnapshotsAbsent bool   // web/imports/ was empty / no snapshot to apply
	SiteURL         string // resolved WP_HOME or default
}

// Run executes the fp init pipeline.
//
// Phase 2 scope: bootstrap (composer + env scaffold) → designer-mode
// env line → docker compose up → wp core install if needed. Apply
// is gated on opts.NoApply (Phase 3 wires the apply path; until then
// NoApply is implicitly true).
func Run(ctx context.Context, opts Options) (*Result, error) {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Stderr == nil {
		opts.Stderr = io.Discard
	}
	if opts.Config == nil {
		return nil, errors.New("setup.Run: Config is required")
	}
	if opts.RepoRoot == "" {
		opts.RepoRoot = opts.Config.RepoRoot
	}
	if opts.Runner == nil {
		return nil, errors.New("setup.Run: Runner is required")
	}

	result := &Result{}

	project := firstNonEmpty(opts.Project, opts.Config.Snapshot.Project, compose.DefaultProject(opts.RepoRoot))
	service := firstNonEmpty(opts.Service, opts.Config.Snapshot.Service, "site")

	fmt.Fprintf(opts.Stdout, "[fp init] repo: %s\n", opts.RepoRoot)
	fmt.Fprintf(opts.Stdout, "[fp init] project: %s / service: %s\n", project, service)

	// Step 1: bootstrap (.env scaffold + composer install + designer-
	// mode S3 line). All three are env-setup operations gated by
	// --skip-setup, since "skip setup" means "I've prepared the
	// environment myself, don't second-guess it".
	if !opts.SkipSetup {
		if err := runBootstrap(ctx, opts, result); err != nil {
			return result, err
		}
	} else {
		fmt.Fprintln(opts.Stdout, "[fp init] --skip-setup: assuming .env + vendor/ are in place")
	}

	// Step 2: resolve the site URL fp will install WP with. WP_HOME
	// from .env wins; default matches site-template's compose default.
	siteURL := resolveSiteURL(opts.RepoRoot)
	result.SiteURL = siteURL

	// Step 4: bring the stack up. `docker compose up -d --wait` gates
	// on healthchecks (or readiness, where no healthcheck is defined),
	// so when this returns the stack is actually ready for wp-cli.
	fmt.Fprintln(opts.Stdout, "[fp init] bringing stack up (docker compose up -d --wait)")
	if err := opts.Runner.ComposeUp(ctx, project, opts.Stdout, opts.Stderr); err != nil {
		return result, fmt.Errorf("docker compose up: %w", err)
	}
	result.StackBrought = true

	// Step 5: install WordPress if needed. is-installed exits 0 when
	// WP is installed, non-zero otherwise.
	installed, err := wpIsInstalled(ctx, opts, project, service)
	if err != nil {
		return result, err
	}
	if !installed {
		if err := wpCoreInstall(ctx, opts, project, service, siteURL); err != nil {
			return result, err
		}
		result.WPInstalled = true
	} else {
		fmt.Fprintln(opts.Stdout, "[fp init] WordPress already installed; skipping wp core install")
	}

	// Step 6 (Phase 3, gated on !opts.NoApply): apply the latest
	// snapshot. Stubbed here so Phase 2 ships independently.
	if opts.NoApply {
		fmt.Fprintln(opts.Stdout, "[fp init] --no-apply: stopping after stack-up + WP install")
		return result, nil
	}

	if err := runApply(ctx, opts, project, service, result); err != nil {
		return result, err
	}

	return result, nil
}

// runBootstrap covers steps 1a-1c of fp init's "fresh clone" path:
//   - copy .env.example → .env when absent
//   - composer install when vendor/ is absent
//
// Each step is independently idempotent. Bootstrap is skipped wholesale
// by --skip-setup.
func runBootstrap(ctx context.Context, opts Options, result *Result) error {
	// .env scaffold.
	created, err := ScaffoldEnvFromExample(opts.RepoRoot)
	if err != nil {
		return fmt.Errorf("scaffold .env: %w", err)
	}
	if created {
		fmt.Fprintln(opts.Stdout, "[fp init] created .env from .env.example")
		result.EnvScaffolded = true
	}

	// Composer install if vendor/ is absent.
	vendorPath := filepath.Join(opts.RepoRoot, "vendor")
	if missing, err := dirMissing(vendorPath); err != nil {
		return fmt.Errorf("stat vendor/: %w", err)
	} else if missing {
		fmt.Fprintln(opts.Stdout, "[fp init] running composer install (vendor/ absent)")
		if err := opts.Runner.ComposerInstall(ctx, opts.RepoRoot, opts.Stdout, opts.Stderr); err != nil {
			return fmt.Errorf("composer install: %w", err)
		}
		result.BootstrapRan = true
	}

	// Designer-mode S3 (unless operator opted out in toml). Lives in
	// runBootstrap so --skip-setup gates it consistently — designers
	// who pre-prepare their .env shouldn't have fp init re-write it.
	if !opts.Config.Init.DisableS3 {
		appended, err := EnsureEnvKey(
			filepath.Join(opts.RepoRoot, ".env"),
			"FP_S3_DISABLED",
			"0",
			"# fp init: designer-mode S3 (MinIO). Set to 1 manually if you need wp-admin zip installers.",
		)
		if err != nil {
			return fmt.Errorf("set designer-mode env: %w", err)
		}
		if appended {
			fmt.Fprintln(opts.Stdout, "[fp init] wrote FP_S3_DISABLED=0 to .env (designer-mode S3)")
			result.S3DesignerMode = true
		}
	}
	return nil
}

// resolveSiteURL returns the URL fp init will pass to `wp core install
// --url=`. Priority: WP_HOME in .env > the site-template compose
// default (http://localhost:8080).
func resolveSiteURL(repoRoot string) string {
	envPath := filepath.Join(repoRoot, ".env")
	if v, found, err := ReadEnvKey(envPath, "WP_HOME"); err == nil && found {
		// Strip optional surrounding quotes — operators sometimes
		// quote env values; wp-cli wants them bare.
		v = strings.Trim(v, `"'`)
		if v != "" {
			return v
		}
	}
	return "http://localhost:8080"
}

// wpIsInstalled wraps `wp core is-installed`. Exit 0 = installed,
// non-zero = not installed. We treat any error as "not installed" so
// a fresh DB (with no wp_options table) doesn't crash the orchestrator
// — wp_core_install will be the response either way.
func wpIsInstalled(ctx context.Context, opts Options, project, service string) (bool, error) {
	_, _, err := opts.Runner.ComposeExec(ctx, project, service, []string{
		"wp", "--allow-root", "--path=/app/web/wp",
		"core", "is-installed",
	})
	if err == nil {
		return true, nil
	}
	// Any non-zero exit means not installed. The site-template's
	// fresh-DB error path emits a "Table 'wordpress.wp_options'
	// doesn't exist" cascade with a non-zero exit, which we want
	// to interpret as "needs install" rather than re-surface.
	var ee *docker.ExecError
	if errors.As(err, &ee) {
		return false, nil
	}
	// Other docker errors (compose not running, container missing)
	// are real failures and should propagate.
	return false, fmt.Errorf("wp core is-installed: %w", err)
}

// wpCoreInstall fires the install. Args mirror site-template's
// quickstart copy (admin/admin/admin@example.test by default; the
// [init] section of frankenpress.toml overrides each).
func wpCoreInstall(ctx context.Context, opts Options, project, service, siteURL string) error {
	fmt.Fprintf(opts.Stdout, "[fp init] installing WordPress (url=%s, admin=%s)\n", siteURL, opts.Config.Init.AdminUser)
	args := []string{
		"wp", "--allow-root", "--path=/app/web/wp",
		"core", "install",
		"--url=" + siteURL,
		"--title=" + opts.Config.Init.SiteTitle,
		"--admin_user=" + opts.Config.Init.AdminUser,
		"--admin_password=" + opts.Config.Init.AdminPassword,
		"--admin_email=" + opts.Config.Init.AdminEmail,
		"--skip-email",
	}
	if err := opts.Runner.ComposeExecStreaming(ctx, project, service, args, opts.Stdout, opts.Stderr); err != nil {
		return fmt.Errorf("wp core install: %w", err)
	}
	return nil
}

// runApply is the Phase 3 placeholder. For Phase 2 it should never
// be reached (opts.NoApply is implicitly true at the CLI layer
// until Phase 3 wires the flag). Defined here so Phase 3 just fills
// the body in without re-arranging Run().
func runApply(ctx context.Context, opts Options, project, service string, result *Result) error {
	// Resolve the slug.
	outputDir := firstNonEmpty(opts.Config.Snapshot.OutputDir, "web/imports")
	target := opts.Slug
	if target == "" {
		slug, _, err := apply.PickLatest(opts.RepoRoot, outputDir)
		if err != nil {
			// Empty `web/imports/` → no snapshot to apply yet, treat
			// as a soft "ready, but no design state yet" and return
			// cleanly. PickLatest currently signals this by including
			// "fp snapshot" in the error string.
			if strings.Contains(err.Error(), "fp snapshot") {
				fmt.Fprintln(opts.Stdout, "[fp init] no snapshot to apply yet — make some changes and run `fp snapshot`")
				result.SnapshotsAbsent = true
				return nil
			}
			return fmt.Errorf("pick latest snapshot: %w", err)
		}
		target = slug
	}

	fmt.Fprintf(opts.Stdout, "[fp init] applying snapshot: %s\n", target)
	applyOpts := apply.Options{
		Target:   target,
		RepoRoot: opts.RepoRoot,
		Config:   opts.Config,
		Runner:   opts.Runner,
		Stdout:   opts.Stdout,
		Stderr:   opts.Stderr,
		Service:  opts.Service,
		Project:  opts.Project,
	}
	if err := apply.Run(ctx, applyOpts); err != nil {
		return err
	}
	result.SnapshotApplied = target
	return nil
}

// dirMissing returns true when `path` does not exist OR exists but
// is not a directory. Other stat errors propagate.
func dirMissing(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return true, nil
	}
	return false, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

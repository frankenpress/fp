// Package config parses the site-repo's frankenpress.toml file.
//
// fp finds the file by walking up from cwd to the filesystem root.
// The first directory containing frankenpress.toml is the site-repo
// root for the purposes of every other fp subcommand (output paths,
// state file location, git status checks). Composer.json is the
// fallback marker if frankenpress.toml is absent — the empty TOML is
// a valid config, but a totally-foreign directory is not.
//
// Only the [snapshot] section is consumed by fp; [site] and [signers]
// are read by other tools (cosign verify in the install Job, etc.)
// and are passed through untouched.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// File name fp looks for when walking up from cwd.
const fileName = "frankenpress.toml"

// ErrRepoRootNotFound is returned when neither frankenpress.toml nor
// composer.json is found above cwd. The plan's Error-UX table (g)
// matches this case to the "not inside a FrankenPress site repo" hint.
var ErrRepoRootNotFound = errors.New("not inside a FrankenPress site repo")

// ErrTOMLParse wraps a TOML parse error. The plan's Error-UX table (f)
// catches this verbatim: an unparseable config is a bug to fix, not
// something to silently fall back from.
type ErrTOMLParse struct {
	Path string
	Err  error
}

func (e *ErrTOMLParse) Error() string {
	return fmt.Sprintf("frankenpress.toml at %s is not valid TOML: %v", e.Path, e.Err)
}

func (e *ErrTOMLParse) Unwrap() error { return e.Err }

// Config is the parsed view of frankenpress.toml. RepoRoot is the
// directory the config was found in (or, if absent, the directory
// containing composer.json). Path is the absolute path to the TOML
// file when present, empty string when fp defaulted everything.
type Config struct {
	RepoRoot string
	Path     string

	Snapshot SnapshotConfig
	Init     InitConfig
	Pull     PullConfig
}

// SnapshotConfig mirrors the [snapshot] TOML section. Every key is
// optional; zero values mean "use the auto-detected default" and the
// snapshot orchestrator fills them in.
type SnapshotConfig struct {
	// Project is the docker-compose project name. Defaults to
	// basename(repo-root), which matches docker-compose v2's own
	// default. Override only when COMPOSE_PROJECT_NAME is baked
	// into a Makefile or compose file.
	Project string `toml:"project"`
	// Service is the compose service running WordPress. Defaults
	// to "site".
	Service string `toml:"service"`
	// OutputDir is the host-side directory snapshots land in,
	// relative to RepoRoot. Defaults to "web/imports".
	OutputDir string `toml:"output_dir"`
	// ContainerOutputDir is the in-container path snapshots are
	// written to (the --output-dir value passed to wp fp snapshot).
	// Defaults to "/app/web/imports" (matches the Bedrock layout
	// site-template ships with).
	ContainerOutputDir string `toml:"container_output_dir"`
}

// InitConfig mirrors the [init] TOML section. Every key is optional;
// zero values mean "use the fp init default" and the setup
// orchestrator fills them in. These knobs are local-dev conveniences;
// production deploys don't touch them (cluster installs land via the
// chart's siteInstall.* values).
type InitConfig struct {
	// SiteTitle is the value passed to `wp core install --title=...`
	// on a fresh DB. Defaults to "FrankenPress site".
	SiteTitle string `toml:"site_title"`
	// AdminUser is the WP admin login on a fresh DB. Defaults to
	// "admin" — local-only convenience. Override for any environment
	// other than docker-compose-on-a-laptop.
	AdminUser string `toml:"admin_user"`
	// AdminPassword is the WP admin password on a fresh DB. Defaults
	// to "admin". Same local-only-convenience caveat.
	AdminPassword string `toml:"admin_password"`
	// AdminEmail is the WP admin email on a fresh DB. Defaults to
	// "admin@example.test".
	AdminEmail string `toml:"admin_email"`
	// DisableS3, when true, tells fp init NOT to write
	// FP_S3_DISABLED=0 into .env. Use this when you specifically
	// want the container's local-disk uploads path (e.g. you're
	// iterating on a wp-admin plugin installer that doesn't work
	// under the s3:// stream wrapper). Default false → fp init
	// enables designer-mode S3 (MinIO).
	DisableS3 bool `toml:"disable_s3"`
}

// PullConfig mirrors the [pull] TOML section. Required only when
// `fp pull` is invoked; sites that don't pull prod content leave it
// unset.
type PullConfig struct {
	// Bucket is the snapshot bucket name (e.g.
	// "sts-production-snapshots-eu-west-2-533158516642"). Required
	// when `fp pull` runs. fp does NOT synthesise it from
	// site/env/region — keep the CLI decoupled from the
	// tg_frankenpress naming convention.
	Bucket string `toml:"bucket"`
	// Profile is passed as `aws --profile <name>` on every shell-out.
	// Optional — leave empty to let aws's own resolution win (env vars
	// inside aws-vault exec, AWS_PROFILE, ~/.aws/credentials).
	Profile string `toml:"profile"`
	// Region is passed as `aws --region <name>`. Optional — leave
	// empty for aws's standard resolution. Set when the bucket lives
	// in a region that doesn't match the designer's default.
	Region string `toml:"region"`
}

// Defaults returns a SnapshotConfig with every default filled in,
// useful for tests and as the baseline merged onto an empty file.
func Defaults() SnapshotConfig {
	return SnapshotConfig{
		Service:            "site",
		OutputDir:          "web/imports",
		ContainerOutputDir: "/app/web/imports",
	}
}

// DefaultsInit returns the InitConfig defaults. Separate function so
// callers that want them independently of the snapshot defaults can
// grab one without the other.
func DefaultsInit() InitConfig {
	return InitConfig{
		SiteTitle:     "FrankenPress site",
		AdminUser:     "admin",
		AdminPassword: "admin",
		AdminEmail:    "admin@example.test",
		DisableS3:     false,
	}
}

// rawConfig is the on-disk TOML shape. Other sections ([site],
// [signers]) are tolerated and discarded — fp doesn't care about them.
type rawConfig struct {
	Snapshot SnapshotConfig `toml:"snapshot"`
	Init     InitConfig     `toml:"init"`
	Pull     PullConfig     `toml:"pull"`
}

// Load walks up from startDir (or cwd when empty), parses
// frankenpress.toml if found, fills in defaults, and returns the
// merged Config. If neither frankenpress.toml nor composer.json is
// found, returns ErrRepoRootNotFound.
func Load(startDir string) (*Config, error) {
	if startDir == "" {
		var err error
		startDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("config: getwd: %w", err)
		}
	}

	tomlPath, repoRoot, err := findUp(startDir)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		RepoRoot: repoRoot,
		Path:     tomlPath,
		Snapshot: Defaults(),
		Init:     DefaultsInit(),
	}

	if tomlPath == "" {
		// No TOML file — only composer.json. Defaults stand.
		return cfg, nil
	}

	data, err := os.ReadFile(tomlPath)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", tomlPath, err)
	}
	var raw rawConfig
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, &ErrTOMLParse{Path: tomlPath, Err: err}
	}

	mergeSnapshot(&cfg.Snapshot, raw.Snapshot)
	mergeInit(&cfg.Init, raw.Init)
	mergePull(&cfg.Pull, raw.Pull)
	return cfg, nil
}

func mergePull(dst *PullConfig, src PullConfig) {
	if src.Bucket != "" {
		dst.Bucket = src.Bucket
	}
	if src.Profile != "" {
		dst.Profile = src.Profile
	}
	if src.Region != "" {
		dst.Region = src.Region
	}
}

func mergeInit(dst *InitConfig, src InitConfig) {
	if src.SiteTitle != "" {
		dst.SiteTitle = src.SiteTitle
	}
	if src.AdminUser != "" {
		dst.AdminUser = src.AdminUser
	}
	if src.AdminPassword != "" {
		dst.AdminPassword = src.AdminPassword
	}
	if src.AdminEmail != "" {
		dst.AdminEmail = src.AdminEmail
	}
	if src.DisableS3 {
		// Only respect true; default false survives.
		dst.DisableS3 = true
	}
}

// findUp walks from start up to "/" looking for frankenpress.toml
// first, then composer.json as a fallback marker. Returns the TOML
// path (empty when only composer.json was found) and the directory
// that should be treated as the repo root.
func findUp(start string) (tomlPath, repoRoot string, err error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", "", err
	}

	dir := abs
	var composerRoot string
	for {
		candidate := filepath.Join(dir, fileName)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, dir, nil
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return "", "", err
		}

		if composerRoot == "" {
			if _, err := os.Stat(filepath.Join(dir, "composer.json")); err == nil {
				composerRoot = dir
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if composerRoot != "" {
		return "", composerRoot, nil
	}
	return "", "", ErrRepoRootNotFound
}

// mergeSnapshot overlays non-empty values from src onto dst.
// Anything the user left blank in TOML stays at its default.
func mergeSnapshot(dst *SnapshotConfig, src SnapshotConfig) {
	if src.Project != "" {
		dst.Project = src.Project
	}
	if src.Service != "" {
		dst.Service = src.Service
	}
	if src.OutputDir != "" {
		dst.OutputDir = src.OutputDir
	}
	if src.ContainerOutputDir != "" {
		dst.ContainerOutputDir = src.ContainerOutputDir
	}
}

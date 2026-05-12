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

// Defaults returns a SnapshotConfig with every default filled in,
// useful for tests and as the baseline merged onto an empty file.
func Defaults() SnapshotConfig {
	return SnapshotConfig{
		Service:            "site",
		OutputDir:          "web/imports",
		ContainerOutputDir: "/app/web/imports",
	}
}

// rawConfig is the on-disk TOML shape. Other sections ([site],
// [signers]) are tolerated and discarded — fp doesn't care about them.
type rawConfig struct {
	Snapshot SnapshotConfig `toml:"snapshot"`
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
	return cfg, nil
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

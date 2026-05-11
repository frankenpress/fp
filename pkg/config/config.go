// Package config loads `frankenpress.toml` — the per-site config file
// fp reads to identify which site repo it's operating against.
//
// As of fp v0.4.0 the config is intentionally small: site identity +
// (future) cosign signer allowlist. The previous v0.2/v0.3 design
// also carried snapshots-bucket + gitops-repo configuration, but
// those went away when snapshots became image-baked artefacts
// committed into the site repo (see frankenpress/mu-plugin v0.8.0
// and frankenpress/charts v0.9.0 for the design rewrite).
//
// Example frankenpress.toml at a tenant site repo root:
//
//	[site]
//	tenant = "EightOEight"
//	name   = "sts"
//	repo   = "EightOEight/sts"
//
//	[signers]
//	identities = ["m.kennedy@aypex.io"]
//
// Lives in pkg/ so third-party tooling can import the same schema.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Filename is the conventional name for the per-site config. fp looks
// for it at the cwd and walks up to the repo root.
const Filename = "frankenpress.toml"

// Config is the deserialised frankenpress.toml.
type Config struct {
	Site    Site    `toml:"site"`
	Signers Signers `toml:"signers"`

	// Path is the absolute path the config was loaded from. Set by
	// Load(); useful for error messages.
	Path string `toml:"-"`
}

// Site describes the tenant + site identity.
type Site struct {
	Tenant string `toml:"tenant"`
	Name   string `toml:"name"`
	Repo   string `toml:"repo"` // "<owner>/<name>"
}

// Signers gates who is allowed to promote (Phase 5 cosign verify in
// the chart's install Job will gate on this list).
type Signers struct {
	Identities []string `toml:"identities"`
}

// Load reads frankenpress.toml. Path is searched starting at startDir
// (defaulting to cwd if empty) and walking up parent directories.
// Returns ErrNotFound if no config is found in the walk.
func Load(startDir string) (*Config, error) {
	if startDir == "" {
		d, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("config: getwd: %w", err)
		}
		startDir = d
	}

	path, err := findConfig(startDir)
	if err != nil {
		return nil, err
	}

	var cfg Config
	meta, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		// Strict mode: unknown keys are a footgun (typo in a critical
		// field silently misses). Report them. Catches stale v0.2/v0.3
		// keys ([snapshots], [gitops]) too.
		var keys []string
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("config: unknown keys in %s: %v (note: [snapshots] and [gitops] sections were removed in fp v0.4.0 — see the migration guide)", path, keys)
	}

	cfg.Path = path
	return &cfg, nil
}

// ErrNotFound is returned by Load when no frankenpress.toml is found
// in the start dir or any of its parents.
var ErrNotFound = errors.New("config: frankenpress.toml not found")

func findConfig(startDir string) (string, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, Filename)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%w (searched from %s up)", ErrNotFound, startDir)
		}
		dir = parent
	}
}

// Validate checks the loaded config has the required fields filled
// in. Returns nil on success, or a composite error listing problems.
func (c *Config) Validate() error {
	var problems []string
	add := func(key, why string) {
		problems = append(problems, fmt.Sprintf("  - %s: %s", key, why))
	}

	if c.Site.Name == "" {
		add("site.name", "required (used as snapshot output-dir slug + log identifier)")
	}
	if c.Site.Repo == "" {
		add("site.repo", "required (owner/name; identifies the site for cross-tool reporting)")
	}

	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("config: %s has missing required fields:\n%s", c.Path, joinLines(problems))
}

func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}

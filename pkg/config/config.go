// Package config loads `frankenpress.toml` — the per-site configuration
// file that fp reads to figure out where snapshot blobs go (S3 bucket)
// and where to open the gitops PR (gitops_repo + applicationset path).
//
// Lives in pkg/ so third-party tooling (a future fleet-management UI,
// say) can import the same schema. Phase 2 keeps the field set tight;
// new fields land additively as later phases need them.
//
// Example frankenpress.toml at a tenant site repo root:
//
//	[site]
//	tenant = "EightOEight"
//	name   = "sts"
//	repo   = "EightOEight/sts"
//
//	[snapshots]
//	bucket = "sts-snapshots"
//
//	[gitops]
//	repo            = "aypex-io/gitops-fp"
//	applicationset  = "apps/applicationset.yaml"
//	site_key        = "sts"
//
//	[signers]
//	identities = ["m.kennedy@aypex.io"]
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
	Site      Site      `toml:"site"`
	Snapshots Snapshots `toml:"snapshots"`
	Gitops    Gitops    `toml:"gitops"`
	Signers   Signers   `toml:"signers"`

	// Path is the absolute path the config was loaded from. Set by
	// Load(); useful for error messages and resolving relative paths
	// within Gitops.Applicationset.
	Path string `toml:"-"`
}

// Site describes the tenant + site identity.
type Site struct {
	Tenant string `toml:"tenant"`
	Name   string `toml:"name"`
	Repo   string `toml:"repo"` // "<owner>/<name>" — used by gh CLI invocations
}

// Snapshots describes where snapshot blobs are stored.
type Snapshots struct {
	Bucket string `toml:"bucket"`
}

// Gitops describes the gitops-fp repo + the applicationset.yaml path
// the chart's siteInstall.snapshot block lives in.
type Gitops struct {
	Repo           string `toml:"repo"`           // "<owner>/<name>"
	Applicationset string `toml:"applicationset"` // relative path inside the gitops repo
	SiteKey        string `toml:"site_key"`       // matches `site: <key>` in the matrix
}

// Signers gates who is allowed to promote (Phase 4+ cosign verify).
type Signers struct {
	Identities []string `toml:"identities"`
}

// Load reads frankenpress.toml. Path is searched starting at startDir
// (defaulting to the cwd if empty) and walking up parent directories
// until found or the filesystem root is reached. Returns ErrNotFound
// if no config is found in the walk.
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
		// field would silently miss). Report them.
		var keys []string
		for _, k := range undecoded {
			keys = append(keys, k.String())
		}
		return nil, fmt.Errorf("config: unknown keys in %s: %v", path, keys)
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

// Validate checks the loaded config has the required fields filled in
// for fp promote to do useful work. Returns nil on success, or a
// composite error listing all problems.
func (c *Config) Validate() error {
	var problems []string
	add := func(key, why string) {
		problems = append(problems, fmt.Sprintf("  - %s: %s", key, why))
	}

	if c.Site.Name == "" {
		add("site.name", "required (matrix entry's site field)")
	}
	if c.Site.Repo == "" {
		add("site.repo", "required (owner/name; gh uses it for PR opening)")
	}
	if c.Snapshots.Bucket == "" {
		add("snapshots.bucket", "required (S3 bucket for snapshot blobs)")
	}
	if c.Gitops.Repo == "" {
		add("gitops.repo", "required (owner/name of the gitops repo to open promote PRs against)")
	}
	if c.Gitops.Applicationset == "" {
		add("gitops.applicationset", "required (path within gitops.repo to the ApplicationSet manifest)")
	}
	if c.Gitops.SiteKey == "" {
		add("gitops.site_key", "required (matrix entry key — usually same as site.name)")
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

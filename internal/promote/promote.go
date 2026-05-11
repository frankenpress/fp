// Package promote orchestrates `fp promote stg|prd <snapshot>`.
//
// Three pieces:
//
//   - Uploader  — pushes snapshot artefacts to the configured S3
//     bucket via the awscli (shells out; matches the
//     Phase-0 Makefile pattern designers already use).
//   - PROpener  — opens a PR against the configured gitops repo
//     (shells out to `gh pr create`); the PR body is
//     produced by the gitops_change package.
//   - Promoter  — drives the workflow end-to-end and returns the
//     promote outcome (PR URL + s3 keys).
//
// Cosign signing of the manifest + the second PR against the site
// repo (composer-patch.json review) land in v0.3.0 / v0.4.0. The
// v0.2.0 surface is deliberately tight: get the blob upload + the
// gitops PR open paths working before adding the next layers.
package promote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/frankenpress/fp/pkg/manifest"
)

// Env names the target environment for a promote.
type Env string

const (
	// EnvStaging targets the staging values file in the gitops repo.
	EnvStaging Env = "stg"
	// EnvProduction targets the production values file.
	EnvProduction Env = "prd"
)

// Valid reports whether the string is a recognised promote target.
func (e Env) Valid() bool {
	switch e {
	case EnvStaging, EnvProduction:
		return true
	default:
		return false
	}
}

// LoadManifest reads manifest.json from a snapshot directory and
// returns the typed Manifest. Surfaces a clear error if the directory
// is malformed (missing manifest, wrong schema, etc.) — callers
// shouldn't need to disambiguate.
func LoadManifest(snapshotDir string) (*manifest.Manifest, error) {
	jsonPath := filepath.Join(snapshotDir, "manifest.json")
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("promote: read %s: %w", jsonPath, err)
	}
	m, err := manifest.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("promote: parsing %s: %w", jsonPath, err)
	}
	return m, nil
}

// SnapshotArtefacts lists the files (relative to the snapshot dir)
// that the uploader pushes to S3 for a v1 snapshot.
//
// db.sql.gz is the only required-present blob; the manifests +
// composer-patch + uploads-manifest are required-present text files.
func SnapshotArtefacts() []string {
	return []string{
		"manifest.yaml",
		"manifest.json",
		"composer-patch.json",
		"uploads-manifest.txt",
		"db.sql.gz",
	}
}

// Uploader pushes snapshot blobs to S3 via `aws s3 cp`. Shell-out
// rather than the AWS SDK keeps the dependency tree small and lets
// the designer's existing AWS credential chain (`~/.aws/`,
// AWS_PROFILE, IAM Identity Center, etc.) Just Work.
type Uploader struct {
	Bucket string // e.g. "sts-snapshots"
	// Stdout / Stderr receive aws CLI output for progress.
	Stdout io.Writer
	Stderr io.Writer
}

// Upload pushes every file in SnapshotArtefacts() from snapshotDir to
// `s3://<Bucket>/snapshots/<id>/`. Returns the S3 key prefix the
// chart's `siteInstall.snapshot.s3Key` should be set to.
//
// Missing files are reported individually so a half-built snapshot
// dir fails fast (rather than producing a half-uploaded result).
func (u *Uploader) Upload(ctx context.Context, snapshotDir, snapshotID string) (string, error) {
	if u.Bucket == "" {
		return "", fmt.Errorf("promote: upload: bucket is empty (snapshots.bucket in frankenpress.toml)")
	}
	if snapshotID == "" {
		return "", fmt.Errorf("promote: upload: snapshot id is empty")
	}

	for _, name := range SnapshotArtefacts() {
		if _, err := os.Stat(filepath.Join(snapshotDir, name)); err != nil {
			return "", fmt.Errorf("promote: upload: missing snapshot artefact %s/%s: %w", snapshotDir, name, err)
		}
	}

	prefix := "snapshots/" + snapshotID + "/"
	stdout := u.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := u.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	for _, name := range SnapshotArtefacts() {
		src := filepath.Join(snapshotDir, name)
		dst := fmt.Sprintf("s3://%s/%s%s", u.Bucket, prefix, name)
		cmd := exec.CommandContext(ctx, "aws", "s3", "cp", src, dst)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("promote: upload %s → %s: %w", src, dst, err)
		}
	}

	return prefix, nil
}

// PROpener opens a PR against the gitops repo with the
// `siteInstall.snapshot` values bump for the requested env.
type PROpener struct {
	GitopsRepo     string // "owner/name"
	Applicationset string // relative path inside the gitops repo
	SiteKey        string // matrix entry key
	Bucket         string // S3 bucket name (echoed in the PR body)

	// Stdout / Stderr receive gh CLI output. Stdout (specifically) is
	// captured for the returned PR URL — gh prints it as the last
	// line of its successful create output.
	Stdout io.Writer
	Stderr io.Writer
}

// Open opens the gitops-fp PR. Phase 2.0 emits a structured body that
// the engineer manually applies (gitops-fp owns its own values-file
// layout; we don't want fp to clone/edit/push a remote repo from a
// designer's laptop without explicit per-tenant config). Phase 2.1
// promotes this to a true automated PR open via `gh api` + repo
// editing once the path conventions stabilise.
//
// Returns the URL of the opened PR (or empty + error on failure).
func (p *PROpener) Open(ctx context.Context, env Env, snapshotID, s3Key string) (string, error) {
	if !env.Valid() {
		return "", fmt.Errorf("promote: invalid env %q (must be stg or prd)", env)
	}
	if p.GitopsRepo == "" {
		return "", fmt.Errorf("promote: gitops repo is empty (gitops.repo in frankenpress.toml)")
	}

	body := p.renderBody(env, snapshotID, s3Key)

	title := fmt.Sprintf("%s(%s): bump snapshot to %s", p.SiteKey, env, snapshotID)

	// For v0.2.0 we use `gh issue create` rather than `gh pr create`:
	// opening a real PR requires us to checkout the repo, edit the
	// file, push a branch — that's structural work the gitops repo
	// owner should sign off on for each tenant. An issue captures the
	// promote request reviewably; the engineer flips it to a PR in
	// gitops-fp manually for the first iteration. Phase 2.1 upgrades
	// this to a full automated PR open.
	cmd := exec.CommandContext(ctx, "gh", "issue", "create",
		"--repo", p.GitopsRepo,
		"--title", title,
		"--body", body,
	)
	var stdoutBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	if p.Stderr != nil {
		cmd.Stderr = p.Stderr
	}
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("promote: `gh issue create`: %w", err)
	}
	if p.Stdout != nil {
		_, _ = p.Stdout.Write(stdoutBuf.Bytes())
	}

	url := strings.TrimSpace(stdoutBuf.String())
	// gh prints the URL as the last non-empty line.
	if idx := strings.LastIndex(url, "\n"); idx >= 0 {
		url = strings.TrimSpace(url[idx+1:])
	}
	return url, nil
}

func (p *PROpener) renderBody(env Env, snapshotID, s3Key string) string {
	values := map[string]any{
		"site": p.SiteKey,
		"env":  string(env),
		"snapshot": map[string]string{
			"ref":    snapshotID,
			"s3Key":  s3Key,
			"bucket": p.Bucket,
		},
	}
	jsonBody, _ := json.MarshalIndent(values, "", "  ")

	return fmt.Sprintf(`Promote request from fp (v0.2.0).

Bump `+"`"+`siteInstall.snapshot`+"`"+` in `+"`%s`"+` for `+"`%s`"+`:

`+"```yaml"+`
siteInstall:
  snapshot:
    ref:    %q
    s3Key:  %q
    bucket: %q
`+"```"+`

Structured form (for any tooling that wants to consume this):

`+"```json"+`
%s
`+"```"+`

Blobs already uploaded to `+"`s3://%s/%s`"+`. Once this is merged + ArgoCD reconciles, the install Job's `+"`wp fp apply`"+` step will pull them down and stamp the idempotency markers (`+"`fp_snapshot_applied_ref`"+` + `+"`fp_snapshot_applied_sha256`"+`).

(Phase 2.1 of the fp design upgrades this issue into a real PR opened against the values file; v0.2.0 leaves the file-edit step to the engineer because the YAML structure varies per gitops layout.)
`, p.Applicationset, env, snapshotID, s3Key, p.Bucket, string(jsonBody), p.Bucket, s3Key)
}

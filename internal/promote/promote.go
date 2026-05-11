// Package promote orchestrates `fp promote stg|prd <snapshot>`.
//
// Pieces:
//
//   - Uploader   — pushes snapshot artefacts to the configured S3
//     bucket via the awscli (shells out; matches the
//     Phase-0 Makefile pattern designers already use).
//   - GitopsPR   — clones the gitops repo, edits the applicationset
//     via EditApplicationSet, commits + pushes + opens
//     a real PR via `gh pr create`.
//   - Env        — promote target enum (stg / prd).
//
// Cosign signing of the manifest + the second PR against the site
// repo (composer-patch.json review) land in v0.4 / v0.5. Manifest
// schema parsing lives in pkg/manifest.
package promote

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

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

package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/diff"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <snapshot-a> <snapshot-b>",
		Short: "Diff two committed snapshot directories",
		Long: `Pure host-side structural diff of two snapshot directories. Compares
the manifest, templates.json, options.json, attachments.json, and
uploads-manifest.txt of each side and prints a terminal-friendly
summary of additions, removals, and modifications.

Each positional arg is interpreted the same way fp apply interprets
its target:

  - a bare slug (e.g. "sts-launch") -> resolved against [snapshot].output_dir
  - a relative path with separators -> relative to your current directory
  - an absolute path

Typical use is comparing two snapshot versions side-by-side during PR
review, e.g.:

  fp diff sts-launch /tmp/old-sts-launch
  fp diff sts-launch-stg sts-launch-prd

For comparing the current live site state against a committed
snapshot, that needs a future mu-plugin "dump scope" command that
isn't shipped yet.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("")
			if err != nil {
				if errors.Is(err, config.ErrRepoRootNotFound) {
					return errors.New(
						"not inside a FrankenPress site repo. fp expects a frankenpress.toml or a composer.json with a frankenpress/* dep at or above cwd",
					)
				}
				return err
			}

			outputDir := cfg.Snapshot.OutputDir
			if outputDir == "" {
				outputDir = "web/imports"
			}

			pathA, err := resolveDiffTarget(cfg.RepoRoot, outputDir, args[0])
			if err != nil {
				return fmt.Errorf("a: %w", err)
			}
			pathB, err := resolveDiffTarget(cfg.RepoRoot, outputDir, args[1])
			if err != nil {
				return fmt.Errorf("b: %w", err)
			}

			snapA, err := diff.Read(pathA)
			if err != nil {
				return err
			}
			snapB, err := diff.Read(pathB)
			if err != nil {
				return err
			}

			res := diff.Compare(snapA, snapB)
			diff.Render(cmd.OutOrStdout(), res)
			return nil
		},
	}

	return cmd
}

// resolveDiffTarget is the same shape as apply's target resolver: slug,
// relative path, or absolute path. Unlike apply, diff does NOT require
// the target be inside the repo (a designer may want to compare against
// a snapshot stashed in /tmp).
func resolveDiffTarget(repoRoot, outputDir, target string) (string, error) {
	if target == "" {
		return "", errors.New("empty target")
	}
	var abs string
	switch {
	case filepath.IsAbs(target):
		abs = filepath.Clean(target)
	case strings.ContainsRune(target, filepath.Separator):
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("getwd: %w", err)
		}
		abs = filepath.Clean(filepath.Join(cwd, target))
	default:
		abs = filepath.Join(repoRoot, outputDir, target)
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("snapshot dir not found: %s", abs)
	}
	return abs, nil
}

package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/frankenpress/fp/internal/wpcli"
	"github.com/spf13/cobra"
)

func newSnapshotCmd() *cobra.Command {
	var (
		name      string
		note      string
		outputDir string
		service   string
		wpPath    string
	)

	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Capture local site state into a portable snapshot bundle",
		Long: `Runs ` + "`wp fp snapshot`" + ` inside the running site container,
producing a snapshot directory containing manifest.yaml, manifest.json,
content.xml.gz (WXR), options.json, composer-patch.json, and
uploads-manifest.txt.

The snapshot directory lands at <site-root>/web/imports/<slug>/ by
default — versioned alongside the rest of the site code in git, baked
into the site image at build time. Override with --output-dir for
non-standard layouts. The site must be up (` + "`make up`" + `).

Designer workflow:

  1. fp snapshot --name=architect-2 --note="The7 FSE Architect demo"
  2. Review web/imports/architect-2/manifest.yaml + composer-patch.json
  3. composer require any pending plugins
  4. git add web/imports/architect-2/ composer.json composer.lock
  5. git commit + open a site-repo PR — engineer reviews, merges
  6. CI rebuilds the site image; ArgoCD reconciles; install Job runs
     ` + "`wp fp apply`" + ` per snapshot subdir on the cluster side.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required (e.g. --name=architect-2)")
			}

			safeName := safeSlug(name)
			if safeName == "" {
				return fmt.Errorf("--name %q produced an empty safe slug; pick a name with at least one alphanumeric character", name)
			}

			dir := outputDir
			if dir == "" {
				dir = filepath.Join("web", "imports", safeName)
			}

			runner := wpcli.Runner{ComposeService: service, WordPressPath: wpPath}

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()

			if err := runner.EnsureSiteIsUp(ctx); err != nil {
				return err
			}

			containerDir := "/app/" + dir
			wpArgs := []string{
				"fp", "snapshot",
				"--slug=" + name,
				"--output-dir=" + containerDir,
			}
			if note != "" {
				wpArgs = append(wpArgs, "--note="+note)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "[fp] capturing snapshot %q → %s\n", name, dir)
			if err := runner.Run(ctx, cmd.OutOrStdout(), cmd.ErrOrStderr(), wpArgs...); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintf(cmd.OutOrStdout(), "snapshot written to %s/\n", dir)
			fmt.Fprintln(cmd.OutOrStdout(), "next steps:")
			fmt.Fprintf(cmd.OutOrStdout(), "  cat %s/manifest.yaml      # review what was captured\n", dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  cat %s/composer-patch.json # composer require any pending plugins\n", dir)
			fmt.Fprintf(cmd.OutOrStdout(), "  git add %s composer.json composer.lock\n", dir)
			fmt.Fprintln(cmd.OutOrStdout(), "  git commit && gh pr create")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Snapshot slug (lowercase + hyphens, e.g. \"architect-2\"). Required.")
	cmd.Flags().StringVar(&note, "note", "", "Optional designer note embedded in the manifest.")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Host-side output directory (relative paths are relative to the site repo). Defaults to web/imports/<slug>/.")
	cmd.Flags().StringVar(&service, "compose-service", "site", "docker-compose service name running the site container.")
	cmd.Flags().StringVar(&wpPath, "wp-path", "/app/web/wp", "In-container path to the WordPress install (--path argument to wp-cli).")

	return cmd
}

// safeSlug lower-cases and replaces any non-alphanumeric runs with a
// single dash, then trims leading / trailing dashes. Matches the
// PHP-side wp fp snapshot's safe_slug logic so directory names look
// identical regardless of which layer creates them.
func safeSlug(s string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

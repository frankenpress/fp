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
db.sql.gz (sanitised), composer-patch.json, and uploads-manifest.txt.

The snapshot directory lands under <site-root>/fp-snapshots/<slug>-<utc-stamp>/
by default; override with --output-dir. The site must be up (` + "`make up`" + `).

Designer workflow:

  1. fp snapshot --name=architect-2 --note="The7 FSE Architect demo"
  2. Review fp-snapshots/architect-2-<stamp>/manifest.yaml + composer-patch.json
  3. make promote SLUG=architect-2 ENV=stg   (Phase 0 W2; fp promote in Phase 2)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if name == "" {
				return fmt.Errorf("--name is required (e.g. --name=architect-2)")
			}

			stamp := time.Now().UTC().Format("20060102-150405")
			safeName := safeSlug(name)
			dir := outputDir
			if dir == "" {
				dir = filepath.Join("fp-snapshots", safeName+"-"+stamp)
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
			fmt.Fprintln(cmd.OutOrStdout(), "next step (Phase 0):")
			fmt.Fprintf(cmd.OutOrStdout(), "  make promote SLUG=%s ENV=stg\n", name)
			fmt.Fprintln(cmd.OutOrStdout(), "(Phase 2 ships `fp promote` and replaces the Makefile target.)")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Snapshot slug (lowercase + hyphens, e.g. \"architect-2\"). Required.")
	cmd.Flags().StringVar(&note, "note", "", "Optional designer note embedded in the manifest.")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Host-side output directory (relative paths are relative to the site repo). Defaults to fp-snapshots/<slug>-<utc-stamp>.")
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

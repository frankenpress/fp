// Package doctor implements fp doctor — a read-only local-stack
// health check. The output is a fixed-shape report card; each check
// reports a status line and, when something is wrong, a one-line
// hint. Doctor exits 0 regardless of findings — it's a report, not
// a gate. Designers act on the hints themselves.
//
// External calls (docker, gh, wp-in-container) all route through the
// existing Runner seams so unit tests substitute fakes and never
// touch the real CLIs.
package doctor

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/frankenpress/fp/internal/compose"
	"github.com/frankenpress/fp/internal/config"
	"github.com/frankenpress/fp/internal/docker"
	"github.com/frankenpress/fp/internal/gh"
	"github.com/frankenpress/fp/internal/repo"
	"github.com/frankenpress/fp/internal/setup"
	"github.com/frankenpress/fp/internal/summary"
	"github.com/frankenpress/fp/internal/version"
)

// Options carries every input fp doctor needs.
type Options struct {
	RepoRoot string
	Config   *config.Config

	Docker docker.Runner
	GH     gh.Runner

	Stdout io.Writer

	// Now is the time source — tests override for deterministic
	// "snapshot age" output.
	Now func() time.Time
}

// check is one line of the report: a left-column label, a right-side
// value, and an optional hint that fires when something is wrong.
type check struct {
	name    string
	value   string
	hint    string // empty unless there's a recovery to suggest
	problem bool
}

// Run executes every check and writes a report card to Stdout.
// Always returns nil — doctor is a report.
func Run(ctx context.Context, opts Options) error {
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}
	if opts.Now == nil {
		opts.Now = func() time.Time { return time.Now().UTC() }
	}

	checks := []check{
		checkFP(),
		checkDocker(ctx, opts.Docker),
	}
	checks = append(checks, checkComposeStack(ctx, opts)...)
	checks = append(checks, checkSnapshots(opts)...)
	checks = append(checks, checkEnv(opts)...)
	checks = append(checks, checkGit(opts))
	checks = append(checks, checkGHAuth(ctx, opts.GH))

	render(opts.Stdout, checks)
	return nil
}

// --- individual checks ---------------------------------------------

func checkFP() check {
	return check{name: "fp version", value: version.String()}
}

func checkDocker(ctx context.Context, d docker.Runner) check {
	v, err := d.ComposeVersion(ctx)
	if err != nil {
		return check{
			name:    "docker compose",
			value:   "unavailable",
			hint:    "install docker desktop / colima / orbstack and ensure `docker compose` is on PATH",
			problem: true,
		}
	}
	return check{name: "docker compose", value: v}
}

func checkComposeStack(ctx context.Context, opts Options) []check {
	service := firstNonEmpty(opts.Config.Snapshot.Service, "site")
	project := firstNonEmpty(opts.Config.Snapshot.Project, compose.DefaultProject(opts.RepoRoot))

	out := []check{
		{name: "compose project", value: project},
		{name: "compose service", value: service},
	}

	status, container, err := compose.Check(ctx, opts.Docker, project, service)
	if err != nil {
		out = append(out, check{
			name:    "service status",
			value:   "error: " + err.Error(),
			hint:    "ensure the docker daemon is running",
			problem: true,
		})
		return out
	}
	switch status {
	case compose.StatusServiceRunning:
		name := ""
		if container != nil {
			name = " (" + container.Name + ")"
		}
		out = append(out, check{name: "service status", value: "running" + name})
	default:
		out = append(out, check{
			name:    "service status",
			value:   "not running",
			hint:    "bring the stack up with `fp up` (or `fp init` for a fresh clone)",
			problem: true,
		})
	}
	return out
}

func checkSnapshots(opts Options) []check {
	outputDir := opts.Config.Snapshot.OutputDir
	if outputDir == "" {
		outputDir = "web/imports"
	}

	entries, err := summary.Walk(opts.RepoRoot, outputDir)
	if err != nil {
		// Almost always means the dir doesn't exist yet — soft signal.
		return []check{{
			name:    "latest snapshot",
			value:   "none",
			hint:    "capture one with `fp snapshot` (or run `fp init` to scaffold + apply the latest committed snapshot)",
			problem: true,
		}}
	}
	if len(entries) == 0 {
		return []check{{
			name:    "latest snapshot",
			value:   "none",
			hint:    "capture one with `fp snapshot`",
			problem: true,
		}}
	}

	latest := entries[0]
	age := snapshotAge(latest.Manifest.Created, opts.Now())
	return []check{
		{name: "latest snapshot", value: fmt.Sprintf("%s (%s)", latest.Slug, age)},
		{name: "snapshots on disk", value: fmt.Sprintf("%d", len(entries))},
	}
}

func checkEnv(opts Options) []check {
	envPath := filepath.Join(opts.RepoRoot, ".env")
	missing, err := setup.EnvFileMissing(envPath)
	if err != nil || missing {
		return []check{{
			name:    "designer-mode S3",
			value:   ".env missing",
			hint:    "run `fp init` to scaffold .env from .env.example (or `cp .env.example .env`)",
			problem: true,
		}}
	}
	val, found, err := setup.ReadEnvKey(envPath, "FP_S3_DISABLED")
	if err != nil || !found {
		return []check{{
			name:    "designer-mode S3",
			value:   "FP_S3_DISABLED unset",
			hint:    "set FP_S3_DISABLED=0 in .env so uploads land in MinIO (or =1 if you need wp-admin plugin/theme zip installs)",
			problem: true,
		}}
	}
	switch val {
	case "0":
		return []check{{name: "designer-mode S3", value: "enabled (FP_S3_DISABLED=0, uploads → MinIO)"}}
	case "1":
		return []check{{name: "designer-mode S3", value: "disabled (FP_S3_DISABLED=1, local filesystem)"}}
	default:
		return []check{{
			name:    "designer-mode S3",
			value:   "FP_S3_DISABLED=" + val + " (unexpected)",
			hint:    "FP_S3_DISABLED should be 0 or 1",
			problem: true,
		}}
	}
}

func checkGit(opts Options) check {
	if !repo.IsGitRepo(opts.RepoRoot) {
		return check{name: "git", value: "not a git working tree"}
	}
	branch := repo.BranchName(opts.RepoRoot)
	outputDir := opts.Config.Snapshot.OutputDir
	if outputDir == "" {
		outputDir = "web/imports"
	}
	dirty, _ := repo.HasUncommittedChanges(opts.RepoRoot, outputDir)
	dirtyStr := "clean"
	if dirty {
		dirtyStr = "uncommitted changes"
	}
	value := branch
	if value == "" {
		value = "(detached)"
	}
	return check{name: "git", value: fmt.Sprintf("%s, %s under %s/", value, dirtyStr, outputDir)}
}

func checkGHAuth(ctx context.Context, g gh.Runner) check {
	loggedIn, line, err := g.AuthStatus(ctx)
	if err != nil {
		return check{
			name:    "gh auth",
			value:   "gh CLI unavailable",
			hint:    "install gh (https://cli.github.com) — needed only for `fp release`",
			problem: true,
		}
	}
	if !loggedIn {
		return check{
			name:    "gh auth",
			value:   line,
			hint:    "run `gh auth login` so `fp release` can open PRs",
			problem: true,
		}
	}
	return check{name: "gh auth", value: line}
}

// --- rendering ------------------------------------------------------

func render(w io.Writer, checks []check) {
	fmt.Fprintln(w, "fp doctor — environment check")
	fmt.Fprintln(w)

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	for _, c := range checks {
		fmt.Fprintf(tw, "  %s:\t%s\n", c.name, c.value)
	}
	_ = tw.Flush()

	var hints []check
	for _, c := range checks {
		if c.problem && c.hint != "" {
			hints = append(hints, c)
		}
	}
	if len(hints) == 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "all systems nominal.")
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d issue(s) detected:\n", len(hints))
	for _, c := range hints {
		fmt.Fprintf(w, "  %s — %s\n", c.name, c.hint)
	}
}

// --- helpers --------------------------------------------------------

func snapshotAge(created string, now time.Time) string {
	if created == "" {
		return "no created field"
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return "unparseable created"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		// Snapshot from "the future" — clock skew or test fixture.
		return t.UTC().Format("2006-01-02 15:04")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minute(s) ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hour(s) ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%d day(s) ago", int(d.Hours()/24))
	default:
		return t.UTC().Format("2006-01-02")
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

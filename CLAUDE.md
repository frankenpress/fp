# CLAUDE.md — fp

Guidance for Claude Code (and other AI agents) when working in this repo.

## What this repo is

The **`fp`** designer-promotion CLI — a Go binary that wraps the
FrankenPress snapshot-and-promote lifecycle from the host side. Every
bit of business logic (what to capture, schema versioning, apply
semantics) lives in
[`frankenpress/mu-plugin`](https://github.com/frankenpress/mu-plugin)'s
`wp fp` WP-CLI subcommands. fp's only job is **ergonomics**: turn the
three-line shell incantation designers used to type into one Enter,
with prompts, sane defaults, friendly summaries, and a round-trip /
release path on top.

Composer/binary name is `fp` (locked in before homebrew tap publish;
renaming later breaks installs). Single binary — no aliases, no other
entry points.

Current shipped surface (v0.5.0+, 2026-05-14):

  - `fp init` — one-command designer onboarding (bootstrap + up + WP install + apply latest)
  - `fp snapshot` — capture local site state into `web/imports/<timestamp>/`
  - `fp apply [slug-or-path]` — stage + `wp fp apply` for round-trip iteration (no arg → latest)
  - `fp diff <a> <b>` — structural delta between two committed snapshots
  - `fp release` — one-shot capture + commit + push + open PR
  - `fp validate <dir>` — still a stub; **hidden from `--help`** as of 2026-05-14 pending real implementation (Phase 12+ — strict schema validation)
  - `fp version` — version + commit

Historical design notes (Phase 1-11 + `fp init` + timestamp-snapshot-slugs) lived in workspace `.aidocs/` during the v0.1 → v0.6 build-out. They were deleted when the work shipped; the summary is in [`../.aidocs/README.md`](../.aidocs/README.md)'s "Recently completed" section, and the detail is recoverable from the linked PRs + commit history (see also the `fp` repo's own git log).
Public docs: **<https://docs.frankenpress.com/designer-flow>** for the user-facing flow.

## File layout

- `cmd/fp/main.go` — thin entrypoint, calls `cli.NewRoot().Run(os.Args[1:])`.
- `internal/cli/` — cobra wiring. One file per subcommand
  (`root.go`, `version.go`, `snapshot.go`, `apply.go`, `diff.go`,
  `release.go`, plus `validate.go` which is still a stub returning
  exit 2). Adding a verb is one new file + one `cmd.AddCommand` line
  in `root.go`.
- `internal/version/` — `Version` + `Commit` baked in via goreleaser
  `-ldflags`. `String()` falls back to `runtime/debug.ReadBuildInfo()`
  for local `go build` so `fp version` is always meaningful.
- `internal/config/` — `frankenpress.toml` parser. Walks up from cwd
  to find the file (or composer.json fallback); merges TOML over
  `Defaults()`. `[site]` and `[signers]` sections are tolerated and
  ignored (other tools own them).
- `internal/state/` — `.fp/state.json` IO. Atomic via tempfile +
  rename. Drops a `.fp/.gitignore` so per-machine slug history stays
  uncommitted.
- `internal/docker/` — **the testability seam for container ops**.
  `Runner` interface with four methods (`ComposeExec` /
  `ComposeExecStreaming` / `Copy` / `PS`), one real `exec.Command`-based
  impl, and a recording `Fake` for tests. fp does **not** link the
  Docker SDK by design — auth + context + credential helpers are the
  user's docker CLI's problem, not ours.
- `internal/git/` — testability seam for git ops. `Runner` interface
  (`CurrentBranch` / `BranchExists` / `Checkout` / `Add` / `Commit` /
  `Push`) + real impl + `Fake`. Used by `internal/release/`.
- `internal/gh/` — testability seam for GitHub CLI. `Runner` interface
  (`PRCreate` / `PRView`) + real impl + `Fake`. Used by
  `internal/release/`. `gh` auth + context discovery is the user's
  problem, not ours — same shape as `docker`.
- `internal/compose/` — project + service detection. `DefaultProject`
  mirrors compose v2's basename-of-cwd default; `Check` maps
  PS output to a status enum that drives the Error-UX (a) hierarchy.
- `internal/repo/` — git branch + composer.json + uncommitted-changes
  helpers. Best-effort: returns empty / false rather than erroring on
  missing git or file. Used by **the snapshot uncommitted-changes
  guard**; pre-Phase-2 also fed the slug cascade (now removed — see
  `Snapshot slugs default to UTC timestamps` below). Distinct from
  `internal/git/` (which is the typed Runner for the release path;
  this one is read-only).
- `internal/prompt/` — interactive prompts (slug readline, note via
  `$EDITOR` when interactive + `EDITOR` set, otherwise readline;
  y/N confirmation). All helpers take explicit `io.Reader` /
  `io.Writer` args so tests drive them with byte buffers.
- `internal/snapshot/` — the capture orchestrator. `Run(ctx, Options)`
  returns `(*Result, error)`; `Result` carries `Slug` / `Note` /
  `ManifestPath` so composing callers (`internal/release/`) can
  reference what was captured without re-deriving. Stateless package,
  testable end-to-end with the docker fake.
- `internal/apply/` — apply orchestrator. `Run(ctx, Options)`. Stages
  the snapshot dir into the container via `docker cp` then streams
  `wp fp apply`. A `captureWriter` sniffs the streaming output for
  the "apply skipped" sentinel so the summary line is accurate
  without parsing exit shapes.
- `internal/diff/` — pure host-side snapshot vs snapshot differ.
  Reads each side's `manifest.yaml` + `templates.json` +
  `options.json` + `attachments.json` + `uploads-manifest.txt` and
  produces a structural `*Result`. `render.go` formats the Result as
  human-readable terminal output. Zero docker / git / gh coupling.
- `internal/release/` — composes `snapshot.Run` + `git.Runner` +
  `gh.Runner` + `prompt.Confirm` into the one-shot designer flow.
  Owns the branch policy (auto-create `feat/snapshot-<slug>` off
  protected branches), commit-message shape, PR body template.
- `internal/summary/` — manifest.yaml parser + post-capture printer
  + tolerant schema check. **Tolerant**: ignores unknown fields and
  accepts any `fp.snapshot/v*` schema (the strict validator is the
  future `fp validate` subcommand). Prints a one-line warning when
  the schema is newer than `knownMaxSchemaMinor`. Reused by `apply`
  (for the pre-flight + post-summary) and `release` (for the PR body).
- `internal/setup/` — `fp init` orchestrator. `Run(ctx, Options)`
  composes `.env` scaffolding + `docker.Runner.ComposerInstall` +
  `docker.Runner.ComposeUp` + `wp core install` + `apply.Run` into
  one ergonomic onboarding flow. Pure file-IO helpers
  (`ScaffoldEnvFromExample`, `EnsureEnvKey`, `ReadEnvKey`) live next
  to it for unit testability. `--skip-setup` skips ALL env-mutating
  steps (env scaffold + composer + FP_S3_DISABLED line); `--no-apply`
  skips just the apply tail.

## Conventions

- **Cobra for the CLI tree.** No global state — `NewRoot()` is the
  single composition point; each subcommand is a `newXCmd() *cobra.Command`
  function.
- **Runner interfaces are the testability boundary.** `docker.Runner` +
  `git.Runner` + `gh.Runner` follow the same shape: interface, one real
  `exec.Command`-based impl, one recording `Fake`. No external CLI is
  required for `go test ./...`. When a new external tool needs to land
  in fp, add a fourth Runner alongside.
- **External CLI auth is the user's problem.** `docker compose` /
  `git` / `gh` all inherit whatever local credentials, contexts, and
  config the user already has. fp does not link any SDK and does not
  reimplement auth.
- **`--quick` is the only safety-bypass on `fp snapshot`.** No `--force`.
  Designers who want to skip only the uncommitted-changes guard
  `rm -rf` the target dir first. Two flags for "be careful less" is
  a smell.
- **`--yes` on `fp release` is a UX accelerator, not a safety bypass.**
  It skips only the "commit and push?" confirmation prompt. The
  underlying capture still runs with full safety (uncommitted-changes
  guard, etc.) — release doesn't expose a `--quick` passthrough.
- **Verbatim wp-cli stderr.** The mu-plugin's error messages are
  written deliberately (especially "no snapshot adapter detected").
  `internal/snapshot` and `internal/apply` both stream stdout/stderr
  through unmodified; failures print a brief framing line + a hint,
  never reformat the underlying message.
- **Snapshot slugs default to UTC timestamps.** `fp snapshot` with
  no `--slug` produces `YYYY-MM-DDTHH-MM-SSZ` — filename-safe (the
  ISO 8601 `:` becomes `-`) and lex-sortable, so `ls web/imports/`
  is naturally chronological. `--slug=<name>` is the explicit
  override for milestone markers. The pre-Phase-2 cascade
  (state.LastSlug → git branch → composer name → timestamp) is
  gone — chart install Jobs (charts ≥ v0.12.0) pick the snapshot
  with the highest `manifest.created`, so designers accumulate
  snapshots in `web/imports/` instead of `git rm`-ing the previous
  one. `slugify` strips to `[a-z0-9-]` and matches the mu-plugin's
  safe_slug semantics — dir names must look identical regardless of
  which side wrote them. `fp apply` / `fp diff` / `fp release` all
  interpret a bare positional `<slug>` the same way: resolve against
  `[snapshot].output_dir`.
- **Sub-second collision guard.** When the default timestamp slug
  resolves to a dir that already exists (designer fired two
  snapshots in the same second), Run() refuses with "wait a
  moment" rather than letting pre-clean wipe the prior capture.
  Only fires for the timestamp-default path; `--slug=<name>` keeps
  its iterate-on-the-named-slug overwrite behaviour.
- **`fp apply` with no positional → pick latest.** Walks
  `[snapshot].output_dir`, reads each `manifest.yaml`, picks the
  highest `created`. Same logic as the charts install Job at deploy
  time, so local apply targets the same snapshot the cluster will.
  Helper lives in `internal/apply/picklatest.go` as `PickLatest()`
  — exported and reused by `fp init` so the two callers don't drift.
  Passing a positional slug-or-path keeps the existing behaviour.
- **`fp init` is the canonical onboarding command.** Brings a fresh
  clone (or a post-`down -v` stack) to "ready to design" in one
  command. The pipeline is deliberately defensive — every step is
  independently idempotent (`.env` scaffold no-ops if `.env` exists,
  composer install no-ops if `vendor/` exists, `EnsureEnvKey` no-ops
  if `FP_S3_DISABLED` is already set, `wp core install` no-ops if WP
  is installed, apply hits the mu-plugin's idempotency markers).
  Re-running `fp init` is safe and cheap. Designer-mode S3
  (`FP_S3_DISABLED=0`) is layered in via `EnsureEnvKey` — the
  helper NEVER overwrites an existing uncommented assignment, so an
  operator's explicit `FP_S3_DISABLED=1` wins. `[init] disable_s3 =
  true` in `frankenpress.toml` is the alternate opt-out path.
- **Schema tolerance.** Summary printer accepts `fp.snapshot/v*` and
  unknown fields. Warning fires when `v<N>` exceeds the build's
  `knownMaxSchemaMinor` — bump that constant when fp adds new fields
  it reads from a newer manifest.
- **`snapshot.Run` returns `(*Result, error)` for composing callers.**
  `release` needs the resolved slug + note + manifest path without
  re-running cascade logic. If you add a new composing caller, thread
  it through `*Result` rather than re-deriving.
- **Errors are sentences with recovery hints.** This is a CLI;
  messages land in terminals. `fmt.Errorf("foo failed: %w", err)`,
  lowercase, no trailing punctuation, no stack-trace flavour. Where
  a step in the pipeline can fail mid-way (release: commit OK but
  push fails), the error should print the manual continuation command.
- **Tag-driven releases.** `git tag vX.Y.Z && git push` runs
  goreleaser; pushes to `main` do **not** auto-release. v0 era —
  breaking changes are allowed in minors.

## Don'ts

- **Don't link the Docker SDK / go-git / a github API client.** All
  three external tools (`docker compose`, `git`, `gh`) are shelled
  out via Runner interfaces. Linking SDKs means owning auth +
  contexts + credential helpers, which is the user's local CLI's job.
- **Don't reimplement WP option deserialisation in Go.** The
  mu-plugin runs inside WordPress with the real PHP deserialiser;
  fp reads JSON/YAML the mu-plugin already emitted. If you're parsing
  PHP-serialised blobs in Go, you're on the wrong side of the seam.
- **Don't add a `--force` flag anywhere.** No fp subcommand has a
  force-bypass. `fp snapshot --quick` is the single safety bypass and
  it's behaviour-changing (timestamped slug, skip state write), not
  a guard-overrider. `fp release --yes` skips one specific confirmation
  prompt and does **not** bypass any safety guard.
- **Don't reformat wp-cli stderr.** Stream it through, both in
  `internal/snapshot` and `internal/apply`. The mu-plugin error text
  is canonical.
- **Don't add a global `--verbose`.** Verbosity is contextual:
  snapshot + apply already stream wp-cli output; diff is structured
  output; release prints what it did at each step. A toggle would
  just split the design surface.
- **Don't extend `fp release` to merge the PR or tag the repo.** The
  human-review checkpoint between PR open and merge is load-bearing.
  The original plan's "tag too" entry was deferred — tags trigger
  image builds via main-merge, not feature-branch push, so there's no
  in-band use case.
- **Don't add a current-state-vs-snapshot mode to `fp diff` without
  a mu-plugin command first.** Phase 10 scope picked the
  snapshot-vs-snapshot shape exactly because the current-state path
  needs `wp fp dump --scope` (or similar) that doesn't exist yet.
  Parsing the running WP DB from the host is on the wrong side of
  the seam.

## Local testing

```bash
go test -race -count=1 ./...
go vet ./...
gofmt -d .
golangci-lint run

# Build + smoke-run:
go build -o fp ./cmd/fp
./fp version
./fp --help                # surface tour
./fp snapshot --help
./fp apply --help
./fp diff --help
./fp release --help
```

End-to-end against a live stack (designer-side, requires docker
+ a running `frankenpress/site-template`-shaped stack):

```bash
cd ~/Developer/EightOEight/sts
make up   # if not already up

# Capture (the canonical loop):
go run ~/Developer/frankenpress/fp/cmd/fp snapshot

# Apply back into the stack (round-trip iteration):
go run ~/Developer/frankenpress/fp/cmd/fp apply sts-launch

# Diff a fresh quick-capture against committed:
go run ~/Developer/frankenpress/fp/cmd/fp snapshot --quick
go run ~/Developer/frankenpress/fp/cmd/fp diff sts-launch <new-quick-slug>

# Full release (commits + pushes + opens a PR — careful!):
go run ~/Developer/frankenpress/fp/cmd/fp release --no-pr   # safer rehearsal
```

The unit tests cover every Runner-fronted operation with `Fake`s, so
real docker / git / gh is **not** required for `go test ./...`.

## When you bump behaviour

Keep these in sync:

1. `README.md` flag table + config-shape example + subcommand surface.
2. The root help text in `internal/cli/root.go` (`rootLong`) — it
   lists every subcommand inline.
3. `knownMaxSchemaMinor` in `internal/summary/summary.go` — bump
   when fp learns about a new manifest field added in mu-plugin.
4. If `snapshot.Run`'s signature or `*Result` shape changes:
   `internal/release/release.go` reads `Slug` / `Note` / `ManifestPath`
   from Result, so a field rename/removal cascades there. Search for
   `snapResult.` in the release package before mutating.
5. If `frankenpress.toml`'s `[snapshot]` shape changes: update
   `internal/config/config.go`'s `SnapshotConfig` AND the example
   block in [`frankenpress/site-template`'s README](https://github.com/frankenpress/site-template/blob/main/README.md).
6. The public docs at [`docs/designer-flow.mdx`](https://github.com/frankenpress/docs/blob/main/designer-flow.mdx)
   if a user-visible flow changes (new subcommand, prompt UX, etc.).

## Companion repos

| Repo | Purpose |
|---|---|
| [`runtime`](https://github.com/frankenpress/runtime) | Base container image |
| [`mu-plugin`](https://github.com/frankenpress/mu-plugin) | Provides `wp fp snapshot` / `wp fp apply` |
| [`site-template`](https://github.com/frankenpress/site-template) | Bedrock-style template for new sites |
| [`charts`](https://github.com/frankenpress/charts) | Helm chart `site` |
| [`homebrew-tap`](https://github.com/frankenpress/homebrew-tap) | Brew formula tap for `fp` |
| `fp` (this repo) | This CLI |
| [`docs`](https://github.com/frankenpress/docs) | Mintlify docs |

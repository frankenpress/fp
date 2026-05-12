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
with prompts, sane defaults, and a friendly summary.

Composer/binary name is `fp` (locked in before homebrew tap publish;
renaming later breaks installs). Single binary — no aliases, no other
entry points.

Detailed design / decisions: [`frankenpress/.aidocs/fp-go-cli.md`](../.aidocs/fp-go-cli.md).
Public docs (after Phase 5/7): **<https://docs.frankenpress.com/components/fp>**.

## File layout

- `cmd/fp/main.go` — thin entrypoint, calls `cli.NewRoot().Run(os.Args[1:])`.
- `internal/cli/` — cobra wiring. One file per subcommand
  (`root.go`, `version.go`, `snapshot.go`, plus stubs `apply.go` /
  `diff.go` / `validate.go` / `release.go`). Adding a verb is one
  new file + one `cmd.AddCommand` line in `root.go`.
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
- `internal/docker/` — **the testability seam**. `Runner` interface
  with three methods (`ComposeExec` / `ComposeExecStreaming` / `Copy`
  / `PS`), one real `exec.Command`-based impl, and a recording `Fake`
  for tests. fp does **not** link the Docker SDK by design — auth +
  context + credential helpers are the user's docker CLI's problem,
  not ours.
- `internal/compose/` — project + service detection. `DefaultProject`
  mirrors compose v2's basename-of-cwd default; `Check` maps
  PS output to a status enum that drives the Error-UX (a) hierarchy.
- `internal/repo/` — git branch + composer.json + uncommitted-changes
  helpers. Every helper is best-effort and returns an empty string /
  false rather than erroring on a missing git binary / file.
- `internal/prompt/` — interactive prompts (slug readline, note via
  `$EDITOR` when interactive + `EDITOR` set, otherwise readline;
  y/N confirmation). All helpers take explicit `io.Reader` /
  `io.Writer` args so tests drive them with byte buffers.
- `internal/snapshot/` — the orchestrator. `Run(ctx, Options)` is
  the single entrypoint; `Options` carries every input (config,
  state, runner, IO streams, flag values). Stateless package,
  testable end-to-end with the Runner fake.
- `internal/apply/` — Phase 2; empty package today.
- `internal/summary/` — manifest.yaml parser + post-capture printer.
  **Tolerant**: ignores unknown fields and accepts any
  `fp.snapshot/v*` schema (the strict validator is the future `fp
  validate` subcommand). Prints a one-line warning when the schema
  is newer than `knownMaxSchemaMinor`.

## Conventions

- **Cobra for the CLI tree.** No global state — `NewRoot()` is the
  single composition point; each subcommand is a `newXCmd() *cobra.Command`
  function.
- **`docker.Runner` is the testability boundary.** Every docker /
  docker-compose call goes through the interface; the real impl
  shells out, the fake records + returns canned responses. There is
  no integration-with-real-docker code in `go test ./...`.
- **`--quick` is the only safety-bypass flag.** No `--force`. If a
  designer wants to skip only the uncommitted-changes guard,
  `rm -rf` first. Two flags for "be careful less" is a smell.
- **Verbatim wp-cli stderr.** The mu-plugin's error messages are
  written deliberately (especially "no snapshot adapter detected").
  `internal/snapshot` streams stdout/stderr through unmodified;
  failures print a brief framing line + a hint, never reformat the
  underlying message.
- **Slug cascade order is load-bearing.** state.LastSlug → git
  branch (sans `feat/` etc. prefix) → `composer.json` `name` →
  timestamped fallback. `slugify` strips to `[a-z0-9-]` and matches
  the mu-plugin's safe_slug semantics — dir names must look identical
  regardless of which side wrote them.
- **Schema tolerance.** Summary printer accepts `fp.snapshot/v*` and
  unknown fields. Warning fires when `v<N>` exceeds the build's
  `knownMaxSchemaMinor` — bump that constant when fp adds new fields
  it reads from a newer manifest.
- **Errors are sentences.** This is a CLI; messages land in
  terminals. `fmt.Errorf("foo failed: %w", err)`, lowercase, no
  trailing punctuation, no stack-trace flavour.
- **Tag-driven releases.** `git tag vX.Y.Z && git push` runs
  goreleaser; pushes to `main` do **not** auto-release. v0 era —
  breaking changes are allowed in minors.

## Don'ts

- **Don't link the Docker SDK for Go.** The shell-out approach is
  load-bearing: it inherits the user's `docker compose` setup (auth,
  contexts, rootless, colima/orbstack) without fp owning any of it.
- **Don't reimplement WP option deserialisation in Go.** The
  mu-plugin runs inside WordPress with the real PHP deserialiser;
  fp reads JSON/YAML the mu-plugin already emitted. If you're parsing
  PHP-serialised blobs in Go, you're on the wrong side of the seam.
- **Don't add a `--force` or `--yes` flag.** `--quick` is the single
  bypass; that's the explicit plan decision.
- **Don't reformat wp-cli stderr.** Stream it through. The mu-plugin
  error text is canonical.
- **Don't add a global `--verbose`.** Verbosity is contextual:
  snapshot already streams wp-cli output; doctor (future) is
  enumeration-style. A toggle would just split the design surface.
- **Don't mutate `.aidocs/fp-go-cli.md`** without explicit user
  approval. The "Resolved:" decisions there are the contract.

## Local testing

```bash
go test -race -count=1 ./...
go vet ./...
gofmt -d .
golangci-lint run

# Build + smoke-run:
go build -o fp ./cmd/fp
./fp version
./fp snapshot --help
```

End-to-end against a live stack (designer-side, requires docker
+ a running `frankenpress/site-template`-shaped stack):

```bash
cd ~/Developer/EightOEight/sts
make up   # if not already up
go run ~/Developer/frankenpress/fp/cmd/fp snapshot
```

## When you bump behaviour

Keep these in sync:

1. The plan at [`frankenpress/.aidocs/fp-go-cli.md`](../.aidocs/fp-go-cli.md)
   — especially the "Resolved" decisions and the Error-UX table.
2. `README.md` flag table + config-shape example.
3. `knownMaxSchemaMinor` in `internal/summary/summary.go` — bump
   when fp learns about a new manifest field added in mu-plugin.

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

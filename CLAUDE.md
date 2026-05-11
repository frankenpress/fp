# CLAUDE.md — fp

Guidance for Claude Code when working in this repo.

## What this repo is

The **FrankenPress designer-promotion CLI**. A standalone Go binary
(`fp`) that captures local site state into a portable snapshot bundle
and promotes it through gitops to staging / production. Pairs with
`wp fp` subcommands provided by `frankenpress/mu-plugin`.

Public docs: **<https://docs.frankenpress.com/components/fp>** (Phase 6)

## File layout

- `cmd/fp/main.go` — entry point. Calls `cli.NewRoot().Execute()`.
- `internal/cli/` — Cobra command tree. One file per subcommand
  (`root.go`, `version.go`, `doctor.go`, `snapshot.go`). Adding a
  new verb is one new file + one `cmd.AddCommand(...)` line in
  `root.go`.
- `internal/version/` — build-time metadata. Values are wired in via
  goreleaser `-ldflags` (see `.goreleaser.yaml`); local `go build`
  falls back to `debug.ReadBuildInfo()` for the commit.
- `internal/doctor/` — pre-flight environment checks. Each check is
  a pure function returning a `Result`; the cli/doctor.go wrapper
  formats them for terminal output.
- `internal/wpcli/` — wraps `docker compose exec` invocations of
  wp-cli. Phase 1 surface: just enough to run `wp fp snapshot`.
  Phase 2 expands to apply-on-remote via `kubectl exec`.
- `pkg/manifest/` — `fp.snapshot/v1` schema. Both the embedded
  JSON Schema (`schema.json`) and the Go types (`types.go`) must
  match the PHP-side emitter in
  `frankenpress/mu-plugin/src/Cli/Snapshot/Manifest.php`.

## Conventions

- **Go 1.24.** Pinned in `.mise.toml`. CI matches via
  `actions/setup-go` with `go-version: "1.24"`.
- **Cobra for the CLI tree.** Standard pattern: each subcommand is a
  `func newXCmd() *cobra.Command` returning a fully-built command.
  No global state; `NewRoot()` is the single composition point.
- **Public API surface is `pkg/`.** `internal/` packages can break
  freely between minor versions. The manifest types in `pkg/manifest/`
  are the only thing third-party tools should depend on, and they
  follow `fp.snapshot/v1` versioning semantics — backward-compatible
  additions OK, breaking changes mean `fp.snapshot/v2` + a new types
  package alongside (don't mutate v1 in place).
- **Errors are for humans.** We're a CLI; error messages get printed
  to terminals. Phrase them as readable sentences, not
  stack-trace-flavoured strings (the `revive` `error-strings` rule is
  intentionally disabled in `.golangci.yaml`).
- **`-trimpath` + stripped binary in releases.** Set in
  `.goreleaser.yaml`. Reproducible-build hygiene + smaller artefacts.
- **Cosign keyless OIDC** for every release artefact. Sigstore is
  the same trust root the rest of FrankenPress uses (runtime images,
  site images).
- **Tag-driven releases.** `git tag vX.Y.Z && git push origin vX.Y.Z`.
  goreleaser handles the rest. Pushes to `main` do NOT auto-release.

## Don'ts

- **Don't reimplement WordPress option deserialisation in Go.** The
  PHP-side `wp fp snapshot` runs inside WordPress with the real
  deserialiser; the Go side reads its JSON manifest output. If you
  catch yourself parsing PHP-serialised blobs in Go, you're working
  on the wrong side of the seam.
- **Don't `kubectl patch` / `argocd app sync` / etc. from `fp`.**
  Every cluster-touching action goes through gitops: `fp promote`
  opens PRs against `gitops-fp`, and ArgoCD reconciles. Phase 2 of
  the design pins this contract.
- **Don't emit `DISALLOW_FILE_MODS=false` from any codepath.** The
  lockdown is a load-bearing safety property. Local LDE relaxes it
  via the `KUBERNETES_SERVICE_HOST` gate in `site-template` /
  `mu-plugin`; `fp` never touches it.
- **Don't add a global `--verbose` flag without thinking it through.**
  CLI verbosity is contextual — `fp snapshot` already streams wp-cli
  log lines through, `fp doctor` is already enumeration-style.

## Local testing

```bash
go test ./...
go vet ./...
golangci-lint run

# Smoke run:
go run ./cmd/fp version
go run ./cmd/fp doctor

# End-to-end (requires a running site stack from
# frankenpress/site-template or EightOEight/sts):
( cd ~/code/sts && go run ../../frankenpress/fp/cmd/fp snapshot --name=test )
```

## When you bump the snapshot schema

If `pkg/manifest/types.go` or `pkg/manifest/schema.json` changes:

1. Update both files in lockstep — the embedded schema must match
   the Go types' JSON shape.
2. Mirror the change in `frankenpress/mu-plugin`'s
   `src/Cli/Snapshot/Manifest.php` (and update its CLAUDE.md
   referencing the manifest schema).
3. If the change is breaking, bump to `fp.snapshot/v2` (don't mutate
   v1 in place). Keep v1 parsing in place until a deprecation
   window has passed.
4. Update `docs.frankenpress.com/operations/promote-from-local` (in
   the `docs` repo) if any user-visible field semantics change.

## Companion repos

| Repo | Purpose |
|---|---|
| [`runtime`](https://github.com/frankenpress/runtime) | Base container image |
| [`mu-plugin`](https://github.com/frankenpress/mu-plugin) | Provides `wp fp snapshot` / `wp fp apply` |
| [`site-template`](https://github.com/frankenpress/site-template) | Designer's Makefile (`make snapshot`) lives here |
| [`charts`](https://github.com/frankenpress/charts) | `siteInstall.snapshot.*` values consumed cluster-side |
| `fp` (this repo) | The Go binary |
| [`docs`](https://github.com/frankenpress/docs) | Mintlify docs |

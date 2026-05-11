# fp — FrankenPress designer-promotion CLI

A standalone Go binary that captures a designer's local FrankenPress site state
(database, plugin set, premium-theme adapter state) into a portable snapshot
bundle and promotes it through gitops to staging / production.

**Status:** v0.1.x — Phase 1 of the [`fp` CLI design](https://github.com/frankenpress/.github/blob/main/.aidocs/fp-cli-design.md). v1.0 ships at Phase 6.

## What it's for

The FrankenPress production model bakes WordPress code into an immutable
container image. That's great for engineer-driven changes (`composer require`,
tag, deploy), but it doesn't fit how designers want to use premium WP themes —
"click the importer button and have it show up in production." `fp` closes
that loop: the designer runs the importer locally, `fp snapshot` captures
what changed, `fp promote` opens reviewable PRs that gitops then applies.

## Install

```bash
# Once Homebrew tap publishing lands (Phase 6):
brew install frankenpress/tap/fp

# In the meantime — grab a binary from the latest release:
# https://github.com/frankenpress/fp/releases
```

## Subcommands (v0.1)

| Command | Phase | Purpose |
|---|---|---|
| `fp version` | 1 | Binary + manifest schema version |
| `fp doctor` | 1 | Pre-flight environment checks (docker, gh, git, aws, jq) |
| `fp snapshot --name=<slug>` | 1 | Capture local site state — wraps `wp fp snapshot` inside the running site container |

Phase 2+ adds `fp promote` / `fp restore` / `fp diff` / `fp adapters`; see the
plan for the full v1 trajectory.

## Pairs with

- **`frankenpress/mu-plugin v0.7.0+`** — provides the `wp fp snapshot` and `wp fp apply` WP-CLI subcommands that `fp` orchestrates.
- **`frankenpress/charts v0.8.0+`** — provides the `siteInstall.snapshot.{ref,s3Key,bucket}` values that the chart's install Job consumes to apply a promoted snapshot.

## Schema

Manifests use the `fp.snapshot/v1` schema; the canonical JSON Schema document
lives at `pkg/manifest/schema.json` and is embedded into the binary. Both the
Go side (here) and the PHP side (in mu-plugin) read the same schema.

## Local development

```bash
go build ./cmd/fp
go test ./...
golangci-lint run

# Run the binary directly without installing:
go run ./cmd/fp version
go run ./cmd/fp doctor
```

Requires Go 1.24+ (see `.mise.toml`).

## Release flow

Tag-driven, signed with cosign keyless OIDC via goreleaser:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow builds linux+darwin amd64+arm64, cosign-signs every
artifact, and publishes a GitHub Release with checksums + the cosign bundle.

## Companion repos

| Repo | Purpose |
|---|---|
| [`runtime`](https://github.com/frankenpress/runtime) | Base container image |
| [`mu-plugin`](https://github.com/frankenpress/mu-plugin) | Must-use plugin (provides `wp fp` subcommands) |
| [`site-template`](https://github.com/frankenpress/site-template) | GitHub template for new sites |
| [`charts`](https://github.com/frankenpress/charts) | Helm chart `site` (consumes `siteInstall.snapshot`) |
| `fp` (this repo) | Designer-promotion CLI |
| [`docs`](https://github.com/frankenpress/docs) | Mintlify docs site |

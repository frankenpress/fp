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

## Subcommands

| Command | Purpose |
|---|---|
| `fp version` | Binary + manifest schema version |
| `fp doctor` | Pre-flight environment checks (docker, gh, git, aws, jq) |
| `fp snapshot --name=<slug>` | Capture local site state into `web/imports/<slug>/` — wraps `wp fp snapshot` inside the running site container |

Phase 3+ will add `fp restore` (round-trip a remote snapshot back into LDE for
iteration). `fp promote` was removed in v0.4 — snapshots are now image-baked
artefacts committed into the site repo at `web/imports/<slug>/` and shipped via
normal git PRs, so no separate promote command is needed (see "design pivot in
v0.4" below).

## Designer workflow

```bash
make up
# (designer runs theme demo importer in wp-admin, tweaks content)

fp snapshot --name=architect-2 --note="The7 FSE Architect demo"
# → web/imports/architect-2/{manifest.yaml,content.xml.gz,options.json,...}

# Review the manifest + composer-patch.json
cat web/imports/architect-2/manifest.yaml
cat web/imports/architect-2/composer-patch.json

# composer require any pending plugins
composer require wpackagist-plugin/<slug>

# Commit + open site-repo PR
git add web/imports/architect-2/ composer.json composer.lock
git commit -m "Add architect-2 design import"
gh pr create
```

Engineer reviews the WXR + options + manifest like any other code. Merge →
CI builds new site image with `web/imports/<slug>/` baked in → tag → ArgoCD
reconciles → chart's install Job iterates `/app/web/imports/*` and runs
`wp fp apply` per snapshot dir. Idempotency markers (`fp_snapshot_applied_ref`
+ `fp_snapshot_applied_sha256`) short-circuit re-application.

## Design pivot in v0.4

v0.1-v0.3 used a different model: snapshots uploaded to S3, separate
gitops-fp PR with a `siteInstall.snapshot.ref` bump. That design had two
problems:

- **Two PRs per promote.** Designer overhead + coordination across repos.
- **`wp db import`-based apply was destructive.** A snapshot apply replaced the
  whole DB, which would clobber any data that accumulated in production
  while the designer was working locally (e.g. WooCommerce orders).

v0.4 (+ frankenpress/mu-plugin v0.8.0 + frankenpress/charts v0.9.0) pivots to
**image-baked snapshots + adapter-scoped additive apply.** Snapshots live in
the site repo at `web/imports/<slug>/`, get committed and reviewed as normal
code, and apply uses WP-Importer (additive — never DROP/DELETE/TRUNCATE). The
scope of each snapshot is declared by a premium-theme adapter (The7 first;
Avada/Divi later), so by construction a snapshot can only carry rows the
adapter knew about. WooCommerce orders, user accounts, comments — never in
scope, never touched.

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

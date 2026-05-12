# fp — FrankenPress designer-promotion CLI

A small Go binary that wraps the [FrankenPress](https://github.com/frankenpress)
snapshot-and-promote lifecycle.

Designers run `fp snapshot` from their site repo root after iterating on local
WordPress; fp shells into the running docker-compose stack, invokes
`wp fp snapshot` (provided by [`frankenpress/mu-plugin`](https://github.com/frankenpress/mu-plugin)),
and `docker cp`'s the result into `web/imports/<slug>/`. The snapshot is
committed alongside the rest of the site code, baked into the next site image,
and applied on-cluster by the chart's install Job.

`fp` is a host-side ergonomics wrapper — every bit of business logic lives in
the mu-plugin. fp's job is to make the capture feel like one Enter, not three
shell commands with quoting traps.

## Install

```bash
brew install frankenpress/tap/fp
```

Or grab a binary from [Releases](https://github.com/frankenpress/fp/releases).
Or, if you have a Go toolchain:

```bash
go install github.com/frankenpress/fp/cmd/fp@latest
```

## Usage

From your site repo's root (or any subdirectory — fp walks up to find
`frankenpress.toml` or `composer.json`):

```bash
fp snapshot
```

Three Enters by default:

1. **slug** — fp suggests the last slug you used (or your git branch / a
   composer-derived default if there's no prior history); Enter accepts.
2. **note** — if `$EDITOR` is set and you're at a TTY, fp opens it
   (`git commit`-style); otherwise reads one line from stdin.
3. **continue** — if the target dir has uncommitted git changes, fp asks before
   overwriting.

After capture, fp prints a summary of what landed (templates, options,
attachments, uploads-audit counts) and the suggested
`git add … && git commit -m "snapshot: …"` commands.

### Flags

| Flag | What it does |
|---|---|
| `--slug <s>` | Skip the slug prompt. |
| `--note <s>` | Skip the note prompt. Mutually exclusive with `--note-file`. |
| `--note-file <path>` | Read the note from a file (multi-line OK). |
| `--quick` | Skip prompts **and** safety guards. Forces a timestamped slug. Does not update `.fp/state.json`. Use for ad-hoc / scripted captures. |
| `--output-dir <p>` | Override `[snapshot].output_dir`. |
| `--service <s>` | Override `[snapshot].service`. |
| `--project <s>` | Override `[snapshot].project`. |

`--quick` is the only safety-bypass flag. If you want to skip only the
uncommitted-changes guard while keeping the prompts, `rm -rf web/imports/<slug>`
before running fp.

### Other subcommands

```text
fp apply <snapshot-dir>     Phase 2 — apply a snapshot back into the local stack
fp diff <slug>              future — diff current state against a committed snapshot
fp validate <snapshot-dir>  future — strict schema validation
fp release                  future — capture + commit + push + open PR in one shot
fp version                  print binary version + commit
```

Stub subcommands print "not implemented yet" and exit 2 so the command tree is
discoverable today.

## Configuration

`fp` reads `frankenpress.toml` at the site repo root. The `[snapshot]` section
is fp-specific; every key is optional:

```toml
[snapshot]
# project = "sts"            # default: basename(repo-root)
# service = "site"           # compose service running WordPress
# output_dir = "web/imports" # host-side, relative to repo root
# container_output_dir = "/app/web/imports"  # in-container path
```

The empty file is valid. `[site]` and `[signers]` (read by other tools) are
tolerated and untouched.

State the CLI persists between invocations lives at `.fp/state.json` in the
repo root. fp drops a `.fp/.gitignore` on first write so this stays
machine-local.

## How it talks to docker

fp shells out to your `docker` CLI; it does not link the Docker SDK. Whatever
authentication / context / credential-helper setup `docker compose ps` works
under, fp inherits — including rootless docker, colima, OrbStack, custom
DOCKER_HOST values. The trade-off is that you must have `docker compose` v2
available locally (the same binary you use for `make up`).

Internally every docker invocation routes through a `Runner` interface
(`internal/docker/`). Tests substitute a recording fake — there is no docker
dependency in `go test ./...`.

## Local development

```bash
go build ./cmd/fp
go test ./...
go vet ./...
gofmt -d .       # must produce no diff
golangci-lint run
```

`mise` pins Go 1.24 (`.mise.toml`); CI matches via `actions/setup-go`.

## Releases

Tag-driven. `git tag vX.Y.Z && git push origin vX.Y.Z` runs goreleaser, which:

- Builds darwin/linux × amd64/arm64.
- Signs the checksum file via cosign keyless OIDC (sigstore — same trust root
  as the rest of the FrankenPress stack).
- Generates an SPDX SBOM per archive.
- Updates the [`frankenpress/homebrew-tap`](https://github.com/frankenpress/homebrew-tap)
  formula so `brew upgrade fp` picks the release up.

## Companion repos

| Repo | Purpose |
|---|---|
| [`runtime`](https://github.com/frankenpress/runtime) | Base container image |
| [`mu-plugin`](https://github.com/frankenpress/mu-plugin) | Provides `wp fp snapshot` / `wp fp apply` |
| [`site-template`](https://github.com/frankenpress/site-template) | Bedrock-style template for new sites |
| [`charts`](https://github.com/frankenpress/charts) | Helm chart `site` |
| [`homebrew-tap`](https://github.com/frankenpress/homebrew-tap) | Brew formula tap for `fp` |
| `fp` (this repo) | This CLI |
| [`docs`](https://github.com/frankenpress/docs) | Mintlify docs site |

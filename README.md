# fp — FrankenPress designer-promotion CLI

A small Go binary that wraps the [FrankenPress](https://github.com/frankenpress)
designer-promotion lifecycle from the host side.

Designers iterate on a local WordPress in docker-compose, then use `fp` to
**capture** that state (`fp snapshot`), **apply** captures back for round-trip
iteration (`fp apply`), **diff** two captures during review (`fp diff`), and
**release** the result in one shot — commit, push, open PR (`fp release`).

Every bit of business logic (what to capture, schema versioning, apply
semantics) lives in [`frankenpress/mu-plugin`](https://github.com/frankenpress/mu-plugin)'s
`wp fp ...` WP-CLI commands. `fp`'s job is **ergonomics**: shell into the
container, hand wp-cli the right args, `docker cp` the result back out, prompt
the designer with sensible defaults — turn three shell commands with quoting
traps into one Enter.

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

Run any subcommand from your site repo's root or any subdirectory — `fp` walks
up to find `frankenpress.toml` or `composer.json` to identify the repo. The
docker-compose stack must already be up (`fp` doesn't bring it up).

### `fp snapshot` — capture local site state

```bash
fp snapshot
```

Three Enters by default:

1. **slug** — `fp` suggests the last slug you used (or your git branch / a
   composer-derived default if there's no prior history); Enter accepts.
2. **note** — if `$EDITOR` is set and you're at a TTY, `fp` opens it
   (`git commit`-style); otherwise reads one line from stdin.
3. **continue** — if the target dir has uncommitted git changes, `fp` asks
   before overwriting.

After capture, `fp` prints a summary (templates, options, attachments,
uploads-audit counts) and the suggested `git add … && git commit` commands.

| Flag | What it does |
|---|---|
| `--slug <s>` | Skip the slug prompt. |
| `--note <s>` | Skip the note prompt. Mutually exclusive with `--note-file`. |
| `--note-file <path>` | Read the note from a file (multi-line OK). |
| `--quick` | Skip prompts **and** safety guards. Forces a timestamped slug. Does not update `.fp/state.json`. Use for ad-hoc / scripted captures. |
| `--output-dir <p>` | Override `[snapshot].output_dir`. |
| `--service <s>` | Override `[snapshot].service`. |
| `--project <s>` | Override `[snapshot].project`. |

`--quick` is the only safety-bypass flag. To skip only the uncommitted-changes
guard while keeping the prompts, `rm -rf web/imports/<slug>` first.

### `fp apply <slug-or-path>` — round-trip iteration

```bash
fp apply sts-launch                # bare slug → resolves against [snapshot].output_dir
fp apply web/imports/sts-launch    # relative path
fp apply /abs/path/to/snapshot     # absolute path
```

`fp` stages the snapshot dir into the running container via `docker cp` and
runs `wp fp apply` against it. Idempotent — the mu-plugin's markers
short-circuit re-applies; `fp` surfaces "snapshot already applied" cleanly
without erroring. Use for **capture → tweak → re-apply** loops without
rebuilding the image.

The path must resolve to a directory inside the site repo; the container only
sees `/app/<rel-to-repo>`.

### `fp diff <a> <b>` — structural delta between two snapshots

```bash
fp diff sts-launch /tmp/old-sts-launch
fp diff sts-launch-stg sts-launch-prd
fp diff web/imports/foo web/imports/bar
```

Pure host-side. Reads each snapshot's `manifest.yaml` + `templates.json` +
`options.json` + `attachments.json` + `uploads-manifest.txt` and prints a
terminal-friendly summary of additions, removals, and modifications. No
docker, no git, no mu-plugin coupling. Designed for PR review and
cross-snapshot comparison.

The "current site state vs committed snapshot" mode is **not** in v0.4.x — it
needs a future mu-plugin "dump scope without writing files" command.

### `fp release` — one-shot capture + commit + push + open PR

```bash
fp release                # interactive: slug + note prompts, then commit-confirm
fp release --yes          # skip the commit-confirm prompt
fp release --no-pr        # commit + push, no gh pr create
fp release --branch X     # override the branch policy
```

The canonical "I'm done iterating, ship it" flow. Captures via the same
pipeline as `fp snapshot`, then:

1. **Branches** — if you're on `main` / `master` / `trunk`, `fp` auto-creates
   `feat/snapshot-<slug>` and switches to it. Otherwise stays on the current
   branch. Override with `--branch <name>`.
2. **Commits** — `snapshot: <slug>` subject, designer note as body. Author is
   **your local git config** (designer-authored commit, not a bot).
3. **Pushes** — `git push -u origin <branch>`. No `--force` ever.
4. **Opens a PR** — title `snapshot: <slug>`, body has a counts table parsed
   from the manifest + the designer note + an "apply path" recap. Skip with
   `--no-pr`.

Every step's error message carries a manual continuation command if recovery
is needed (push failed → "retry the push manually, then `gh pr create`").
PR-already-exists is detected via `gh pr view` and surfaces the existing URL
instead of erroring.

`--yes` skips only the "commit and push?" confirmation prompt; it is **not** a
safety bypass.

### `fp version` / `fp validate`

```bash
fp version              # version + commit SHA (baked at build time via -ldflags)
fp validate <dir>       # stub — strict schema validation is future scope
```

Only `fp validate` is still a stub.

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

## How it talks to docker, git, and gh

`fp` shells out to your local `docker`, `git`, and `gh` CLIs. It does **not**
link the Docker SDK, go-git, or a GitHub API client. Whatever authentication,
context, and credential-helper setup those CLIs work under, `fp` inherits —
rootless docker, colima, OrbStack, custom `DOCKER_HOST`, SSH agents, gh's
stored token, all of it.

The trade-offs:

- `docker compose` v2 must be available locally (the same binary you use for
  `make up`).
- `fp release` additionally needs `git` and `gh` on `PATH`, authenticated to
  the right remote. `git push` and `gh pr create` are the only places `fp`
  touches them.

Internally every external-CLI call routes through a `Runner` interface
(`internal/docker/`, `internal/git/`, `internal/gh/`). Tests substitute a
recording fake — `go test ./...` has zero external-binary dependencies.

## Local development

```bash
go build ./cmd/fp
go test ./...
go vet ./...
gofmt -d .       # must produce no diff
golangci-lint run

# Subcommand tour:
./fp --help
./fp snapshot --help
./fp apply --help
./fp diff --help
./fp release --help

# End-to-end against a real stack:
cd ~/path/to/your-site
make up
go run ~/path/to/fp/cmd/fp snapshot
go run ~/path/to/fp/cmd/fp apply <slug>
go run ~/path/to/fp/cmd/fp release --no-pr   # safer rehearsal — skips PR open
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

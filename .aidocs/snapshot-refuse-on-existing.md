# `fp snapshot` — refuse capture when a prior snapshot dir exists

**Status:** shelved (2026-05-14) — superseded by the timestamp-snapshot-slugs work (charts ≥ v0.12.0 picks the snapshot with the highest `manifest.created`, so `web/imports/` now accumulates dirs by design rather than enforcing one-snapshot-per-release). The triggering 2026-05-13 sts-stg failure is no longer reproducible with the accumulation model.
**Created:** 2026-05-13
**Owner:** unassigned

Kept for searchable rationale. The "rejected alternative" + "open questions" sections below are no longer load-bearing; treat this as a historical artefact, not a plan.

## Context

The snapshot/apply contract carries a **one-snapshot-per-release**
invariant: a site repo should have exactly one directory under
`web/imports/<slug>/` containing a `manifest.json` at any given commit.
The mu-plugin's install hook enforces this at apply time — if it finds
more than one snapshot dir, it refuses to proceed, because it can't
deterministically pick which one to load.

On 2026-05-13 the sts-stg rollout hit this:

```
[snapshot] ERROR: 2 snapshot directories with manifest.json under
/app/web/imports — the convention is one snapshot per release.
[snapshot] Snapshot directories found:
/app/web/imports/msk
/app/web/imports/sts-launch
[snapshot] Remove stale snapshot dirs from the site repo
(web/imports/<old-slug>/) and re-tag. Apply will not proceed until
this is resolved.
```

A new `msk` snapshot had been captured without removing the prior
`sts-launch` dir. Fix was a one-line `git rm -r web/imports/sts-launch`
+ retag — but the failure shipped from local capture all the way out
to a staging install Job before being caught, and the user had to
diagnose it via cluster logs.

The apply hook's loud failure is the right behaviour and should stay —
it's a last-line-of-defence and catches "someone else's stale snapshot
got pulled into your branch" scenarios that capture-time checks can't
see. The gap is upstream: `fp snapshot` happily writes a second
snapshot dir when it should refuse, or at least confirm.

## Rejected alternative

> "Make `fp apply` (and the mu-plugin's install hook) pick the latest
> snapshot when multiple exist."

This silently masks "forgot to clear a colleague's WIP snapshot"
mistakes and trades a loud, actionable error for an implicit guess
about ordering (filename? mtime? manifest field?). Keep apply strict;
fix the capture step.

## Proposed change

Before writing `web/imports/<slug>/`, `fp snapshot` scans
`web/imports/` for any sibling directory containing a `manifest.json`.
If one is found that isn't the slug currently being captured, branch
on `--replace` and TTY:

| Condition | Behaviour |
|---|---|
| `--replace` passed | Remove the existing snapshot dir(s) before capturing. Print one line per removed dir. |
| Interactive (stdin is a TTY), no `--replace` | Prompt: `Replace existing snapshot 'sts-launch'? [y/N]` — `y` proceeds with replacement, anything else aborts. |
| Non-interactive, no `--replace` | Refuse with a clear error: name the stale dir(s), state the one-snapshot-per-release invariant, suggest either `git rm -r web/imports/<old-slug>` or rerunning with `--replace`. |

If the existing dir's slug matches the one being captured, treat it as
a normal recapture-into-same-slug case (current behaviour: overwrite
contents). No new prompt or flag needed for that path — it's the
expected workflow.

### Edge cases

- **Multiple stale dirs.** List all of them in the error / prompt.
  `--replace` removes all of them.
- **A dir without `manifest.json`.** Ignore. The invariant is keyed on
  manifest presence (matching the mu-plugin's check), not directory
  existence — `web/imports/.gitkeep` and `web/imports/uploads-staging/`
  etc. shouldn't trigger this.
- **`--quick` flag.** `--quick` currently bypasses the slug prompt by
  generating a timestamped slug. It should *not* implicitly bypass
  this new check — a `--quick` capture on top of an existing snapshot
  should still refuse (or prompt). If the user wants
  capture-and-replace in one flag, they'd pass `--quick --replace`.

## Implementation sketch

Touch points:

- **`internal/snapshot/snapshot.go`** — in `Run()`, after `resolveSlug`
  (line ~90) and before the `wp fp snapshot` exec (line ~166): walk
  `filepath.Join(opts.RepoRoot, outputDir)` one level deep, filter to
  dirs containing `manifest.json`, exclude the about-to-be-written
  slug, branch on `opts.Replace` + `prompt.AskReplace(...)`.
- **`internal/cli/snapshot.go`** — add `--replace` flag, wire to
  `Options.Replace`.
- **`internal/prompt/`** — new `AskReplace(stdin, stdout, slugs []string) (bool, error)` mirroring the existing `AskSlug` style.
- **`internal/snapshot/snapshot_test.go`** — three new table cases:
  - refuse non-interactive without `--replace` (error message contains the stale slug + the suggested remediation)
  - replace with `--replace` (stale dir removed, capture proceeds, mock runner sees the expected `wp fp snapshot` invocation)
  - prompt-accept with simulated TTY input (`y\n`)
- **`README.md`** + **`CLAUDE.md`** in this repo — short note on the
  new flag + the refuse-by-default behaviour.

The host-side removal should use `os.RemoveAll` on the resolved path
(not shell out). Print one line per removed dir so the user has an
audit trail in their terminal scrollback.

## Out of scope

- Any change to the apply / install-hook side (the mu-plugin and
  charts). The loud failure stays.
- Auto-generating timestamped slugs as the default (separate
  proposal — `--quick` already does this on demand and the
  human-readable slug is useful in commit messages + log lines).
- Cleaning up older committed snapshots from history (not a CLI
  concern — site repos handle that with normal `git rm`).

## Open questions

1. **Prompt default — y or N?** Drafted as `N` above (safer:
   accidental Enter doesn't delete prior work). The `fp snapshot`
   slug prompt currently defaults the accepted answer, not destructive,
   so the asymmetry is justified.
2. **`--replace` short flag?** `-r` is unused in `fp snapshot` today
   but it's a strong candidate for `--repo` if we ever add multi-repo
   support. Lean toward no short flag.

## Links

- Triggering incident: sts-stg apply failure 2026-05-13, fixed by
  `EightOEight/sts@v0.4.3` (drop `sts-launch` snapshot dir in favour
  of `msk`).
- Workspace-level mention: `~/Developer/frankenpress/.aidocs/followups.md` item #3.

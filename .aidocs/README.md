# .aidocs — fp CLI design notes

Long-form design notes, ADRs, and decision logs for the
`frankenpress/fp` Go CLI. Lives in the fp repo (not the workspace-wide
`.aidocs/`) when the content is fp-specific enough that it belongs
alongside the code.

## Conventions

- **One file per topic.** Don't accumulate everything in a single backlog.
- **Lead with context** (the "why") before solution.
- **Mark status at the top** — `draft / proposed / accepted / done (reference) / shelved`.
- **Link to PRs + tags** that ship the work, so the file's role shifts from "proposal" to "log" as code lands.

Cross-cutting platform-wide plans (touching runtime + site-template + charts + mu-plugin + docs together) belong in `~/Developer/frankenpress/.aidocs/` at the workspace root, not here.

The original `fp` planning + implementation arc lives at
`~/Developer/frankenpress/.aidocs/fp-go-cli.md` (still workspace-level
since it predates this directory). New fp-specific proposals land here.

## Index

| File | Status | Topic |
|---|---|---|
| [`snapshot-refuse-on-existing.md`](./snapshot-refuse-on-existing.md) | proposed | `fp snapshot` should refuse to capture when a prior snapshot dir exists under `web/imports/`, rather than silently creating a second one that then trips the apply-hook's one-snapshot-per-release invariant. Triggered by the 2026-05-13 sts-stg apply failure. |

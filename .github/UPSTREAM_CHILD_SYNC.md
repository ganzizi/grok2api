# Upstream and Child Sync

This repository uses `chenyme/grok2api` `main` as its upstream baseline and also
tracks `lij768423-svg/grok2api` `main`. Synchronization never overwrites this
repository's `main`; it maintains `automation/upstream-child-sync` and creates or
updates a pull request for review.

## Automatic Flow

`.github/workflows/sync-upstream-child.yml` runs every six hours and can also be
started manually from Actions:

1. Fetch the upstream, child, and fork `main` refs.
2. Merge the fork `main` and upstream `main` into the sync branch first.
3. Read `.github/child-sync-state.json` and skip adapted, equivalent, or explicitly excluded child commits.
4. Automatically port only small, non-deleting commits in controlled code paths.
5. Put documentation, version, Docker, and workflow changes into manual review instead of forcing them over the upstream code.
6. Stop child processing on a cherry-pick conflict while preserving the upstream update and state report.

The sync branch is pushed fast-forward only; it is never force-pushed. If a pull
request remains open, the next run continues from its existing result without
rewriting review history.

## State File

`.github/child-sync-state.json` records three classes of commits:

- `integrated`: fully merged, adapted to this repository, or already implemented equivalently.
- `excluded`: documentation, version, or release metadata only; no behavior change.
- `pending`: requires manual review or had a conflict; after handling it, add the result to `integrated` or explicitly to `excluded`.

The current state file records the Codex MCP schema simplification from 665a73a0
and the selectively ported residential generator from 94133621. The child
branch's installation prompt and branding documentation are intentionally not
copied verbatim.

## Handling Pending Commits

1. Open the sync pull request and confirm that upstream changes were not reverted by old child code.
2. Inspect each `pending` commit with `git show <commit>` and port only the required files or logic.
3. Run the backend, frontend, and script checks in this repository.
4. Record the result in the state file before merging the pull request.

This design deliberately does not merge the child branch tree as a whole. The
child can lag behind upstream and contain deletions or branding configuration;
commit- and path-level selection is what keeps long-term maintenance manageable.

---
description: >
  Describes the GitHub Actions cache and cleanup tools: the caches of a repository with their usage and the listed-only delete by cache identifier or by key, the listed-only delete of a run or an artifact and force-cancel of a run with their proof reads, and the reads of one run attempt and of the deployment reviews of a run.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# GitHub Actions caches and cleanup

The Actions cache and cleanup tools of the [GitHub provider](README.md), in the tool group Actions. Every tool
takes `repository` as described under [the target of a call](README.md#the-target-of-a-call).

| Tool | Does |
| --- | --- |
| `github.action-caches.list` | lists the caches with GitHub's filters by key prefix and ref and its sort orders |
| `github.action-cache-usage.get` | reads the number and total size of the active caches |
| `github.action-caches.delete` | deletes one cache by identifier, or every cache with one key on one ref or on every ref; listed-only |
| `github.workflow-run-attempts.get` | reads one attempt of a run |
| `github.workflow-run-approvals.list` | lists who approved or rejected which environments of a run, with comments |
| `github.workflow-runs.delete` | deletes one completed run; listed-only |
| `github.workflow-artifacts.delete` | deletes one artifact; listed-only |
| `github.workflow-runs.force-cancel` | force-cancels a run that does not respond to a cancel; listed-only |

No profile selects these tools. A connection that selects by `effects` offers the reads; each delete and the
force-cancel need their name in the connection's `tools` list and confirmation (`--confirm`). Cache contents,
artifact contents, logs, and download addresses are never read. Keys, refs, run titles, names, and comments are
untrusted data, and no address is fetched. A key is answered whole, up to the 512 characters actions/cache
accepts, so it can be handed to a delete. GitHub refreshes the usage about every five minutes, so it may lag
behind a delete.

## Deleting caches

Exactly one of `cache_id` or `key` selects what is deleted.

- **By identifier.** GitHub offers no read of a single cache, so Qatlas first looks for the identifier among the
  newest 1000 caches of the chosen repository and sends nothing when it is not there: an identifier of another
  repository, an unknown one, and one older than the newest 1000 are answered not found. Delete an older cache
  by key and ref. A cache that is gone between that read and the delete is answered `already_deleted: true`
  with `deleted_count: 0`.
- **By key.** `key` is compared exactly, unlike the prefix filter of the list. It needs `ref`, a full ref such
  as `refs/heads/main` or `refs/pull/1/merge`, or `all_refs: true` for every ref; a ref left out never widens a
  delete. Qatlas reads the caches with the same key and ref first, so a repository the token cannot see stops
  the call before the delete. GitHub's not found to the delete then means that no cache matched and is answered
  `already_deleted: true` with `deleted_count: 0`. `deleted_count` is the number GitHub reports; `caches` lists
  at most 100 of them.

## Runs and artifacts

Each run and artifact tool first reads the run or artifact and checks that GitHub's own REST address of it
names the chosen repository and the requested object. An identifier GitHub does not show is answered not found;
an answer that names another repository is refused. Either way nothing is changed or shown.

- **Deleting a run.** Only a completed run is deleted; cancel a running one and wait until it has completed.
- **Deleting an artifact.** The answer carries the artifact as read before the delete.
- **Force-cancel.** It bypasses conditions such as `always()`, so cleanup steps of the run may not run; use it
  only for a run that does not respond to `github.workflow-runs.cancel`. A completed run is reported with its
  state and nothing is sent. GitHub's refusal of a run it cannot cancel is answered as such.
- **Attempt.** `github.workflow-run-attempts.get` answers the members of `github.workflow-runs.get` as they were
  for that attempt; an answer for another run or attempt is refused.
- **Deployment reviews.** `github.workflow-run-approvals.list` shows the reviews only when every environment
  they name belongs to the chosen repository, at most 100 reviews. It approves or rejects nothing.

A run or artifact that the read found and GitHub reports missing to the delete is answered
`already_deleted: true`.

## Sending a change

Every delete and the force-cancel are sent once and never repeated. An unclear outcome (a timeout, a dropped
connection, a server error, an unreadable answer) says that the change may have been applied. An object that
changes between the read and the change is changed as GitHub finds it. The token rights are listed under
[GitHub Actions](setup.md#github-actions).

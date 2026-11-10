---
description: >
  Describes the GitHub Actions cache tools: listing the caches of a repository, reading their usage, and the listed-only delete by cache identifier or by key with its proof read and limits.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# GitHub Actions caches and cleanup

The Actions cache tools of the [GitHub provider](README.md), in the tool group Actions. Every tool takes
`repository` as described under [the target of a call](README.md#the-target-of-a-call).

| Tool | Does |
| --- | --- |
| `github.action-caches.list` | lists the caches with GitHub's filters by key prefix and ref and its sort orders |
| `github.action-cache-usage.get` | reads the number and total size of the active caches |
| `github.action-caches.delete` | deletes one cache by identifier, or every cache with one key on one ref or on every ref; listed-only |

No profile selects these tools. A connection that selects by `effects` offers the two reads; the delete needs
its name in the connection's `tools` list and confirmation (`--confirm`). Cache contents are never read. Keys
and refs are untrusted data. A key is answered whole, up to the 512 characters actions/cache accepts, so it can
be handed to a delete. GitHub refreshes the usage about every five minutes, so it may lag behind a delete.

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

The delete is sent once and never repeated. An unclear outcome (a timeout, a dropped connection, a server error,
an unreadable answer) says that caches may have been deleted. A cache that changes between the read and the
delete is deleted as GitHub finds it. The token rights are listed under [GitHub Actions](setup.md#github-actions).

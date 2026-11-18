---
description: >
  Describes the Google Cloud Storage tool managed-folders.list: its arguments, the target check through the bucket's project, paging with a cursor, the fields returned, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud managed folders

`managed-folders.list` reads the managed folders of a bucket from Cloud Storage (`managedFolders.list` of
`storage` v1, discovery revision 20261111). It changes nothing: creating, updating, and deleting managed folders and
reading their IAM policies are not offered. The request is a GET to
`https://storage.googleapis.com/storage/v1/b/{bucket}/managedFolders`.

| Tool | Arguments |
| --- | --- |
| `managed-folders.list` | `bucket`, `prefix`, `page_size`, `cursor` |

`bucket` follows the rules of [buckets](buckets.md): it is checked before any secret is read and is never a path,
and the bucket is bound to the targets by one read of the bucket and its project number before the list is read.
`prefix` is 1 to 1024 characters without control characters and is sent as the query parameter only. `page_size`
is 1 to 1000 and defaults to 100. `cursor` is the opaque `next_cursor` of a previous page of the same list; it is
bound to the connection's targets, the bucket, and the prefix.

The result is `managed_folders`, each with `name`, `metageneration`, `createTime`, `updateTime`, `id`, `kind`,
`bucket`, `selfLink`, and `rapidCacheConfig`, with `has_more` and `next_cursor`. Names are untrusted text. A row
without a name, or of another bucket, is an invalid response. Only a bucket with uniform bucket-level access has
managed folders; Google refuses the call for another bucket.

The method lists `devstorage.read_only`, so the tool asks for it and belongs to the profile `read`. The account
needs `storage.buckets.get` for the proof read and `storage.managedFolders.list` for the list, and the Cloud Storage
JSON API must be enabled in the project.

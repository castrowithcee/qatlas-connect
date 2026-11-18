---
description: >
  Describes the Google Cloud Storage tool bucket-access-controls.list: its arguments, the target check through the bucket's project, the access control entries returned and why entities are returned unchanged, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud bucket access controls

`bucket-access-controls.list` reads the access control list of a bucket from Cloud Storage
(`bucketAccessControls.list` of `storage` v1, discovery revision 20261111). It changes nothing: creating,
updating, and deleting entries are not offered. The request is a GET to `https://storage.googleapis.com/storage/v1/b/{bucket}/acl`.

| Tool | Arguments |
| --- | --- |
| `bucket-access-controls.list` | `bucket`, `user_project` |

`bucket` and `user_project` follow the rules of [buckets](buckets.md): the bucket name is checked before any secret
is read and is never a path, and the bucket is bound to the targets by one read of the bucket and its project
number before the list is read. A bucket outside the targets is refused without naming it.

The result is `items`, each entry with `entity`, `role`, `email`, `domain`, `entityId`, `projectTeam`
(`projectNumber` and `team`), `id`, `kind`, `bucket`, `selfLink`, and `etag`. Entities such as `allUsers`,
`allAuthenticatedUsers`, `user-EMAIL`, and `group-EMAIL` are personal or untrusted data and are returned unchanged,
with nothing flagged or removed. Google allows at most 100 entries per bucket; an answer with more, or with an
entry of another bucket, is an invalid response. A bucket with uniform bucket-level access refuses the call with
an error of Google. The list is empty if Google sends no entries.

The method lists only `cloud-platform` and `devstorage.full_control`, so the tool asks for
`devstorage.full_control` and belongs to the profile `read-storage-access`, not to `read`. The token can change
what its account may in Cloud Storage, although no tool does. The account needs `storage.buckets.get` for the proof read of the
bucket's project and `storage.buckets.getIamPolicy` for the list, and the Cloud Storage JSON API must be enabled in the project.

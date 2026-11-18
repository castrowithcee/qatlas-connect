---
description: >
  Describes the Google Cloud Storage tool object-access-controls.list: its arguments, the target check through the bucket, the access control entries returned and why entities are returned unchanged, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud object access controls

`object-access-controls.list` reads the access control list of one object from Cloud Storage
(`objectAccessControls.list` of `storage` v1, discovery revision 20261111). It changes nothing: creating, updating,
and deleting entries are not offered. The request is a GET to
`https://storage.googleapis.com/storage/v1/b/{bucket}/o/{object}/acl`.

| Tool | Arguments |
| --- | --- |
| `object-access-controls.list` | `bucket`, `object`, `generation`, `user_project` |

`bucket`, `object`, and `user_project` follow the rules of [objects](objects.md): the bucket name and the object name
are checked before any secret is read, the object name goes out as one encoded path segment, and the bucket is bound
to the targets by one read of the bucket and its project number before the list is read. A bucket outside the targets
is refused without naming it.

The result is `items`, each entry with `entity`, `role`, `email`, `domain`, `entityId`, `projectTeam`
(`projectNumber` and `team`), `id`, `kind`, `bucket`, `object`, `generation`, `selfLink`, and `etag`. Entities such as
`allUsers`, `allAuthenticatedUsers`, `user-EMAIL`, and `group-EMAIL` are personal or untrusted data and are returned
unchanged, with nothing flagged or removed. Google allows at most 100 entries; an answer with more, or with an entry
of another bucket or object, is an invalid response. A bucket with uniform bucket-level access refuses the call with
an error of Google. Reading is billed like the other object tools (see [objects](objects.md)).

The method lists only `cloud-platform` and `devstorage.full_control`, so the tool asks for `devstorage.full_control`
and belongs to the profile `read-storage-access`, not to `read`. The token can change what its account may in Cloud
Storage, although no tool does.

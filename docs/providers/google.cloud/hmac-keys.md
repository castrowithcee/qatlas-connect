---
description: >
  Describes the Google Cloud Storage tools hmac-keys.list, hmac-keys.get, hmac-keys.deactivate, and hmac-keys.delete: their arguments, the project and service account checks, the fixed metadata fields, why a key value is never returned, how a key is deactivated and deleted, the scopes and profiles, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud HMAC keys

Two tools read the metadata of the HMAC keys of a project from Cloud Storage (`projects.hmacKeys.list` and
`projects.hmacKeys.get` of `storage` v1, discovery revision 20261111). They change nothing. The requests are GETs to
`https://storage.googleapis.com/storage/v1/projects/{project}/hmacKeys` and `.../hmacKeys/{accessId}`. Two more
tools [revoke a key](#revoking-a-key). Creating a key (`hmacKeys.create`) is not offered.

| Tool | Arguments |
| --- | --- |
| `hmac-keys.list` | `project_id`, `service_account_email`, `show_deleted_keys`, `max_results`, `cursor`, `user_project` |
| `hmac-keys.get` | `project_id`, `access_id`, `user_project` |
| `hmac-keys.deactivate` | `project_id`, `access_id`, `etag`, `user_project` |
| `hmac-keys.delete` | `project_id`, `access_id`, `user_project` |

**Key values are never returned.** The result has a fixed list of fields: `accessId`, `state`,
`serviceAccountEmail`, `projectId`, `timeCreated`, `updated`, `etag`, `id`, `kind`, and `selfLink`. Anything else in
Google's answer, a `secret` at any depth included, is dropped, and a field of another type than a string makes the
answer invalid. Google sends a key value only for `hmacKeys.create`, which is not offered.

**Project.** `project_id` is checked against the targets before any secret is read, like the bucket tools. Qatlas
then reads the project with `projects.get` to learn its ID and number, so the connection needs that tool, and its
scope `cloud-platform.read-only`, next to these two (`read` holds both). Every returned key is checked again: a key
whose `projectId` is not the project (in its ID or number form) is dropped from a list and refused by `get`.

**Service account.** `service_account_email` must be a service account of the project:
`NAME@PROJECT_ID.iam.gserviceaccount.com`, the Compute Engine default account
`PROJECT_NUMBER-compute` in the domain `developer.gserviceaccount.com`, or the App Engine default account. The form is checked
before any secret is read. The project ID and number come from the `projects.get` read, and an account of another
project is refused before the list is requested, without naming it. Qatlas does not look the account up. A returned
key of another account than the filter is dropped.

`access_id` is 8 to 128 letters, digits, hyphens, and underscores, is checked before any secret is read, and is
escaped in the path. `max_results` is 1 to 250 and defaults to 100. `cursor` is bound to the targets, the project,
the filter, `show_deleted_keys`, and `user_project`. `show_deleted_keys` includes keys in the state `DELETED`.

Both methods list `devstorage.read_only`, so the tools ask for it and belong to the profile `read`. The account needs
`storage.hmacKeys.list` or `storage.hmacKeys.get` on the project, and the Cloud Storage JSON API must be enabled.

## Revoking a key

`hmac-keys.deactivate` is `projects.hmacKeys.update` (`PUT .../hmacKeys/{accessId}`) and `hmac-keys.delete` is
`projects.hmacKeys.delete`. Each needs the confirmation of a person and is sent once, without a retry. After a
timeout, a 5xx answer, or an unreadable answer the tool says the outcome is unknown, and the key must be read before
it is changed again.

**Deactivating.** The request body holds `state` fixed to `INACTIVE` and, if `etag` is given, that etag, nothing
else. No argument sets a state, so `ACTIVE` cannot be sent, and no tool reactivates a key: this is a revocation, not
a new access. `etag` is `hmac-keys.get`'s etag and is only a comparison. Qatlas refuses the change if the key it reads
has another etag, and Google refuses it if the key changed after that read. Without `etag` the key is deactivated
whatever its etag. The result is the metadata of the key as Google returns it, with the fixed fields above.

**Deleting.** Google deletes only a key in the state `INACTIVE` and answers an `ACTIVE` key with an error, which the
tool returns; deactivate the key first. A deleted key cannot be restored. Google answers with an empty body, so the
result is `projectId` and `accessId` as given and `deleted`.

**Project check before the change.** Both tools check `project_id` against the targets, read the project with
`projects.get`, and then read the key with `hmacKeys.get`. A key whose `projectId` is not the project, in its ID or
number form, ends the call with a refusal that names neither project, and no change is sent. The confirmation names
the project and the `accessId` (and the etag, if given), as the person reads them. The service account of the key is
not an argument: it is part of what `hmac-keys.get` shows, and the key is bound to the project, not to the account.
The time between the reading and the change is a documented limit.

No result or error text holds a key value: the metadata type has no member for it, and an error text of Google is
cut and masked.

**Scope and profile.** `hmacKeys.update` lists `devstorage.full_control` and `cloud-platform`, so the deactivation asks
for `devstorage.full_control`. `hmacKeys.delete` lists `devstorage.read_write` as well, but the tool reads the key
first and `hmacKeys.get` accepts only `full_control` and `read_only`, so it asks for `full_control` too. Google also
uses that scope to create keys, so the token may do that although no tool does. `hmac-keys.deactivate` belongs to the profile
`storage-revoke`, which is not recommended and needs `projects.get`. `hmac-keys.delete` is listed-only: it belongs to no
profile, and a connection offers it only when its `tools` list names it. The account needs `storage.hmacKeys.update` or
`storage.hmacKeys.delete` on the project.

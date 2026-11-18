---
description: >
  Describes the Google Cloud Storage tools hmac-keys.list and hmac-keys.get: their arguments, the project and service account checks, the fixed metadata fields, why a key value is never returned, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud HMAC keys

Two tools read the metadata of the HMAC keys of a project from Cloud Storage (`projects.hmacKeys.list` and
`projects.hmacKeys.get` of `storage` v1, discovery revision 20261111). They change nothing: creating, updating, and
deleting keys are not offered. The requests are GETs to
`https://storage.googleapis.com/storage/v1/projects/{project}/hmacKeys` and `.../hmacKeys/{accessId}`.

| Tool | Arguments |
| --- | --- |
| `hmac-keys.list` | `project_id`, `service_account_email`, `show_deleted_keys`, `max_results`, `cursor`, `user_project` |
| `hmac-keys.get` | `project_id`, `access_id`, `user_project` |

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

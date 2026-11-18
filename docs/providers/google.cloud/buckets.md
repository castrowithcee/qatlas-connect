---
description: >
  Describes the Google Cloud Storage tools buckets.list, buckets.get, buckets.get-iam-policy, and buckets.delete: their arguments, the bucket name rule, how a bucket is bound to the targets through its project number, user_project, paging, the fields returned, how policy members are returned, the confirmed, listed-only deletion of an empty bucket, the scopes and profiles, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud Storage buckets

Three tools read the buckets of a project from Cloud Storage (`buckets.list`, `buckets.get`, and
`buckets.getIamPolicy` of `storage` v1, discovery revision 20261111). They change nothing. A fourth tool deletes an
empty bucket, see [Deleting](#deleting). Creating, patching, and locking a bucket, and changing its IAM policy or
ACL, are not offered. The access control list of a
bucket is read by [bucket-access-controls.list](bucket-access-controls.md). Every request is a GET to the fixed
origin `https://storage.googleapis.com` below `/storage/v1`; no argument takes a path, URL, header, or body.

| Tool | Arguments |
| --- | --- |
| `buckets.list` | `project_id`, `prefix`, `max_results`, `cursor`, `projection`, `soft_deleted`, `return_partial_success`, `user_project` |
| `buckets.get` | `bucket`, `projection`, `if_metageneration_match`, `soft_deleted`, `generation`, `user_project` |
| `buckets.get-iam-policy` | `bucket`, `options_requested_policy_version`, `user_project` |
| `buckets.delete` | `bucket`, `project` (required), `if_metageneration_match`, `if_metageneration_not_match`, `user_project` |

## Bucket names

`bucket` must be 3 to 63 lowercase letters, digits, hyphens, underscores, and dots, starting and ending with a
letter or digit. Qatlas checks it before any secret is read and never takes it as a path: a slash, a URL, a
`gs://` name, a query, and a traversal are refused, and the name is escaped in the request path. A bucket whose
name is longer than 63 characters (Google allows up to 222 for names with dots) is therefore not reachable.

## Targets

A bucket belongs to a project that only Google can name. Qatlas therefore binds a bucket through its
`projectNumber`:

- `buckets.list` checks `project_id` against the targets before any secret is read, as the Compute tools do. The
  filter `project` that Qatlas sends is only an optimisation: every row is checked again by its `projectNumber`,
  and a row outside the targets or without a project number is dropped on a connection with targets. A page can
  therefore be empty while more follow.
- `buckets.get` reads the bucket and refuses it, with the same message for every bucket, if its project number
  lies outside the targets. The refusal does not name the bucket or the project.
- `buckets.get-iam-policy` and `bucket-access-controls.list` first read the bucket once (`buckets.get` with the
  fields `name` and `projectNumber`) and read the policy or list only if its project lies in the targets. A
  connection without targets needs no proof and reads the policy directly.
- A project number that the targets do not decide, for instance with a folder target or a project target named by
  its ID, is proved by reading the project and the folders above it, once per number and call. These reads use
  the scope of `projects.get`, so a connection bound to an organization, a folder, or a project named by ID needs
  `projects.get` next to the Storage tools.
- `user_project` names the project billed for a Requester Pays bucket. It is checked against the targets like
  `project_id`, before any secret is read where the targets decide it, and only a project in the targets is
  accepted.

## Listing

`max_results` is 1 to 1000. Without it the tool asks for 100, which keeps a page with the full projection below
the size Qatlas accepts for an answer; a larger page with ACLs can exceed that size and then ends with an error.
`next_cursor` carries Google's `pageToken` and is bound to the tool, the targets, the project, and the filters.
`projection` is `full` (the rows carry `acl` and `defaultObjectAcl`) or `noAcl`, which is Google's default. With
`return_partial_success`, `unreachable` names the locations Google could not read.

## Fields

Each bucket carries every documented, not deprecated member of the Bucket resource: `name`, `id`, `kind`,
`selfLink`, `projectNumber`, `metageneration`, `generation`, `location`, `locationType`, `storageClass`, `rpo`,
`timeCreated`, `updated`, `softDeleteTime`, `hardDeleteTime`, `etag`, `defaultEventBasedHold`, `satisfiesPZS`,
`satisfiesPZI`, `labels`, `versioning`, `lifecycle`, `encryption`, `iamConfiguration`, `retentionPolicy`,
`softDeletePolicy`, `autoclass`, `website`, `logging`, `cors`, `hierarchicalNamespace`, `billing`,
`customPlacementConfig`, `ipFilter`, `objectRetention`, `owner`, and with `projection` `full` `acl` and
`defaultObjectAcl`. `buckets.get` refuses an answer for another bucket. Names, labels, and website settings are
untrusted third-party text; strings are cut to 1024 bytes, and labels, lists, and nested values are bounded.

`generation` is accepted only together with `soft_deleted`. `if_metageneration_match` makes Google refuse the
read if the metageneration differs; Qatlas reports that refusal (HTTP 412) as an invalid response, because the
shared error mapping holds no class for it. `ifMetagenerationNotMatch` is not offered, because Google
answers it with an empty 304.

## IAM policy

`buckets.get-iam-policy` returns `kind`, `resourceId`, `version`, `etag`, and `bindings`, each with `role`,
`members`, and the optional `condition` (`title`, `description`, `expression`, `location`). Members such as
`allUsers`, `allAuthenticatedUsers`, users, groups, and service accounts are personal or untrusted data and are
returned unchanged, with nothing flagged or removed. At most 500 bindings and 500 members per binding are
returned, and `truncated` is true if more were cut. Without `options_requested_policy_version` the tool asks
for version 3 so that conditional bindings are visible; 1 and 3 are accepted.

## Deleting

`buckets.delete` (`DELETE /storage/v1/b/{bucket}`) deletes one bucket. It is listed-only: it is offered only on a
connection whose `tools` list names `google.cloud.buckets.delete` exactly, belongs to no profile, and needs the
confirmation of a person for each call. The bucket must be empty. Google refuses the call otherwise, and the tool
never deletes an object, so emptying a bucket is a separate step. The bucket name becomes free for others, and its
settings, labels, and policies are lost. Before the request the tool reads the bucket once and binds it to the
targets by its project number; a bucket outside the targets is refused without naming it, and nothing is deleted.
`project` is required, a project ID or number, and is only compared, never written. It must lie in the targets. The
confirmation names the bucket and this project. After the read of the bucket, and before the DELETE, the tool checks
that the project number of the bucket is that of `project` (an ID is resolved by reading the project, which needs
`projects.get`, as for any project the targets do not decide) and refuses the call without a change otherwise,
naming neither project; the bucket is read for this even on a connection without targets.
`if_metageneration_match` and `if_metageneration_not_match` are decimal numbers. Google answers with an empty body,
so the result is `bucket` and `deleted`. The request is sent once and never repeated; after an unclear outcome the
error says so, and the bucket should be read before the call is made again.

## Scopes and profiles

`buckets.list` and `buckets.get` ask for `devstorage.read_only` and belong to the profile `read`.
`buckets.getIamPolicy` lists only `cloud-platform` and `devstorage.full_control`, so `buckets.get-iam-policy`
asks for `devstorage.full_control`, the narrower of the two, and belongs, with `bucket-access-controls.list`, to
the profile `read-storage-access`, not to `read`. That scope also lets a token change what its account may in
Cloud Storage, although no tool does. The account needs `storage.buckets.list`, `storage.buckets.get`, and
`storage.buckets.getIamPolicy` on the project or bucket, and the Cloud Storage JSON API must be enabled in the project. For a Requester Pays
bucket the account also needs `serviceusage.services.use` on `user_project`.

`buckets.delete` lists `devstorage.read_write` next to `devstorage.full_control` and `cloud-platform`, so it asks for
`devstorage.read_write`. A connection that names it in `tools` asks for that scope and needs the effect `delete`
through the list. The account needs `storage.buckets.delete` and `storage.buckets.get`.

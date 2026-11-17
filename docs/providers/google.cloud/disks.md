---
description: >
  Describes the Google Cloud Compute disk tools disks.list, disks.aggregated-list, and disks.get: their arguments, the compute.readonly scope and the profile read, the target check, the cursor, partial results with unreachables and warning, the result members, the rule that encryption key material is never returned, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud Compute disks

Three tools read the persistent disks of a project from Compute Engine (`compute` v1, discovery revision
20261106). They change nothing and create, resize, or attach no disk. Every request goes to the fixed origin
`https://compute.googleapis.com` with the base path `/compute/v1`; no argument takes a path, URL, header, or body.

| Tool | Google method | Arguments |
| --- | --- | --- |
| `disks.list` | `disks.list` | `project_id`, `zone`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `disks.aggregated-list` | `disks.aggregatedList` | as above without `zone`, plus `include_all_scopes` |
| `disks.get` | `disks.get` | `project_id`, `zone`, `disk` (a name or its numeric ID) |

`project_id`, `zone`, the filter, the sort order, the page size, the cursor, partial results, and the target
check work as for [Compute instances](instances.md). Without `max_results` Google's default page applies.

## Result

Every documented member of the Disk resource is returned except `options` (internal use only), `params` (input only), and
the key material below, among them `name`, `id`, `sizeGb`, `type`, `status`, `zone` or `region`,
`sourceImage`, `sourceSnapshot`, `users`, `labels`, `physicalBlockSizeBytes`, `lastAttachTimestamp`,
`lastDetachTimestamp`, `provisionedIops`, `resourcePolicies`, and `creationTimestamp`. Text is untrusted
third-party content and bounded like the other Compute tools. A row must name a disk, a row that names a zone or
region must name the one asked for, and `disks.get` refuses an answer for another name or ID.

`disks.aggregated-list` groups the disks in `scopes`, each entry `{scope, disks, warning}` with `scope` as
`zones/ZONE` or `regions/REGION` (regional disks), sorted by scope. Scopes without disks whose only warning is
`NO_RESULTS_ON_PAGE` are left out.

**Encryption keys.** `rawKey` and `rsaEncryptedKey` are never returned, in any letter case and at every depth of an
answer, including `diskEncryptionKey`, `sourceImageEncryptionKey`, `sourceSnapshotEncryptionKey`, and `warning`.
`kmsKeyName`, `kmsKeyServiceAccount`, and `sha256` stay. This is the one exception to the full breadth of the
result.

## Scope and rights

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account needs
`compute.disks.list` or `compute.disks.get` (the Compute Viewer role has them), and the Compute Engine API must be
enabled in the project.

## Not verified

The answers of Compute Engine were not seen live; which members Google fills depends on the disk.

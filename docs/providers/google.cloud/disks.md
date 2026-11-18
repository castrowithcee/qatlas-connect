---
description: >
  Describes the Google Cloud Compute disk tools disks.list, disks.aggregated-list, and disks.get the changes disks.insert, disks.resize, and disks.create-snapshot, and the listed-only deletion disks.delete: their arguments, the scopes compute.readonly and compute with the profiles read and compute-disks, the target check, the cursor, partial results with unreachables and warning, the result members, the rule that encryption key material is never returned or taken, how sources and references are built, the size check before growing, the comparison of size and attached instances before deleting, the confirmation, the request ID and the returned operation, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Cloud Compute disks

Three tools read the persistent disks of a project from Compute Engine (`compute` v1, discovery revision
20261106), three more create a disk, grow a disk, and snapshot a disk ([Changes](#changes)), and one deletes a disk
([Deleting a disk](#deleting-a-disk)). None of them attaches or shares a disk or snapshot. Every request goes to the fixed origin `https://compute.googleapis.com` with
the base path `/compute/v1`; no argument takes a path, URL, header, or body.

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

The read tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.disks.list` or `compute.disks.get` (the Compute Viewer role has them), and the Compute Engine API must
be enabled in the project. The rights of the changes are in [Set up Google Cloud](setup.md).

## Changes

| Tool | Google method | Effect | Named in the confirmation |
| --- | --- | --- | --- |
| `disks.insert` | `disks.insert` | `create` | project, zone, name, size, type, source, provisioned IOPS and throughput; the disk is billed until it is deleted |
| `disks.resize` | `disks.resize` | `update` | project, zone, disk, new size; a disk cannot be shrunk again |
| `disks.create-snapshot` | `disks.createSnapshot` | `create` | project, zone, disk, snapshot, type, storage location; the snapshot is billed until it is deleted |

The three tools belong to the profile `compute-disks`, which is not recommended, asks for the scope `compute`, needs
the effects `create` and `update`, and holds `projects.get` as well. Every call needs the confirmation of a person.

- **Body.** The tools build the Disk or Snapshot from structured arguments, one per documented field that a request
  may set; there is no passthrough of a body. Left out are the fields of a regional disk (`replicaZones`, `region`),
  `sourceStorageObject` (a free Cloud Storage address), `asyncPrimaryDisk` (a lasting replication link from another
  disk), `params.resourceManagerTags` (tags can decide IAM conditions and organization policies), `options` and
  `locationHint` (internal use), `labelFingerprint` (only for changing labels), and of a snapshot the sources other
  than the disk of the call.
- **Sources and references.** Google takes every other resource as a URL; the tools take names and build the URL
  themselves. Images, image families, snapshots, disks, instant snapshots, disk types, storage pools, and resource
  policies are those of the project of the call, which the target check covers: disks, instant snapshots, disk
  types, and storage pools of the zone, resource policies of its region. An image, image family, or license may also
  come from one of Google's public image projects, a fixed list that the descriptor of `disks.insert` names. Any
  other project is refused, because an image of a project outside the targets would otherwise be copied into the
  disk. At most one source is taken.
- **Encryption keys.** `rawKey` and `rsaEncryptedKey` are no argument of any tool, and the key fields of a source
  (`sourceImageEncryptionKey`, `sourceSnapshotEncryptionKey`, `sourceDiskEncryptionKey`,
  `sourceInstantSnapshotEncryptionKey`) are left out, because they hold only customer-supplied keys, which Google
  marks deprecated. A disk or snapshot is encrypted with a Cloud KMS key through `kms_key_name` and optionally
  `kms_key_service_account`. The key may lie in another project; the Compute Engine service agent needs the right to
  use it. Key material is dropped from the returned operation as from the reads.
- **Size check.** `disks.resize` reads the disk after the confirmation and sends the change only if `size_gb` is
  larger than its `sizeGb`; otherwise it fails without a change. The time between the read and the change is a
  limit.
- **Labels and description.** Labels follow the read rule of [Google Cloud](README.md#boundary) (lowercase ASCII),
  which is narrower than what Google accepts, and a value may be empty. The length of a description is a Qatlas
  bound, because the discovery document gives none.
- **Request and result.** Each call sends one POST and never repeats it. `request_id` is a UUID that is not all
  zero; without it the tool creates one and returns it. Repeating a call with the same `request_id` is idempotent:
  Google ignores a request whose ID it already received. The tool never waits: the result is
  `{operation, scope: "zone", zone, request_id}`, and `compute-operations.get` with scope `zone` reads the state. An
  operation that is already `DONE` with an error is reported as a provider error. An answer without an operation of
  the zone, or a failure after which the request may have arrived, says that the outcome is unknown: read the disk
  with `disks.get` or the snapshot with `snapshots.get`.

## Deleting a disk

`disks.delete` (`disks.delete` of Compute Engine, effect `delete`) removes a disk and all data on it for good; its
snapshots stay. It is listed-only: it is in no profile, and only a `tools` list that names it exactly releases it. It
asks for the scope `compute`; the rights are in [Set up Google Cloud](setup.md).

- **Size and attached instances.** The call names the size and the attached instances that `disks.get` showed, as
  `expected_size_gb` and `expected_users` (the instance names of `users`, empty for an unused disk), and the
  confirmation shows them. After the confirmation the tool reads the disk and deletes it only if `sizeGb` and the
  instances of `users` still match; otherwise it fails without a deletion. A user that is no instance of the zone of
  the disk counts as an invalid answer. The time between the read and the deletion is a limit.
- **Disk in use.** Google refuses a disk that an instance still uses; the tool reports that refusal.
- **Request and result.** One DELETE, never repeated, with the request ID, the zonal operation, and the unknown
  outcome as for the changes above.

## Not verified

- The answers of Compute Engine were not seen live; which members Google fills depends on the disk.
- The changes and the deletion were checked against a simulated Compute Engine only. The list of public image projects follows
  Google's list of operating system details; a project Google adds later is refused until the list is extended.

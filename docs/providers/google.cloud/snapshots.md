---
description: >
  Describes the Google Cloud Compute tools snapshots.list and snapshots.get: their arguments, the target check, the fields returned, why customer-supplied key material is never returned while the Cloud KMS key name stays, the scope and profiles, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud disk snapshots

Two tools read the global disk snapshots of a project from Compute Engine (`snapshots.list` and `snapshots.get` of
`compute` v1, discovery revision 20261106). They change nothing, create no snapshot, and read no IAM policy of a
snapshot. Every request goes to the fixed origin `https://compute.googleapis.com`; no argument takes a path, URL,
header, or body.

| Tool | Arguments |
| --- | --- |
| `snapshots.list` | `project_id`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `snapshots.get` | `project_id`, `snapshot` (a snapshot name or its numeric ID) |

The checks, the target rule, the structured `filter`, the cursor, the page size, and the handling of `warning` are
those of [instances](instances.md). The list has no zone and no `include_all_scopes`. A snapshot name or ID that is
not in Compute's form is refused before any secret is read or request is sent.

The result is `snapshots`, each with the documented members of the Snapshot resource, except those Google marks
deprecated or input only: `name`, `id`, `description`, `status`, `sourceDisk`, `sourceDiskId`, `diskSizeGb`,
`storageBytes`, `downloadBytes`, `creationSizeBytes`, `snapshotType`, `storageLocations`, `labels`,
`labelFingerprint`, `autoCreated`, `chainName`, `licenses`, `licenseCodes`, `guestOsFeatures`, `architecture`,
`snapshotEncryptionKey`, `sourceDiskEncryptionKey`, `sourceInstantSnapshotEncryptionKey`,
`sourceInstantSnapshot`, `sourceInstantSnapshotId`, `sourceDiskForRecoveryCheckpoint`,
`sourceSnapshotSchedulePolicy`, `sourceSnapshotSchedulePolicyId`, `snapshotGroupName`, `snapshotGroupId`,
`locationHint`, `region`, `enableConfidentialCompute`, `satisfiesPzs`, `satisfiesPzi`, `selfLink`, `kind`, and
`creationTimestamp`. `storageBytesStatus` (deprecated), `params`, and `guestFlush` (input only) are left out. A row
must name a snapshot, and `snapshots.get` refuses an answer for another name or ID. The content is untrusted
third-party text and bounded.

**Key material.** `rawKey` and `rsaEncryptedKey`, the customer-supplied key material of a
`CustomerEncryptionKey`, are never returned: not in the result and not in the output schema. Qatlas drops both
members, in any letter case and with or without `_` or `-`, at every depth of an answer before it does anything else
with it, so no encryption key member of a snapshot, nor `labels` or `warning`, can carry them. `kmsKeyName`,
`kmsKeyServiceAccount`, and `sha256` stay visible.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account needs
`compute.snapshots.list` or `compute.snapshots.get` (the Compute Viewer role has them), and the Compute Engine API
must be enabled in the project.

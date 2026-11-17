---
description: >
  Describes the Google Cloud Compute tools images.list and images.get: their arguments, the target check, why only the images of the bound project are read, the fields returned, why customer-supplied key material is never returned while the Cloud KMS key name stays, the scope and profiles, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud images

Two tools read the images of a project from Compute Engine (`images.list` and `images.get` of `compute` v1,
discovery revision 20261106). They change nothing, create and deprecate no image, and read no IAM policy of an
image. Every request goes to the fixed origin `https://compute.googleapis.com`; no argument takes a path, URL,
header, or body.

| Tool | Arguments |
| --- | --- |
| `images.list` | `project_id`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `images.get` | `project_id`, `image` (an image name or its numeric ID) |

The checks, the target rule, the structured `filter`, the cursor, the page size, and the handling of `warning` are
those of [instances](instances.md). The list has no zone and no `include_all_scopes`. Both tools read the images of
the project named in `project_id` only: the public image projects (such as `debian-cloud`) are not read, and the
newest image of a family (`images.getFromFamily`) is not offered. An image name or ID that is not in Compute's form
is refused before any secret is read or request is sent.

The result is `images`, each with the documented members of the Image resource, except `params` (input only):
`name`, `id`, `description`, `status`, `family`, `archiveSizeBytes`, `diskSizeGb`, `sourceType`, `rawDisk`,
`sourceDisk`, `sourceDiskId`, `sourceImage`, `sourceImageId`, `sourceSnapshot`, `sourceSnapshotId`, `licenses`,
`licenseCodes`, `guestOsFeatures`, `storageLocations`, `labels`, `labelFingerprint`, `deprecated`, `architecture`,
`shieldedInstanceInitialState`, `imageEncryptionKey`, `sourceDiskEncryptionKey`, `sourceImageEncryptionKey`,
`sourceSnapshotEncryptionKey`, `enableConfidentialCompute`, `satisfiesPzs`, `satisfiesPzi`, `selfLink`, `kind`, and
`creationTimestamp`. `deprecated` holds the deprecation state of the image as Google sends it. A row must name an
image, and `images.get` refuses an answer for another name or ID. The content is untrusted third-party text and
bounded.

**Key material.** `rawKey` and `rsaEncryptedKey`, the customer-supplied key material of a
`CustomerEncryptionKey`, are never returned, as on [snapshots](snapshots.md): Qatlas drops both members, in any
letter case and with or without `_` or `-`, at every depth of an answer. `kmsKeyName`, `kmsKeyServiceAccount`, and
`sha256` stay visible.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account needs
`compute.images.list` or `compute.images.get` (the Compute Viewer role has them), and the Compute Engine API must
be enabled in the project.

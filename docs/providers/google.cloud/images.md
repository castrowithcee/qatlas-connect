---
description: >
  Describes the Google Cloud Compute tools images.list and images.get and the changes images.insert and images.delete: their arguments, the target check, why only the images of the bound project are read or used as a source, the fields returned and the fields left out of a new image, why customer-supplied key material is never returned or taken while the Cloud KMS key name stays, the confirmation, the request ID and the returned operation, the scopes and profiles, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Cloud images

Two tools read the images of a project from Compute Engine (`images.list` and `images.get` of `compute` v1,
discovery revision 20261106), and two more create and delete an image ([Changes](#changes)). No tool deprecates an
image, shares it, or reads or changes its IAM policy. Every request goes to the fixed origin
`https://compute.googleapis.com`; no argument takes a path, URL, header, or body.

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

The read tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.images.list` or `compute.images.get` (the Compute Viewer role has them), and the Compute Engine API
must be enabled in the project. The rights of the changes are in [Set up Google Cloud](setup.md).

## Changes

| Tool | Google method | Effect | Named in the confirmation |
| --- | --- | --- | --- |
| `images.insert` | `images.insert` | `create` | project, image, source, family, storage location, `force_create`; the image is billed until it is deleted |
| `images.delete` | `images.delete` | `delete` | project, image; the image is lost for good and can no longer be used as a source |

`images.insert` belongs to the profile `compute-images-create`, which is not recommended, asks for the scope
`compute`, needs the effect `create`, and holds `projects.get` as well. `images.delete` is listed-only: it belongs to
no profile, and a connection offers it only when its `tools` list names it. Every call needs the confirmation of a
person.

- **Source.** A new image has exactly one source, a disk with its zone, a snapshot, or an image, named by name.
  The tool builds the URL in the project of the call, which the target check covers, so a resource of another
  project, including a public image project, cannot be the source. An archive in Cloud Storage (`rawDisk`, with
  `sourceType`) is no source, because it would read a free Cloud Storage address. Google refuses a disk attached to
  a running instance unless `force_create` is set; the image may then not be consistent.
- **Body.** The tool builds the Image from structured arguments, one per documented field that a request may set;
  there is no passthrough of a body. Licenses follow the rule of [disks](disks.md#changes): of the project itself or
  of a public image project. Left out are `licenseCodes` (a code names a license of any project),
  `params.resourceManagerTags` (tags can decide IAM conditions and organization policies), `labelFingerprint` (only
  for changing labels), and `diskSizeGb` and `archiveSizeBytes`, which Google derives from the source.
  `shieldedInstanceInitialState` takes the secure boot key files as base64 within a Qatlas bound; Google describes
  them as public keys.
- **Encryption keys.** `rawKey` and `rsaEncryptedKey` are no argument, and the key fields of a source
  (`sourceDiskEncryptionKey`, `sourceSnapshotEncryptionKey`, `sourceImageEncryptionKey`) are left out, because they
  hold only customer-supplied keys, which Google marks deprecated. An image is encrypted with a Cloud KMS key through
  `kms_key_name` and optionally `kms_key_service_account`, as for a disk.
- **Deleting.** `images.delete` takes the name or numeric ID of an image of the project and sends one DELETE without
  reading the image first. Disks and instances created from it keep running; a family that named it as its newest
  image then names the next newest one.
- **Request and result.** Each call sends one request and never repeats it. `request_id` is a UUID that is not all
  zero; without it the tool creates one and returns it. Repeating a call with the same `request_id` is idempotent:
  Google ignores a request whose ID it already received. The tool never waits: the result is
  `{operation, scope: "global", request_id}`, and `compute-operations.get` with scope `global` reads the state. An
  operation that is already `DONE` with an error is reported as a provider error; a failure after which the request
  may have arrived says that the outcome is unknown: read the image with `images.get`. Key material is dropped from
  the returned operation as from the reads.

## Not verified

- The changes were checked against a simulated Compute Engine only; which sources and fields Google accepts in a
  project, and the form of its refusals, were not seen live.

---
description: >
  Describes the Google Cloud Compute tool machine-types.list: its arguments, the target check, the shared filter, cursor, and partial-result rules, the fields returned, the scope, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud machine types

`machine-types.list` lists the machine types available in one zone of a project from Compute Engine
(`machineTypes.list` of `compute` v1, discovery revision 20261106). It changes nothing. The arguments are
`project_id`, `zone`, `filter`, `max_results`, `order_by`, `return_partial_success`, and `cursor`, with the
checks, the target rule, the structured `filter`, the cursor, and the handling of `warning` described on
[instances](instances.md). Without `max_results` Google's page size applies, because a machine type is small.

The result is `machine_types`, each with the documented, not deprecated members of the MachineType resource:
`name`, `id`, `description`, `guestCpus`, `memoryMb`, `maximumPersistentDisks`, `maximumPersistentDisksSizeGb`,
`isSharedCpu`, `accelerators`, `architecture`, `bundledLocalSsds`, `deprecated` (the deprecation status),
`zone`, `selfLink`, `kind`, and `creationTimestamp`; `imageSpaceGb` is left out because Google marks it
deprecated. A `warning` of the list is passed on. A row must name a machine type, and a row that names a zone must
name the zone asked for. The content is untrusted third-party text and bounded.

The tool asks for the scope `compute.readonly` and belongs to the profiles `read` and `read-iam`. The
account needs `compute.machineTypes.list` (the Compute Viewer role has it), and the Compute Engine API must be
enabled in the project.

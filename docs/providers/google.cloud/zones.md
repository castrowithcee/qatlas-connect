---
description: >
  Describes the Google Cloud Compute tool zones.list: its arguments, the target check, the shared filter, cursor, and partial-result rules, the fields returned, the scope, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud zones

`zones.list` lists the zones available to a project from Compute Engine (`zones.list` of `compute` v1,
discovery revision 20261106). It changes nothing. The arguments are `project_id`, `filter`, `max_results`,
`order_by`, `return_partial_success`, and `cursor`, with the checks, the target rule, the structured `filter`,
the cursor, and the handling of `warning` described on [instances](instances.md). The tool takes no `zone`.
Without `max_results` Google's page size applies, because a zone is small.

The result is `zones`, each with the documented members of the Zone resource: `name`, `id`, `description`,
`status`, `region` (a URL), `availableCpuPlatforms`, `supportsPzs`, `resourceStatus`, `deprecated` (the
deprecation status), `selfLink`, `kind`, and `creationTimestamp`. A `warning` of the list is passed on. A row
must name a zone. The content is untrusted third-party text and bounded.

The tool asks for the scope `compute.readonly` and belongs to the profiles `read` and `read-iam`. The
account needs `compute.zones.list` (the Compute Viewer role has it), and the Compute Engine API must be enabled
in the project. [Regions](regions.md) lists the regions the zones belong to.

---
description: >
  Describes the Google Cloud Compute tool regions.list: its arguments, the target check, the shared filter, cursor, and partial-result rules, the fields returned including the quotas of the region, the scope, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud regions

`regions.list` lists the regions available to a project from Compute Engine (`regions.list` of `compute` v1,
discovery revision 20261106). It changes nothing. The arguments are `project_id`, `filter`, `max_results`,
`order_by`, `return_partial_success`, and `cursor`, with the checks, the target rule, the structured `filter`,
the cursor, and the handling of `warning` described on [instances](instances.md). The tool takes no `region`.
Without `max_results` the tool asks for 25 regions, because every region carries its quotas.

The result is `regions`, each with the documented members of the Region resource: `name`, `id`,
`description`, `status`, `zones` (URLs), `quotas`, `quotaStatusWarning`, `deprecated` (the deprecation
status), `supportsPzs`, `selfLink`, `kind`, and `creationTimestamp`. `quotas` lists each quota of the project
in the region as `metric`, `limit`, `usage`, and `owner`, up to 500 entries per region. These are the figures of
Compute Engine for the region at the time of the call. Whether quota failures of all services can be traced
this way is not verified: the tool reads only what `regions.list` returns, and other services report their
quotas elsewhere (see [quotas](quotas.md)). A `warning` of the list is passed on, among them
`QUOTA_INFO_UNAVAILABLE` where Google withholds the quotas. A row must name a region. The content is untrusted
third-party text and bounded.

The tool asks for the scope `compute.readonly` and belongs to the profiles `read` and `read-iam`. The
account needs `compute.regions.list` (the Compute Viewer role has it), and the Compute Engine API must be enabled
in the project. [Zones](zones.md) lists the zones of a project.

---
description: >
  Describes the Google Cloud Compute tools subnetworks.list and subnetworks.get: their arguments including the region and the utilization view, the target check, the cursor bound to the region, the fields returned, the scope and profile, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud subnetworks

Two tools read the subnetworks of a region of a project from Compute Engine (`subnetworks.list` and
`subnetworks.get` of `compute` v1, discovery revision 20261106). They change nothing. Every request goes to the
fixed origin `https://compute.googleapis.com`; no argument takes a path, URL, header, or body. The aggregated list
over all regions (`subnetworks.aggregatedList`) is not offered.

| Tool | Arguments |
| --- | --- |
| `subnetworks.list` | `project_id`, `region`, `filter`, `max_results`, `order_by`, `return_partial_success`, `views`, `cursor` |
| `subnetworks.get` | `project_id`, `region`, `subnetwork` (a name or its numeric ID), `views` |

`region` is a region name such as `us-central1`, checked against Compute's name form before any secret is read;
a path, a `regions/` name, a traversal, or a query is refused. `views` is `DEFAULT` or `WITH_UTILIZATION`, which
adds `utilizationDetails`. The other checks, the target rule, the structured `filter`, the page size, and the
handling of `warning` are those of [instances](instances.md). The cursor is tied to the tool, the targets, the
project, the region, and everything that shapes the list, so it continues only this list.

The result is `subnetworks`, each with the documented members of the Subnetwork resource except `params`, which is
input only: `name`, `id`, `description`, `network` (URL), `ipCidrRange`, `secondaryIpRanges`,
`reservedInternalRange`, `gatewayAddress`, `region` (URL), `privateIpGoogleAccess`, `purpose`, `role`, `state`,
`logConfig`, `enableFlowLogs`, `privateIpv6GoogleAccess`, `ipv6CidrRange`, `externalIpv6Prefix`,
`internalIpv6Prefix`, `ipv6AccessType`, `stackType`, `ipv6GceEndpoint`, `systemReservedExternalIpv6Ranges`,
`systemReservedInternalIpv6Ranges`, `allowSubnetCidrRoutesOverlap`, `utilizationDetails`, `resolveSubnetMask`,
`ipCollection`, `fingerprint`, `selfLink`, `kind`, and `creationTimestamp`. A row must name a subnetwork, a row that
names a region must name the region asked for, and `subnetworks.get` refuses an answer for another name or ID. The
content is untrusted third-party text and bounded.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.subnetworks.list` or `compute.subnetworks.get` (the Compute Viewer role has them), and the Compute
Engine API must be enabled in the project.

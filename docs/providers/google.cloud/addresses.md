---
description: >
  Describes the Google Cloud Compute tools addresses.list and addresses.get: their arguments including the region, the target check, the cursor bound to the region, the fields returned, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud IP addresses

Two tools read the reserved IP addresses of a region of a project from Compute Engine (`addresses.list` and
`addresses.get` of `compute` v1, discovery revision 20261106), internal and external. They change nothing:
reserving, changing, and deleting an address, and `addAccessConfig`, are not offered. Every request is a GET to the
fixed origin `https://compute.googleapis.com`; no argument takes a path, URL, header, or body. The global and the
aggregated lists (`globalAddresses`, `addresses.aggregatedList`) are not offered.

| Tool | Arguments |
| --- | --- |
| `addresses.list` | `project_id`, `region`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `addresses.get` | `project_id`, `region`, `address` (the name of the address resource or its numeric ID, not the IP) |

`region` is a region name such as `us-central1`, checked before any secret is read. The other checks, the target
rule, the structured `filter`, the page size, and the handling of `warning` are those of [instances](instances.md).
The cursor is tied to the tool, the targets, the project, the region, and everything that shapes the list.

The result is `addresses`, each with the documented members of the Address resource: `name`, `id`, `description`,
`address`, `prefixLength`, `addressType` (`INTERNAL` or `EXTERNAL`), `status`, `users` (URLs), `network`,
`subnetwork`, `purpose`, `ipVersion`, `ipv6EndpointType`, `networkTier`, `labels`, `labelFingerprint`,
`ipCollection`, `region` (URL), `selfLink`, `kind`, and `creationTimestamp`. Addresses are operating data and are
returned unchanged. A row must name an address, a row that names a region must name the region asked for, and
`addresses.get` refuses an answer for another name or ID. The content is untrusted third-party text and bounded.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.addresses.list` or `compute.addresses.get` (the Compute Viewer role has them), and the Compute
Engine API must be enabled in the project.

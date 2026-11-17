---
description: >
  Describes the Google Cloud Compute tools networks.list and networks.get: their arguments, the target check, the shared filter, cursor, and partial-result rules, the fields returned, the scope and profile, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud VPC networks

Two tools read the VPC networks of a project from Compute Engine (`networks.list` and `networks.get` of
`compute` v1, discovery revision 20261106). They change nothing. Every request goes to the fixed origin
`https://compute.googleapis.com`; no argument takes a path, URL, header, or body.

| Tool | Arguments |
| --- | --- |
| `networks.list` | `project_id`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `networks.get` | `project_id`, `network` (a network name or its numeric ID) |

The checks, the target rule, the structured `filter`, the cursor, the page size, and the handling of `warning` are
those of [instances](instances.md). The list has no zone and no `include_all_scopes`. A network name or ID that is
not in Compute's form is refused before any secret is read or request is sent.

The result is `networks`, each with the documented members of the Network resource, except those Google marks
deprecated or input only: `name`, `id`, `description`, `autoCreateSubnetworks` (true in the auto mode, false in the
custom mode), `subnetworks` (URLs), `peerings`, `routingConfig`, `mtu`, `networkFirewallPolicyEnforcementOrder`,
`firewallPolicy`, `gatewayIPv4`, `enableUlaInternalIpv6`, `internalIpv6Range`, `networkProfile`, `selfLink`,
`selfLinkWithId`, `kind`, and `creationTimestamp`. `IPv4Range` (deprecated) and `params` (input only) are left
out. A row must name a network, and `networks.get` refuses an answer for another name or ID. The content is
untrusted third-party text and bounded.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.networks.list` or `compute.networks.get` (the Compute Viewer role has them), and the Compute
Engine API must be enabled in the project.

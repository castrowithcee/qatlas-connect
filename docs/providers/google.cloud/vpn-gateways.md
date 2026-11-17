---
description: >
  Describes the Google Cloud Compute tools vpn-gateways.list and vpn-gateways.get: their arguments including the region, the target check, the cursor bound to the region, the fields returned, the scope and profile, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud HA VPN gateways

Two tools read the HA VPN gateways of a region of a project from Compute Engine (`vpnGateways.list` and
`vpnGateways.get` of `compute` v1, discovery revision 20261106). They change nothing and create no gateway. Every
request goes to the fixed origin `https://compute.googleapis.com`; no argument takes a path, URL, header, or body.
Classic target VPN gateways, external VPN gateways, and the aggregated list over all regions are not offered.

| Tool | Arguments |
| --- | --- |
| `vpn-gateways.list` | `project_id`, `region`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `vpn-gateways.get` | `project_id`, `region`, `vpn_gateway` (a name or its numeric ID) |

`region` is a region name such as `us-central1`, and `vpn_gateway` a gateway name or numeric ID. Both are checked
against Compute's name form before any secret is read; a path, a `regions/` name, a traversal, or a query is
refused. The other checks, the target rule, the structured `filter`, the page size, and the handling of `warning`
are those of [instances](instances.md). The cursor is tied to the tool, the targets, the project, the region, and
everything that shapes the list, so it continues only this list.

The list returns `vpn_gateways`, the get one object, each with the documented members of the VpnGateway resource
except `params`, which is input only: `name`, `id`, `description`, `region` (URL), `network` (URL),
`vpnInterfaces` (each with `id`, `ipAddress`, `ipv6Address`, and `interconnectAttachment`), `stackType`,
`gatewayIpVersion`, `labels`, `labelFingerprint`, `selfLink`, `kind`, and `creationTimestamp`. A row must name a
gateway, a row that names a region must name the region asked for, and `vpn-gateways.get` refuses an answer for
another name or ID. The content is untrusted third-party text and bounded.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account needs
`compute.vpnGateways.list` or `compute.vpnGateways.get` (the Compute Viewer role has them), and the Compute Engine
API must be enabled in the project. Creating a gateway is not offered. The tunnels of a gateway are read with
[VPN tunnels](vpn-tunnels.md).

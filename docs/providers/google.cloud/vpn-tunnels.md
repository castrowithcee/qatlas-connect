---
description: >
  Describes the Google Cloud Compute tools vpn-tunnels.list and vpn-tunnels.get: their arguments including the region, the target check, the fields returned, why the shared secret and its hash are never returned, the scope and profile, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud VPN tunnels

Two tools read the VPN tunnels of a region of a project from Compute Engine (`vpnTunnels.list` and
`vpnTunnels.get` of `compute` v1, discovery revision 20261106). They change nothing and create no tunnel. Every
request goes to the fixed origin `https://compute.googleapis.com`; no argument takes a path, URL, header, or body.
The aggregated list over all regions is not offered.

| Tool | Arguments |
| --- | --- |
| `vpn-tunnels.list` | `project_id`, `region`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `vpn-tunnels.get` | `project_id`, `region`, `vpn_tunnel` (a name or its numeric ID) |

`region` and `vpn_tunnel` are checked like the arguments of [HA VPN gateways](vpn-gateways.md), and the cursor of
the list is tied to the tool, the targets, the project, the region, and everything that shapes the list.

The list returns `vpn_tunnels`, the get one object, each with the documented members of the VpnTunnel resource
except `params` (input only) and the secrets below: `name`, `id`, `description`, `region` (URL), `status`,
`detailedStatus`, `peerIp`, `peerExternalGateway`, `peerExternalGatewayInterface`, `peerGcpGateway`, `ikeVersion`,
`router`, `localTrafficSelector`, `remoteTrafficSelector`, `vpnGateway`, `vpnGatewayInterface`,
`targetVpnGateway` (classic tunnels only), `cipherSuite`, `labels`, `labelFingerprint`, `selfLink`, `kind`, and
`creationTimestamp`. A row must name a tunnel, a row that names a region must name the region asked for, and
`vpn-tunnels.get` refuses an answer for another name or ID. The content is untrusted third-party text and bounded.

**Secrets.** `sharedSecret` and `sharedSecretHash` are never returned: not in the result, not in the output schema,
and not in an error. Qatlas drops both members, in any letter case, at every depth of an answer before it does
anything else with it, including in `cipherSuite`, `labels`, and `warning`. A failed request keeps the status, type,
and code of Google's error but never its message, not even as the original of the error.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account needs
`compute.vpnTunnels.list` or `compute.vpnTunnels.get` (the Compute Viewer role has them), and the Compute Engine API
must be enabled in the project. Creating a tunnel is not offered.

---
description: >
  Describes the Google Cloud Compute tools vpn-gateways.list, vpn-gateways.get, and vpn-gateways.delete: their arguments including the region, the target check, the cursor bound to the region, the fields returned, the confirmed deletion, the scope and profiles, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud HA VPN gateways

Two tools read the HA VPN gateways of a region of a project from Compute Engine (`vpnGateways.list` and
`vpnGateways.get` of `compute` v1, discovery revision 20261106). They change nothing and create no gateway. A third tool, `vpn-gateways.delete`, deletes a gateway (see
[Deleting a gateway](#deleting-a-gateway)). Every
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

## Deleting a gateway

`vpn-gateways.delete` deletes one HA VPN gateway of a region (`vpnGateways.delete` of `compute` v1,
`DELETE .../regions/{region}/vpnGateways/{vpn_gateway}`). The gateway is gone afterwards and the external IP addresses of its interfaces are released; Google refuses a gateway that tunnels still use. It cannot be restored. Every call needs the confirmation of a person; without it
nothing is sent, not even a token request or a read.

| Tool | Arguments |
| --- | --- |
| `vpn-gateways.delete` | `project_id`, `region`, `vpn_gateway` (a name or its numeric ID), `request_id` (optional UUID) |

The confirmation names the project, the kind of resource, the region, and the name. The tool reads nothing first.
`project_id`, `region`, `vpn_gateway`, and `request_id` are checked against their form and the targets before any
request. `request_id` makes a repeat safe: Google ignores a request whose ID it already received, and the tool
sends the ID you give unchanged. Without it the tool creates a UUID and returns it as `request_id`. The tool never
repeats the call itself, and after an unclear outcome it says so. An unknown name is `not-found`.

The tool never waits. The result is `operation`, the Compute operation as Google returned it, plus `scope`
(`region`), `region`, and `request_id`; `compute-operations.get` with that scope and region reads its state, see
[Compute operations](compute-operations.md). An operation that is already `DONE` with an error is reported as a
provider error. The operation names the person who started it (`user`), which is personal data. Like the reads,
the deletion never returns a shared secret or the text of a Google error: `sharedSecret` and `sharedSecretHash`
are dropped from the answer, and a failure keeps status, type, and code but not the message.

The tool asks for the scope `compute`, the smallest one Google lists for the method, and is listed-only: it
belongs to no profile, and a connection offers it only when its `tools` list names it. The account needs `compute.vpnGateways.delete` (the Compute Network Admin role has it), and the Compute
Engine API must be enabled in the project.

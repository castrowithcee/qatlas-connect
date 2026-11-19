---
description: >
  Describes the Google Cloud Compute tools vpn-tunnels.list, vpn-tunnels.get, and vpn-tunnels.delete: their arguments including the region, the target check, the fields returned, why the shared secret and its hash are never returned, the confirmed deletion, the scope and profiles, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud VPN tunnels

Two tools read the VPN tunnels of a region of a project from Compute Engine (`vpnTunnels.list` and
`vpnTunnels.get` of `compute` v1, discovery revision 20261106). They change nothing and create no tunnel. A third tool, `vpn-tunnels.delete`, deletes a tunnel (see
[Deleting a tunnel](#deleting-a-tunnel)). Every request goes to the fixed origin `https://compute.googleapis.com`;
no argument takes a path, URL, header, or body. The aggregated list over all regions is not offered.

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

## Deleting a tunnel

`vpn-tunnels.delete` deletes one VPN tunnel of a region (`vpnTunnels.delete` of `compute` v1,
`DELETE .../regions/{region}/vpnTunnels/{vpn_tunnel}`). The tunnel is gone afterwards and the traffic it carried stops; it cannot be restored. Every call needs the confirmation of a person; without it
nothing is sent, not even a token request or a read.

| Tool | Arguments |
| --- | --- |
| `vpn-tunnels.delete` | `project_id`, `region`, `vpn_tunnel` (a name or its numeric ID), `request_id` (optional UUID) |

The confirmation names the project, the kind of resource, the region, and the name. The tool reads nothing first.
`project_id`, `region`, `vpn_tunnel`, and `request_id` are checked against their form and the targets before any
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
belongs to no profile, and a connection offers it only when its `tools` list names it. The account needs `compute.vpnTunnels.delete` (the Compute Network Admin role has it), and the Compute
Engine API must be enabled in the project.

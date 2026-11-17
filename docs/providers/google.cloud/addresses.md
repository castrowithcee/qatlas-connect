---
description: >
  Describes the Google Cloud Compute tools addresses.list, addresses.get, and addresses.delete: their arguments including the region, the target check, the cursor bound to the region, the fields returned, the confirmed deletion, the scope and profiles, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud IP addresses

Two tools read the reserved IP addresses of a region of a project from Compute Engine (`addresses.list` and
`addresses.get` of `compute` v1, discovery revision 20261106), internal and external. They change nothing. A third
tool, `addresses.delete`, deletes an address (see [Deleting an address](#deleting-an-address)); reserving and
changing an address and `addAccessConfig` are not offered. Every request of the two reads is a GET to the
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

## Deleting an address

`addresses.delete` deletes one reserved address of a region (`addresses.delete` of `compute` v1,
`DELETE .../regions/{region}/addresses/{address}`). A released external address may be given to another customer,
so it cannot be got back, and DNS records or allow lists that name it then point nowhere or elsewhere. Every call
needs the confirmation of a person; without it nothing is sent, not even a token request or a read.

| Tool | Arguments |
| --- | --- |
| `addresses.delete` | `project_id`, `region`, `address` (the name of the address resource or its numeric ID, not the IP), `request_id` (optional UUID) |

The confirmation names the project, the kind of resource, the region, and the name. The tool reads nothing first.
An address that is still in use (see `users` of `addresses.get`) is refused by Google, and the tool reports that
refusal; an unknown name is `not-found`. `project_id`, `region`, `address`, and `request_id` are checked against
their form and the targets before any request.

`request_id` makes a repeat safe: Google ignores a request whose ID it already received, and the tool sends the ID
you give unchanged. Without it the tool creates a UUID and returns it as `request_id`. The tool never repeats the
call itself, and after an unclear outcome it says so.

The tool never waits. The result is `operation`, the Compute operation as Google returned it, plus `scope`
(`region`), `region`, and `request_id`; `compute-operations.get` with that scope and region reads its state, see
[Compute operations](compute-operations.md). An operation that is already `DONE` with an error is reported as a
provider error. The operation names the person who started it (`user`), which is personal data.

The tool asks for the scope `compute`, the smallest one Google lists for the method, and belongs to the profile
`compute-network-delete`. The account needs `compute.addresses.delete` (the Compute Network Admin role has it), and
the Compute Engine API must be enabled in the project.

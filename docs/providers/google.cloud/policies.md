---
description: >
  Describes the Google Cloud DNS tools policies.list and policies.get: their arguments, the check of the project against the targets and of policy names before any request, paging with a cursor, the fields returned including forwarding targets, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud DNS server policies

`policies.list` and `policies.get` read the DNS server policies of a project from Cloud DNS (`policies.list` and
`policies.get` of `dns` v1, discovery revision 20261111). A server policy sets inbound forwarding, query logging,
alternative name servers, and DNS64 for the VPC networks it applies to. The tools change nothing: creating, changing,
and deleting policies are not offered. The requests are GETs to
`https://dns.googleapis.com/dns/v1/projects/{project}/policies` and `.../policies/{policy}`.

| Tool | Arguments |
| --- | --- |
| `policies.list` | `project_id`, `max_results`, `cursor` |
| `policies.get` | `project_id`, `policy` |

The rules of every DNS tool for the project, the targets, paging, untrusted content, and the enabled API are those
of [DNS managed zones](managed-zones.md#rules-of-the-dns-tools).

- `policy` is the name of the policy: 1 to 63 characters, lowercase letters, digits, and dashes, a letter first and a
  letter or digit last. Google takes only the name in the path, never the numeric ID, so an ID is refused before any
  request. The answer must be the policy asked for; any other answer is invalid. The policy is read below the project
  in the path, so a successful read proves that it belongs to that project.
- The list result is `policies`, `next_cursor`, and `has_more`.
- The alternative name servers are operational data of the network: they are read and reported, never changed.
- The account needs `dns.policies.list` or `dns.policies.get` on the project (the DNS Reader role has them).

## Fields

Each policy has `name`, `id`, `description`, `enableInboundForwarding`, `enableLogging`,
`alternativeNameServerConfig` (`targetNameServers` with `ipv4Address`, `ipv6Address`, and `forwardingPath`, and
`outboundEndpoints`), `networks` (`networkUrl`), `dns64Config` (`scope.allQueries`), and `kind`, as far as Google sends
them. Members Google does not document are dropped. No documented member of the policy is deprecated or withheld.

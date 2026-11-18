---
description: >
  Describes the Google Cloud DNS tools response-policies.list and response-policies.get: their arguments, the check of the project against the targets and of response policy names before any request, paging with a cursor, the fields returned, the scope and profile, and that the rules of a response policy are not read.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud DNS response policies

`response-policies.list` and `response-policies.get` read the response policies of a project from Cloud DNS
(`responsePolicies.list` and `responsePolicies.get` of `dns` v1, discovery revision 20261111). A response policy
overrides DNS answers for the VPC networks and GKE clusters it applies to. The tools change nothing, and they do not
read the rules of a response policy (`responsePolicyRules`). The requests are GETs to
`https://dns.googleapis.com/dns/v1/projects/{project}/responsePolicies` and `.../responsePolicies/{responsePolicy}`.

| Tool | Arguments |
| --- | --- |
| `response-policies.list` | `project_id`, `max_results`, `cursor` |
| `response-policies.get` | `project_id`, `response_policy` |

The rules of every DNS tool for the project, the targets, paging, untrusted content, and the enabled API are those
of [DNS managed zones](managed-zones.md#rules-of-the-dns-tools).

- `response_policy` is the name of the response policy, in the form of a [policy](policies.md) name. Google takes only
  the name in the path, never the numeric ID. The answer must be the response policy asked for; any other answer is
  invalid. A successful read proves that the response policy belongs to the project.
- The list result is `response_policies`, `next_cursor`, and `has_more`.
- The account needs `dns.responsePolicies.list` or `dns.responsePolicies.get` on the project (the DNS Reader role has
  them).

## Fields

Each response policy has `responsePolicyName`, `id`, `description`, `networks` (`networkUrl`), `gkeClusters`
(`gkeClusterName`), `labels`, and `kind`, as far as Google sends them. Members Google does not document are dropped. No
documented member of the response policy is deprecated or withheld.

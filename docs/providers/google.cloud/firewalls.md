---
description: >
  Describes the Google Cloud Compute tools firewalls.list and firewalls.get: their arguments, the target check, the shared filter, cursor, and partial-result rules, the fields returned, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud firewall rules

Two tools read the VPC firewall rules of a project from Compute Engine (`firewalls.list` and `firewalls.get` of
`compute` v1, discovery revision 20261106). They change nothing: creating, patching, updating, and deleting a rule
are not offered. Every request is a GET to the fixed origin `https://compute.googleapis.com`; no argument takes a
path, URL, header, or body.

| Tool | Arguments |
| --- | --- |
| `firewalls.list` | `project_id`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `firewalls.get` | `project_id`, `firewall` (a name or its numeric ID) |

`firewall` is checked against Compute's name form or a numeric ID before any secret is read, and a path, a
traversal, or a query is refused. The other checks, the target rule, the structured `filter`, the page size, the
cursor, and the handling of `warning` are those of [instances](instances.md).

The result is `firewalls`, each with the documented members of the Firewall resource except `params`, which is
input only: `name`, `id`, `description`, `network` (URL), `priority`, `direction` (`INGRESS` or `EGRESS`),
`allowed` and `denied` (protocols and ports), `sourceRanges`, `destinationRanges`, `sourceTags`, `targetTags`,
`sourceServiceAccounts`, `targetServiceAccounts`, `disabled`, `logConfig`, `selfLink`, `kind`, and
`creationTimestamp`. Ranges and tags are operating data and are returned unchanged. A row must name a rule, and
`firewalls.get` refuses an answer for another name or ID. The content is untrusted third-party text and bounded.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.firewalls.list` or `compute.firewalls.get` (the Compute Viewer role has them), and the Compute
Engine API must be enabled in the project.

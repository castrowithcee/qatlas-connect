---
description: >
  Describes the Google Cloud Compute tools firewalls.list, firewalls.get, and firewalls.delete: their arguments, the target check, the shared filter, cursor, and partial-result rules, the fields returned, the confirmed deletion with the expected direction, action, and priority, the scope and profiles, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud firewall rules

Two tools read the VPC firewall rules of a project from Compute Engine (`firewalls.list` and `firewalls.get` of
`compute` v1, discovery revision 20261106). They change nothing. A third tool, `firewalls.delete`, deletes a rule
(see [Deleting a rule](#deleting-a-rule)); creating, patching, and updating a rule are not offered. Every request of the two reads is a GET to the fixed origin `https://compute.googleapis.com`; no argument takes a
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

## Deleting a rule

`firewalls.delete` deletes one rule (`firewalls.delete` of `compute` v1, `DELETE .../global/firewalls/{firewall}`).
The rule cannot be restored. Every call needs the confirmation of a person; without it nothing is sent, not even
a token request or a read.

| Tool | Arguments |
| --- | --- |
| `firewalls.delete` | `project_id`, `firewall` (a name or its numeric ID), `expected_direction` (`INGRESS` or `EGRESS`), `expected_action` (`allow` or `deny`), `expected_priority` (0 to 65535), `request_id` (optional UUID) |

The three `expected_*` arguments are required and are only compared, never sent to Google. The confirmation names
the project, the rule, and these three values. For a `deny` rule it says that deleting it can open access the rule
blocked; for an `allow` rule, that it can cut access. Read the rule with `firewalls.get` first and pass what you
read. After the confirmation the tool reads the rule again and deletes it only if direction, action (`deny` when
the rule has `denied` entries, otherwise `allow`), and priority still match; a rule that left out direction or
priority is taken with Google's documented defaults, `INGRESS` and 1000. On a mismatch the tool fails without
deleting and asks you to read the rule again.

`project_id`, `firewall`, the `expected_*` values, and `request_id` are checked against their form and the targets
before any request. `request_id` makes a repeat safe: Google ignores a request whose ID it already received, and the
tool sends the ID you give unchanged. Without it the tool creates a UUID and returns it as `request_id`. The tool
never repeats the call itself, and after an unclear outcome it says so.

The tool never waits. The result is `operation`, the Compute operation as Google returned it (usually `RUNNING` or
`PENDING`) with the members of [compute-operations.get](compute-operations.md), plus `scope` (`global`) and
`request_id`; `compute-operations.get` with `scope` `global` reads its state. An operation that is already `DONE`
with an error is reported as a provider error. The operation names the person who started it (`user`), which is
personal data. An unknown name is `not-found`.

The tool asks for the scope `compute`, the smallest one Google lists for the method, and belongs to the profile
`compute-network-delete`. The account needs `compute.firewalls.delete` and, for the comparison,
`compute.firewalls.get` (the Compute Network Admin role has both), and the Compute Engine API must be enabled in the
project.

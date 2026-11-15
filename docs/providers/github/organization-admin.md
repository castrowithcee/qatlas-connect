---
description: >
  Describes the GitHub organization administration tools: the billing usage, usage summary, premium request, and AI credit reports of a user or an organization and the budgets of an organization, all read only, with their owner target, filters, bounds, the unavailable state, and how excluded repositories are treated.
type: knowledge
edit: shared
created: 2026-11-14
updated: 2026-11-14
---

# GitHub organization administration

The tools of the group `organization-admin` of the [GitHub provider](README.md). The group holds administrative
reads of an account or an organization that belong to no repository. It starts with the billing reads below;
further areas of organization administration join it on this page.

## Billing

| Tool | Does |
| --- | --- |
| `github.billing-usage.get` | reads the usage report of a user or an organization as line items per day, product, SKU, and repository; optional `year`, `month`, `day` |
| `github.billing-usage-summary.get` | reads the summarized usage per product and SKU; optional `year`, `month`, `day`, `repository` (`OWNER/REPO`), `product`, `sku` |
| `github.billing-premium-requests.get` | reads the premium request usage per product, SKU, and model; optional `year`, `month`, `day`, `model`, `product`, and for an organization `user` |
| `github.billing-ai-credits.get` | reads the AI credit usage per product, SKU, and model; the same filters as the premium request report |
| `github.billing-budgets.list` | lists the budgets of an organization in batches; optional `scope` (`enterprise`, `organization`, `repository`, `cost_center`, `multi_user_customer`, `user`) and `user` |
| `github.billing-budgets.get` | reads one budget of an organization by `budget_id` |

These tools only read and are offered by the read effect. No tool creates, changes, or deletes a budget, and
none touches a payment method or assigns a seat; money is read only. They are not part of any setup profile.

**Owner.** Every tool takes `owner` as `users/LOGIN` or `orgs/LOGIN` under the rules of
[the target of a call](README.md#the-target-of-a-call): an owner target of its own, since a repository or project
target of the owner is not enough, checked against `exclude` before a secret is read, and named in every result.
The reports take both kinds of owner. The budget tools take an organization only; a user owner is refused with
`invalid-request` before any request, because GitHub documents budgets for organizations only. The `user` filter
of the premium request and AI credit reports is accepted for an organization only, since a user report is one
user already. An organization is read below `/organizations/LOGIN/settings/billing/`, a user below
`/users/LOGIN/settings/billing/`; the routes are fixed.

**Filters and dates.** `year` (four digits), `month` (1 through 12), and `day` (1 through 31) are checked before
anything is sent; GitHub decides which combinations and periods it serves, and keeps the data of the past 24
months for the summary and the premium request and AI credit reports. `product`, `sku`, and `model` are texts of
at most 100 characters without control characters. `repository` of the summary must be `OWNER/REPO` with the owner
of the call and must not be excluded. GitHub documents no filter for the hour or a cost center on these routes, so
none is offered.

**The state `unavailable`.** GitHub answers a billing route with 403 or 404 for an account that does not use the
enhanced billing platform, a plan that does not offer the report, a token without the billing right, an unknown
owner, or an unknown budget alike. Qatlas cannot tell these apart, so every tool ends such an answer as the result
`state: unavailable` with a fixed `reason` that names the causes and never quotes GitHub; no other member is given.
A rate limit, a timeout, a 5xx, and an unusable answer stay errors named by their class. Reads are never repeated.

**Bounds.** The three usage reports are not paged by GitHub, so each gives at most 500 line items and says
`truncated: true` when it cut the rest. The budget list follows the [cursor contract](README.md#cursor-contract),
bound to the organization, the tool, and its filters; at most 100 alert recipients of a budget are given and
`alert_recipients_truncated` marks a cut. Amounts, prices, and quantities are passed on as the number GitHub sent,
never rounded or cut. Product, SKU, model, repository, entity, and user names and every text are untrusted data
with control characters replaced and a length bound.

**Excluded repositories.** `exclude` takes an excluded repository out of the line items of
`github.billing-usage.get` and out of the budgets of `github.billing-budgets.list`, counted in `withheld`; a budget
of an excluded repository read by `github.billing-budgets.get` is `not-found`. The summed reports (summary,
premium requests, AI credits) and the budget list's `total_count` are GitHub's sums over the whole owner: usage of an
excluded repository is part of them and cannot be told apart. Connect such a report only with an owner whose
repositories may all be counted.

**Omitted and added fields.** Every member GitHub documents for the six answers (OpenAPI 1.1.4, rest-api-description
commit 99b1432) is output, in snake case (`unitType` as `unit_type`, `timePeriod` as `time_period`). The single
budget read gives no `expires_at`, because GitHub documents none for it. `has_next_page` is not output; it becomes
`has_more` and `next_cursor`. Qatlas adds `owner`, `truncated`, `withheld`, `alert_recipients_truncated`, `state`,
and `reason`. Budget alert recipients and users are logins GitHub reports and are output as untrusted data.

The token rights are listed under [Organization administration](setup.md#organization-administration).

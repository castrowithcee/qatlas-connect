---
description: >
  Describes the GitHub organization administration tools: the billing usage, usage summary, premium request, and AI credit reports of a user or an organization and the budgets of an organization, all read only, the GitHub Apps installed in an organization with the removal of a repository from an installation, and the audit log of an organization, with their targets, filters, bounds, the unavailable state, the proof of an installation id, and how excluded repositories are treated.
type: knowledge
edit: shared
created: 2026-11-14
updated: 2026-11-15
---

# GitHub organization administration

The tools of the group `organization-admin` of the [GitHub provider](README.md). The group holds administrative
reads of an account or an organization that belong to no repository. It holds the billing reads and the GitHub
Apps and the audit log below; further areas of organization administration join it on this page.

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

## GitHub Apps

| Tool | Does |
| --- | --- |
| `github.app-installations.list` | lists the GitHub Apps installed in an organization in batches, each installation with its app slug, account, repository selection, permissions, events, and suspension |
| `github.apps.get` | reads one GitHub App by its `app_slug`: name, description, owner, permissions, events, and installation count |
| `github.app-installation-repositories.remove` | withdraws one repository from one installation, listed only |

The two reads are offered by the read effect and are not part of any setup profile. The removal is offered only
where a connection's `tools` list names it and needs a confirmation.

No tool adds a repository to an installation, suspends or deletes an installation, or creates an installation
token, because those routes grant access outward or need an app JWT, which a Qatlas credential does not hold. The
webhook configuration and deliveries of an app are not read either.

**Targets.** `github.app-installations.list` takes `owner` as `orgs/LOGIN` under the rules of
[the target of a call](README.md#the-target-of-a-call): an owner target of its own, checked against `exclude` before a
secret is read. A user owner is refused with `invalid-request` before any request. `github.apps.get` reads GitHub-wide
data and names no target, so it is refused before a secret is read on a connection whose targets name a repository or
a project, like the global security advisories. The removal takes `repository` as `OWNER/REPO` inside the targets and
not excluded.

**Removing a repository.** The numeric repository id never comes from an argument. The call reads the repository
first and takes its id from that read, which must name the repository of the call and an owner of the type
`Organization`. It then reads `GET /orgs/OWNER/installations` of that organization page by page, 100 entries at a
time and at most 50 pages, until it finds `installation_id`. The id counts as foreign only after the whole list was
read. These cases end the call without any write request: the repository of a user account, an `installation_id`
that the list does not hold, an installation that does not name `selected` as its repository selection (GitHub
removes a single repository only from an installation with selected repositories), and a list that cannot be read completely (a failed
page, a total that the pages do not reach, or more pages than the bound). Only then is one `DELETE` sent, never
repeated. An unclear outcome says so and is not retried.

**Rights of the token.** Reading the installations needs an owner of the organization and `admin:read` on a classic
token. GitHub documents the removal for a classic token with `repo` whose account administers the repository, and
the reachability depends on the token: GitHub answers `403` to a token it does not serve. Qatlas names this in the
`permission` message and never quotes GitHub's text. See [Organization administration](setup.md#organization-administration).

**Omitted and added fields.** The installation list and the app read output every member GitHub documents
(OpenAPI 1.1.4, rest-api-description commit 99b1432) in snake case, except `access_tokens_url`, which is a token
address, and any secret or webhook configuration. The accounts of an installation or an app (`account`,
`suspended_by`, `owner`) keep the members of a user and of an enterprise and give the REST address and the other
`*_url` addresses as `links`. Every text and every address, among them `repositories_url`, `html_url`, and
`external_url`, is untrusted data with control characters replaced and a length bound, and no address is ever
fetched. `permissions`, `events`, and `single_file_paths` are capped at 100 entries and say so through
`permissions_truncated`, `events_truncated`, and `single_file_paths_truncated`. Qatlas adds `owner` to the list
and `repository` to the removal, and `app_slug` to the removal result from the proof read.

## Audit log

| Tool | Does |
| --- | --- |
| `github.audit-log.list` | lists one batch of the audit log of an organization; optional `phrase`, `include` (`web`, `git`, `all`), `order` (`desc`, `asc`), `limit`, and `cursor` |

The tool only reads and is offered by the read effect. It is not part of any setup profile. The enterprise audit
log and the audit log streams are out of reach: a stream sends data outward permanently, so no tool configures or
reads one.

**Target.** `owner` is `orgs/LOGIN` under the rules of [the target of a call](README.md#the-target-of-a-call): an
owner target of its own, checked against `exclude` and the targets before a secret is read. A user owner is refused
with `invalid-request` before any request. The route `GET /orgs/LOGIN/audit-log` is fixed.

**Filters.** `phrase` is a search phrase as GitHub documents it, for example `action:repo.create`. It is passed on as
one text value and never interpreted or split; it has at most 500 characters and no control characters. `include`
defaults to `web` at GitHub. `order` defaults to `desc`. The tool offers no `after` or `before`: it follows the
cursor GitHub names in its `Link` header and carries it in `next_cursor`, which follows the
[cursor contract](README.md#cursor-contract), is bound to the organization and the three filters, and keeps the
batch size of its first batch. The address of the `Link` header is only read for that cursor and never requested.

**The state `unavailable`.** GitHub documents the route in its Enterprise Cloud description only. A plan without an
audit log, a token without the audit log right, a user who is no owner, and an unknown organization answer 403 or
404 alike, so the tool ends such an answer as `state: unavailable` with a fixed `reason` that names the causes and
never quotes GitHub. A rate limit, a timeout, a 5xx, and an unusable answer stay errors named by their class. The
read is never repeated.

**Bounds and fields.** A batch has at most `limit` events (1 through 100, 30 when omitted); `truncated` says that
GitHub answered more than that and the rest was cut. An event outputs only the members GitHub documents for it
(OpenAPI 1.1.4 of GitHub Enterprise Cloud, rest-api-description commit 99b1432); every other key, among them an
actor address or a user agent that GitHub may send, is dropped. The free-form members `data`, `config`,
`config_was`, `events`, and `events_were` keep flat texts, numbers, and booleans, at most 100 keys and 100 entries;
nested values are left out and a cut is shown by `data_truncated`, `config_truncated`, `config_was_truncated`,
`events_truncated`, and `events_were_truncated`. Names, actions, messages, and every other text of an event are
untrusted data with control characters replaced and a length bound, and no address is ever fetched. Qatlas adds
`owner`, `truncated`, `withheld`, `state`, and `reason`.

**Excluded repositories.** An event whose `repo` or `repository` names a repository the connection excludes, as a bare
name of the organization or as `OWNER/REPO`, is not output; it is counted in `withheld`, so its name and texts never
reach the result. An event without a repository stays unchanged, even when its other members mention a repository, for
example in `data` or `message`: the exclusion cannot see such a mention, so connect the audit log only with an
organization whose events may all be seen when that matters.

The token rights are listed under [Organization administration](setup.md#organization-administration).

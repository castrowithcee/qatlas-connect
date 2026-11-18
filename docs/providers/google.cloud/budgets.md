---
description: >
  Describes the Google Cloud budget tools budgets.list and budgets.get: their arguments, the billing-account target and the row filter against project targets, the fields, the scope and the profile read-budgets, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud budgets

Two tools read the budgets of a billing account from the Cloud Billing Budget API (`billingAccounts.budgets.list`
and `billingAccounts.budgets.get` of `billingbudgets` v1, discovery revision 20261108). They change nothing:
creating, changing, and deleting budgets are not offered, and no tool takes a method or a path. Every request is a
GET to `https://billingbudgets.googleapis.com`. Budgets are billing data, so the tools carry the data sensitivity
`cloud-billing` of the [Billing](billing.md) tools.

| Tool | Arguments |
| --- | --- |
| `budgets.list` | `billing_account`, `scope`, `page_size`, `cursor` |
| `budgets.get` | `billing_account`, `budget_id` |

**The target `billing-account`.** A budget belongs to a billing account, so both tools run only on a connection
whose targets name that account as `billingAccounts/XXXXXX-XXXXXX-XXXXXX`, and a connection without such a target is
not offered them. `billing_account`, `budget_id`, `scope`, `page_size`, and `cursor` are checked before any secret is
read and before any request, and a refusal does not name the account or a project. `budget_id` is the last segment of
the budget's name and never a path.

**Rows and project targets.** `scope` is Google's filter for the budgets that track one project
(`projects/ID` or `projects/NUMBER`). It only narrows the request. If the connection names organizations, folders,
or projects next to the billing account, Qatlas also checks every budget against them: a budget whose filter names
projects is shown only if at least one of those projects lies in the targets, and `budgets.get` refuses any other.
The projects of a filter are given by number or by ID, so Qatlas reads a project to compare it with a target of the
other spelling, and below an organization or a folder it reads the folders above it, like `projects.get`. One call
spends at most 30 such reads, and a call that would need more is refused, so use a smaller `page_size`. A project the
account cannot read cannot be placed and does not make a budget visible. The projects in Google's answer never add a
target. A page can be empty while `has_more` is true; follow `next_cursor`. A cursor continues only the list that
issued it, with the same account and scope, on the same connection. `page_size` is 1 to 100.

A budget without `budgetFilter.projects` tracks the costs of every project of the account, and the target
`billing-account` releases that account expressly, so such a budget is visible on every connection that names the
account, whatever other targets it has. That includes a budget that Google limits only by `resourceAncestors`
(folders and organizations), which Qatlas does not compare with the targets. A connection that names no
organization, folder, or project shows every budget of the account it names.

**Fields.** Every documented field is returned: a budget has `name`, `displayName`, `budgetFilter`
(`projects`, `resourceAncestors`, `creditTypes`, `creditTypesTreatment`, `services`, `subaccounts`, `labels`,
`calendarPeriod`, `customPeriod`), `amount` (`specifiedAmount`, which Google leaves without `units` or `nanos` for
zero), `thresholdRules` (`thresholdPercent`, 1.0-based, and `spendBasis`), `notificationsRule` (`pubsubTopic`,
`schemaVersion`, `monitoringNotificationChannels`, `disableDefaultIamRecipients`, `enableProjectLevelRecipients`),
`spendCap` (`inputState`, `outputState`, `reconciling`), `etag`, and `ownershipScope`. The Pub/Sub topic and the
notification channels are only read. Qatlas leaves empty objects out of every result, so a budget that takes the
spend of the last period (Google's `lastPeriodAmount`, an empty object) has no `amount`. Display names and labels are
untrusted text: control characters become spaces, strings are cut, and lists and labels are limited.

**Scope and profile.** The tools ask for `cloud-billing`. The discovery document of `billingbudgets` v1 lists only
`cloud-billing` and `cloud-platform` for these two methods and no read-only scope, so `cloud-billing` is the smallest
one. Google also uses it to change budgets, so a token with it may do that although no tool does. That is why the
tools do not belong to the recommended profile `read` but to the profile `read-budgets`, which holds the tools of
`read` and the two budget tools. Google states that the account needs `billing.budgets.list` or `billing.budgets.get`
on the billing account (the Billing Account Viewer role has them). The Cloud Billing Budget API
(`billingbudgets.googleapis.com`) must be enabled in the project of the credential.

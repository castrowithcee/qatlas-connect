---
description: >
  Describes the Google Cloud Billing tools billing-accounts.list, billing-accounts.get, billing-accounts.list-projects, and project-billing-info.get: their arguments, the billing-account target and the checks against the targets, the row filter, the fields, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud Billing

Four tools read billing accounts and the link between projects and billing accounts from Cloud Billing
(`billingAccounts.list`, `billingAccounts.get`, `billingAccounts.projects.list`, and `projects.getBillingInfo` of
`cloudbilling` v1, discovery revision 20261106). They change nothing: linking a project to an account, moving an
account, renaming it, and the budgets and IAM of an account are not offered. Every request is a GET to
`https://cloudbilling.googleapis.com`. Account names, links between projects and accounts, and display names are
billing data, so the tools carry their own data sensitivity.

| Tool | Arguments |
| --- | --- |
| `billing-accounts.list` | `parent`, `master_billing_account`, `page_size`, `cursor` |
| `billing-accounts.get` | `billing_account` |
| `billing-accounts.list-projects` | `billing_account`, `page_size`, `cursor` |
| `project-billing-info.get` | `project_id` |

**The target `billing-account`.** A billing account belongs to no organization, folder, or project, so only a
connection that names it as `billingAccounts/XXXXXX-XXXXXX-XXXXXX` reaches it. An unbound connection and a
connection bound to organizations, folders, or projects reach none, and a connection without such a target is not
offered the three account tools. `billing_account` is checked for that form and against the targets before any
secret is read, and a refusal does not name the account. `project-billing-info.get` needs no billing target: it is
bound through the project, like the Service Usage tools, and names the account of the project by its ID only.

**List.** `billing-accounts.list` returns only the accounts the targets name. Google's `parent`
(`organizations/ID` or `billingAccounts/ID`) and its filter only narrow the request: `master_billing_account`
(`billingAccounts/ID`) is the one filter Google documents and lists the subaccounts of that master account. Every
returned row is checked again, so a page can be empty while `has_more` is true; follow `next_cursor`. A cursor
continues only the list that issued it, on the same connection. `page_size` is 1 to 100.

**Projects of an account.** `billing-accounts.list-projects` needs the account among the targets and, next to it,
an organization, a folder, or a project, because it returns only projects that lie in those targets. A row whose
project ID a project target names costs no read. For any other row Qatlas reads the project and the folders above it,
like `projects.get`, so the account needs `resourcemanager.projects.get` on them, and the connection needs the scope
`cloud-platform.read-only` that `read` holds. One call spends at most 30 such reads. A call that would need more is
refused, and a smaller `page_size` avoids that. A project the account cannot read cannot be placed and is dropped.
A row that names another account than the one asked for makes the answer invalid.

**Fields.** Every documented field is returned. An account has `name`, `open`, `displayName`,
`masterBillingAccount`, `parent`, and `currencyCode`. A project's billing information has `name`, `projectId`,
`billingAccountName`, and `billingEnabled`. Google omits a boolean that is false, so a missing `open` is returned as
`false` (a closed account) and a missing `billingEnabled` as `false` (no account, or a closed one). Display names
are untrusted text: control characters become spaces and strings are cut to 256 bytes.

The tools ask for `cloud-billing.readonly`, the smallest scope that the discovery document lists for these four
methods, and belong to the profile `read`. Google states the rights as follows: the account must be a viewer of the
billing account for `billing-accounts.list` and `billing-accounts.get`, must have `billing.resourceAssociations.list`
for `billing-accounts.list-projects`, and must have `resourcemanager.projects.get` on the project for
`project-billing-info.get`. The Cloud Billing API (`cloudbilling.googleapis.com`) must be enabled in the project of
the credential.

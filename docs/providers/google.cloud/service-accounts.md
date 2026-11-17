---
description: >
  Describes the Google Cloud service account reads service-accounts.list and service-accounts.get: the structured project and account arguments, the project check before and after the request, paging, the returned fields, the scope, and what is left out.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud service accounts

Two read tools of the IAM API (`https://iam.googleapis.com`, discovery `iam` v1, revision 20261106): the
service accounts of a project and one service account. They create, change, disable, and delete nothing. The keys
of an account are read by [service-account-keys](service-account-keys.md). [The provider page](README.md) holds
targets, errors, and rate spacing.

| Tool | Google method | Arguments |
| --- | --- | --- |
| `service-accounts.list` | `projects.serviceAccounts.list` | `project_id`, `page_size` (1 to 100, default 100), `cursor` |
| `service-accounts.get` | `projects.serviceAccounts.get` | `project_id`, `account` |

## Arguments

`project_id` is a project ID or number, never a `projects/` name. `account` is the email of the account or its
numeric unique ID. Both are checked against fixed patterns and escaped as one path segment each, so neither
adds a segment, a query, or a wildcard. The placeholder `-` that Google accepts for a project is not offered.

## Project check

The project must lie in the targets of the connection, and Qatlas decides before it reads the secret wherever
the targets alone settle it. Otherwise it reads the project and the folders above it, as `projects.get` does. A
project given by number is read once so that the request and the checks use the project ID Google reports.

An email of the form `NAME@PROJECT_ID.iam.gserviceaccount.com` names its project. If that project differs from
`project_id`, the call is refused before any request, or right after the project read when `project_id` was a
number. The default accounts (`@appspot.gserviceaccount.com`, `@developer.gserviceaccount.com`) and the unique ID
carry no project, so for them the answer decides: an account whose `projectId` or `name` is not the checked
project is an invalid response and is not returned. The same holds for every row of a list.

## Result

Each account carries `name`, `projectId`, `uniqueId`, `email`, `displayName`, `description`, `oauth2ClientId`, and
`disabled` (present only when true). The `etag` is deprecated by Google and is not returned. Names and
descriptions are untrusted third-party text: control characters become spaces and strings are cut (256
characters, descriptions 1024). The results carry the data sensitivity `access-control-data`.

`service-accounts.list` wraps Google's `nextPageToken` in the Qatlas `next_cursor`, bound to the tool, the
targets, and the project. A cursor of another request is refused before a secret is read. The end is a missing
token: `has_more` is then false.

The account of the credential itself can be read like any other; this does not read its keys unless
`service-account-keys` is called.

## Scope and profile

Discovery `iam` v1 lists only `cloud-platform` for these methods and offers no read-only variant, so the tools
belong to the profile `read-iam`, not to the recommended `read`. See [IAM](iam.md) for the reasoning. The
rights are listed in [setup](setup.md).

## Left out on purpose

Creating, updating, patching, disabling, enabling, undeleting, and deleting an account, the policy of an
account (read with `iam-policies.get`), and signing, token, and key creation methods.

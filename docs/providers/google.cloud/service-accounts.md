---
description: >
  Describes the Google Cloud service account tools: the reads service-accounts.list and service-accounts.get, the changes create, patch, disable, and enable (profile service-accounts), and the listed-only service-accounts.delete, with the structured project and account arguments, the project check before and after the request, paging, the returned fields, the scope, and what is left out.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud service accounts

Two read tools of the IAM API (`https://iam.googleapis.com`, discovery `iam` v1, revision 20261106): the
service accounts of a project and one service account. Four more tools create, change, disable, and enable an
account (see [Changing accounts](#changing-accounts)). One more tool deletes an account and is only offered
where a connection names it (see [Deleting an account](#deleting-an-account)). The keys
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

Undeleting an account, the policy of an account (read with `iam-policies.get`), and signing, token,
and key creation methods. No tool creates a key, a token, or a signature.

## Changing accounts

| Tool | Effect | Google method | Arguments |
| --- | --- | --- | --- |
| `service-accounts.create` | create | `projects.serviceAccounts.create` | `project_id`, `account_id`, `display_name`, `description` |
| `service-accounts.patch` | update | `projects.serviceAccounts.patch` | `project_id`, `account`, `display_name`, `description` |
| `service-accounts.disable` | update | `projects.serviceAccounts.disable` | `project_id`, `account` |
| `service-accounts.enable` | update | `projects.serviceAccounts.enable` | `project_id`, `account` |

They belong to the profile `service-accounts`, which holds only these four tools, is not recommended, and is not
part of `read-iam`. The connection needs the effects `create` and `update`. They ask for `cloud-platform`, the
only scope discovery `iam` v1 lists for these methods, so the token may change what its account may. The account
needs the permissions `iam.serviceAccounts.create`, `update`, `enable`, or `disable` on the project, which
Google decides.

Every call needs the confirmation of a person. The confirmation names the project, the account, and the change;
display names and descriptions are untrusted text and are cut and quoted there. The project check is the one of
the reads and happens before any secret is read, and an email of another project is refused before any request.
For `create` there is no account yet, so only the project is checked.

- `account_id` is 6 to 30 characters of `[a-z][-a-z0-9]*[a-z0-9]`. `create` is not idempotent: a repeated call
  ends with `ALREADY_EXISTS` (HTTP 409), shown as a provider error that says the account exists.
- `display_name` is at most 100 and `description` at most 256 UTF-8 bytes, without control characters. `patch`
  needs at least one of them and sends exactly the set ones as the update mask. An empty string clears the member.
- `create` and `patch` return the account like `service-accounts.get`. Google may leave members out of a patch
  answer. The answer must belong to the addressed account in the checked project, otherwise the call fails and
  says that its outcome is unknown.
- `disable` and `enable` are idempotent. Google answers with an empty body, so the result is `projectId`,
  `account` as given, and `disabled` (true after `disable`). Disabling keeps keys, roles, and policies.
- A new account has no roles and no key. Roles come only from the IAM binding tools, and no tool creates a key.
  Answers are decoded into a type without key fields, so a `privateKeyData` in an answer is never returned.
- A change is sent once and never repeated. After a transport failure, a 5xx answer, or an unreadable answer the
  message says that the outcome is unknown: read the account before changing it again.

## Deleting an account

| Tool | Effect | Google method | Arguments |
| --- | --- | --- | --- |
| `service-accounts.delete` | delete | `projects.serviceAccounts.delete` | `project_id`, `account` |

`service-accounts.delete` is listed-only: no effect and no prefix such as `service-accounts.*` offers it, and no
profile holds it. A connection has it only when its `tools` list names `google.cloud.service-accounts.delete`
exactly. It asks for `cloud-platform` and needs the permission `iam.serviceAccounts.delete` on the project.

Every call needs the confirmation of a person. It names the project and the account and says that services that
use the account fail afterwards. Deleting removes the account with its keys and role bindings. Google may allow a
recovery for a limited time only, and `undelete` is not a tool. Block an account with `service-accounts.disable`
first. The project check is the one of the other tools and happens before any secret is read. The result is
`projectId`, `account` as given, and `deleted`. The change is sent once; after a transport failure, a 5xx answer,
or an unreadable answer the message says that the outcome is unknown.

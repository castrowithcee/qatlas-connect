---
description: >
  Describes the Google Merchant user tools: listing and reading the users of a Merchant Center account, replacing the roles of a user, and removing a user, with the target rule, the checked address form, the allowed roles, what the results show, the effects of the changing tools, and what the tools exclude.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant users

The user tools manage who may access a Merchant Center account. They belong to the [Google Merchant
provider](README.md), which describes the credential, targets, quotas, and error classes.

| Tool | Effect | Offered |
| --- | --- | --- |
| `google.merchant.users.list` | reads the users of an account | by the effect `read` and the profile `read` |
| `google.merchant.users.get` | reads one user | by the effect `read` and the profile `read` |
| `google.merchant.users.update` | replaces the roles of one user | only if the `tools` list of the connection names it |
| `google.merchant.users.delete` | removes one user and withdraws its access | only if the `tools` list of the connection names it |

A change asks for confirmation every time. The confirmation names the user and, for `update`, the new roles and
that every other role is removed.

## Target

The user tools act on the account itself, not on a sub-account through it. On a connection bound to targets the
`account` argument must be an advanced account the connection is bound to (`advanced-accounts/ACCOUNT`), and it may
be left out when exactly one is bound. A sub-account, an account target, or an advanced account that is not bound
is refused before any secret is read. A connection bound only to account targets is not offered the tools. Without
targets any account ID is accepted.

## The user argument

`user` is the e-mail address of the user, because Google names a user by it. Qatlas accepts only a plain ASCII
address of at most 254 characters: a local part of 1 to 64 letters, digits, and `.`, `_`, `+`, `'`, `-`, then `@`
and a domain of dot-separated labels of letters, digits, and `-`. Any other character, in particular `/`, `?`,
`#`, `%`, a space, or a control character, and any address with non-ASCII characters, is refused before any secret is
read or a request is sent. The accepted address is escaped as one path segment.

Google also accepts the alias `me` for the user the credential acts as. `users.get` takes it. `users.update` and
`users.delete` refuse it and need the address, so that the access of the credential cannot be changed or withdrawn
without naming it.

## Results

Every answer shows only `name`, `state`, and `accessRights`. `name` has the form `accounts/ACCOUNT/users/EMAIL`, so
it holds the e-mail address of a person: the results are personal data and are declared as such. `state` is
`PENDING` or `VERIFIED`. `accessRights` holds the roles. All text comes from Google and is untrusted: a name is cut
at 300 characters, every other text at 100, a list of roles holds at most 100 entries, and an unknown member is
dropped.

`users.list` takes `page_size` from 1 to 100; Google sends 50 when it is left out. A cursor is tied to the tool, the
account, the page size, and the targets.

## Changes

`users.update` sends one `PATCH` with the update mask `accessRights`, which is the only member Google supports.
`access_rights` is the complete new list of roles and replaces the current one: a role that is left out is removed,
and the lists are never merged. It must hold 1 to 5 roles, each of `STANDARD`, `READ_ONLY`, `ADMIN`,
`PERFORMANCE_REPORTING`, `API_DEVELOPER`, each at most once. `ACCESS_RIGHT_UNSPECIFIED` is refused. `API_DEVELOPER`
works only for an account with a developer registration. An update can take rights away from the user, including
the role Admin.

`users.delete` sends one `DELETE`. It permanently withdraws the access of the user. Google refuses to delete the
last admin of an account. The user can only be invited again in Merchant Center, because this provider does not
invite users.

Both changes need the role Admin of the credential. Neither is repeated, not after a `401` with a renewed token
and not after `429` or a server error, because Google may already have applied the request. The result reports
only that Google accepted the request, with the resource name and, for an update, the member that was named
(`updated: ["accessRights"]`). Google applies the change with a delay: read the user again to see the outcome.

## Boundary

Checked against the discovery document of the accounts API v1 (revision 20261118): the methods use the path
`accounts/{account}/users/{email}`, although the description of the `name` member spells it `accounts/{account}/user/{email}`;
`users.list` returns 50 users by default and at most 100; `users.patch` supports only `accessRights` in its
mask; `users.delete` answers with an empty object.

Excluded:

- `users.create`: it invites an e-mail address to the account.
- `users.me.verifySelf`: it accepts an invitation, which gives the credential access to a new account.

Not verified: no live call was made. Which roles Google accepts for a user that is still `PENDING`, and how long a
change takes to show, are not documented.

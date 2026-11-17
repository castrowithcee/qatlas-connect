---
description: >
  Describes the user permission tools of the Google Tag Manager provider: listing and reading the users of an account, changing the rights of an existing user, removing a user, why no tool adds a user, the own profile and scope, the availability by target, the personal data in the results, the confirmation, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: user permissions

The user permission tools of the [Google Tag Manager provider](README.md). They read who has access to an
account and change that access for people who already have it.

**The results hold email addresses.** `emailAddress` is personal data. It is returned uncut. Beside the
results, only the confirmation of `update` shows an address, the one the caller gives as
`previous_email_address`. No error or log entry carries it, and Google's own words of a failed answer are reduced
to the status. Names and addresses are untrusted content of whoever edits the account.

| Tool | Does |
| --- | --- |
| `user-permissions.list` | reads the users of an account with their rights, a batch per call with `cursor` |
| `user-permissions.get` | reads one user by `user_permission` |
| `user-permissions.update` | replaces the account and container rights of an existing user |
| `user-permissions.delete` | removes a user from the account and with it all access |

`user_permission` is a path such as `accounts/1/user_permissions/4`, as `list` returns it. It is checked
against the target of the connection before any request.

## No tool adds a user

There is no tool for creating a user permission. `update` has no argument for an address and rejects
`emailAddress` as an unknown member, so a call cannot give rights to a new person. Before it writes, `update`
reads the entry of the path. A path that does not exist ends as `not-found` and nothing is sent to change.
Rights of an existing user may be widened, also on containers the user did not hold before.

## Changing rights

`update` replaces the rights as a whole: send `accountAccess` and `containerAccess` as `get` returned them, with
the changes made. A container that is left out of `containerAccess` loses its right. `accountAccess.permission` is
`noAccess`, `user`, or `admin`. Each entry of `containerAccess` is `container`, a path of the form
`accounts/ACCOUNT/containers/CONTAINER`, with `permission` `noAccess`, `read`, `edit`, `approve`, or `publish`.
The account of each container must be the account of the user permission, and a container may be named once;
anything else is `invalid-request` before any request. The values for an unspecified permission are never sent.
The results name a container as `containerId`.

`previous_email_address`, `previous_account_access`, and `previous_container_access` are needed as well: the
address and the rights of the user as `get` returned them, with the containers in the same form as the new
values. They are only compared and never written, and no argument sets an address. Before `update` changes
anything, it reads the user and refuses the call as `invalid-request` when the address (compared without regard
to case) or the rights are not exactly what the user holds now; the refusal does not quote the address. The
address in the request is the stored one.

Removing or reducing the rights of the user of the connection itself can lock the connection out of the account.
The tool cannot tell, because the answers do not name the user of the token.

## Confirmation

`update` and `delete` need their own confirmation. The confirmation of `update` shows the address, the account
right before and after, and each container before and after, a container that is added or removed with `(none)`
on one side. It is built from the arguments, and the comparison with the stored entry makes the old values the
stored ones. The confirmation of `delete` names the path and the removal, not the address.

## Profile, scope, and availability

All four tools are in the profile `user-permissions`, which is not recommended and is in neither `read` nor
`write`. They need `https://www.googleapis.com/auth/tagmanager.manage.users`, also the two that only read,
because Google lists no other scope for these methods. A token with that scope can change users, although the
tools of the profile change only what is described here. The account of the credential needs the right to
manage users in Tag Manager.

A connection bound to a container does not offer the group, because user rights belong to an account. A
connection without a target, or bound to an account, offers it; with an account target only that account is
reached.

## Not verified

- Behaviour against the live API, including whether Google creates an entry when `update` meets a path that does
  not exist; the read before the write makes that irrelevant for these tools.
- The form of the IDs in a user permission path. The tools accept decimal IDs, like all other paths.
- That an update accepts the stored address in the request body. The tool sends it back as read.
- Whether a service account can be a user of an account.

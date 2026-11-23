---
description: >
  Describes the Google Merchant tools for notification subscriptions: reading and deleting a subscription, how a subscription is bound to the targets of a connection, the callback address as read-only untrusted text, the form of the subscription ID, rights and developer registration, and the limits checked against the Merchant API reference.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Notification subscriptions

Three tools list, read, and delete the notification subscriptions of a Merchant Center account in the notifications
sub-API of the Merchant API v1 (`notifications/v1`). A subscription makes Google push events, product status
changes or account service changes, to a callback address. The `account` argument follows the rules of
[Targets](README.md#targets): digits only, checked against the targets before any secret is read, and proven for
an account that no target names.

| Tool | Effect | Offered by |
| --- | --- | --- |
| `google.merchant.notification-subscriptions.list` | read | the profile `read` |
| `google.merchant.notification-subscriptions.get` | read | the profile `read` |
| `google.merchant.notification-subscriptions.delete` | delete, confirmation required | a connection whose `tools` list names it |

## Prerequisites

Google ties notifications to the developer registration of the account, which creates a user with the role API
developer ([setup](setup.md)). The reference of the three methods names neither a role nor the registration, so
which role a call needs is not verified. A call that Google refuses ends as `permission`.

## Results

A subscription has the members `name` (`accounts/ACCOUNT/notificationsubscriptions/ID`), `registeredEvent`
(`PRODUCT_STATUS_CHANGE` or `ACCOUNT_SERVICE_CHANGE`), `callBackUri`, `targetAccount` (`accounts/ACCOUNT`), and
`allManagedAccounts`. All are output. A flag that is `false` is shown as `false`, and a member that is not documented
is dropped.

`callBackUri` is untrusted text that whoever set it chose. It is cut at 2048 characters, never fetched or called, and
no tool takes it as an argument: this provider offers neither `create` nor `patch`, because both set the address
that Google sends events to. A subscription is created or changed outside Qatlas.

`list` takes `page_size` from 1 to 200 and a `cursor`. Without `page_size` Google sends up to 100. A cursor is tied
to the account, the page size, and the targets.

## Targets and subscriptions

A subscription belongs to the account of the call. It can name a `targetAccount` as the source of its events, or
set `allManagedAccounts`, which means all accounts managed by the owner. Both are output; neither gives access to
another account.

On a connection bound to targets, a `targetAccount` has to lie inside the binding: it is the owner itself, an
account a target names, or a direct sub-account of a bound advanced account. Otherwise the subscription would show
the address and the events of an account the connection does not reach. Qatlas therefore:

- drops such a row from `list`, together with a row that has no usable `name`, a `name` of another account, or a
  `targetAccount` that is not spelled `accounts/ACCOUNT`. A batch can be short or empty while `next_cursor` says there
  is more. Reading the sub-accounts of bound advanced accounts costs further calls, once per call.
- refuses it in `get` and `delete` without naming the other account.
- ends `get` when the answer is not the subscription asked for.

A subscription with `allManagedAccounts` and no `targetAccount` is shown. Without a target, every subscription of
the account is shown.

## Subscription ID

`subscription_id` is the last part of the `name`. The reference gives only one path segment as the form. Qatlas
accepts letters, digits, hyphen, and underscore, starting with a letter or digit, at most 64 characters. A longer or
differently spelled ID is refused before any secret is read.

## Delete

`delete` removes one subscription. Google then sends no further events to its callback address, so whoever relies on
them is no longer told about product status changes or account service changes. Qatlas cannot create the
subscription again. The confirmation text names this.

The subscription is read first, which costs a call, so that a subscription with a `targetAccount` outside the targets
is refused before anything is deleted. The time between that read and the deletion is not covered. The
deletion is sent once, without a body, and is never repeated. The result is only the acceptance, `accepted` and
`name`: Google processes the deletion with a delay, so list the subscriptions again after a while to see the
result.

## Checked against the reference

Checked against the discovery document of the notifications API v1 (revision 20261118):

- The paths are `notifications/v1/accounts/ACCOUNT/notificationsubscriptions` for the list and
  `.../notificationsubscriptions/ID` for read and delete.
- `pageSize` has the default 100 and the maximum 200; larger values are coerced to 200. Qatlas refuses values above
  200 instead of coercing them.
- The delete answers with an empty message.
- The subscription ID has the pattern `[^/]+` in the discovery document. The narrower form above is Qatlas' own.
- The reference names no role or registration for these methods.

Not verified: no live call was made. What Google answers for a subscription of an account that is no longer
reachable, and whether a deleted subscription still shows for a while, are not stated in the reference.

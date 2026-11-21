---
description: >
  Describes the account tools of the Google Merchant provider that list sub-accounts and change accounts: listing the sub-accounts of an advanced account, updating account settings, deleting an account, and creating a test account, with their targets, confirmation, tools list requirement, delayed processing, quota, bounds, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant: account tools

These tools list the sub-accounts of an advanced account and change Merchant Center accounts. They add to the
read tools of the [Google Merchant provider](README.md), whose configuration, targets, quotas, and errors apply
here too.

| Tool | Effect | Tools list | What it does |
| --- | --- | --- | --- |
| `google.merchant.accounts.list-subaccounts` | read | not required | lists the accounts an account aggregates |
| `google.merchant.accounts.update` | update | not required | changes name, language, time zone, or adult content of an account |
| `google.merchant.accounts.delete` | delete | required | deletes an advanced account or a standalone account for good |
| `google.merchant.accounts.create-test-account` | create | required | creates a test account |

The three tools that change data always need confirmation. `delete` and `create-test-account` are offered only to
a connection whose `tools` list names them; the effect profile of a connection is not enough. The profile `read`
holds only tools with the effect read, so it never offers a change.

## Targets

`list-subaccounts`, `delete`, and `create-test-account` act on an advanced account as a whole. On a connection
bound to targets, the `account` argument must be an advanced account the connection is bound to as
`advanced-accounts/ACCOUNT`. A sub-account, an account target, or an advanced account that is not bound is refused
before any secret is read or request is sent, and a connection bound only to account targets is not offered these
tools. Without targets any account ID is accepted. `delete` requires the `account` argument: a deletion never
assumes the one advanced account of the binding. Because of this rule, `delete` cannot remove a sub-account
through its advanced account's target; it removes the bound advanced account itself.

`update` acts on one account. Its `account` argument is required and follows the rule of `accounts.get`: an
account that a target names is changed directly, any other account only after one proof read shows it to be a
direct sub-account of a bound advanced account. An account outside the targets is refused without a change and
without naming it.

## Changes are accepted, not confirmed as applied

Google accepts a change and applies it with a delay. After `update` or `create-test-account`, `accounts.get` and
`accounts.list` can take several minutes to show it. The answer of a tool tells only that Google accepted the
request: `update` and `create-test-account` return the account as Google answered it, `delete` returns the
account ID and `deleted: true`.

A change is sent once and never repeated, not after a `401` and not after a timeout. If a request may have reached
Google and its outcome is unknown (a server error, no answer, or an answer that cannot be read), the error says so:
read the account before changing it again. Every call counts against the quota of the Accounts method group, also
a failed one, and calls for sub-accounts count against the pool of the advanced account. An answer of `429` holds
the next request of the credential for the seconds of `Retry-After`, at least 10 and at most 60. These tools need
the role Admin in Merchant Center; a missing role ends as `permission`.

## accounts.update

Qatlas sends only the members you pass and names exactly those in the update mask, so everything else stays as it
is. At least one member is required. There is no free mask. The members are the writable ones of the account:

| Argument | Form |
| --- | --- |
| `account_name` | 1 to 300 characters without control characters; Google asks for no punctuation, capital letters, or symbols such as `/` or `_` |
| `adult_content` | true or false; false is sent when you pass it |
| `language_code` | BCP-47 language code such as `en-US` or `sr-Latn`, at most 35 characters |
| `time_zone` | `id` (IANA name such as `Europe/Zurich`, at most 64 characters) and optionally `version` (IANA release such as `2025a`, at most 16 characters); it sets both the reporting and the display time zone |

The home page address (`homePageUri`) is output only in the Account resource and cannot be written, so no tool
here changes an address that directs customer traffic. `name`, `accountId`, and `testAccount` are output only too.
The 300 characters and the pattern checks of the language code and time zone are limits of Qatlas, not documented
by Google; Google validates the values itself and refuses a wrong one.

## accounts.delete

Google deletes the account for good, also an advanced account together with all its sub-accounts, and deletes the
developer registration of the account and its associated Google Cloud project. That removes the access of every
credential of that project, Qatlas included. Without `force`, the deletion succeeds only if the account provides
no services to other accounts and has no processed offers. With `force: true` Google deletes it anyway; the
documentation states no default for `force`, and Qatlas sends it only when you pass `true`. No tool restores an
account. The confirmation text names the account, the sub-accounts, and the Cloud project.

## accounts.create-test-account

A test account is not published to any Google surface and can never become a live account, and a live account can
never become a test account. All of `account_name`, `language_code`, and `time_zone` are required, `adult_content`
is optional. Google allows at most 5 test accounts per Google Account, and they count toward the limit of
Merchant Center accounts. The method always creates a standalone test account, not an advanced one: the new
account lies outside the targets of the connection, so no tool of a bound connection reaches it until a target
names it. The `account` argument is the account the test account is created under.

## accounts.list-subaccounts

Lists the accounts for which the given account is the aggregation provider, a batch per call with `page_size` (1 to
500; Google sends up to 250 when it is left out) and a `cursor` from the previous answer. Cursors are tied to the
account, the page size, and the targets. The quota of the Accounts group applies, and Google counts up to two
reads per sub-account and day. An account that is not an advanced account answers with an empty list or an error
of Google.

## Boundary

Checked against the reference pages of the accounts API v1: `listSubaccounts` is a `GET` with
`pageSize` (default 250, maximum 500, larger values coerced) and `pageToken`; `patch` takes an `updateMask` with
the members `accountName`, `adultContent`, `languageCode`, and `timeZone`, and needs admin access; `delete` takes
the optional `force` and needs admin access; `createTestAccount` takes an Account body with the members above and
5 test accounts per Google Account (guide, not reference). In the Account resource `accountName`, `timeZone`, and
`languageCode` are required and `homePageUri`, `accountId`, and `testAccount` are output only.

`createTestAccount` does not need an advanced account as its parent, because the guide says the parent is your Merchant Center account and the
result is standalone. Qatlas still requires the advanced-account target of the parent on a bound connection, so
that a bound connection does not create accounts under an account it was not bound to as an advanced account.

Not verified:

- No live call was made. Fixtures stand for the answers of Google.
- What Google answers for `delete` on an account with sub-accounts and `force` left out, or for a time zone or
  language code it does not know. Both end as an error class, not by reason.
- What `createTestAccount` does when the parent is a sub-account or an account without the right role.
- Whether `listSubaccounts` on an account that is not an advanced account is an empty list or an error.
- Which Merchant Center role `listSubaccounts` and `createTestAccount` need; the documentation names the role
  Admin only for `patch` and `delete`.

`accounts.createAndConfigure` is not offered: it adds users and a service link to another provider in one call.
No tool of this provider changes the home page address of an account, adds a user, or links an account to another.

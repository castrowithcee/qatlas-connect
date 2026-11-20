---
description: >
  Describes the Google Merchant tools that read and change the email preferences of the credential's user: the single-user service behind the alias users/me, the one setting newsAndTips, the confirmation of a change, and the limits checked against the Merchant API reference.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant email preferences

Two tools of the accounts API v1 read and change the email preferences of the user behind the credential. The
`account` argument follows the rules of [Targets](README.md#targets): digits only, checked against the targets before
any secret is read, and proven for an account that no target names.

| Tool | Effect | Offered by |
| --- | --- | --- |
| `google.merchant.email-preferences.get` | read | the profile `read` |
| `google.merchant.email-preferences.update` | update, confirmation required | a connection that enables the effect `update`, or whose `tools` list names it |

The service reads and changes the preferences of the authenticated user only: the person who gave the consent, or the
service account of the key. The path always uses the alias `users/me`; no other user is addressed. Google advises
users of an advanced account to name the advanced account rather than a sub-account.

`get` returns `newsAndTips` (`OPT_IN_STATE_UNSPECIFIED`, `OPTED_OUT`, `OPTED_IN`, or `UNCONFIRMED`, where an opt-in awaits
its confirmation mail in some countries). The `name` of the result is the alias
`accounts/ACCOUNT/users/me/emailPreferences`: the name Google returns holds the address of the user and is not repeated.

`update` takes `news_and_tips` (`OPTED_IN` or `OPTED_OUT`, required) and sends one `PATCH` with the update mask
`newsAndTips`. `UNCONFIRMED` is refused by Google and not offered here. The tool changes the mail settings of the person,
or of the service account, behind the credential. It needs a confirmation that names this, is never repeated, and
reports only that Google accepted the request: `accepted`, `name`, and `updated`. Google applies the change with a delay,
so read the preferences again after a while. Mandatory announcements are not part of the preferences.

## Limits

Checked against the discovery document of the accounts API v1 (revision 20261118): the reference names the field
`newsAndTips` and the mask `news_and_tips` or `newsAndTips`; no field `newsUpdates` exists.

Not verified: no live call was made. Which role the methods need and whether a service account has email preferences
at all are not shown by the reference.

Not offered: the email preferences of any other user.

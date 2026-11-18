---
description: >
  Describes the invitation tools of the Google Business Profile provider: listing the pending invitations of an account with their role and target, declining an invitation with the previous role and target as required comparison arguments, why no tool accepts an invitation, the Google methods behind them, the personal data, and how a target narrows them.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: invitations

The invitation tools of the [Google Business Profile provider](README.md). Credential, target, approval,
quotas, bounds, and errors are described there. A connection bound to a location does not offer them: an
invitation belongs to an account, and a location names none.

An invitation offers the consenting account the role of an admin on another account or location. The tools only
read invitations and decline them. No tool accepts one, see [Accepting is left out](#accepting-is-left-out).

## google.business-profile.invitations.list

`GET https://mybusinessaccountmanagement.googleapis.com/v1/accounts/{account}/invitations`. Lists the
invitations that are pending for one account. It is part of the profile `read`.

| Argument | Meaning |
| --- | --- |
| `account` | optional on a connection bound to an account, and then it must be that account; otherwise required as `accounts/ACCOUNT` |
| `filter` | optional, `ACCOUNTS_ONLY` or `LOCATIONS_ONLY`: only invitations for that kind of target |

Google offers no paging here; Qatlas shows at most 250 entries, although Google may list up to 1000. Each entry
carries every documented member: `name` (`accounts/ACCOUNT/invitations/INVITATION`), `role` (the invited role),
`targetType`, `targetAccount` (a sparse account, shown with the members of [accounts](accounts.md)), and
`targetLocation` (`locationName`, `address`, `placeId`). An empty list is left out of the result.

The documentation of Google names the filter only as "supported via the `targetType` field". Qatlas sends it as
`targetType=ACCOUNTS_ONLY` or `targetType=LOCATIONS_ONLY`, and it also drops every returned entry whose
`targetType` names the other kind, because Google may ignore a filter it does not know. An entry without a
`targetType` is kept.

**Personal data.** The target of an invitation is an account or location of whoever sent the invitation: its
name and, for a location, its address. The invitation is addressed to the account that is read, so its target is
shown in full; that is what a person needs to decide on it. The result is untrusted third-party content, and
the address is text that is never fetched. A connection that must not show it leaves the tool out with a `tools`
list instead of using the profile `read`.

## google.business-profile.invitations.decline

`POST https://mybusinessaccountmanagement.googleapis.com/v1/accounts/{account}/invitations/{invitation}:decline`
with the empty body `{}`. Declines one pending invitation. It needs confirmation and is part of the profile
`write`. Declining only withdraws an offer of access.

| Argument | Meaning |
| --- | --- |
| `invitation` | required, `accounts/ACCOUNT/invitations/INVITATION` of an invitation as `invitations.list` shows it |
| `previous_role` | required, the `role` of the entry as last read; only compared, never sent |
| `previous_target` | required, the target as last read: `targetAccount.name`, or `targetLocation.locationName` for a location; only compared, never sent |

Before it sends the decline, the tool reads the invitations of the account and refuses, without a change, when
the invitation is not in the list (within the first 250 entries) or when `previous_role` or `previous_target` is
not exactly what the list holds now. The confirmation shows the path of the invitation, its target, and its
role. A refusal does not repeat a name.

The path is checked against its form (the invitation ID is up to 64 letters, digits, `_`, or `-`) and against
the target before a secret is read or a request is sent. An account target accepts only invitations of that
account. Google allows 10 changes per minute and profile; Qatlas counts a decline against the account, like the
other changes of that account. A decline whose outcome is unclear (timeout, `5xx`, unreadable answer) is
reported as such and never repeated. The result is `{"name": ..., "declined": true}` once Google answered.

## Accepting is left out

`accounts.invitations.accept` is not a tool, and no argument accepts or creates an invitation. Accepting makes
the user of the connection an admin of an account or location that someone else controls. That widens the
reach of the credential through an entry that only the other side controls, so it is a new access, even if the
invitation was not sent to an address that Qatlas set. A person accepts an invitation in Google's own
interface. Declining is offered because it only reduces access.

## Not verified

- No live call was made. The methods, parameters, and answer members are taken from the discovery document of
  the My Business Account Management API v1, revision 20261116. That document lists no `state` member of an
  invitation, so the tool shows none.
- The syntax of `filter`, as described above.
- Whether `targetAccount` and `targetLocation` are always populated, and whether `locationName` is a resource
  name or a display name. The comparison of `previous_target` uses whatever the list shows.

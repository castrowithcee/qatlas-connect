---
description: >
  Describes the accounts tools of the Google Business Profile provider: listing the accounts of the consenting person with type filter, parent account, page size and cursor, reading one account, the Google methods behind them, the account fields with their personal data, and how a target narrows them.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Business Profile: accounts

The account tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. Both tools read and are safe to repeat. A connection bound to a
location does not offer them.

An account is a personal account, a location group, a user group, or an organization. Each answer carries the
documented fields: `name`, `accountName`, `type`, `role`, `permissionLevel`, `verificationState`, `vettedState`,
`accountNumber`, and for an organization `organizationInfo` with its registered domain, postal address, and
phone number. For a personal account `accountName` is the first and last name of the person, so the results
hold personal data.

## google.business-profile.accounts.list

`GET https://mybusinessaccountmanagement.googleapis.com/v1/accounts`. Lists the accounts the consenting
account owns or manages, the personal account first. Google returns up to 20 per page; `page_size` asks for
fewer. `next_cursor` reads on and is bound to the type and the parent account, not to the page size. An empty
list is no error.

- `type` keeps only accounts of one type and is sent as Google's filter `type=TYPE`, the only filter Google
  supports for accounts.
- `parent_account` lists the accounts an organization or a user group reaches directly.

A connection bound to an account answers with that account alone, read with `accounts.get`, and with an empty
list when `type` does not match it. It refuses `parent_account` and a cursor, since the accounts below a group
are not part of the target.

## google.business-profile.accounts.get

`GET https://mybusinessaccountmanagement.googleapis.com/v1/accounts/{account}`. Reads one account. Google
answers `404` both for an account that does not exist and for one the consenting account does not manage; both
are `not-found`. On a connection bound to an account, `account` may be left out and any other account is
refused before anything is sent.

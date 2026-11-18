---
description: >
  Describes the accounts tools of the Google Business Profile provider: renaming an account with the previous name as comparison, listing the accounts of the consenting person with type filter, parent account, page size and cursor, reading one account, the Google methods behind them, the account fields with their personal data, and how a target narrows them.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Business Profile: accounts

The account tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The two read tools are safe to repeat; `accounts.update` changes. A connection bound to a
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

## google.business-profile.accounts.update

`PATCH https://mybusinessaccountmanagement.googleapis.com/v1/accounts/{account}?updateMask=accountName`. Renames
one account. It needs confirmation and is part of the profile `write`. A connection bound to a location does not
offer it.

| Argument | Meaning |
| --- | --- |
| `account` | `accounts/ACCOUNT`; optional on a connection bound to an account, which it must then match |
| `account_name` | required, the new name, 1 to 500 characters without control characters |
| `previous_account_name` | required, the `accountName` as last read with `accounts.get`; only compared, never sent |
| `update_mask` | optional, only `accountName`, which is also the default |
| `validate_only` | optional, `true` asks Google to validate the change and make none |

`accountName` is the only field Google lets an update change, and it is the only member of the body, so the
tool changes no owner and no type. The tool reads the account first and sends nothing unless its `accountName`
is exactly `previous_account_name`; it also refuses a personal account, which Google refuses as well. The
confirmation shows the old and the new name. Changes are spaced like those of the
[account admin tools](account-admins.md): 6 seconds per account.

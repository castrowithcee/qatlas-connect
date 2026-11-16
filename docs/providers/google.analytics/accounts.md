---
description: >
  Describes the Google Analytics account tools: accounts.list-summaries lists the accounts the connection reaches with summaries of their properties and is the list of targets, accounts.list lists the accounts with their details and deletion mark, each with paging, the effect of targets, the result members, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Analytics accounts

The account tools of the [Google Analytics provider](README.md). They read and change nothing, a repeat is
safe, and the profile `read` holds both. They need the scope `analytics.readonly`; see
[setup](setup.md#rights-and-scopes). Configuration, targets, paging, and the boundary are described in the
provider page.

## google.analytics.accounts.list-summaries

Reads `accountSummaries.list` of the Analytics Admin API: the accounts the credential sees, each with a short
summary of its properties. This is the list to read first, since it names the accounts and properties that
other tools and the targets use.

- `page_size`: 1 to 200; Google returns at most 50 without it.
- `cursor`: the `next_cursor` of the previous page.

Each entry has `name` (`accountSummaries/ID`), `account` (`accounts/ID`), `displayName`, and
`propertySummaries` with `property` (`properties/ID`), `displayName`, `propertyType`, `parent`, and `canEdit`.
`propertyType` is `PROPERTY_TYPE_ORDINARY`, `PROPERTY_TYPE_SUBPROPERTY`, or `PROPERTY_TYPE_ROLLUP`. The parent
of a subproperty is a property.

On a connection with targets a bound account shows all its properties. Any other account shows only its bound
properties and is left out when it has none. An empty answer is an empty list.

## google.analytics.accounts.list

Reads `accounts.list`: the accounts with their own details.

- `page_size` and `cursor` as above.
- `show_deleted`: also lists accounts in the trash. They carry `deleted` true. The default is false.

Each entry has `name` (`accounts/ID`), `displayName`, `regionCode` (a CLDR region code), `createTime` and
`updateTime` exactly as Google sends them, `deleted` (absent when false), and `gmpOrganization` when the
account belongs to a Google Marketing Platform organization. The cursor fits only the same `show_deleted`.

On a connection with targets only the bound accounts are shown, so a page can be empty while `has_more` is
true. A connection bound to properties only is refused, because it binds no account.

## Not verified

- Whether `accountSummaries.list` shows an account whose properties the credential cannot see; it is shown as
  Google answers.
- The `gmpOrganization` address is kept as bounded text and is never fetched.

---
description: >
  Describes the Google Analytics account tools: accounts.list-summaries lists the accounts the connection reaches with summaries of their properties and is the list of targets, accounts.list lists the accounts with their details and deletion mark, accounts.get and accounts.get-data-sharing-settings read one account, accounts.update renames it or changes its region, and accounts.delete moves it to the trash under strict rules (listed only, explicit account binding, 35 day trash period), with paging, the effect of targets, the result members, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Analytics accounts

The account tools of the [Google Analytics provider](README.md). `accounts.list-summaries`, `accounts.list`,
`accounts.get`, and `accounts.get-data-sharing-settings` read and change nothing, a repeat is safe, the
profile `read` holds them, and they need the scope `analytics.readonly`. `accounts.update` changes an account
and needs the profile `write` or a `tools` entry. `accounts.delete` is in no profile. Both of these need the
scope `analytics.edit` and confirmation; see [setup](setup.md#rights-and-scopes). Configuration, targets,
paging, and the boundary are described in the provider page.

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

## google.analytics.accounts.get

Reads `accounts.get`: one account. `account` is `accounts/ID`. It may be left out when the connection binds
exactly one account. The result has the members of an entry of `accounts.list`. On a connection with targets
the account must be a bound account; a connection bound to properties only is refused. A different account in
the answer than the one asked for is an `invalid-response`.

## google.analytics.accounts.get-data-sharing-settings

Reads `accounts.getDataSharingSettings`: the data sharing settings of one account, with `account` as above and
the same targets rule. The result has `name` (`accounts/ID/dataSharingSettings`) and the switches
`sharingWithGoogleProductsEnabled` (Google products and services), `sharingWithOthersEnabled` (modeling
contributions and business insights, benchmarking), `sharingWithGoogleSupportEnabled` (technical support), and
`sharingWithGoogleAssignedSalesEnabled` (recommendations for your business). A switch that Google leaves out
is shown as `false`. The deprecated member `sharingWithGoogleAnySalesEnabled` is left out, because Google says
it is unused and always false.

## google.analytics.accounts.update

Calls `accounts.patch`. It changes only what is given, and needs confirmation:

- `account`: required, `accounts/ID`, a bound account on a connection with targets.
- `display_name`: 1 to 100 UTF-16 code units without control characters.
- `region_code`: a CLDR region code, two capital letters such as `CH` or three digits.

At least one of the two is required, and a call with neither is refused before any request. The update mask
follows from the arguments given. The result is the account after the change. The credential needs the scope
`analytics.edit` and the Editor role on the account in Analytics.

## google.analytics.accounts.delete

Calls `accounts.delete`: moves the account **with all its properties and their child resources** to the
Analytics trash. Nothing is erased at once. Google's help states that an item stays in the trash for 35 days
and is permanently purged after that, unless a person restores it in the Analytics trash (the Editor role is
needed). The API has no method to restore an account, and no tool of this provider restores one. Data is not
processed while the account is in the trash.

The tool takes `account` (`accounts/ID`, required), needs confirmation, and is the most restricted tool of the
provider:

- It is **listed only**: no profile holds it and no effect offers it. A connection offers it only when its
  `tools` list names `google.analytics.accounts.delete`.
- It runs only on a connection that **binds this account itself** as the target `accounts/ID` and binds **no
  property**. A connection without targets is refused, although the credential may reach the account. A
  connection that also binds a property is refused, as is any account other than a bound one. This is checked
  before a secret is read, and the refusal does not name the account.
- The result is the account name and `deleted` true once Google accepted the request.

The deletion is exactly one request without a retry. After a timeout, a `5xx` answer, or an unreadable answer
the outcome is unknown, and the error says so; read the account with `accounts.get` (its `deleted` mark) or
`accounts.list` with `show_deleted` before trying again. A `429` or `4xx` answer means nothing was changed.

## Not verified

- The 35 days come from the Analytics help page on the trash, which carries the label of the former Universal
  Analytics. The Admin API reference states a purge after an expiry time but gives no number.
- Whether a repeated `accounts.delete` on an account that is already in the trash succeeds or fails.

- Whether `accountSummaries.list` shows an account whose properties the credential cannot see; it is shown as
  Google answers.
- The `gmpOrganization` address is kept as bounded text and is never fetched.

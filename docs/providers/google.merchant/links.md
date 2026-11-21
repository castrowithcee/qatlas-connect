---
description: >
  Describes the Google Merchant tools that read the Google Business Profile accounts linked to an account and the Local Feeds Partnership providers offered in a country: the arguments, the region code, who may use them, which display values a connection bound to targets withholds and why, the result bounds, and the link tools that are left out on purpose.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant linked accounts

A Merchant Center account can be linked to a Google Business Profile account, which brings the store listings of the
profile into local programs, and to a Local Feeds Partnership (LFP) provider, which supplies local inventory and sales
data for a country. Two read tools show these objects. Targets, quotas, results, and errors are as described in the
[provider overview](README.md).

| Tool | Method | Effect |
| --- | --- | --- |
| `google.merchant.gbp-accounts.list` | `accounts.gbpAccounts.list` | reads the Business Profile accounts linked to an account |
| `google.merchant.lfp-providers.find` | `accounts.omnichannelSettings.lfpProviders.find` | reads the LFP providers Google offers in a country |

Both are reads, belong to the profile `read`, and need no confirmation. The `account` argument is checked against the
targets before any secret is read or a request is sent; an account that no target names is read only after the proof
read of the sub-accounts of a bound advanced account, as for `accounts.get`. An account target is enough. With exactly
one target, `account` may be left out.

## Left out on purpose

There is no tool to link a Business Profile account (`linkGbpAccount`) or an LFP provider (`linkLfpProvider`). A link
connects the account to an account of another party and gives that party access to the data of the account. This
provider never creates such a link: it reads existing ones and, where the contract allows, ends access. Linking is done
in Merchant Center. A test of the provider keeps both out of the catalog.

## Arguments

`gbp-accounts.list` takes `account`, `page_size`, and `cursor`. `lfp-providers.find` also requires `region_code`, two
uppercase letters of the country such as `CH`; Google names the country by the id of the omnichannel setting, which is
the region code, and Qatlas builds the path from that checked code alone. A code of another form is refused before any
secret is read. Google documents no filter for either list.

Both lists send 50 entries by default and at most 1000 per batch; Qatlas always sends the page size, 50 when `page_size`
is left out. A cursor is tied to the tool, the account, the region, the page size, and the targets.

## Result

A Business Profile account shows `name`, `gbpAccountId`, `type` (`USER` or `BUSINESS_ACCOUNT`), `listingCount`, and
`gbpAccountName`. `listingCount` is a decimal string as Google sends it; a value that is not a number is dropped. An LFP
provider shows `name`, `regionCode`, and `displayName`. Every member is shown as documented, none is derived, and a
member that is not documented is dropped.

### Names of other parties

`gbpAccountName` is the email address of the owner for a personal account and the name of the business for a business
account. It is a display value of an account that is not an account of the connection, and for a personal account
it is personal data. Objects outside the binding give identifiers only, so Qatlas reads the rule this way:

- A Business Profile account is never named by a target, which names Merchant Center accounts. On a connection bound
  to targets it therefore lies outside the binding, and `gbpAccountName` is dropped while `name`, `gbpAccountId`,
  `type`, and `listingCount` stay.
- A connection without targets reaches what the credential reaches, and shows `gbpAccountName` in full, email address
  included.
- `displayName` of an LFP provider is the public name of a provider that Google offers as a candidate. It is no link
  of the account and no value of a linked account, so it is always shown.

This is the narrower reading of the rule; the data-sensitivity of both tools is `merchant-account-data`.

Texts come from Google and from the owner of the account and are untrusted: names are cut at 300 characters, IDs and
codes at 100, a cut text ends with `…`, and a value of the wrong type is dropped. A batch holds at most 1000 entries.

## Boundary

Checked against the discovery document of the accounts API v1 (revision 20261118):

- `gbpAccounts.list` takes `parent`, `pageSize` (default 50, maximum 1000, a larger value is cut to 1000 by Google),
  and `pageToken`. `lfpProviders.find` takes the same three, with the parent `accounts/{account}/omnichannelSettings/{id}`.
- `GbpAccount` has `name`, `gbpAccountId`, `type` (`TYPE_UNSPECIFIED`, `USER`, `BUSINESS_ACCOUNT`), `listingCount`
  (int64), and `gbpAccountName`; `LfpProvider` has `name`, `regionCode`, and `displayName`. No member is deprecated.
- The research behind this provider named `gbpEmail` and a country as parameters of the list methods. They are
  parameters of the link methods and of the setting path, not of the lists, so no `gbp_email` argument exists.

Not verified:

- No live call was made. Answers are taken from the discovery document and the guides of the Merchant API.
- Which Merchant Center role the two reads need; the discovery document names none.
- Whether `find` accepts a country without an omnichannel setting, and what it answers then.
- The path form of a Business Profile account name: the discovery document writes `accounts/{account}/gbpAccount/{id}`,
  singular, while the method path says `gbpAccounts`.

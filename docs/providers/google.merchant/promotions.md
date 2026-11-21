---
description: >
  Describes the promotion tools of the Google Merchant provider: listing and reading processed promotions, inserting a promotion that creates or replaces one through the tools list of a connection, the arguments and their bounds, the documented rules Qatlas checks, the https rule for the promotion address, the refusal of addresses in custom attributes, the data source argument, confirmation, delayed processing, the missing delete, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant: promotions

The promotion tools of the [Google Merchant provider](README.md). The account argument, targets, quotas, cursors, and
errors are described there.

A promotion is an offer such as a percentage off, a money-off amount, a free gift, or free shipping that Google
shows next to your products in Google Shopping. A promotion belongs to a promotion data source (see [data
sources](data-sources.md)) and is identified by its promotion ID, its target country, and its content language.

| Tool | Does |
| --- | --- |
| `promotions.list` | reads the processed promotions of an account, a batch per call with `cursor` |
| `promotions.get` | reads one processed promotion |
| `promotions.insert` | creates a promotion, or replaces the one with the same promotion ID, country, and language |

`promotions.list` and `promotions.get` are in the `read` profile. `promotions.insert` is in no profile: a connection
offers it only when its `tools` list names it, and every call needs a confirmation. `account` is checked against the
targets before any secret is read; a sub-account of a bound advanced account is proven by one read of the sub-accounts
first, and an account outside the targets is refused without a request.

## No delete

Google documents no method that deletes or stops a promotion, and Qatlas offers none. **A promotion inserted through
Qatlas cannot be removed through Qatlas.** What the documentation supports is replacing it: inserting again with the
same promotion ID, country, and language updates the existing promotion. A promotion also ends with its effective
time period; the states `STOPPED` and `EXPIRED` appear in its status. Whether a promotion can be stopped or removed
through the API, or only in Merchant Center, is not verified. Treat every insert as visible to shoppers once Google
approves it and its display period has begun, and use a test account to try a promotion.

## Reading

`promotions.list` takes `account`, `page_size` (1 to 250), and `cursor`. Google returns as many promotions as it can
when no page size is sent, so Qatlas always sends one: 100 when you leave it out. A cursor is tied to the tool, the
account, the page size, and the targets. Google offers no filter. `promotions.get` takes `account` and `promotion`,
the last segment of the promotion name `accounts/ACCOUNT/promotions/ID` as the list shows it.

`promotion` is 1 to 200 ASCII letters, digits, dots, tildes, underscores, and dashes, starting with a letter or
digit. Google documents no form for it, so this is Qatlas' narrow form, chosen so that a value can never alter a path
or a query. A promotion whose name has another form can be listed but not read.

A promotion shows `name`, `promotionId`, `contentLanguage`, `targetCountry`, `redemptionChannel`, `dataSource`,
`versionNumber`, `customAttributes`, all 41 `attributes`, and `promotionStatus` (`destinationStatuses`,
`itemLevelIssues`, `creationDate`, `lastUpdateDate`). Integer values such as `percentOff` and `amountMicros` are
shown as strings, as Google sends them. Titles, descriptions, codes, store codes, and addresses come from whoever runs
the account and are untrusted: texts are cut at 300 characters (IDs, codes, and enum values at 100, addresses at
2048), every list inside a promotion holds at most 300 entries, a list of promotions at most 300 entries, and a member
that is not documented is dropped. Addresses are text and are never fetched.

Google computes the status and the issues asynchronously. After an insert, the processed promotion can take several
minutes until it can be read.

## Inserting

`promotions.insert` takes structured arguments only and builds the body itself; nothing is passed through.

| Argument | Meaning |
| --- | --- |
| `account` | the account, checked against the targets |
| `data_source` | ID of the data source, digits only; the tool sends `accounts/ACCOUNT/dataSources/ID` with the checked account, so the data source always belongs to the account of the call |
| `promotion_id` | your ID, 1 to 128 characters without control characters |
| `content_language` | two lower case letters |
| `target_country` | two upper case letters |
| `redemption_channel` | `ONLINE`, `IN_STORE`, or both |
| `version_number` | optional; Google refuses the insert when it is lower than the current version of the promotion |
| `attributes` | the promotion attributes under their Google names |
| `custom_attributes` | optional name and value pairs, or a name with `groupValues` |

`attributes` accepts the 41 documented attributes: `productApplicability`, `eventApplicability`, `offerType`,
`genericRedemptionCode`, `longTitle`, `couponValueType`, `promotionDestinations`, the inclusion and exclusion lists for
item IDs, brands, item group IDs, and product types, `minimumPurchaseAmount`, `minimumPurchaseQuantity`,
`limitQuantity`, `limitValue`, `percentOff`, `moneyOffAmount`, `getThisQuantityDiscounted`, `freeGiftValue`,
`freeGiftDescription`, `freeGiftItemId`, `promotionEffectiveTimePeriod`, `promotionDisplayTimePeriod`,
`storeApplicability`, `storeCodesInclusion`, `storeCodesExclusion`, `promotionUrl`, `redemptionRestriction`,
`customRedemptionRestriction`, `maxDiscountAmount`, `googleProductCategories`, `audience`, `regionIdInclusion`,
`minMoneyOffAmount`, `maxMoneyOffAmount`, `minPercentOff`, and `maxPercentOff`. The enum values are those of the
discovery document without the `UNSPECIFIED` values. Prices are `amountMicros` (an integer, one millionth of the
currency unit) with a three-letter upper case `currencyCode`. Times are RFC 3339 date-times.

Qatlas checks what Google documents and leaves the rest to Google:

- `offerType`, `longTitle`, `couponValueType`, `promotionDestinations` (at least one), and
  `promotionEffectiveTimePeriod` are required, and exactly one of `productApplicability` and `eventApplicability` is
  set.
- `customRedemptionRestriction` is required with `CUSTOM`, `regionIdInclusion` with the audience `LOCATION`, and at
  least one of the minimum and maximum bounds with `MONEY_OFF_RANGE` or `PERCENT_OFF_RANGE`. Store codes cannot be
  combined with `ALL_STORES`. `googleProductCategories` holds at most 5 entries.
- A period has `startTime`, `endTime`, or both, and the end is after the start.

Which attributes a coupon value type needs beyond that (for example `percentOff` with `PERCENT_OFF`), the allowed
languages and countries, and the length of a title or a code are not checked: Google documents them in Merchant
Center help pages, not in the method reference, and reports a violation as an error or as an item-level issue of the
processed promotion. The caps that Qatlas applies are its own: lists hold at most 300 entries of at most 100
characters, `longTitle`, descriptions, and restriction texts at most 300 characters, `custom_attributes` at most 100
entries with at most 100 `groupValues` each.

**Addresses.** `promotionUrl` sends shoppers to a page, so it must be an absolute `https` address with a host and
without user information, spaces, control characters, or backslashes. It is checked before any secret is read and is
never fetched. A custom attribute can carry any attribute of the Google data specification, including addresses, so
Qatlas refuses a custom attribute whose name contains `url`, `uri`, `link`, or `href`, or whose value looks like an
address (`://`, a leading `www.` or `//`). Only `promotionUrl` can direct shoppers.

## Replacing, confirmation, and result

An insert is an upsert: Qatlas sends the whole promotion of the call, and Google documents no merge of attributes that
the call leaves out, so name every attribute the promotion should keep. The confirmation shows the account, the promotion ID, the country, the language, the data source, and the
title, and states that an existing promotion is replaced, that it shows publicly once approved, and that it cannot be
deleted through Qatlas. There is no dry run.

The request is sent exactly once and never repeated, also not after `401`, `429`, or `5xx`. The result says only that
Google accepted the request (`accepted`) and shows the promotion as Google returned it. Processing, review, and the
start of the display period follow afterwards. A failure that leaves the outcome open (no answer, a timeout, a `5xx`,
or an unreadable answer) says that the outcome of the change is unknown, so read the promotion before inserting
again. `429` is `rate-limited` and `403` is `permission`; neither is repeated. Every call, failed ones included,
counts against the quota.

## Checked and not verified

Checked against the discovery document of the promotions API v1 (revision 20261118) and the method reference pages:
`list` sends at most 250 promotions and returns as many as possible without a page size, `insert` needs `promotion`
(with `promotionId`, `contentLanguage`, `targetCountry`, and at least one `redemptionChannel`) and `dataSource`,
`Attributes` has 41 members, at most 5 Google product categories are allowed, and an existing promotion is updated by
an insert. The reference gives no limit for text lengths, list lengths, or the number of promotions.

Not verified:

- Whether a promotion can be deleted or stopped through the API. No such method is documented.
- The form of the last segment of a promotion name (the documented pattern is any text without a slash), the maximum
  length of a promotion ID, and which languages and countries Google accepts.
- Which Merchant Center role `insert` needs; the method reference names none.
- Whether `data_source` must be a promotion data source; Google documents only the format of the name.
- How Google answers an insert with a lower `version_number` beyond an aborted error, and how long review takes.
- No live call was made.

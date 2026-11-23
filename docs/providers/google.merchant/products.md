---
description: >
  Describes the Google Merchant tools for products and issues: products.list and products.get read the processed products with all 145 attributes, aggregate-product-statuses.list reads the counts and issues per reporting context and country, and account-issues.list reads the account-level issues, with the argument forms, the product name forms, the delay before a change shows, the result bounds, and the limits checked against the discovery documents.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-19
---

# Google Merchant products and issues

The product and issue tools of the [Google Merchant provider](README.md). All four read and change nothing, a
repeat is safe, none needs a confirmation, and the profile `read` holds them. They share the `account` argument
and the targets of the provider page.

| Tool | What it reads | API method |
| --- | --- | --- |
| `google.merchant.products.list` | the processed products of an account, a page per call | `products/v1` `accounts.products.list` |
| `google.merchant.products.get` | one processed product | `products/v1` `accounts.products.get` |
| `google.merchant.aggregate-product-statuses.list` | product counts and issues per reporting context and country | `issueresolution/v1` `accounts.aggregateProductStatuses.list` |
| `google.merchant.account-issues.list` | the account-level issues | `accounts/v1` `accounts.issues.list` |

Each request is one `GET` to the fixed origin. The path is built from the checked account ID and, for
`products.get`, the checked product, never from free input, and nothing is requested twice.

## Account argument

`account` is the plain account ID. It is checked against the form and the targets before any secret is read, as
described for [targets](README.md#targets): an account that a target names is read at once, an account below a
bound advanced account only after one proof read of its sub-accounts, and any other account is refused without
being named. With exactly one target the argument may be left out.

`aggregate-product-statuses.list` works only for a standalone account or a sub-account, as the method documents
for its parent. Qatlas cannot tell the kind of an account, so it passes the call on and shows what Google
answers.

## Processed products

`products.list` and `products.get` read the processed product, the one Merchant Center shows after rules and
supplemental data sources apply. A product that was just inserted or changed shows in it only after several
minutes. Neither tool reads the product inputs, and neither changes anything.

`products.list` takes `page_size` and `cursor`. The API has no filter. Google sends 25 products when `page_size`
is left out and accepts up to 1000. The tool takes 1 to 100, because every product carries all its attributes
and a page of 1000 can pass the 4 MiB Qatlas reads from one answer. `next_cursor` is missing at the last page. A
cursor belongs to the tool, the account, the page size, and the targets, and another combination refuses it.

### Product argument

`products.get` takes `product`, the last part of the product name `accounts/ACCOUNT/products/PRODUCT`. It is
checked before any secret is read, in one of the two forms of the API:

- Plain: `content_language~feed_label~offer_id`, for example `en~US~sku-123`. A legacy local product has the
  prefix `local~`. The content language is two letters, the feed label has 1 to 20 letters, digits, hyphens,
  or underscores, and the offer ID has 1 to 200 letters, digits, dots, underscores, or hyphens. Any other
  character in the offer ID, among them `/`, `%`, `~`, `?`, `:`, and a space, needs the encoded form.
- Encoded: the unpadded base64url encoding of that string, for example `ZW5-VVN-c2t1LzEyMw` for `en~US~sku/123`.
  It is the last part of `base64EncodedName` in a listed product. The text must decode to the same structure,
  without control characters, with an offer ID of at most 200 characters, and a padded or non-canonical
  spelling is refused.

Qatlas puts the checked argument into the path as it is. Neither form can hold `/`, `..`, `?`, or `#`, so the
argument cannot leave the path.

### Result

A product has `name`, `offerId`, `contentLanguage`, `feedLabel`, `dataSource`, `versionNumber`, `legacyLocal`,
`archived`, `base64EncodedName`, `automatedDiscounts` (`priorPrice`, `gadPrice`, `priorPriceProgressive`),
`customAttributes` (`name`, `value`, `groupValues`), `productStatus`, and `productAttributes`.

`productStatus` has `creationDate`, `lastUpdateDate`, `googleExpirationDate`, `itemLevelIssues` (`code`,
`severity`, `resolution`, `reportingContext`, `attribute`, `applicableCountries`, `description`, `detail`,
`documentation`), and `destinationStatuses` (`reportingContext`, `approvedCountries`, `pendingCountries`,
`disapprovedCountries`).

`productAttributes` holds all 145 members of the `ProductAttributes` type of the discovery document, with the
members of the types they use, in the shape Google documents: `title`, `description`, `link`, `imageLink`,
`price`, `salePrice`, `availability`, `gtins`, `brand`, `condition`, `shipping`, `returns`, `loyaltyPrograms`,
the vehicle and property attributes, and the rest. A price is `currencyCode` and `amountMicros`, 64-bit integers
are strings as Google sends them, and an attribute the product does not set is missing. No member of the
document is deprecated. `base64EncodedProduct` belongs to the product input, not to the processed product, and
is not output.

### Untrusted content

Titles, descriptions, links, and every other text of a product come from whoever runs the account. They are
output as bounded text and are never fetched, among them `link`, `imageLink`, `adsRedirect`, the other link
attributes, and `documentation`. A text is cut with `…` at 2048 characters for an address, 1000 for a
description (`description`, `detail`, `content`, `answer`), 300 for other texts, and 100 for codes and enum
values. A list keeps at most 100 entries, objects nest at most 6 levels, and a member the document does not list
and a value of the wrong type are dropped. A list answer of more than 1000 entries is refused.

## Aggregate product statuses

`aggregate-product-statuses.list` reads, for each reporting context and country of the account, the number of
active, pending, disapproved, and expiring products and the issues that affect them. The counts follow the
processing of the products and lag a change by several minutes.

`reporting_context` and `country` are the only fields the filter of the API supports, so they are the only
filters here. Qatlas builds the filter from them, `reporting_context = "SHOPPING_ADS" AND country = "US"`, and
checks both values before any secret is read:

- `reporting_context` is one of the 19 values of the discovery document: `SHOPPING_ADS`, `DEMAND_GEN_ADS`,
  `DEMAND_GEN_ADS_DISCOVER_SURFACE`, `VIDEO_ADS`, `DISPLAY_ADS`, `LOCAL_INVENTORY_ADS`, `VEHICLE_INVENTORY_ADS`,
  `FREE_LISTINGS`, `FREE_LISTINGS_UCP_CHECKOUT`, `FREE_LOCAL_LISTINGS`, `FREE_LOCAL_VEHICLE_LISTINGS`,
  `YOUTUBE_AFFILIATE`, `YOUTUBE_SHOPPING`, `CLOUD_RETAIL`, `LOCAL_CLOUD_RETAIL`, `PRODUCT_REVIEWS`,
  `MERCHANT_REVIEWS`, `YOUTUBE_CHECKOUT`, and `RENTAL_ADS`. `DISCOVERY_ADS` is deprecated and refused.
- `country` is two capital letters, a CLDR territory code such as `US`.

`page_size` takes 1 to 250. Google sends 25 entries when it is left out. An entry has `name`, `reportingContext`,
`country`, `stats` (`activeCount`, `pendingCount`, `disapprovedCount`, `expiringCount`), and `itemLevelIssues`
(`code`, `severity`, `resolution`, `attribute`, `description`, `detail`, `documentationUri`, `productCount`).

## Account issues

`account-issues.list` reads the issues of the account itself, the ones that can affect all its products: `name`,
`title`, `detail`, `severity` (`CRITICAL`, `ERROR`, `SUGGESTION`), `impactedDestinations` (`reportingContext` and
`impacts` with `regionCode` and `severity`), and `documentationUri`. `page_size` takes 1 to 100, and Google sends
50 when it is left out.

`language_code` is a BCP-47 language code such as `en-US` or `sr-Latn` and sets the language of the readable
text, `en-US` when left out. `time_zone` is an IANA time zone such as `Europe/Zurich` and sets the time zone of
the readable text, `America/Los_Angeles` when left out. Both are checked against their form before any secret
is read.

## Quotas and errors

Each call counts against the method group of its sub-API, and the proof reads count against the accounts group.
A failure maps as described in [Errors](README.md#errors), and nothing is repeated after a `429`.

## Boundary

Checked against the discovery documents of the products, issue resolution, and accounts APIs v1 (revision
20261118):

| Value | Checked |
| --- | --- |
| `products.list` `pageSize` | default 25, maximum 1000, no filter; the tool stops at 100 |
| `ProductAttributes` | 145 members, none deprecated |
| product name | `content_language~feed_label~offer_id`, `local~` prefix for legacy local products, or unpadded base64url; the encoded form is required when a part holds `/`, `%`, or `~` |
| `aggregateProductStatuses.list` `pageSize` | default 25, maximum 250 |
| `aggregateProductStatuses.list` `filter` | only the fields `reporting_context` and `country` |
| `aggregateProductStatuses.list` parent | standalone accounts and sub-accounts only |
| `accounts.issues.list` `pageSize` | default 50, maximum 100 |
| `accounts.issues.list` `languageCode`, `timeZone` | BCP-47, default `en-US`; IANA, default `America/Los_Angeles` |

Not verified:

- No live call was made. The answers are taken from the discovery documents and the guides.
- How long a product takes to show in the processed resource: Google says several minutes.
- What Google answers when `aggregate-product-statuses.list` is called for an advanced account.
- Which Merchant Center role these methods need: the discovery documents name none for them.
- The size of an answer of a real catalog: the cuts above bound each product, not the number of attributes a
  merchant sets.

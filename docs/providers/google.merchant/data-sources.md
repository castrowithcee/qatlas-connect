---
description: >
  Describes the data source tools of the Google Merchant provider: listing and reading data sources of every kind, creating and updating API data sources, the arguments and their bounds, the update mask Qatlas builds, the refusal of file input, fetch settings, and Google Sheets sources, the shown members and the hidden fetch password, confirmation, delayed processing, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-19
---

# Google Merchant: data sources

The data source tools of the [Google Merchant provider](README.md). The account argument, targets, quotas, cursors,
and errors are described there.

A data source tells Merchant Center where the products, inventory, promotions, or reviews of an account come from.
The tools read data sources of every kind and create and change **API data sources** only: sources that receive their
data through the API. Every change is confirmed.

| Tool | Does |
| --- | --- |
| `data-sources.list` | reads the data sources of an account, a batch per call with `cursor` |
| `data-sources.get` | reads one data source |
| `data-sources.create` | creates a new API data source; a repeated call creates another one |
| `data-sources.update` | changes the display name, countries, destinations, or default rule of a data source |

`data-sources.list` and `data-sources.get` are in the `read` and `write` profiles, `data-sources.create` and
`data-sources.update` in `write` only. No tool of this group is limited to a connection's `tools` list. `account` is
checked against the targets before any secret is read, as for the account tools; a sub-account of a bound advanced
account is proven by one read of the sub-accounts first, and an account outside the targets is refused without a
request.

## Reading

`data-sources.list` takes `account`, `page_size` (1 to 1000), and `cursor`. Google returns every data source of the
account when no page size is sent, so Qatlas sends 100 when `page_size` is left out. A cursor is tied to the tool, the
account, the page size, and the targets. `data-sources.get` takes `account` and `data_source`, the numeric ID of the
data source (digits only, without a leading zero, up to 19 digits).

A data source shows `name`, `dataSourceId`, `displayName`, and `input` (`API`, `FILE`, `UI`, or `AUTOFEED`), and the
block for its kind: `primaryProductDataSource` (`feedLabel`, `contentLanguage`, `countries`, `destinations` with
`destination` and `state`, `legacyLocal`, `defaultRule`), `supplementalProductDataSource` (`feedLabel`,
`contentLanguage`, `referencingPrimaryDataSources`), `localInventoryDataSource`, `regionalInventoryDataSource`
(`feedLabel`, `contentLanguage`), `promotionDataSource` (`targetCountry`, `contentLanguage`),
`productReviewDataSource`, or `merchantReviewDataSource`. The reference lists of a default rule hold
`supplementalDataSourceName` or `self`. The deprecated `primaryDataSourceName` is not shown.

A file source shows `fileInput` with `fileName`, `fileInputType`, and `fetchSettings` (`fetchUri`, `username`,
`frequency`, `timeZone`, `timeOfDay`, `dayOfMonth`, `dayOfWeek`, `enabled`). **The fetch password is never shown**,
and user info inside `fetchUri` is removed. Names, file names, user names, and addresses are untrusted content of
whoever runs the account: they are cut (names at 300 characters, addresses at 2048, other texts at 100), addresses
are text that is never fetched, a member that is not documented is dropped, and every list inside a data source
holds at most 300 entries.

## API data sources only

Qatlas does not create or change data sources that fetch data from an address or read a Google Sheet, because
Merchant Center would then call that address, with the credentials given, on a schedule. `fileInput`, `fetchSettings`,
a fetch address, a user name, a password, and Google Sheets sources have no argument. A request that carries any of
these names, at any depth, is refused before a secret is read or a request is sent, and the message does not quote
it. Any other member that is not an argument is refused the same way. Local inventory sources exist only as file
sources and cannot be created. `update` changes only the four members below, whatever kind the source is, and never its file input.

## Creating

`data-sources.create` takes `account`, `display_name` (1 to 200 characters, no control characters), and exactly one
source block:

| Argument | Members |
| --- | --- |
| `primary_product` | `feedLabel`, `contentLanguage`, `countries`, `destinations`, `legacyLocal`, `defaultRule` |
| `supplemental_product` | `feedLabel`, `contentLanguage` |
| `regional_inventory` | `feedLabel`, `contentLanguage`, both needed |
| `promotion` | `targetCountry`, `contentLanguage`, both needed |
| `product_review` | an empty object |
| `merchant_review` | an empty object |

`feedLabel` is at most 20 upper case letters, digits, and dashes, `contentLanguage` a two-letter ISO 639-1 code in
lower case, and a country a two-letter CLDR territory code in upper case. For a primary or supplemental source,
`feedLabel` and `contentLanguage` are set together or not at all. `countries` holds 1 to 250 distinct codes,
`destinations` 1 to 20 distinct entries with one of the destination names of the Merchant API (such as
`SHOPPING_ADS` or `FREE_LISTINGS`) and an optional `state` of `ENABLED` or `DISABLED`. `defaultRule` holds
`takeFromDataSources` with 1 to 50 entries, each either `{"self": true}` or `{"supplementalDataSourceId": "ID"}`.
Qatlas builds the resource name from the ID and the account, so a rule cannot name a data source of another
account. `feedLabel`, `contentLanguage`, `legacyLocal`, and the target country cannot be changed after creation.
The primary source `defaultRule` replaces the whole rule and is no addition.

```json
{"account": "1000001", "display_name": "Main products",
 "primary_product": {"feedLabel": "US", "contentLanguage": "en", "countries": ["US"]}}
```

The body is built from these checked arguments; no free body, path, or mask goes through. A call creates a new data
source each time.

## Updating

`data-sources.update` takes `account`, `data_source`, and at least one of `display_name`, `countries`,
`destinations`, and `default_rule`. Qatlas builds the update mask from the arguments that are set, one path each:
`displayName`, `primaryProductDataSource.countries`, `primaryProductDataSource.destinations`, and
`primaryProductDataSource.defaultRule`. The caller cannot name a mask, so it cannot reach a file input or fetch
setting. Google deletes a member that is in the mask without a value, so every argument that is set needs a value
(an empty list is refused): nothing is cleared. A list replaces the list on the data source. The countries,
destinations, and default rule belong to a primary product source, and for another kind Google answers with an
error. Whether the data source is an API source is not read first.

## Confirmation, processing, and repetition

Both changes need a confirmation that shows the account, the display name or data source, and the kind or the changed
members. The result is `accepted: true` with the data source as Google returned it, and says only that Google accepted
the request. Google processes it afterwards: a data source can take some minutes to show in Merchant Center, and the
products of a data source appear in the processed products after minutes.

Qatlas sends a change once. It never repeats it, not after `401` (the cached token is dropped, so the next call renews
it), not after `429`, and not after an error. When a change may have reached Google but the answer is lost or
unreadable (a transport failure, a timeout, a `5xx`, or an answer that is not JSON), the error says the outcome is
unknown: read the data source with `data-sources.get` or `data-sources.list` before changing it again. A repeated
`create` would make another data source. Every call, also a failed one, counts against the quota of the method group.

## Boundary

Checked against the discovery document of the data sources API v1 (revision 20261118):

- `dataSources.list` has no default page size: without `pageSize` it returns every data source, and a value above
  1000 is cut to 1000. Qatlas sends 100 when none is given.
- `dataSources.patch` needs `updateMask`, the value `*` is not supported, and a member in the mask without a value is
  deleted.
- `dataSources.create` always creates a new data source, `input` is output only, and local inventory sources can only
  be file sources.
- `fetchSettings.password` is documented as an optional member of a file source and is not part of any result.

Not verified:

- No live call was made. Methods, members, and their bounds come from the discovery document and the guides.
- The bounds of `display_name`, which the document does not state, are Qatlas' own (1 to 200 characters).
- Which kinds Google lets an API create besides the primary, supplemental, regional inventory, and promotion sources,
  and whether the review sources are accepted for every account.
- Which Merchant Center role a data source change needs.
- What deleting or changing a data source does to its products. No tool deletes a data source, fetches one, or reads
  a file upload.

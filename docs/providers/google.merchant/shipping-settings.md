---
description: >
  Describes the Google Merchant tools for the shipping settings of an account: reading the whole setting and replacing it, who may use them, the arguments and the checks Qatlas makes before any request, the full replacement by insert and the etag, the results, the delayed processing and the missing repetition, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant shipping settings

The shipping settings of a Merchant Center account name the services a shop ships with: the countries, the rates,
the delivery times, the minimum order values, and the warehouses. Google Shopping shows them to customers. Two tools
cover them. Targets, quotas, results, and errors are as described in the [provider overview](README.md).

| Tool | Method | Effect |
| --- | --- | --- |
| `google.merchant.shipping-settings.get` | `accounts.shippingSettings.get` | reads the whole setting |
| `google.merchant.shipping-settings.insert` | `accounts.shippingSettings.insert` | replaces the whole setting |

## Access

`get` is a read and belongs to the profile `read`. `insert` is offered only to a connection whose `tools` list
names it, no effect profile selects it, and it needs a confirmation on every call: it changes what customers pay
and when they expect delivery. The method needs the role Admin in Merchant Center; Google answers a missing role with `403`, which Qatlas
reports as `permission`.

The `account` argument is the account ID. `insert` requires it, `get` takes it from a connection with exactly one
target. It is checked against the targets before any secret is read or a request is sent. An account that no target
names is read or changed only after the proof read of the sub-accounts of a bound advanced account, as for
`accounts.get`; a refused account is never changed. For an advanced account Google needs the ID of the
sub-account, never that of the advanced account. An account target is enough: these tools act on one account.

## Insert replaces everything

`insert` does not merge and has no update mask. Google replaces the shipping setting of the account with the
setting in the request: every service and every warehouse that the call does not send is removed. The tool says so
in its description and in the text of the confirmation, which names the account, the services and countries, and
the number of warehouses. The body that is sent is exactly the setting that the call passes, in the members
Google names, with the `etag` and nothing else: no member is added, dropped, or renamed on the way, and no
`name` is sent.

To change one detail, read the setting with `get`, pass all of it back with the change made, and pass the `etag`
of that read. Google fails the call when the setting changed after the read. For an account without a setting,
pass an empty `etag`.

## Arguments

The arguments are the writable members of the `ShippingSettings` resource, written as Google names them.

| Argument | Meaning |
| --- | --- |
| `etag` | required: the `etag` of `get`, or an empty string for the first insert |
| `services` | required, 1 to 50 services; every service of the new setting |
| `warehouses` | optional, up to 50 warehouses, referenced by services that use warehouse based delivery times |

A service takes `deliveryCountries` (two-letter upper case territory codes, each once), `serviceName` (unique in
the setting), `active`, `currencyCode`, and `deliveryTime` as required members, and `rateGroups`,
`shipmentType` (`DELIVERY`, `LOCAL_DELIVERY`, `COLLECTION_POINT`), `minimumOrderValue`,
`minimumOrderValueTable`, `storeConfig`, and `loyaltyPrograms` as optional ones. The delivery time takes either
`warehouseBasedDeliveryTimes`, or `minTransitDays` and `maxTransitDays`, or a `transitTimeTable`, together with
`cutoffTime`, the handling days, and the business day configurations. A rate group takes `applicableShippingLabels`,
`name`, `carrierRates`, and either a `singleValue` or a `mainTable` with optional `subtables`. A value is exactly
one of `noShipping`, `flatRate`, `pricePercentage`, `carrierRate`, and `subtable`. Table headers are exactly one of
`prices`, `weights`, `numberOfItems`, `postalCodeGroupNames`, and `locations`. A warehouse takes `name`,
`shippingAddress`, `cutoffTime`, `handlingDays`, and `businessDayConfig`. The resource name is output only and no
argument. Prices are an `amountMicros` string and a three-letter `currencyCode`.

Before any secret is read or a request is sent, Qatlas refuses: a member that is not documented, a missing
required member, a value outside its enum, members that Google documents as exclusive (`minimumOrderValue` with
`minimumOrderValueTable`, `warehouseBasedDeliveryTimes` with the other transit members, a `transitTimeTable` with
`minTransitDays` or `maxTransitDays`, `localCutoffTime` with `storeCloseOffsetHours`, both kinds of table
headers), a maximum below its minimum, handling days that are not set together, a `minimumOrderValueTable` outside
pickup or a `storeConfig` outside local delivery, a price in another currency than the service, a negative
price or percentage where Google documents none, `-1` (infinity) in a header anywhere but the last entry, a table
whose rows or cells do not match its headers, a `carrierRate` or `subtable` that no carrier rate or subtable of the
same rate group has, a label shared by two rate groups, an empty label list in any group but the last, and duplicate
service, warehouse, rate group, carrier rate, or subtable names. The counts and text lengths are Qatlas' own
bounds, which Google does not document: texts are 1 to 200 characters without control characters (300 for a street),
a call holds at most 1 MiB, and the clock and day bounds (hour 0 to 23, minute 0 to 59, days 0 to 365) are sanity
bounds. Everything else, such as supported carriers, time zone names, postal code groups, loyalty labels, and the
non-overlap of postal code groups, is left to Google.

## Results

`get` shows `name`, `etag`, `services`, and `warehouses` with every documented member. `insert` answers
`accepted: true` and the setting as Google answered. Everything is untrusted content of whoever runs the account:
texts are cut to 300 characters, lists are capped, members that are not documented are dropped, and a value of
the wrong type makes the answer invalid. Whole numbers that Google writes as strings are shown as numbers.

## Delayed processing and no repetition

The answer of `insert` only tells that Google accepted the change. It can take some time until `get` and Google
Shopping show it; Google documents no duration. Each call counts against the quota of the Merchant API, a failed
one too. Qatlas sends the change once and never repeats it, also not after a renewed token. A failure that may
have reached Google, such as a server error, a lost connection, or an unreadable answer, says that the outcome is
unknown: read the setting before changing it again. A `4xx` answer rejected the request before any change.

## Boundary

Checked against the reference pages of `accounts.shippingSettings.get` and `.insert` and of the `ShippingSettings`
resource in the accounts API v1, the discovery document, and the shared types `Price` and `Weight`: `get` is a `GET` on
`accounts/{account}/shippingSettings`, `insert` is a `POST` on `accounts/{account}/shippingSettings:insert` with
the resource as body and answer, no query parameters, and the scope `content`. `etag` is required, empty for the
first insert, and a stale one fails the call. Google documents no count, length, or size limit for the resource.

Where the pages differ, Qatlas follows the more cautious reading:

- The `get` page writes the resource name as `shippingsettings`, the URL as `shippingSettings`; Qatlas sends
  the URL form.
- The text of the table headers names `num_items` and `location`, the JSON members are `numberOfItems` and
  `locations`; Qatlas uses the JSON members. The text of a value names `carrier_rateName` for `carrierRate`.
- The text of `Weight.unit` says "kg and lb", while the enum of the discovery document is `POUND` and
  `KILOGRAM` (and `WEIGHT_UNIT_UNSPECIFIED`, which Qatlas refuses). Qatlas uses the enum names, which the JSON
  form requires, and refuses `kg` and `lb` before any request.
- The page does not state whether `storeConfig` is required for local delivery; Qatlas requires it to carry
  `storeServiceType` and a `serviceRadius`, which Google documents as required for local delivery.
- The page states no defaults for the cutoff time of a service other than 8 AM Pacific time; Qatlas sends none.
- `services` is optional on the page; Qatlas requires at least one service, so that no call empties the setting
  by leaving it out.

Not verified:

- No live call was made.
- The quota group of these methods and their limits.
- Which of the documented rules Google enforces on `insert` and which only on use, such as the match of
  warehouses, loyalty labels, and carriers.
- Whether Google accepts whole numbers as JSON numbers in every int64 member; Qatlas sends them as numbers.

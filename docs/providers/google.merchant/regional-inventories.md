---
description: >
  Describes the Google Merchant tools for the regional inventory of a product: regional-inventories.list reads the entries of a product per region, regional-inventories.insert replaces the whole entry of one region, and regional-inventories.delete removes it, with the product and region forms, the arguments and their bounds, the replacement per region, the delay before a change shows, confirmation, the tools list, error and unknown-outcome handling, and the limits checked against the reference of the inventories API v1.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant regional inventories

The regional inventory tools of the [Google Merchant provider](README.md). A regional inventory is the price and
availability of one product in one region of the account, identified by the region ID. The `account` argument,
targets, quotas, cursors, and errors are described on the provider page, the product forms on the page about
[products and issues](products.md), the regions themselves on the page about [regions](regions.md). The
[local inventories](local-inventories.md) work the same way per store.

| Tool | Does |
| --- | --- |
| `regional-inventories.list` | reads the entries of one product, one per region, a page per call with `cursor` |
| `regional-inventories.insert` | replaces the whole entry of one region of a product |
| `regional-inventories.delete` | deletes the entry of one region of a product |

`regional-inventories.list` is in the `read` profile. `insert` and `delete` are in no profile: a connection offers
them only when its `tools` list names them, and every call needs a confirmation that shows the account, the product,
the region, and, for an insert, which attributes are given. Both change what Google Shopping shows for a product in
a region. Neither is repeated by Qatlas.

All arguments are checked before any secret is read or any request is made: the account against the targets (a
sub-account of a bound advanced account is proven by one read of the sub-accounts first, and an account outside the
targets is refused without naming it), the product against its documented forms, the region, and every attribute.
The path is built from the fixed sub-API path, the checked account ID, the checked product, and the checked region,
so no `/`, `?`, `#`, `%`, or `..` can reach it.

## Product and region

`product` is the last part of the product name: `content_language~feed_label~offer_id` such as `en~US~sku123`, with
letters, digits, dot, underscore, and hyphen in the offer ID, or the unpadded base64url form of that string, which an
offer ID with any other character needs. A legacy local product carries the prefix `local~`.

`region` is the ID of a region of the account, as the [region tools](regions.md) create it. The reference marks it
required and immutable but gives no form, so Qatlas accepts the form the region tools use: 1 to 64 ASCII letters,
digits, underscores, and hyphens, starting with a letter or digit. An entry of a region with another ID can be
neither inserted nor deleted through these tools; `list` still shows it. Whether the region exists, and whether it
is eligible for regional inventories, is checked by Google.

## Reading

`regional-inventories.list` takes `account`, `product`, `page_size`, and `cursor`. Google sends 25000 entries when
`pageSize` is left out and allows at most 100000. The tool sends 100 when `page_size` is left out and takes 1 to
250, because an answer of that size is the largest this provider reads. A cursor is tied to the tool, the account,
the product, the page size, and the targets.

An entry shows `name`, `base64EncodedName`, `account`, `region`, and `regionalInventoryAttributes` with `price`,
`salePrice`, `salePriceEffectiveDate`, `loyaltyPrograms`, and `availability`. Prices are `currencyCode` and
`amountMicros`. Every text comes from whoever runs the account and is untrusted: texts are cut, lists keep at most
100 entries, a member that is not documented is dropped, and so is a value of the wrong type. Addresses are never
fetched; the type has none.

## Inserting: the entry of a region is replaced

**An insert replaces the whole entry of its region.** The reference says that Google "replaces the full
RegionalInventory resource if an entry with the same region already exists for the product". There is no field mask
and no merge: every attribute that the call leaves out is gone afterwards, not kept. To change one attribute, read
the entry with `list` and send all attributes that should stay. An insert with only `region` replaces the entry by an
empty one. The entries of other regions of the product are not touched, and a new region adds an entry. The body
that Qatlas sends holds `region` and exactly the attributes the call gave, nothing from an earlier read; a test pins
this by inserting twice for one region and reading the entries back from a fake that follows the documented
behavior.

`insert` takes `account`, `product`, `region`, and these optional attributes:

- `price` and `sale_price`: `amountMicros` (digits, millionths of the unit) and `currencyCode` (three capital letters).
- `sale_price_effective_date`: `startTime` and `endTime` as RFC 3339 times, as the API defines an interval: either
  may be left out for an open start or end, they may be equal, and only the times given are sent. `endTime` before
  `startTime` and an interval without any time are refused. Google requires `sale_price` whenever the date is set,
  and Qatlas checks that.
- `availability`: `IN_STOCK` or `OUT_OF_STOCK`. The other values of the local inventories are not accepted by this
  API, and the unspecified value is not a state to write.
- `loyalty_programs`: 1 to 20 entries with `programLabel`, `tierLabel`, `shippingLabel`, `price`,
  `cashbackForFutureUse`, `loyaltyPoints`, and `memberPriceEffectiveInterval` (`startTime`, `endTime`, with the
  rules of `sale_price_effective_date`). Google allows them only for programs set up in Merchant Center, for
  merchants that use regional availability and pricing, and in supported countries; Google checks these. The cap of 20
  entries and the label length of 100 characters are Qatlas' own.

The entry has `region` as its only required field, and `regionalInventoryAttributes` is optional. There is no
quantity and no custom attribute in this API. Qatlas checked the bounds, values, and members against the reference
page and the discovery document of the inventories API v1: `RegionalInventory`, `RegionalInventoryAttributes`,
`InventoryLoyaltyProgram`, `Interval`, and `Price` match the arguments and the result above, and the methods match
the paths and the page size given on this page. The reference page of the resource states neither the delay, nor the
page size, nor the replacement; these come from the method descriptions of the discovery document.

The role needed in Merchant Center is not verified.

## Deleting

`regional-inventories.delete` takes `account`, `product`, and `region` and sends one `DELETE` of that entry. The
answer is an empty object, and the result is `accepted` and the `name` of the entry. Google says that it can take up
to an hour until the entry is deleted from the product, and that after a successful answer the caller should wait
for that time before deleting again. Treat a deletion as permanent; Qatlas offers no undo, and the entry can only be
inserted again.

## Delay and result

An insert is processed afterwards: the reference says that it might take up to 30 minutes until a new or replaced
entry appears in the product. A delete can take up to an hour. The result of both says only that Google accepted
the request (`accepted`, and for an insert the entry as Google returned it), so a `list` right after a change can
still show the old state.

Both changing tools send exactly one request. A failure that leaves the outcome open (no answer, a timeout, a `5xx`,
or an unreadable answer) says that the outcome of the change is unknown, so read the entries before changing them
again. `429` is `rate-limited`, `403` is `permission`, `404` is `not-found`, and `401` is `auth`; none is repeated.
Every call counts against the quota, also a failed one.

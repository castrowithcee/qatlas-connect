---
description: >
  Describes the Google Merchant tools for the local inventory of a product: local-inventories.list reads the entries of a product per store, local-inventories.insert replaces the whole entry of one store code, and local-inventories.delete removes it, with the product and store code forms, the arguments and their bounds, the replacement per store code, the delay before a change shows, confirmation, the tools list, error and unknown-outcome handling, and the limits checked against the reference of the inventories API v1.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant local inventories

The local inventory tools of the [Google Merchant provider](README.md). A local inventory is the price, stock, and
pickup data of one product at one physical store, identified by the store code. The `account` argument, targets,
quotas, cursors, and errors are described on the provider page, the product forms on the page about
[products and issues](products.md).

| Tool | Does |
| --- | --- |
| `local-inventories.list` | reads the entries of one product, one per store, a page per call with `cursor` |
| `local-inventories.insert` | replaces the whole entry of one store code of a product |
| `local-inventories.delete` | deletes the entry of one store code of a product |

`local-inventories.list` is in the `read` profile. `insert` and `delete` are in no profile: a connection offers them
only when its `tools` list names them, and every call needs a confirmation that shows the account, the product, the
store code, and, for an insert, which attributes are given. Both change what Google Shopping shows for a product at
a store. Neither is repeated by Qatlas.

All arguments are checked before any secret is read or any request is made: the account against the targets (a
sub-account of a bound advanced account is proven by one read of the sub-accounts first, and an account outside the
targets is refused without naming it), the product against its documented forms, the store code, and every
attribute. The path is built from the fixed sub-API path, the checked account ID, the checked product, and the
checked store code, so no `/`, `?`, `#`, `%`, or `..` can reach it.

## Product and store code

`product` is the last part of the product name: `content_language~feed_label~offer_id` such as `en~US~sku123`, with
letters, digits, dot, underscore, and hyphen in the offer ID, or the unpadded base64url form of that string, which an
offer ID with any other character needs. A legacy local product carries the prefix `local~`.

`store_code` is the store ID of the Business Profile. The reference marks it required and immutable but gives no
form, so Qatlas accepts 1 to 64 ASCII letters, digits, dots, underscores, and hyphens, not starting with a dot. A
store with another code can be neither inserted nor deleted through these tools; `list` still shows its entry.

## Reading

`local-inventories.list` takes `account`, `product`, `page_size`, and `cursor`. Google sends 25000 entries when
`pageSize` is left out and allows at most 25000. The tool sends 100 when `page_size` is left out and takes 1 to 250,
because an answer of that size is the largest this provider reads. A cursor is tied to the tool, the account, the
product, the page size, and the targets.

An entry shows `name`, `base64EncodedName`, `account`, `storeCode`, and `localInventoryAttributes` with `price`,
`salePrice`, `salePriceEffectiveDate`, `loyaltyPrograms`, `customAttributes`, `availability`, `quantity`,
`pickupMethod`, `pickupSla`, `instoreProductLocation`, and `localShippingLabel`. Prices are `currencyCode` and
`amountMicros`; a group of custom attributes shows its sub-attributes in `groupValues`. Every text comes from whoever runs the account and is untrusted: texts are cut, lists keep at most
100 entries, a member that is not documented is dropped, and so is a value of the wrong type. Addresses are never
fetched; the type has none.

## Inserting: the entry of a store code is replaced

**An insert replaces the whole entry of its store code.** The reference says that Google "replaces the full
LocalInventory resource if an entry with the same storeCode already exists for the product". There is no field mask
and no merge: every attribute that the call leaves out is gone afterwards, not kept. To change one attribute, read
the entry with `list` and send all attributes that should stay. An insert with only `store_code` replaces the entry
by an empty one. The entries of other store codes of the product are not touched, and a new store code adds an
entry. The body that Qatlas sends holds `storeCode` and exactly the attributes the call gave, nothing from an
earlier read; a test pins this by inserting twice for one store and reading the entries back from a fake that
follows the documented behavior.

`insert` takes `account`, `product`, `store_code`, and these optional attributes:

- `price` and `sale_price`: `amountMicros` (digits, millionths of the unit) and `currencyCode` (three capital letters).
- `sale_price_effective_date`: `startTime` and `endTime` as RFC 3339 times, as the API defines an interval: either may be left
  out for an open start or end, they may be equal, and only the times given are sent. `endTime` before `startTime`
  and an interval without any time are refused; an interval without a time matches any time, which leaving the
  argument out says as well. Google
  requires `sale_price` whenever the date is set, and Qatlas checks that.
- `availability`: `IN_STOCK`, `LIMITED_AVAILABILITY`, `ON_DISPLAY_TO_ORDER`, or `OUT_OF_STOCK`. The reference text
  describes `LIMITED_AVAILABILITY` like `OUT_OF_STOCK`, which looks like a mistake of the page; Qatlas passes the value on.
- `quantity`: a whole number from 0, sent as the string Google expects.
- `pickup_method` (`BUY`, `RESERVE`, `SHIP_TO_STORE`, `NOT_SUPPORTED`) and `pickup_sla` (`SAME_DAY`, `NEXT_DAY`,
  `TWO_DAY` to `SEVEN_DAY`, `MULTI_WEEK`). The reference requires them together unless the method is `NOT_SUPPORTED`,
  and an SLA never goes without a method; Qatlas checks both rules.
- `instore_product_location`: 1 to 20 bytes, as documented, without control characters.
- `local_shipping_label`: 1 to 100 characters. The reference gives no length; the cap is Qatlas' own.
- `loyalty_programs`: 1 to 20 entries with `programLabel`, `tierLabel`, `shippingLabel`, `price`,
  `cashbackForFutureUse`, `loyaltyPoints`, and `memberPriceEffectiveInterval` (`startTime`, `endTime`, with the rules of `sale_price_effective_date`; this API
  names it differently from the product API, which calls it `memberPriceEffectiveDate`). Google allows them only in
  supported countries and for programs set up in Merchant Center, and requires the member `price` to be at most the
  regular price; Google checks these.
- `custom_attributes`: 1 to 100 entries, each a `name` (1 to 100 characters) with exactly one of `value` or
  `group_values`, as the type requires; Qatlas checks that before any secret is read. `group_values` holds
  sub-attributes of `name` and `value` only: **groups are one level deep**, a limit of Qatlas where Google allows
  groups inside groups. An attribute counts with its name and value, or with its group name and all names and values
  of its sub-attributes, and that sum is at most 10240 characters (Google documents the limit for name plus value;
  applying it to the whole group is the stricter reading). Attributes and sub-attributes together are at most 100,
  a cap of Qatlas where Google allows 2500 attributes with 102.4 kB in total. Google replaces underscores in custom
  attribute names by spaces.

The entry has `storeCode` as its only required field, and `localInventoryAttributes` is optional. Qatlas checked
the bounds, values, and members against the reference page and the discovery document of the inventories API v1
(revision 20261118): `LocalInventory`, `LocalInventoryAttributes`, `InventoryLoyaltyProgram`, `CustomAttribute`,
`Interval`, and `Price` match the arguments and the result above, and the methods match the paths and the page
size given on this page. `Interval` allows an open start or end, a start equal to the end, and no time at all; the
tool takes all of these except the last, which means no restriction and is expressed by leaving the argument out, and
it refuses an end before the start. A `quantity` and a price are sent as the
strings Google expects.

The call needs the program for local inventories in the account; the finding could not verify what Google requires
for it, and Google refuses the call otherwise. The role needed in Merchant Center is not verified either.

## Deleting

`local-inventories.delete` takes `account`, `product`, and `store_code` and sends one `DELETE` of that entry. The
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

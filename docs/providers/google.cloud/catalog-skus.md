---
description: >
  Describes the Google Cloud tool catalog-skus.list, which reads the SKUs and prices of one service of the public Cloud Billing Catalog: its arguments, time window and currency, page size and cursor, the fields, the scope and profile, and the lack of a target.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud billing catalog SKUs

`catalog-skus.list` reads the SKUs of one service of the public Cloud Billing Catalog, with their pricing
(`services.skus.list` of `cloudbilling` v1, discovery revision 20261106). It changes nothing and is a GET to
`https://cloudbilling.googleapis.com/v1/services/ID/skus`.

| Tool | Arguments |
| --- | --- |
| `catalog-skus.list` | `service_id`, `start_time`, `end_time`, `currency_code`, `page_size`, `cursor` |

**No target.** The catalog is public and tied to no target, so any connection of the provider may read it and the
tool is always offered.

**Arguments.** `service_id` is `services/XXXX-XXXX-XXXX` or the bare ID with uppercase letters and digits, as
[`catalog-services.list`](catalog-services.md) returns it, and is never a path. `start_time` and `end_time` are RFC
3339 timestamps: the inclusive start and the exclusive end of the time range of the pricing versions. Google
requires both to lie within one calendar month in America/Los_Angeles and not in the future; without them only the
latest pricing is returned. `currency_code` is an ISO 4217 code of three uppercase letters, and the conversion rate
is that of `start_time`; without it prices are in USD. Qatlas checks the form of every argument before any secret
is read and before any request, and Google checks the rest.

**Pages.** `page_size` is 1 to 100. Google's own default is 5000 rows, more than a page may hold here, so a call
without `page_size` asks for 100. `has_more` and `next_cursor` continue the list; a cursor continues only the list
with the same service, window, and currency, on the same connection. A row that does not belong to the service asked
for makes the answer invalid.

**Fields.** Every documented field is returned: `name`, `skuId`, `description`, `category` (`serviceDisplayName`,
`resourceFamily`, `resourceGroup`, `usageType`), `serviceRegions`, `pricingInfo` (`summary`, `pricingExpression`
with `usageUnit`, `usageUnitDescription`, `baseUnit`, `baseUnitDescription`, `baseUnitConversionFactor`,
`displayQuantity`, and `tieredRates` of `startUsageAmount` and `unitPrice`; `currencyConversionRate`,
`effectiveTime`, and `aggregationInfo`), `geoTaxonomy` (`type`, `regions`), and `serviceProviderName`. A price is
`currencyCode`, `units`, and `nanos`; Google leaves out a zero `units` or `nanos`. Texts are untrusted: control
characters become spaces and strings are cut, and a SKU holds at most 100 pricing versions, a pricing version at most
100 tiers, and a list of regions at most 100 entries.

**Scope and profile.** The tool asks for `cloud-billing.readonly`, the smallest scope the discovery document lists
for `services.skus.list`, and belongs to the profile `read`. The Cloud Billing API (`cloudbilling.googleapis.com`)
must be enabled in the project of the credential.

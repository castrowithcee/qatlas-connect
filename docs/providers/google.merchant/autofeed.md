---
description: >
  Describes the Google Merchant tools for the autofeed settings and the automatic improvements of an account: what the two updates change publicly, the arguments and update masks, acceptance without a result, the delayed processing, and the limits checked against the Merchant API reference.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Autofeed and automatic improvements

Four tools read and change two settings of a Merchant Center account in the accounts sub-API of the Merchant API
v1. The `account` argument follows the rules of [Targets](README.md#targets): digits only, checked against the
targets before any secret is read, and proven for an account that no target names.

| Tool | Effect | Offered by |
| --- | --- | --- |
| `google.merchant.autofeed-settings.get` | read | the profile `read` |
| `google.merchant.automatic-improvements.get` | read | the profile `read` |
| `google.merchant.autofeed-settings.update` | update, confirmation required | a connection whose `tools` list names it |
| `google.merchant.automatic-improvements.update` | update, confirmation required | a connection whose `tools` list names it |

## Public effect

Both updates change what Google shows publicly, and neither has a dry run.

- The autofeed takes products that Google crawls from the website of the account into Google Shopping
  automatically, without any product being submitted. A first activation usually shows products at once, a
  renewed one can take up to 24 hours. The account must meet Google's conditions (`eligible` of the get tool) and
  must not be a marketplace.
- Automatic improvements let Google change offers by itself: item updates overwrite price, availability, and
  condition with what Google finds on the website, image improvements replace images of disapproved offers with
  corrected ones, and shipping improvements change the delivery times shown to shoppers. Shipping improvements are
  not available for advanced accounts.

An update reports only that Google accepted the request: `accepted`, the `name` of the settings resource, and
`updated`, the members the request named. Google processes the change with a delay, so read the settings again
after a while. The confirmation text names the change and this effect.

## Arguments

`autofeed-settings.update` takes `enable_products` (required boolean) and sends `{"enableProducts": ...}` with the
update mask `enableProducts`.

`automatic-improvements.update` takes at least one of:

| Argument | Sent as | Mask |
| --- | --- | --- |
| `item_updates`: object with at least one of `allow_price_updates`, `allow_availability_updates`, `allow_strict_availability_updates`, `allow_condition_updates` | `itemUpdates.accountItemUpdatesSettings` | `itemUpdates` |
| `image_improvements`: boolean | `imageImprovements.accountImageImprovementsSettings.allowAutomaticImageImprovements` | `imageImprovements` |
| `shipping_improvements`: boolean | `shippingImprovements.allowShippingImprovements` | `shippingImprovements` |

The mask names only the members given, so the others stay as they are. A given member replaces that whole setting:
a flag of `item_updates` that is left out is not kept. There is no argument that deletes account level settings, so
an account that inherits its settings from an advanced account keeps inheriting until a value is set.
`allow_strict_availability_updates` has an effect only together with `allow_availability_updates`.

## Results

The get tools show the documented members and drop the others: `name`, `enableProducts`, `eligible` for the
autofeed, and for the improvements `itemUpdates` (the four account level flags and the four `effectiveAllow...`
values), `imageImprovements` (the account level flag and `effectiveAllowAutomaticImageImprovements`), and
`shippingImprovements`. A flag that is `false` is shown as `false`. The `effective` values are what applies, and can
be inherited from the advanced account.

## Checked against the reference

Checked against the discovery document of the accounts API v1 (revision 20261118):

- `updateMask` is required on both PATCH methods. The reference of the autofeed update lists no paths; only
  `enableProducts` can be written (`eligible` is output only). The reference of the improvements update lists
  `item_updates`, `item_updates.account_level_settings`, `image_improvements`,
  `image_improvements.account_level_settings`, `shipping_improvements`, and
  `shipping_improvements.allow_shipping_improvements`, in `snake_case` and `lowerCamelCase`. The provider uses
  only the top level names, because the nested names of the reference do not match the member names of the
  resources (`accountItemUpdatesSettings`, `accountImageImprovementsSettings`).
- A member given in the body but missing from the mask is not updated, and a member named in the mask but not
  present in the body is cleared; the provider never sends that case.
- The reference describes item updates as changing price, availability, and condition, and image improvements as
  correcting images of disapproved offers. It does not say that Google changes titles, so the tools do not claim it.

Not verified: no live call was made. Which Merchant Center role the two updates need is not stated in the reference.
Whether Google reports an ineligible account as an error or accepts the request without effect is not stated.

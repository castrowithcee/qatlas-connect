---
description: >
  Describes the asset set tools of the Google Ads provider that change the asset sets of an account: asset-sets.create, asset-sets.update, and asset-sets.remove, with the Mutate service and its operations list, the bound of 10 entries, the members of the AssetSet resource that each tool offers and the ones it leaves out (the Business Profile credential and email address, the output only members), name and type rules against the reference of v26, validate_only, confirmation, how create and update are reached by effect while remove needs the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-23
---

# Google Ads: asset set tools

These tools create, change, and remove the asset sets of a Google Ads account. An asset set is a named group of
assets of a type: a page feed, a Merchant Center feed, a dynamic feed, a location sync, or a location group. They
add to the read tools of the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors
apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.asset-sets.create` | create | creates one to 10 asset sets |
| `google.ads.asset-sets.update` | update | changes members of existing asset sets |
| `google.ads.asset-sets.remove` | delete | removes existing asset sets |

Reading asset sets is not a tool of this page: run `reports.search` on `asset_set`. Putting an asset into a set
and attaching a set to a campaign, an ad group, or the account are the [asset set assignment tools](asset-set-links.md). The assets themselves are the [asset tools](assets.md).

## How the tools are offered

- **`asset-sets.create` and `asset-sets.update`** are offered by a connection whose effects include `create` or
  `update`, and by a connection whose `tools` list names them. They belong to no profile: the `read` profile holds
  none of them. A connection with a `tools` list gets exactly the tools the list names, whatever its effects are.
- **`asset-sets.remove`** is offered only through the `tools` list. A connection with the `delete` effect and no
  `tools` list does not have it.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account and, per entry, the name, the type, and the names of the other members given, with texts cut and without
  control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `AssetSetService.MutateAssetSets` with an `operations`
  list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 10 entries. The 10
  are Qatlas's own bound; Google accepts up to 10,000 mutate operations per request.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation and the same checks and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the asset set after the change to
  each result as `asset_set`. Without it a result holds the `resource_name` only. `remove` has no such argument.
- **Names come from checked arguments.** `update` and `remove` name an asset set by `customers/ID/assetSets/N` of
  the `customer_id` of the call. A foreign account, another collection, a zero ID, and a temporary negative ID
  are refused. A caller never passes a field mask or an operation: the mask names exactly the members given.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the asset sets with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse
  the whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be `customers/ID/assetSets/N`
  of the `customer_id`. For `update` and `remove` the results must be exactly the asset sets asked for, in the
  order asked for. A returned asset set must carry the resource name of its result and, when it has an `id`, the
  same number, and every member must have the form of its kind. Anything else is `invalid-provider-response` with
  the note about the unknown outcome, since the change may have been made. A `partial_failure_error` in an answer
  is treated the same way, as the request asked for none.
- **Answer text is untrusted.** Texts in a returned asset set lose control characters and are cut.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message, such as `assetSetError DUPLICATE_ASSET_SET_NAME`. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create`, `update` | `asset_sets` | 1 to 10 entries; for `update` each `resource_name` once |
| `create` | `asset_sets[].name` | required, 1 to 128 characters, no control characters |
| `create` | `asset_sets[].type` | required, fixed after creation: `BUSINESS_PROFILE_DYNAMIC_LOCATION_GROUP`, `CHAIN_DYNAMIC_LOCATION_GROUP`, `DYNAMIC_CUSTOM`, `DYNAMIC_EDUCATION`, `DYNAMIC_FLIGHTS`, `DYNAMIC_HOTELS_AND_RENTALS`, `DYNAMIC_JOBS`, `DYNAMIC_LOCAL`, `DYNAMIC_REAL_ESTATE`, `DYNAMIC_TRAVEL`, `HOTEL_PROPERTY`, `LOCATION_SYNC`, `MERCHANT_CENTER_FEED`, `PAGE_FEED`, `STATIC_LOCATION_GROUP`, `TRAVEL_FEED` |
| `update` | `asset_sets[].resource_name` | `customers/ID/assetSets/N` of the account |
| `update` | `asset_sets[].name` | 1 to 128 characters |
| `create`, `update` | `merchant_center_feed` | `merchant_id` (positive number, required on create), `feed_label` |
| `create`, `update` | `location_set` | for `LOCATION_SYNC`. `location_ownership_type` (`BUSINESS_OWNER`, `AFFILIATE`; required, fixed after creation) and at most one of `chain_location_set` and `maps_location_set` |
| `create`, `update` | `location_set.chain_location_set` | `relationship_type` (`AUTO_DEALERS`, `GENERAL_RETAILERS`; required, fixed after creation) and `chains` (list of `chain_id` and `location_attributes`) |
| `create`, `update` | `location_set.maps_location_set` | `maps_locations`, a list of `place_id` |
| `create`, `update` | `business_profile_location_group` | `dynamic_business_profile_location_group_filter` with `label_filters`, `business_name_filter` (`business_name`, `filter_type` `EXACT`), and `listing_id_filters` |
| `create`, `update` | `chain_location_group` | `dynamic_chain_location_group_filters`, a list of `chain_id` and `location_attributes` |
| `create` | `location_group_parent_asset_set_id` | positive number, fixed after creation; Google requires it for location group types |
| `remove` | `resource_names` | 1 to 10 distinct asset sets of the account |
| all | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

At most one of `location_set`, `business_profile_location_group`, and `chain_location_group` may be given, as
Google's `asset_set_source` is a one-of. A list member replaces the whole list. An `update` entry needs at least
one member besides the resource name, and the members Google accepts only at creation (`type`,
`location_group_parent_asset_set_id`, `location_ownership_type`, `relationship_type`) are refused there.

Required members that Google's reference marks (name, type, `merchant_id`, `location_ownership_type`,
`relationship_type`, `chains`, `chain_id`, `maps_locations`) are checked before any request. Which type takes
which member, and whether a name may be used twice, is Google's to validate. Use `validate_only` to see it.

### Qatlas bounds

The reference names a length only for `name` (1 to 128). These bounds are Qatlas's own and refuse nothing Google
documents: 10 entries per call, 50 entries per list (`chains`, `maps_locations`, `label_filters`,
`listing_id_filters`, `location_attributes`), 256 bytes per filter text, place ID, and feed label.

## Answer

A result holds `resource_name`, and with `MUTABLE_RESOURCE` the asset set as `asset_set`: `id`, `name`, `type`,
`status`, `merchant_center_feed`, `location_set`, `business_profile_location_group`, `chain_location_group`,
`location_group_parent_asset_set_id`, `hotel_property_data` (`hotel_center_id`, `partner_name`), and
`travel_feed_data` (`subset_id`, `travel_feed_vertical_type`, `hotel_center_account_id`, `merchant_center_id`,
`partner_center_id`). `id`, `status`, `hotel_property_data`, and `travel_feed_data` are output only: they are
answered and never sent. Members of a newer API minor version that this page does not name are dropped.

## Boundary

Left out on purpose:

- **`business_profile_location_set`** of `location_set` as an argument. Google requires
  `http_authorization_token` of it, a credential for the Business Profile of another party, which never passes
  through a tool. A location sync from a Business Profile therefore cannot be set up with these tools. When
  Google answers such a set, `location_set.business_profile_location_set` holds `email_address`,
  `business_account_id`, `business_name_filter`, `label_filters`, and `listing_id_filters`; the token is never
  answered.
  Chain and Maps location syncs, location groups, and all other types are offered.
- The asset set assets (`AssetSetAsset`) and the links of a set to a campaign, which are the
  [asset set assignment tools](asset-set-links.md), the links of a set to an ad group or the account, and reading
  asset sets.
- `partial_failure`, any free operation, mask, or resource name, and `GoogleAdsService.Mutate`.

Checked against the Google Ads API reference of v26: the pages `AssetSet`, `AssetSetService`,
`AssetSetService/MutateAssetSets`, `MutateAssetSetsRequest`, `MutateAssetSetsResponse`, `AssetSetOperation`,
`LocationSet`, `BusinessProfileLocationGroup`, `ChainLocationGroup`, `AssetSet.MerchantCenterFeed`, and the enum
pages `AssetSetTypeEnum.AssetSetType`, `LocationOwnershipTypeEnum.LocationOwnershipType`,
`ChainRelationshipTypeEnum.ChainRelationshipType`, and `LocationStringFilterTypeEnum.LocationStringFilterType`.
The reference pages of the messages `BusinessProfileLocationSet`, `ChainSet`, `ChainFilter`, `MapsLocationSet`,
`DynamicBusinessProfileLocationGroupFilter`, and `BusinessProfileBusinessNameFilter` give no field list; their
fields come from the `asset_set_types.proto` and `asset_set.proto` files of v26 in Google's public API
repository. Besides the Business Profile and chain sources, v26 has the Maps source `maps_location_set`, which
these tools offer.

Not verified:

- Which role in Google Ads an account needs for these calls. Google documents it per call nowhere.
- Whether Google accepts the camelCase mask Qatlas sends for nested members, and which type accepts which member.
  No live call has been made.
- The limit on the number of asset sets of an account and the limits of the lists and texts of the nested members,
  which the pages read do not state.
- What happens to the use of a set by a campaign, an ad group, or the account when the set is removed. The pages
  read do not say. Whether the length of `name` counts characters or bytes is not stated either; Qatlas counts
  characters, which cannot refuse a name Google accepts.

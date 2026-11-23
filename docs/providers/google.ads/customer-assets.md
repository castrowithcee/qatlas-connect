---
description: >
  Describes the customer asset tools of the Google Ads provider that attach existing assets to the account as a whole: customer-assets.create, customer-assets.update, and customer-assets.remove, with the Mutate service and its operations list, the bound of 50 operations, the key of asset and field type and the resource name built from it, the statuses ENABLED and PAUSED, validate_only, confirmation, how all three are reached by effect or by the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, the result fields, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: customer asset tools

These tools attach existing assets to a Google Ads account as a whole, pause or enable such a use, and end it.
A customer asset is the use of one asset by the account for one field type, for example a sitelink asset as
`SITELINK` of the account; the account is the parent, so no campaign or ad group is named. They add to the read tools of the [Google Ads provider](README.md), whose configuration,
targets, quotas, and errors apply here too. The assets themselves are made with the [asset tools](assets.md).

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customer-assets.create` | create | attaches assets to the account, each for one field type |
| `google.ads.customer-assets.update` | update | pauses or enables an attached asset |
| `google.ads.customer-assets.remove` | delete | ends the use of an asset by the account for one field type |

Reading customer assets is not a tool of this page: run `reports.search` on `customer_asset`. Assets used by
campaigns or ad groups and asset sets are other tools.

## How the tools are offered

- **All three tools** are offered by a connection whose effects include `create`, `update`, or `delete`
  respectively, and by a connection whose `tools` list names them. They belong to no profile: the `read`
  profile holds none of them. A connection with a `tools` list gets exactly the tools the list names, whatever its
  effects are.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account, the field types and the asset IDs, cut after ten entries.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CustomerAssetService.MutateCustomerAssets` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50
  entries, each key at most once. The 50 are Qatlas's own bound; the reference pages of v26 name no maximum.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation, the same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the link after the change to each
  result as `customer_asset`. Google then returns the mutable fields only, so the fields it fills in itself
  (`source`, `primary_status`, `primary_status_reasons`, `primary_status_details`) are shown when Google sends
  them and are otherwise absent. `remove` has no such argument.
- **Names come from checked IDs.** The resource name
  `customers/ID/customerAssets/ASSET_ID~FIELD_TYPE` is built from `customer_id`, `asset_id`, and
  `field_type`; `create` builds the `asset` resource name the same way. A caller
  never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the links with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** For all three tools the results must be exactly the links asked for,
  in the order asked for, each with the resource name Qatlas built, which names only the account of the call.
  A returned link must carry that resource name, and an `asset` or `field_type` that it names must
  be the one asked for. Anything else is `invalid-provider-response` with the note about the unknown outcome,
  since the change may have been made. A `partial_failure_error` in an answer is treated the same way, as the
  request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message, such as `assetLinkError FIELD_TYPE_INCOMPATIBLE_WITH_ASSET_TYPE`, and the field path in the
  original. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target,
  are refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| all | `customer_assets` | 1 to 50 entries, each asset and field type once |
| all | `customer_assets[].asset_id` | positive number, the `asset.id`; no temporary ID |
| all | `customer_assets[].field_type` | name of an `AssetFieldType` of v26 other than `UNSPECIFIED` and `UNKNOWN`, such as `HEADLINE`, `SITELINK`, `CALLOUT` |
| `create` | `customer_assets[].status` | optional, `ENABLED` or `PAUSED`; left out, Google decides |
| `update` | `customer_assets[].status` | required, `ENABLED` or `PAUSED` |
| all | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

An `update` sends the field mask `status` and nothing else: asset and field type are immutable in the
reference and make up the resource name, so a link with another field type is a new link.

## Result

`customer_id`, `validate_only`, and `results`, one per operation in the order of the arguments: `asset_id`, `field_type`, `resource_name`, and with `MUTABLE_RESOURCE` `customer_asset` with
`resource_name`, `asset`, `field_type`, `status`, `source`, `primary_status`, `primary_status_reasons`, and
`primary_status_details` (each with `status`, `reason`, and for a disapproved asset
`asset_disapproved.offline_evaluation_error_reasons`). All texts of Google are cut and stripped of control
characters.

## Limits against the reference

Checked against the v26 reference pages `CustomerAsset`, `CustomerAssetService`,
`MutateCustomerAssets`, `MutateCustomerAssetsRequest`, `MutateCustomerAssetsResponse`, `CustomerAssetOperation`,
and `MutateCustomerAssetResult`. The status and primary status enums are the ones of the campaign asset and ad
group asset tools and were not read again for this page.

- **No numeric limits.** The pages name no maximum of operations, so the 50 per call are Qatlas's own. `partial_failure` defaults to false, which Qatlas keeps: all operations apply in one transaction or none.
- **Statuses.** The `CustomerAsset` page names the advertiser set values enabled, paused, and removed. The
  tools set `ENABLED` and `PAUSED` only. A removal is the remove operation; `REMOVED` as a status is refused
  before any request, since the pages do not say whether it is accepted on create or update.
- **Field types.** The page of `CustomerAsset` requires `field_type` and lists no values for the account. Any
  `AssetFieldType` passes Qatlas's check. Whether it fits the asset type is Google's to decide, and the
  errors come as `assetLinkError` codes. Use `validate_only` to see it.
- **Request shape.** `MutateCustomerAssetsRequest` takes `customer_id`, `operations[]`, `partial_failure`,
  `response_content_type`, and `validate_only`; a create names `asset` and `field_type`, an update needs the
  resource name, and a remove takes the resource name. The tools never send `partial_failure`. Pinning, call to
  action, and similar fields are not part of the `CustomerAsset` page, so no tool offers them.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a remove of a link that is already removed, and what an update of a removed link
  answers. No live call has been made.
- Which status a created link gets when `status` is left out.
- Whether `MUTABLE_RESOURCE` returns any of the output-only fields; Qatlas shows what Google sends.

Left out on purpose:

- Reading links and the assets themselves (see [asset tools](assets.md)).
- `partial_failure`, and any free operation, mask, or resource name.
- Links of campaigns and ad groups, and asset sets.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the customer asset family.

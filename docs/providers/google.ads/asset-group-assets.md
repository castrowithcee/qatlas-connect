---
description: >
  Describes the asset group asset tools of the Google Ads provider that attach existing assets to Performance Max asset groups: asset-group-assets.create, asset-group-assets.update, and asset-group-assets.remove, with the Mutate service and its operations list, the bound of 50 operations, the key of asset group, asset, and field type and the resource name built from it, the statuses ENABLED and PAUSED, validate_only, confirmation, how all three are reached by effect or by the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, the result fields, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: asset group asset tools

These tools attach existing assets to Performance Max asset groups of a Google Ads account, pause or enable such a use, and end it.
An asset group asset is the use of one asset by one asset group for one field type, for example a text asset as
`HEADLINE` of an asset group. They add to the read tools of the [Google Ads provider](README.md), whose configuration,
targets, quotas, and errors apply here too. The asset groups are made with the [asset group tools](asset-groups.md), the assets with the
[asset tools](assets.md).

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.asset-group-assets.create` | create | attaches assets to asset groups, each for one field type |
| `google.ads.asset-group-assets.update` | update | pauses or enables an attached asset |
| `google.ads.asset-group-assets.remove` | delete | ends the use of an asset by an asset group for one field type |

Reading asset group assets is not a tool of this page: run `reports.search` on `asset_group_asset`. Assets used by campaigns
(see [campaign asset tools](campaign-assets.md)) or by the account, and asset sets are other tools.

## How the tools are offered

- **All three tools** are offered by a connection whose effects include `create`, `update`, or `delete`
  respectively, and by a connection whose `tools` list names them. They belong to no profile: the `read`
  profile holds none of them. A connection with a `tools` list gets exactly the tools the list names, whatever its
  effects are.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account, the field types, the asset IDs, and the asset group IDs, cut after ten entries.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `AssetGroupAssetService.MutateAssetGroupAssets` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50
  entries, each key at most once. The 50 are Qatlas's own bound; the reference pages of v26 name no maximum.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation, the same checks, and costs the same operations as the change.
- **No `response_content_type`.** `MutateAssetGroupAssetsRequest` has no such field in v26, and
  `MutateAssetGroupAssetResult` holds `resource_name` only. The tools offer no such argument and the results carry no
  link object. Read the link, with its status and policy summary, with `reports.search` on `asset_group_asset`.
- **Names come from checked IDs.** The resource name
  `customers/ID/assetGroupAssets/ASSET_GROUP_ID~ASSET_ID~FIELD_TYPE` is built from `customer_id`, `asset_group_id`,
  `asset_id`, and `field_type`; `create` builds the `assetGroup` and `asset` resource names the same way. A caller
  never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the links with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** For all three tools the results must be exactly the links asked for,
  in the order asked for, each with the resource name Qatlas built, which names only the account of the call.
  A returned link must carry that resource name, and an `asset_group`, `asset`, or `field_type` that it names must
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
| all | `asset_group_assets` | 1 to 50 entries, each asset group, asset, and field type once |
| all | `asset_group_assets[].asset_group_id` | positive number, the `asset_group.id` that `reports.search` returns; no temporary ID |
| all | `asset_group_assets[].asset_id` | positive number, the `asset.id`; no temporary ID |
| all | `asset_group_assets[].field_type` | name of an `AssetFieldType` of v26 other than `UNSPECIFIED` and `UNKNOWN`, such as `HEADLINE`, `SITELINK`, `CALLOUT` |
| `create` | `asset_group_assets[].status` | optional, `ENABLED` or `PAUSED`; left out, Google decides |
| `update` | `asset_group_assets[].status` | required, `ENABLED` or `PAUSED` |
| all | `validate_only` | true validates only |

An `update` sends the field mask `status` and nothing else: asset group, asset, and field type are immutable in the
reference and make up the resource name, so a link with another field type is a new link.

## Result

`customer_id`, `validate_only`, and `results`, one per operation in the order of the arguments: `asset_group_id`,
`asset_id`, `field_type`, `resource_name`. Google returns nothing else per result. The resource name is the one Qatlas built, checked against
the answer.

## Limits against the reference

Checked against the v26 reference pages `AssetGroupAsset`, `AssetGroupAssetService`, `MutateAssetGroupAssets`,
`MutateAssetGroupAssetsRequest`, `AssetGroupAssetOperation`, and `MutateAssetGroupAssetResult`.

- **No numeric limits.** The pages name no maximum of operations, so the 50 per call are Qatlas's own.
- **Statuses.** `AssetLinkStatus` has `ENABLED`, `PAUSED`, and `REMOVED`. The tools set `ENABLED` and `PAUSED`
  only. A removal is the remove operation; `REMOVED` as a status is refused before any request, since the pages do
  not say whether `REMOVED` is accepted on create or update.
- **Field types.** The page of `AssetGroupAsset` lists no values for an asset group, only `HEADLINE` and
  `YOUTUBE_VIDEO` as examples. Any `AssetFieldType` other than `UNSPECIFIED` and `UNKNOWN` passes Qatlas's check.
  Whether it fits the asset type and the asset group is Google's to decide; use `validate_only` to see it. The
  error codes of that decision were not checked for this service.
- **Other fields.** Pinning, call to action, and similar fields are not part of the `AssetGroupAsset` page, so no
  tool offers them. `policy_summary` and the primary status fields are output only and are read with
  `reports.search`.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a remove of a link that is already removed, and what an update of a removed link
  answers. No live call has been made.
- Which status a created link gets when `status` is left out.

Left out on purpose:

- Reading links and the assets themselves (see [asset tools](assets.md)).
- `partial_failure`, and any free operation, mask, or resource name.
- Links of campaigns, ad groups, and the account, and asset sets.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the asset group asset family.

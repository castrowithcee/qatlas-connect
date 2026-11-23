---
description: >
  Describes the campaign asset tools of the Google Ads provider that attach existing assets to campaigns: campaign-assets.create, campaign-assets.update, and campaign-assets.remove, with the Mutate service and its operations list, the bound of 50 operations, the key of campaign, asset, and field type and the resource name built from it, the statuses ENABLED and PAUSED, validate_only, confirmation, how all three are reached by effect or by the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, the result fields, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: campaign asset tools

These tools attach existing assets to campaigns of a Google Ads account, pause or enable such a use, and end it.
A campaign asset is the use of one asset by one campaign for one field type, for example a sitelink asset as
`SITELINK` of a campaign. They add to the read tools of the [Google Ads provider](README.md), whose configuration,
targets, quotas, and errors apply here too. The assets themselves are made with the [asset tools](assets.md).

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-assets.create` | create | attaches assets to campaigns, each for one field type |
| `google.ads.campaign-assets.update` | update | pauses or enables an attached asset |
| `google.ads.campaign-assets.remove` | delete | ends the use of an asset by a campaign for one field type |

Reading campaign assets is not a tool of this page: run `reports.search` on `campaign_asset`. Assets used by ad
groups or by the account, asset sets, and the removal of automatically created assets
(`campaign-automatically-created-assets.remove`, see [asset tools](assets.md)) are other tools.

## How the tools are offered

- **All three tools** are offered by a connection whose effects include `create`, `update`, or `delete`
  respectively, and by a connection whose `tools` list names them. They belong to no profile: the `read`
  profile holds none of them. A connection with a `tools` list gets exactly the tools the list names, whatever its
  effects are.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account, the field types, the asset IDs, and the campaign IDs, cut after ten entries.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignAssetService.MutateCampaignAssets` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50
  entries, each key at most once. The 50 are Qatlas's own bound; the reference pages of v26 name no maximum.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation, the same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the link after the change to each
  result as `campaign_asset`. Google then returns the mutable fields only, so the fields it fills in itself
  (`source`, `primary_status`, `primary_status_reasons`, `primary_status_details`) are shown when Google sends
  them and are otherwise absent. `remove` has no such argument.
- **Names come from checked IDs.** The resource name
  `customers/ID/campaignAssets/CAMPAIGN_ID~ASSET_ID~FIELD_TYPE` is built from `customer_id`, `campaign_id`,
  `asset_id`, and `field_type`; `create` builds the `campaign` and `asset` resource names the same way. A caller
  never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the links with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** For all three tools the results must be exactly the links asked for,
  in the order asked for, each with the resource name Qatlas built, which names only the account of the call.
  A returned link must carry that resource name, and a `campaign`, `asset`, or `field_type` that it names must
  be the one asked for. Anything else is `invalid-provider-response` with the note about the unknown outcome,
  since the change may have been made. A `partial_failure_error` in an answer is treated the same way, as the
  request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message, such as `assetLinkError FIELD_TYPE_INCOMPATIBLE_WITH_ASSET_TYPE`, and the field path in the
  original. The other codes of the README apply.
- **Quota.** Each request counts as one operation, a refused one too, however many operations the list holds. A
  call may add one operation for the membership read below a manager target and one for the check that the
  account is no manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target,
  are refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| all | `campaign_assets` | 1 to 50 entries, each campaign, asset, and field type once |
| all | `campaign_assets[].campaign_id` | positive number, the `campaign.id` that `reports.search` returns; no temporary ID |
| all | `campaign_assets[].asset_id` | positive number, the `asset.id`; no temporary ID |
| all | `campaign_assets[].field_type` | name of an `AssetFieldType` of v26 other than `UNSPECIFIED` and `UNKNOWN`, such as `HEADLINE`, `SITELINK`, `CALLOUT` |
| `create` | `campaign_assets[].status` | optional, `ENABLED` or `PAUSED`; left out, Google decides |
| `update` | `campaign_assets[].status` | required, `ENABLED` or `PAUSED` |
| all | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

An `update` sends the field mask `status` and nothing else: campaign, asset, and field type are immutable in the
reference and make up the resource name, so a link with another field type is a new link.

## Result

`customer_id`, `validate_only`, and `results`, one per operation in the order of the arguments: `campaign_id`,
`asset_id`, `field_type`, `resource_name`, and with `MUTABLE_RESOURCE` `campaign_asset` with `campaign`,
`resource_name`, `asset`, `field_type`, `status`, `source`, `primary_status`, `primary_status_reasons`, and
`primary_status_details` (each with `status`, `reason`, and for a disapproved asset
`asset_disapproved.offline_evaluation_error_reasons`). All texts of Google are cut and stripped of control
characters.

## Limits against the reference

Checked against the v26 reference pages `CampaignAsset`, `CampaignAssetService`,
`MutateCampaignAssets`, `MutateCampaignAssetsRequest`, `CampaignAssetOperation`, `MutateCampaignAssetResult`,
`AssetLinkStatusEnum`, `AssetSourceEnum`, `AssetLinkPrimaryStatusEnum`, `AssetLinkPrimaryStatusReasonEnum`,
`AssetLinkPrimaryStatusDetails`, `AssetDisapproved`, and `AssetLinkErrorEnum`.

- **No numeric limits.** The pages name no maximum of operations, so the 50 per call are Qatlas's own.
- **Statuses.** `AssetLinkStatus` has `ENABLED`, `PAUSED`, and `REMOVED`. The tools set `ENABLED` and `PAUSED`
  only. A removal is the remove operation; `REMOVED` as a status is refused before any request, since Google
  documents `INVALID_STATUS` for a status that does not fit the operation and the pages do not say whether
  `REMOVED` is accepted on create or update.
- **Field types.** The page of `CampaignAsset` requires `field_type` and lists no values for a campaign. Any
  `AssetFieldType` passes Qatlas's check. Whether it fits the asset type and the campaign type is Google's to
  decide (`FIELD_TYPE_INCOMPATIBLE_WITH_ASSET_TYPE`, `FIELD_TYPE_INCOMPATIBLE_WITH_CAMPAIGN_TYPE`,
  `UNSUPPORTED_FIELD_TYPE`, `EXCLUDED_PARENT_FIELD_TYPE`). Use `validate_only` to see it.
- **Other Google rules.** Links that Google created automatically cannot be created or turned into advertiser
  links (`CANNOT_CREATE_AUTOMATICALLY_CREATED_LINKS`, `CANNOT_MODIFY_ASSET_LINK_SOURCE`); use
  `campaign-automatically-created-assets.remove` to end the use of such an asset. Pinning, call to action, and
  similar fields are not part of the `CampaignAsset` page, so no tool offers them.

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
- Links of ad groups and of the account, and asset sets.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the campaign asset family.

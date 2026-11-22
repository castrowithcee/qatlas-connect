---
description: >
  Describes the asset set assignment tools of the Google Ads provider: asset-set-assets.create and remove, which put assets into an asset set or take them out, and campaign-asset-sets.create and remove, which attach an asset set to a campaign or detach it, with the Mutate services and their operations lists, the bound of 10 entries, the composite resource names and how they are built and checked, the absence of an update, validate_only, confirmation, how both create and remove are reached by effect without the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: asset set assignment tools

These tools connect asset sets to what they contain and to where they serve. An asset set assignment is a link
resource of v26 that ties two objects of one account: `AssetSetAsset` ties an asset to an asset set,
`CampaignAssetSet` ties an asset set to a campaign. A link has no member that can be changed, so each family has
a create and a remove tool and no update tool. They add to the read tools of the [Google Ads provider](README.md),
whose configuration, targets, quotas, and errors apply here too, and to the [asset set tools](asset-sets.md) that
create the sets and the [asset tools](assets.md) that create the assets.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.asset-set-assets.create` | create | puts one to 10 assets into asset sets |
| `google.ads.asset-set-assets.remove` | delete | takes assets out of asset sets again |
| `google.ads.campaign-asset-sets.create` | create | attaches one to 10 asset sets to campaigns |
| `google.ads.campaign-asset-sets.remove` | delete | detaches asset sets from campaigns again |

Reading the links is not a tool of this page: run `reports.search` on `asset_set_asset` or `campaign_asset_set`.
The same links for ad groups (`ad_group_asset_set`) and for the account (`customer_asset_set`) are not offered, nor
are the links of assets to campaigns, ad groups, and the account.

## How the tools are offered

- **All four tools** are offered by a connection whose effects include the effect of the tool (`create` for the
  create tools, `delete` for the remove tools), and by a connection whose `tools` list names them. They belong to
  no profile: the `read` profile holds none of them. A connection with a `tools` list gets exactly the tools the
  list names, whatever its effects are. Unlike `asset-sets.remove`, the remove tools of this page need no `tools`
  list: they end a link only and delete no asset set, asset, or campaign.
- **Confirmed per call.** Every call needs the confirmation of that one call. The confirmation text names the
  account and, per entry, the parts of the link or its resource name, with texts cut and without control
  characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `AssetSetAssetService.MutateAssetSetAssets` or
  `CampaignAssetSetService.MutateCampaignAssetSets` with an `operations` list and without `partial_failure`: Google
  applies all operations or none. A list holds 1 to 10 entries. The 10 are Qatlas's own bound; the reference of v26
  names none.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation and the same checks and costs the same operations as the change.
- **`response_content_type`** (create tools). `MUTABLE_RESOURCE` adds the link after the change to each result,
  as `asset_set_asset` or `campaign_asset_set`. Without it a result holds the `resource_name` only. The remove
  tools have no such argument.
- **Names come from checked arguments.** A create entry names the two objects it ties, each as
  `customers/ID/<collection>/N` of the `customer_id` of the call. A remove entry is the link's resource name,
  `customers/ID/assetSetAssets/ASSET_SET_ID~ASSET_ID` or `customers/ID/campaignAssetSets/CAMPAIGN_ID~ASSET_SET_ID`,
  which `reports.search` returns as `resource_name`. A foreign account, another collection, a zero ID, a leading
  zero, a temporary negative ID, and a key of the wrong number of IDs are refused. A caller never passes a field
  mask or an operation. The same link twice in one call is refused.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the links with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be the link collection of the
  `customer_id` with the key of the right shape. The results must be exactly the links asked for, in the order
  asked for: for a create call the resource name follows from the two IDs of each entry. A returned link must carry
  the resource name of its result, and the objects it names must be those the resource name names, in the account
  of the call. Anything else is `invalid-provider-response` with the note about the unknown outcome, since the
  change may have been made. A `partial_failure_error` in an answer is treated the same way, as the request asked
  for none.
- **Answer text is untrusted.** Texts in a returned link lose control characters and are cut.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message. The other codes of the README apply.
- **Quota.** Each request counts as one operation, a refused one too. A call may add one operation for the
  membership read below a manager target and one for the check that the account is no manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `asset-set-assets.create` | `asset_set_assets` | 1 to 10 entries, each an object of `asset_set` (`customers/ID/assetSets/N`) and `asset` (`customers/ID/assets/N`), both required, no other member, each pair once |
| `campaign-asset-sets.create` | `campaign_asset_sets` | 1 to 10 entries, each an object of `campaign` (`customers/ID/campaigns/N`) and `asset_set` (`customers/ID/assetSets/N`), both required, no other member, each pair once |
| `asset-set-assets.remove` | `resource_names` | 1 to 10 distinct `customers/ID/assetSetAssets/N~M` of the account |
| `campaign-asset-sets.remove` | `resource_names` | 1 to 10 distinct `customers/ID/campaignAssetSets/N~M` of the account |
| all | `validate_only` | true validates only |
| create tools | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

The `status` of a link is output only and is not an argument. Which asset set type accepts which asset or which
campaign type, and whether a link may exist twice, is Google's to validate. Use `validate_only` to see it.

### Qatlas bounds

The reference names no bound for these requests. The 10 entries per call are Qatlas's own and refuse nothing
Google documents.

## Answer

A result holds `resource_name`, and with `MUTABLE_RESOURCE` the link: `asset_set_asset` with `asset_set`, `asset`,
`resource_name`, and `status`, or `campaign_asset_set` with `campaign`, `asset_set`, `resource_name`, and `status`.
`status` is output only (`ENABLED` or `REMOVED`) and is answered, never sent. Members of a newer API minor version
that this page does not name are dropped. A removed link is answered by its resource name only.

## Boundary

Left out on purpose:

- The update of a link. The services of v26 offer create and remove only, and every member of a link is immutable.
- `ad-group-asset-sets` and `customer-asset-sets`, the same links for an ad group and for the account, and the links
  of assets to campaigns, ad groups, and the account.
- Reading the links, and everything of the asset sets and assets themselves: see the [asset set tools](asset-sets.md)
  and the [asset tools](assets.md).
- `partial_failure`, any free operation, mask, or resource name, and `GoogleAdsService.Mutate`.

Checked against the Google Ads API reference of v26: the pages `AssetSetAsset`, `CampaignAssetSet`,
`AssetSetAssetService`, `AssetSetAssetService/MutateAssetSetAssets`, `MutateAssetSetAssetsRequest`,
`AssetSetAssetOperation`, `MutateAssetSetAssetResult`, `CampaignAssetSetService`,
`CampaignAssetSetService/MutateCampaignAssetSets`, `MutateCampaignAssetSetsRequest`, `CampaignAssetSetOperation`,
`MutateCampaignAssetSetResult`, and the enum page `AssetSetLinkStatusEnum.AssetSetLinkStatus`. The page of the
method describes both as "creates, updates or removes", but the operation of each has the members `create` and
`remove` only, and no field mask. The page `AssetSetAssetStatusEnum.AssetSetAssetStatus` could not be read, so
the status of an asset set asset is answered as Google writes it and not checked against a list.

Not verified:

- Which role in Google Ads an account needs for these calls. Google documents it per call nowhere.
- Which asset set types and campaign types accept which links, and the limits on the number of links per set or
  campaign. The pages read do not state them. No live call has been made.
- Whether removing a link that Google already marked `REMOVED` is answered as success or as an error.

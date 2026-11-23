---
description: >
  Describes the ad tools of the Google Ads provider that spend money: ad-group-ads.create, ad-group-ads.update, ad-group-ads.remove and ad-group-ads.remove-automatically-created-assets, with the tools list requirement, the required ad group, status and ad, the ad types offered and left out on purpose with the numbers the v26 reference states, assets by resource name, the checks on resource names, addresses and image bytes, immutability, field masks, validate_only as the ad policy check, how policy findings are reported, policy_validation_parameter, the atomic request, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: ad tools

These tools create, change, and remove the ads of a Google Ads account, and remove text customization that
Google created from an ad. An ad that is enabled serves in its ad group and costs per click or view within the
budget of its campaign, and its final URLs and tracking send the clicks of real people to an address. The tools
add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and
follow the rules of the [settings tools](settings.md) (tools list, confirmation per call, one request sent once,
unknown outcome, `validate_only`, `response_content_type`, the manager account rule) and of the
[ad group tools](ad-groups.md) (a list of up to 10 entries in one atomic request, resource names that stay with
the account); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.ad-group-ads.create` | create | creates up to 10 ads |
| `google.ads.ad-group-ads.update` | update | changes status, dates, and asset automation of up to 10 ads |
| `google.ads.ad-group-ads.remove` | delete | removes up to 10 ads |
| `google.ads.ad-group-ads.remove-automatically-created-assets` | delete | ends the use of automatically created assets by one ad |

No profile offers them: a connection has a tool only when its `tools` list names it. The bytes of an image ad are
inline only: `content_base64`, up to 4 MiB; a larger image or a local file goes through
[assets.create](assets.md) and `image_asset`. Read ads with
`reports.search` on `ad_group_ad`, `ad_group_ad_asset_view`, and `ad`. The content of an existing ad
(`AdService.MutateAds`) and the ad parameters are not part of these tools: they are the
[ad content tools](ad-content.md).

## What differs from the ad group tools

- **A list of ads, one atomic request.** `create`, `update`, and `remove` call `AdGroupAdService.MutateAdGroupAds`
  with up to 10 operations and without `partial_failure`: Google applies all of them or none. Ten is the bound
  of Qatlas; the reference of v26 names none for this call. The answer holds one entry per ad, in the order given.
- **Required members.** `create` requires `ad_group`, `status` (`ENABLED` or `PAUSED`), and `ad` with exactly one
  ad type member. Google documents the ad type as required and does not require the status: Qatlas
  asks for it so that no ad starts serving by a default of Google. `update` takes `ENABLED` or `PAUSED`; an ad is
  removed only with `ad-group-ads.remove`, and a removed ad cannot be enabled again.
- **Resource names stay with the account.** The `ad_group` of an ad and every asset inside an ad must belong to
  the `customer_id` of the call, with a positive ID, and the ads of `update`, `remove`, and the removal of
  automatically created assets must be `customers/ID/adGroupAds/AD_GROUP_ID~AD_ID` of it. A temporary (negative)
  ID is refused. This is checked before a secret is read. In the answer, the result of an operation must be an ad
  of the account (for `create`, in the ad group asked for; for `update` and `remove`, the ad asked for), and the ad
  group, the ad name, and the assets inside a returned ad must belong to the account; anything else is
  `invalid-provider-response` with the note about the unknown outcome.
- **The ad is immutable.** The reference marks `ad`, `ad_group`, and the content of an ad immutable in
  `AdGroupAd`. They are arguments of `create` only; the schema of `update` offers `status`, `start_date_time`,
  `end_date_time` (an empty text clears the member), and `ad_group_ad_asset_automation_settings` (the list
  replaces the current one). `update` sends a field mask with the path of each member given.
- **`remove` has no `response_content_type`.** A removed ad has no mutable resource to show.
- **The answer with `MUTABLE_RESOURCE`.** Google returns the mutable members only. Each entry then holds the ad
  with the members the tools offer; members Google fills itself, such as the approval status and the ad
  strength, are not returned in this mode and are read with `reports.search`.

## Ad types

`ad` takes exactly one of the ad type members below, nested as the `Ad` resource of v26 nests them, with the
names in snake case. Images, videos, media bundles, call-to-action assets, and app deep links are assets of the
account, named by resource name (`customers/ID/assets/N`); they are made with [assets.create](assets.md). The bytes of an
image ad are base64 text of at most 4 MiB, one call carries up to 10 MiB of them, and the bytes never appear in an
answer; `local_path` is no argument of an ad. A text of an ad is an object with `text` and, for a responsive search ad, `pinned_field`.

| Type | Counts the reference states |
| --- | --- |
| `responsive_search_ad` | 3 to 15 headlines, 2 to 4 descriptions, at least one final URL; `path2` only with `path1`; pins `HEADLINE_1` to `HEADLINE_3`, `DESCRIPTION_1`, `DESCRIPTION_2` |
| `expanded_dynamic_search_ad`, `text_ad` | text members only |
| `hotel_ad`, `travel_ad`, `shopping_product_ad` | no members: the ad takes its content from the feed of the campaign |
| `shopping_comparison_listing_ad` | `headline` required, 25 to 45 characters |
| `image_ad` | `image_asset`, or the bytes of an image as `content_base64`, exactly one of the two |
| `video_ad` | `video` and one format: `in_stream`, `bumper`, `out_stream`, `non_skippable`, `in_feed`, or `audio` |
| `video_responsive_ad` | one headline, long headline, description, call to action, video, companion banner each; business name, video, and logo images required |
| `responsive_display_ad` | at least one marketing image and one square marketing image (combined at most 15), logo images combined at most 5, 1 to 5 headlines and descriptions, long headline required, at most 5 videos |
| `app_ad`, `app_engagement_ad`, `app_pre_registration_ad` | headlines, descriptions, images, videos, media bundles, deep link |
| `local_ad` | 1 to 5 headlines, descriptions, and calls to action, 1 to 20 marketing images and videos, 1 to 5 logo images; `path2` only with `path1` |
| `display_upload_ad` | `display_upload_product_type` and `media_bundle` |
| `smart_campaign_ad` | 3 to 15 headlines, 2 to 4 descriptions |
| `demand_gen_multi_asset_ad` | business name required, at most 20 images per kind, 1 to 5 logo images, at most 5 headlines and descriptions |
| `demand_gen_carousel_ad` | business name, logo image, headline, description required, 2 to 10 carousel cards |
| `demand_gen_video_responsive_ad` | videos, logo images, business name required |
| `demand_gen_product_ad` | headline, description, logo image, business name required |

Qatlas checks the counts the reference states (the lists above) and the form of an argument. It does not check
the length of a text: Google counts display width, and the guide for responsive search ads names 30 characters
for a headline, 90 for a description, and 15 for a path; the check is Google's, so use `validate_only`. The
combined maximums of the image lists of a responsive display ad and a Demand Gen ad are Google's to check too.

**Left out on purpose.**

- Ad types Google no longer creates or marks as old: `expanded_text_ad` (the reference calls expanded text ads
  deprecated and names `responsive_search_ad` instead), `legacy_responsive_display_ad` and
  `legacy_app_install_ad` (legacy types, the second for a few select customers only), and `shopping_smart_ad`
  (Google creates no new Smart Shopping ads; the [ad group tools](ad-groups.md) leave out the matching type).
  Call-only ads: Google creates none since 2026-01-01, will stop serving them in February 2027, and `Ad` of v26 has
  no member for them, so no tool can create one. Use a responsive search ad with a call asset.
- Members Google fills itself (output only), which `reports.search` reads: the ad ID and type,
  `added_by_google_ads`, the policy summary, the ad strength, the action items, `labels`, the primary status and
  its reasons, and the system managed source.
- The image an image ad copies from another ad (`ad_id_to_copy_image_from`): an ad ID cannot be bound to the
  account. The image members Google fills itself (sizes, URLs, mime type) are left out too.
- Labels on ads: they are set with their own tools or none.

## Final URLs and tracking

Final URLs and tracking can send click data to a third party. They follow the checks of
[customers.update](settings.md#customersupdate): `final_urls`, `final_mobile_urls`, `tracking_url_template`, and
the same members of each entry of `url_collections` are `https` addresses without user info and with a plain
host name (the template may hold ValueTrack parameters after the host), and `final_url_suffix` is parameters, not
an address. `final_app_urls` takes an `os_type` (`IOS` or `ANDROID`) and a `url` that is a deep link such as
`exampleapp://productid_1234`, and `display_url` is the text shown in the ad; both are plain text without control
characters and have no check of the address. Custom parameters carry a key of letters, digits, and underscores
and a value. `device_preference` takes `MOBILE`, the one preference the reference allows. A responsive search ad
needs at least one final URL.

## The ad policy check

Google checks the policy of an ad while it validates a change. A call with `validate_only` therefore shows
whether the ads would pass, without creating them. When they would not, Google rejects the request with a
`GoogleAdsFailure` whose error details hold the policy findings: `policy_finding_details` (the topic entries, each
with a topic and a type) and `policy_violation_details` (a violated policy with its key and whether it is
exemptible). The error does not repeat what Google wrote. It names the findings from the values Qatlas can
check against the documented form: for each finding the topic and its type (`PROHIBITED`, `LIMITED`,
`FULLY_LIMITED`, `DESCRIPTIVE`, `BROADENING`, or `AREA_OF_INTEREST_ONLY`), the name of a violated policy and
whether it is exemptible, and the path of the field in the request, such as `operations[0].create.ad`. A topic or
name that is not of the documented form is left out. The original answer of Google, with the evidence texts and
the violating text, goes along as the original of the error, marked as provider content that cannot be trusted.

`policy_validation_parameter` is an argument of each entry of `create` and `update` and maps to the
`PolicyValidationParameter` of the operation. It holds exactly one of:

- `ignorable_policy_topics`: topics, as the error names them, for which Google saves the ad although it has a
  finding. Google documents this for ad policy exemptions.
- `exempt_policy_violation_keys`: violation keys (`policy_name` and optional `violating_text`) of a policy
  violation. Google documents this for keyword policy exemptions and refuses a violation that is not exemptible.

An ad that is saved with an exemption cannot serve. It may start serving later if the policy changes or the ad
is reviewed again. Qatlas never sets the parameter by itself: it is given per ad, with at most 20 topics or keys,
and the confirmation names it.

## Removing automatically created assets

`ad-group-ads.remove-automatically-created-assets` calls `AdGroupAdService.RemoveAutomaticallyCreatedAssets`
for one `ad_group_ad` and one to 10 entries of `assets_with_field_type`, each with `asset` (`customers/ID/assets/N`
of the account) and `asset_field_type`, the name of an asset field type, such as `HEADLINE` or `DESCRIPTION`. The
asset stays in the account; only the use of it by the ad for that field type ends. Google calls this text
customization, formerly automatically created assets. Differences from the other tools of this page:

- The request has no `validate_only`, no `partial_failure`, and no `response_content_type`, so the tool offers
  none. The ad is part of the path of the request. The bound of 10 entries is Qatlas's own.
- Google answers with an empty body. The tool answers with the ad and the removals as asked.
- Campaign-level assets are removed with `campaign-automatically-created-assets.remove`, see the
  [asset tools](assets.md).

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds, and the path of the removal with
  the ad as `AD_GROUP_ID~AD_ID`. No live call has been made.
- Which ad types a given campaign type and ad group type accept, and whether Google still accepts the creation of
  every type the reference lists without a note, such as `text_ad`, `local_ad`, `app_engagement_ad`, and
  `smart_campaign_ad` (new Smart campaigns cannot be created since 2026-09-23). Google validates this: use
  `validate_only`.
- The length limits of texts, the combined image maximums, and the pin rules (each position must stay fillable).
- Whether `MutateAdGroupAds` has an upper limit of operations; Qatlas keeps its own.
- Whether Google puts `policy_finding_details` on every policy rejection of an ad. A rejection without them is
  reported by its error code only.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- The change of the content of an ad and the ad parameters (see the [ad content tools](ad-content.md)), and
  labels on ads: they have their own tools or none.

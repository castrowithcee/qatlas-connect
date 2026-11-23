---
description: >
  Describes the ad group criterion tools of the Google Ads provider that spend money: ad-group-criteria.create, ad-group-criteria.update and ad-group-criteria.remove for up to fifty criteria per call, with the tools list requirement, the required ad group and status, the 30 criterion types offered and the ones left out with the reason, listing groups under an existing subdivision, policy exemption keys, the negative criterion, the limits of keyword text and placements, the checks on resource names, constants of Google and addresses, field masks and clearing, validate_only, the atomic request, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: ad group criterion tools

These tools create, change, and remove the criteria of ad groups of a Google Ads account: keywords, audiences,
placements, locations, demographics, and the other types the `AdGroupCriterion` resource of v26 holds. A
criterion that targets and is enabled serves in its ad group, and its bids and bid modifier decide how much a
click or a view costs. The tools add to the [Google Ads provider](README.md), whose configuration, targets,
quotas, and errors apply here too, and follow the rules of the [settings tools](settings.md) (tools list,
confirmation per call, one request sent once, unknown outcome, `validate_only`, `response_content_type`, the
manager account rule) and of the [ad group tools](ad-groups.md), whose field tree they share; this page names
what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.ad-group-criteria.create` | create | creates up to 50 criteria |
| `google.ads.ad-group-criteria.update` | update | changes the given members of up to 50 criteria |
| `google.ads.ad-group-criteria.remove` | delete | removes up to 50 criteria |

No profile offers them: a connection has a tool only when its `tools` list names it.

## What differs from the ad group tools

- **A list of criteria, one atomic request.** The tools call `AdGroupCriterionService.MutateAdGroupCriteria`
  with up to 50 operations and without `partial_failure`: Google applies all of them or none. Fifty is the
  bound of Qatlas; the reference of v26 names none for this call. The answer holds one entry per criterion, in
  the order given.
- **Required members of `create`.** Each criterion needs `ad_group`, exactly one criterion type, and, unless it
  is negative, `status` (`ENABLED` or `PAUSED`). Google documents neither `ad_group` nor `status` as required
  and its default status is `ENABLED`: Qatlas asks for the status so that no criterion starts serving by a
  default. `update` takes `ENABLED` or `PAUSED`; a criterion is removed only with `ad-group-criteria.remove`.
- **A negative criterion.** `negative: true` excludes the criterion. It takes no bid, bid modifier, or URL
  member and either no status or `ENABLED`, because it spends nothing and Google does not update a negative
  criterion (an update of one is refused by Google as `CANT_UPDATE_NEGATIVE`). To change it, remove it and
  create it again. Which types can be excluded at the ad group level is Google's to validate: the reference
  lists audience, brand list, combined audience, custom affinity, custom audience, custom intent, language, app
  payment model, listing group, and entity bid as targeting only.
- **Resource names stay with the account.** `ad_group` and every account resource inside a criterion
  (`user_list`, `audience`, `custom_audience`, `custom_affinity`, `custom_intent`, `combined_audience`,
  `user_interest_category`, and the `shared_set` of a brand list, a retail filter bundle, or a rule list of
  vertical ads) must belong to the `customer_id` of the call, with a positive ID. The criteria of `update` and
  `remove` must be `customers/ID/adGroupCriteria/ADGROUPID~CRITERIONID` of it. A temporary (negative) ID is
  refused. This is checked before a secret is read. The resource names in the answer must belong to the account
  and, for `update` and `remove`, be the criteria asked for, for `create` belong to the ad group asked for;
  anything else is `invalid-provider-response` with the note about the unknown outcome.
- **Constants of Google.** A location, a language, a topic, and a mobile app category name a constant that
  belongs to Google and no account. Qatlas checks their form only (`geoTargetConstants/N`,
  `languageConstants/N`, `topicConstants/N`, `mobileAppCategoryConstants/N`); whether the constant exists is
  Google's to validate.
- **Fixed after creation.** `ad_group`, `negative`, and the criterion type are arguments of `create` only, as the
  reference marks them immutable; the schema of `update` does not offer them.
- **The mask names what was given.** `update` sends a field mask with the path of each member given. A list
  member (final URLs, custom parameters) replaces the whole list, and an empty list clears it. An empty text for
  `tracking_url_template` or `final_url_suffix` clears the member.
- **`remove` has no `response_content_type`.** A removed criterion has no mutable resource to show.
- **The answer with `MUTABLE_RESOURCE`.** Each entry then holds the criterion with the members the tools offer
  and its resource name; members the tools do not offer are dropped, and a member without a value is left out.

## ad-group-criteria.create and ad-group-criteria.update

The members are those of the `AdGroupCriterion` resource of v26, nested as the resource nests them, with the
names in snake case: `ad_group`, `negative`, `status`, `bid_modifier`, the bids (`cpc_bid_micros`,
`cpm_bid_micros`, `cpv_bid_micros`, `percent_cpc_bid_micros`), `final_urls`, `final_mobile_urls`,
`final_url_suffix`, `tracking_url_template`, `url_custom_parameters`, and the criterion type. The descriptor
lists every member with its type, enum values, and bounds. Enum values are those of the field reference of v26
without the unspecified and unknown values. Amounts are in micros of the account currency.

**Criterion types offered (30).** `age_range`, `app_payment_model`, `audience`, `brand_list`,
`combined_audience`, `custom_affinity`, `custom_audience`, `custom_intent`, `entity_bid`,
`extended_demographic`, `gender`, `income_range`, `keyword`, `language`, `life_event`, `listing_group`, `location`,
`mobile_app_category`, `mobile_application`, `parental_status`, `placement`, `retail_filter_bundle`, `topic`,
`user_interest`, `user_list`, `vertical_ads_item_group_rule_list`, `video_lineup`, `webpage`,
`youtube_channel`, and `youtube_video`. Exactly one is given on creation. Which type an ad group or campaign
type accepts is Google's to validate, so use `validate_only`: the reference notes, among others, that `audience`
needs `audience_setting.use_audience_grouped` on the ad group, that a Search campaign refuses a `language`
criterion, that `retail_filter_bundle` is for allowlisted accounts, and that the demographic types refuse
excluding every target of one dimension.

Qatlas checks the form of an argument and the rules Google documents:

- **Keyword text:** 1 to 80 characters, at most 255 bytes and 10 words, no control character. Google allows an
  asterisk only in a negative keyword and decides that itself. `match_type` is required as well, so that no
  keyword gets a match type by default.
- **Placement URL:** 1 to 250 characters. Google also limits the depth to two levels and refuses
  `adsenseformobileapps.com`, which it validates itself.
- **Mobile application:** `app_id` is required, as `1-` and the iOS ID or `2-` and the Android package name.
- **Webpage:** `criterion_name` and at least one condition are required, and each condition needs `operand`,
  `operator`, and `argument`. Google reads an empty list of conditions as every page of the website; the tool
  asks for a condition so that a criterion never covers every page by accident.
- **`bid_modifier`:** from 0.1 to 10.
- **`percent_cpc_bid_micros`:** from 0 up to, not including, 1 (stored as millionths), so 0 to 999999.
- **Listing group:** `type` is required. `case_value` and each entry of `path.dimensions` hold exactly one
  dimension of the hotel, product, and activity dimensions of v26 (`unknown_listing_dimension` is the rest case).
  The dimensions that Google marks deprecated and supports only in Display campaigns are not offered:
  `product_grouping`, `product_labels`, `product_legacy_condition`, and `product_type_full`. Hotel and activity
  places are `geoTargetConstants/N`, checked for their form. `parent_ad_group_criterion` must be a criterion of the
  same entry's ad group and of the account (`customers/ID/adGroupCriteria/ADGROUPID~CRITERIONID`, positive IDs, no
  temporary ID), and the same holds for a parent in an answer. This creates units under an existing subdivision.
  A new tree, with a subdivision and its children in one request, needs temporary IDs and cannot be created with
  these tools; whether Google accepts a unit without its sibling cases is its own validation.
- **YouTube:** a video ID has 11 characters; a channel ID is up to 64 characters of letters, digits, `-`, and `_`.

The rules that depend on the ad group, its campaign, and the criterion type are Google's to validate, so use
`validate_only`: that a bid must fit the bidding of the ad group, that a keyword-level CPM bid is refused for
manual CPM, that final URLs, final mobile URLs, and the other URL members are accepted only for some types and
only together with final URLs, and that a criterion cannot be targeted and excluded at once.

**Policy exemption keys.** An entry of `create` or `update` may carry `exempt_policy_violation_keys`, up to 20
keys of `policy_name` and optionally `violating_text`, sent with the operation. Google then saves the criterion
despite those violations, but it does not serve until the policy allows it. Without `violating_text` the whole
policy is exempted. The confirmation names the exemption. Google decides which violations can be exempted.

**Addresses.** The tracking template, the final URLs, and the final URL suffix can pass click data to a third
party. They follow the checks of [customers.update](settings.md#customersupdate): the template and each final URL
(and final mobile URL) are an `https` address without user info and with a plain host name, a suffix is parameters
and no address. Custom parameters carry only a key and a value, and their values are text.

**Left out on purpose.**

- Members Google fills itself (output only), which `reports.search` reads: `criterion_id`, `display_name`,
  `type`, the approval and serving states and the disapproval reasons, the quality information, the position
  estimates, the effective bids and their sources, `labels`, `primary_status` and its reasons, and
  `require_eu_political_advertising_declaration`. The resource has no deprecated member.
- A new listing tree with temporary IDs, see the listing group above. The bids of an existing listing group can
  be changed with `update`.
- `topic.path`, which Google derives from the topic constant, and the read-only members of `webpage`.
- The criterion types of the campaign and customer level (device, ad schedule, proximity, IP block, and the
  others), which the resource does not hold. Device adjustments of an ad group are bid modifiers, which have
  their own tools or none.
- The default status: a criterion that targets must say `ENABLED` or `PAUSED`.

## ad-group-criteria.remove

Calls `MutateAdGroupCriteria` with remove operations for criteria named by `resource_names`. A criterion appears
once. Removing stops serving at once, and a removed criterion is not enabled again. The history of a removed
criterion stays readable with `reports.search`.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds. No live call has been made.
- Which criterion types each ad group and campaign type accepts, and which can be excluded: the reference
  states some of it and the rest is Google's validation.
- Whether `MutateAdGroupCriteria` has an upper limit of operations; Qatlas keeps its own.
- Whether Google accepts a listing group unit, a subdivision, or the rest case in the order and combination given:
  the tree rules are Google's validation.
- Whether Google accepts `PAUSED` for every criterion type: the reference documents the status values and no
  exception.
- Whether the custom affinity and custom intent types are still creatable: the reference marks neither as
  deprecated, and Google validates the call.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- Labels on criteria, customizers, and bid modifiers of ad groups: they have their own tools or none.

---
description: >
  Describes the Google Ads recommendation tools: recommendations.generate in the profile read, recommendations.apply that only the tools list offers, and recommendations.dismiss that the update effect offers, the arguments of generate and the necessary ones per recommendation type, every apply variant with its parameters and which variants and members are left out and why, the checks of addresses, names and numbers before any request, the atomic request without partial_failure and the missing validate_only, the unknown outcome, the bounded answers, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: recommendations

These tools ask Google Ads to generate recommendations for a campaign, apply recommendations of an account, and
dismiss them. They add to the read tools of the [Google Ads provider](README.md), whose configuration, targets,
quotas, and errors apply here too. What every changing tool does (confirmation, one atomic request, no repetition,
unknown outcome) is described for the [settings tools](settings.md); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.recommendations.generate` | read | generates recommendations for a campaign described in the arguments |
| `google.ads.recommendations.apply` | update | applies 1 to 10 recommendations, optionally with parameters of one variant each |
| `google.ads.recommendations.dismiss` | update | dismisses 1 to 10 recommendations |

`generate` belongs to the profile `read`. `apply` belongs to no profile and is offered only when the `tools` list
of a connection names it, because applying a recommendation can change what an account spends. `dismiss` belongs
to no profile either, but it needs no `tools` entry: a connection with the effect `update` offers it. Both changing
tools need a confirmation per call.

The recommendations that an account holds are read with `reports.search` on the `recommendation` resource; `apply`
and `dismiss` take their resource names, `customers/CUSTOMER_ID/recommendations/ID`. The customer of the name must
be the one of `customer_id`; a name of another account is refused as `invalid-request` before a secret is read.

## generate

`RecommendationService.GenerateRecommendations`. Nothing is created or changed in the account: the arguments
describe a campaign and Google answers what it would recommend. Every documented argument is a structured
argument: `recommendation_types`, `advertising_channel_type`, `campaign_sitelink_count`,
`conversion_tracking_status`, `bidding_info` (with at most one of `target_cpa_micros`, `target_roas` and
`target_impression_share_info`), `ad_group_info`, `seed_info`, `budget_info`, `campaign_image_asset_count`,
`campaign_call_asset_count`, `country_codes`, `language_codes`, `positive_location_ids`, `negative_location_ids`,
`asset_group_info`, `target_partner_search_network`, `target_content_network`, `merchant_center_account_id` and
`is_new_customer`. The reference spells the two location lists `positive_locations_ids` and
`negative_locations_ids`; the tool names them `positive_location_ids` and `negative_location_ids`.

Google supports the types `CAMPAIGN_BUDGET`, `KEYWORD`, `MAXIMIZE_CLICKS_OPT_IN`, `MAXIMIZE_CONVERSIONS_OPT_IN`,
`MAXIMIZE_CONVERSION_VALUE_OPT_IN`, `SET_TARGET_CPA`, `SET_TARGET_ROAS`, `SITELINK_ASSET`, `TARGET_CPA_OPT_IN` and
`TARGET_ROAS_OPT_IN`, and the channels `SEARCH` and `PERFORMANCE_MAX`. Other values are refused by the schema.

Qatlas refuses a request before a secret is read when it leaves out what the reference calls necessary for a
requested type:

- `SITELINK_ASSET`: `campaign_sitelink_count`.
- The opt-in types, `SET_TARGET_CPA` and `SET_TARGET_ROAS`: `conversion_tracking_status` and
  `bidding_info.bidding_strategy_type`.
- `CAMPAIGN_BUDGET`: `bidding_info.bidding_strategy_type`. On `SEARCH` also `ad_group_info` with keywords,
  `country_codes`, `language_codes`, one of the two location lists, and `target_impression_share_info` when the
  strategy is `TARGET_IMPRESSION_SHARE`. On `PERFORMANCE_MAX` also `asset_group_info`.
- `KEYWORD`: `seed_info`, and `ad_group_info.ad_group_type` when `ad_group_info` is given.
- `merchant_center_account_id` only with `PERFORMANCE_MAX`.

Google decides whether the rest is enough: a type for which it has too little data, or whose campaign is already in
the recommended state, is simply missing from the answer.

Addresses in the arguments are checked, although Qatlas never fetches them. `asset_group_info.final_url` must be
`https` without user info. `seed_info.url_seed` is a page that Google reads keyword ideas from. The reference writes
it without a scheme, such as `www.example.com/cars`; the tool accepts that form or an `https` address, and refuses
user info, a port, a braces template and any other scheme.

The answer holds the recommendations in the member names of the reference in snake_case, with every member Google
returns: `type`, `impact` (base and potential metrics), `campaign_budget`, `campaign`, `ad_group`, `campaigns`,
`dismissed` and the one member of the type, such as `campaign_budget_recommendation` or
`target_cpa_opt_in_recommendation`, with everything below it. Everything in it is untrusted third-party text:

- Control characters become spaces, and a text is cut at 1,024 bytes. A list is cut at 500 entries; more than 500
  recommendations set `truncated`. An answer deeper than 16 levels or with more than 50,000 values is
  `invalid-provider-response`.
- A resource name of another account than the one asked for makes the answer `invalid-provider-response`.
- Members the reference marks as deprecated (`app_conversion_goals`, `current_auto_goals` and `current_custom_goal`
  of the app goal recommendation) and the delivery routes of lead forms (`webhook`, `delivery_methods`,
  `advertiser_webhook_url`, `google_secret`) are left out. The `apply_link` of the Performance Max migration is
  returned as text and never fetched.
- A generated recommendation may carry no resource name. Only a recommendation the account holds can be applied or
  dismissed.

A request that Google refuses with HTTP 400 and a documented error code, such as a `RecommendationError` derived
from the arguments, ends as `provider-error` with the category and the code in the message, as the refusal of
the input; nothing was changed, since the tool only reads. The original of the error carries the field path. A 400
without a code stays `invalid-provider-response`.

## apply

`RecommendationService.ApplyRecommendation` with 1 to 10 operations (Qatlas' own bound; Google allows 100). Each
operation names a recommendation by `resource_name` and may carry `parameters` with exactly one member, one variant
of the `apply_parameters` of the reference. Without `parameters` Google applies the recommendation as it is. The
variant has to fit the type of the recommendation; Google refuses it otherwise.

The request is atomic. Qatlas never sets `partial_failure`, so Google applies all operations or none, and the
reference asks for recommendations of one type per request. `ApplyRecommendationRequest` has no `validate_only` in
v26, so the tool has none: the call applies. It is sent once and never repeated. A timeout, a cut connection or a
server error ends with the note that the outcome is unknown. An answer with another number of results, another
name or order, a result for another account, or a `partial_failure_error` is reported the same way, since the
request was sent. A raise of a target CPA that is too low is applied by Google asynchronously and can take minutes.

An application can change budgets, bids, bidding strategies, keywords, ads and assets. The confirmation names the
account, every recommendation and the parameters.

### Variants

Numbers named `*_micros` are whole amounts in micros of the currency of the account, at least 1. A target ROAS is
0.01 to 1000, inclusive. Unknown members are refused.

| Variant | Parameters |
| --- | --- |
| `campaign_budget` | `new_budget_amount_micros` (required) |
| `keyword` | `ad_group` (required, an ad group of the account), `match_type` (required, `EXACT`, `PHRASE`, `BROAD`), `cpc_bid_micros` |
| `target_cpa_opt_in` | `target_cpa_micros` (required), `new_campaign_budget_amount_micros` |
| `target_roas_opt_in` | `target_roas`, `new_campaign_budget_amount_micros`; at least one |
| `move_unused_budget` | `budget_micros_to_move` (required) |
| `use_broad_match_keyword` | `new_budget_amount_micros` |
| `raise_target_cpa_bid_too_low` | `target_multiplier` (required, above 1) |
| `forecasting_set_target_roas`, `set_target_roas` | `target_roas`, `campaign_budget_amount_micros` |
| `forecasting_set_target_cpa`, `set_target_cpa` | `target_cpa_micros`, `campaign_budget_amount_micros` |
| `raise_target_cpa` | `target_cpa_multiplier` (required) |
| `lower_target_roas` | `target_roas_multiplier` (required) |
| `raise_target_cpa_performance_bid_too_low` | `target_cpa_multiplier` (required, above 1) |
| `lower_target_roas_performance_bid_too_low` | `target_roas_multiplier` (required, above 0 and below 1) |
| `raise_budget_performance_too_low` | `new_budget_amount_micros` (required) |
| `optimize_for_new_customers` | `account_default_incremental_conversion_value`, `campaign_override_incremental_conversion_value`; plain currency units, not micros |
| `callout_asset` | `scope` (required, `CUSTOMER` or `CAMPAIGN`), `new_assets` (each `callout_text`), `existing_assets`; at least one of the two lists |
| `call_asset` | `scope`, `new_assets` (each `country_code`, `phone_number`), `existing_assets` |
| `sitelink_asset` | `scope`, `new_assets` (each `link_text`, `description1`, `description2`, `final_urls`), `existing_assets` |
| `responsive_search_ad` | `ad` with `final_urls`, `headlines`, `descriptions`, `path1`, `path2` |
| `responsive_search_ad_asset`, `responsive_search_ad_improve_ad_strength` | `updated_ad`: the same members and `resource_name` of the ad |

The asset variants flatten the member `ad_asset_apply_parameters` of the reference. An existing asset is named by
`customers/CUSTOMER_ID/assets/ID`, an ad by `customers/CUSTOMER_ID/ads/ID`, an ad group by
`customers/CUSTOMER_ID/adGroups/ID`, all of the account of the call. A headline or description is
`{"text":..., "pinned_field":...}` with `HEADLINE_1` to `HEADLINE_3` or `DESCRIPTION_1` and `DESCRIPTION_2`; `path2`
needs `path1`. The counts and lengths are Qatlas' own bounds (at most 20 assets, 20 headlines, 10 descriptions,
255 characters per text); Google applies its own rules on top, which are not repeated here.

### Left out

Every address that an ad or asset can carry is `https` without user info and is never fetched. What would send
clicks or signals to an address that Qatlas does not check, or deliver data to a third party, is not offered:

- **`tracking_url_template`, `final_url_suffix`, `url_custom_parameters`, `final_mobile_urls`, `final_app_urls`,
  `url_collections` and `display_url`** of ads and assets. Tracking templates and URL suffixes are allowed for
  the tools of the settings; here only `final_urls` is offered. An ad that replaces another through
  `updated_ad` is sent without these members; whether Google keeps the old ones or clears them is not stated.
- **`lead_form_asset`.** Lead form assets can deliver leads to a webhook, and an existing asset cannot be
  checked for that. The whole variant is left out.
- **`call_conversion_reporting_state`, `call_conversion_action` and the schedules of call assets**, which route
  calls through forwarding numbers or name conversion actions. Start and end dates and ad schedules of callout and
  sitelink assets are left out as well.
- **`text_ad`, `callout_extension`, `call_extension` and `sitelink_extension`.** The reference of v26 still lists
  them, but names no recommendation type for the last three, and expanded text ads can no longer be created in
  Google Ads. The narrower reading is to offer none of the four.
- **The other members of `Ad` and `Asset`** such as image, video and the other ad types: no variant of the
  reference needs them for the recommendation types that exist.

## dismiss

`RecommendationService.DismissRecommendation` with 1 to 10 names in `resource_names`. Google stops showing them;
the reference has no call that restores a dismissed recommendation and Google may generate a new one. Nothing else
in the account changes. The request is atomic without `partial_failure` and has no `validate_only`; it is sent
once and never repeated, and the answer is checked like the one of `apply`.

## Manager accounts

A recommendation belongs to an account that runs campaigns. For the two changing tools, a manager account on a
connection with targets needs a manager target that names it, as for the other changing tools; Qatlas reads
`customer.manager` once to tell. `generate` follows the ordinary rule for reads. A connection without targets
reaches what its credential reaches and makes no such read.

## Boundary

Checked against the v26 proto files of `RecommendationService`, `Recommendation` and the common types: the
arguments, the variants and their members, and the enums named above. Differences from the research that preceded
this package: `ApplyRecommendationRequest` and `DismissRecommendationRequest` have no `validate_only`, and Google
documents 100 operations per request, which Qatlas lowers to 10.

Not verified:

- No live call has been made. The form of the recommendation IDs (`[A-Za-z0-9_.:=~-]`, up to 128 characters) and
  whether a generated recommendation carries a resource name follow from the reference only.
- Which Google role an account needs for `apply` and `dismiss`, and which variant fits which recommendation type
  beyond what the reference says.
- Whether Google counts a failed `apply` or `dismiss` against the daily operation quota.
- The meaning of the error codes of `RecommendationError` for the tools of this page; a call that Google refuses
  with HTTP 400 and a documented code ends as `provider-error` with the code in the message.

---
description: >
  Describes the campaign criteria tools of the Google Ads provider: campaign-criteria.create, campaign-criteria.update, and campaign-criteria.remove for up to 50 criteria per call, with the tools list requirement, the criterion types offered and the ones left out, the fixed form of Google constants, the binding of references and answers to the account, the bid modifier range, the limit that Google refuses language criteria on Search campaigns, validate_only, the unknown outcome, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: campaign criteria tools

These tools create, change, and remove the criteria of campaigns in a Google Ads account: who the ads of a campaign
reach (locations, languages, devices, audiences, schedules, keywords) and what is excluded. They add to the read
tools of the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.
A criterion changes who sees the ads, and a bid modifier changes what a click costs, so all three tools are
offered only through the `tools` list.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-criteria.create` | create | creates criteria of campaigns |
| `google.ads.campaign-criteria.update` | update | changes the bid modifier or the status of criteria |
| `google.ads.campaign-criteria.remove` | delete | removes criteria |

Reading criteria is not a tool of this page: run `reports.search` on `campaign_criterion`.

## How the tools are offered

- **Only through the `tools` list.** A connection with the `create`, `update`, or `delete` effect and no `tools`
  list does not have them. They belong to no profile.
- **Confirmed per call.** The confirmation text names the account, the criterion types, the campaigns, exclusions,
  bid modifiers, and statuses.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignCriterionService.MutateCampaignCriteria` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A call holds 1 to 50
  entries. The 50 are Qatlas's own bound.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. The answer then holds
  `validate_only: true` and an empty `results`. Validation needs the same confirmation and costs the same.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the criterion after the change to each
  result, with the members the tools offer, `criterion_id`, `display_name`, and `type`.
- **No repetition, unknown outcome.** The request is sent once. When a timeout, a cut connection, a server error,
  or an unreadable answer leaves open whether Google took the change, the error says that the outcome is unknown:
  read the criteria with `reports.search` before changing them again. A `GoogleAdsFailure` refuses the whole
  request and says so.
- **References stay with the account.** The campaign and every audience, user list, interest, asset set, or shared
  set a criterion names must be `customers/ID/...` of the `customer_id` of the call, with a positive ID. A temporary
  negative ID is refused. The names of criteria are `customers/ID/campaignCriteria/CAMPAIGN~CRITERION`.
- **Constants of Google.** Locations, languages, carriers, mobile devices, operating system versions, topics, and
  mobile app categories are constants that belong to no account. They are accepted and returned only in their
  fixed form (`geoTargetConstants/N`, `languageConstants/N`, `carrierConstants/N`, `mobileDeviceConstants/N`,
  `operatingSystemVersionConstants/N`, `topicConstants/N`, `mobileAppCategoryConstants/N`,
  `keywordThemeConstants/ID~SUB_ID`, and the `geoTargetConstants/N` of the listing dimensions, with positive
  numbers).
- **Answers are bound to the account.** Every result must be a criterion of the account, in the order asked for.
  For `update` and `remove` it is the criterion asked for, for `create` a criterion of the campaign asked for. A
  returned criterion with a reference to another account, a constant outside its fixed form, or another name is
  `invalid-provider-response` with the note about the unknown outcome. A `partial_failure_error` is treated the
  same way.
- **Quota.** Google counts every element of a Mutate request as one operation of the daily quota, so a call with
  50 entries counts 50, and a request that fails with a `GoogleAdsFailure` counts too. A call adds one operation for
  the membership read below a manager target and one for the check that the account is no manager.
- **Manager accounts.** A manager account has no campaigns. On a connection with targets, a customer target that
  names it is refused as `invalid-request`.

## Create

Each entry names the `campaign` and exactly one criterion type, and may give `negative`, `bid_modifier`, and
`status`.

- **`negative`.** `true` excludes what the criterion names. An excluded criterion takes no `bid_modifier` and no
  `status`, and Google does not let it be changed afterwards: remove it and create it again.
- **`bid_modifier`.** A factor from 0.1 to 10.0. `0` is accepted for a `device` criterion only, to opt out of the
  device type.
- **`status`.** `ENABLED` or `PAUSED`. A criterion is removed with `remove`.

| Type | Members | Required |
| --- | --- | --- |
| `keyword` | `text` (up to 80 characters and 10 words), `match_type` | both |
| `mobile_app_category` | `mobile_app_category_constant` | yes |
| `mobile_application` | `app_id` (`1-` iOS or `2-` Android, then the app ID), `name` | `app_id` |
| `location` | `geo_target_constant` | yes |
| `device` | `type` | yes |
| `proximity` | `radius`, `radius_units`, `geo_point` (`longitude_in_micro_degrees`, `latitude_in_micro_degrees`), `address` | `radius`, and `geo_point` or `address` |
| `language` | `language_constant` | yes |
| `ip_block` | `ip_address`, one IPv4 or IPv6 address or CIDR block (up to 500 per campaign) | yes |
| `content_label` | `type` | yes |
| `carrier` | `carrier_constant` | yes |
| `user_interest` | `user_interest_category` of the account | yes |
| `operating_system_version` | `operating_system_version_constant` | yes |
| `mobile_device` | `mobile_device_constant` | yes |
| `location_group` | `radius`, `radius_units` (`METERS`, `MILES`, `MILLI_MILES`), `geo_target_constants` (one), `location_group_asset_sets`, `enable_customer_level_location_asset_set` | `radius`, `radius_units` |
| `custom_affinity`, `custom_audience`, `combined_audience`, `user_list` | the reference of the account | yes |
| `age_range`, `gender`, `income_range`, `parental_status` | `type` | yes |
| `youtube_video` | `video_id` | yes |
| `youtube_channel` | `channel_id` | yes |
| `topic` | `topic_constant`, `path` (up to 10 texts, general to specific) | one of them |
| `placement` | `url`, a domain or address with the scheme http or https or none, no user info, no spaces, up to 1024 bytes | yes |
| `webpage` | `criterion_name`, `conditions` (`operand`: `URL`, `CATEGORY`, `PAGE_TITLE`, `PAGE_CONTENT`, `CUSTOM_LABEL`; `operator`: `EQUALS`, `CONTAINS`; `argument`); an empty list targets all pages | `criterion_name`, `conditions` |
| `keyword_theme` | `keyword_theme_constant` (`keywordThemeConstants/ID~SUB_ID`) or `free_form_keyword_theme` | exactly one |
| `local_service_id` | `service_id` | yes |
| `video_lineup` | `video_lineup_id`, which Google hands out on request | yes |
| `listing_scope` | `dimensions`, each entry exactly one of `hotel_id`, `hotel_class` (1 to 5), `hotel_country_region`, `hotel_state`, `hotel_city`, `product_category` (`category_id`, `level`), `product_brand`, `product_channel`, `product_channel_exclusivity`, `product_condition`, `product_custom_attribute` (`value`, `index`), `product_item_id`, `product_type` (`value`, `level`), `activity_id`, `activity_rating` (1 to 5), `activity_country`, `activity_state`, `activity_city`, `unknown_listing_dimension` | `dimensions` |
| `ad_schedule` | `day_of_week`, `start_hour` (0 to 23), `start_minute`, `end_hour` (0 to 24), `end_minute` | all five; at most six per day |
| `life_event`, `extended_demographic` | the ID of the taxonomy of Google | yes |
| `brand_list` | `shared_set` of the account | yes |

`location_group_asset_sets` is not accepted together with `geo_target_constants` or with
`enable_customer_level_location_asset_set`, as Google states. Which combinations of types, campaign types, and
exclusions Google accepts is Google's to validate; use `validate_only` to see it.

## Update

An entry names the criterion by `resource_name` and gives `bid_modifier`, `status`, or both. The mask Qatlas sends
names exactly those members. Google accepts no other change: the type, its members, the campaign, and `negative`
are fixed after creation, so they are not arguments of `update`.

## Language criteria and Search campaigns

Google no longer allows language criteria on Search campaigns, on AI Max for Search, and on the Search placements
of Performance Max (deprecation of 2026-09-30). Creating or changing one there ends with
`ContextError.OPERATION_NOT_PERMITTED_FOR_CONTEXT`. Performance Max requests do not get that error, because the
setting still applies to its other channels.

Qatlas does not read the channel of a campaign to decide this: it sends the request once, and Google's refusal
ends the call as `provider-error` (`contextError OPERATION_NOT_PERMITTED_FOR_CONTEXT`, nothing was changed). It is
a refusal of the change, not an unknown outcome. `validate_only` shows the refusal without a change. Existing
language criteria stay readable with `reports.search` and can be removed with `remove`.

## Other limits that Google decides

Whether a type applies to the channel of a campaign is Google's to decide in the same way: `keyword_theme` and
`local_service_id` belong to Smart and Local Services campaigns, `listing_scope` to Shopping, hotel, and activity
campaigns, and `webpage` to the campaign types that use webpage targets. Google may also limit how many criteria of
a type a campaign holds; the pages read state that only for `ip_block` (500). Qatlas sends the request once and shows the refusal as `provider-error` with its category and code,
for example `campaignCriterionError CANNOT_EXCLUDE_CRITERIA_TYPE`. `validate_only` shows it without a change.

The addresses of `placement` and the arguments of `webpage` conditions are targets of the ads: Qatlas never calls
them. The scheme rule of tracking templates (https only) does not apply to them, since a placement is often named
by its domain. A `webpage` criterion in an answer also holds `coverage_percentage` and `sample.sample_urls`, which
Google measures; they cannot be set.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a mask of `bidModifier` or `status` alone, since the reference also lists a rule that
  the concrete type is needed for update. No live call has been made.
- The account limits on the number of criteria and the further per-type limits that the pages read do not state.
- The length of `placement.url` (Qatlas bounds it at 1024 bytes), of `criterion_name`, of `argument`, and of
  `free_form_keyword_theme` (255), and the form of `service_id`, which the pages read do not state; Qatlas's bounds
  are its own.
- The range of `radius` of `proximity` and `location_group`, which Qatlas leaves to Google, and whether `status` is
  accepted for a given type: Google answers `CANNOT_SET_STATUS_FOR_CRITERIA_TYPE` for some.
- The reference of the account of `user_interest_category`: Qatlas assumes `customers/ID/userInterests/N` for
  every interest, as Google documents the resource name.

Left out on purpose:

- `webpage_list`, which Google marks as not publicly available.
- The `feed_item_sets` of `location_group`: they belong to the feeds Google has retired.
- The deprecated product dimensions of `listing_scope` (`product_grouping`, `product_labels`,
  `product_legacy_condition`, `product_type_full`), which Google supports in Display campaigns only.
- Reading criteria, `partial_failure`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the criterion family.

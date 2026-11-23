---
description: >
  Describes the Google Ads tools content-creator-insights.generate-creator, content-creator-insights.generate-trending, geo-target-constants.suggest, keyword-theme-constants.suggest and brand-suggestions.suggest: what they read, their arguments and bounds, which of them need no account, the allowlist of the creator insights, the omitted insights_application_info and unsupported attribute types, how answers are cleaned, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: suggestions and insights

These tools ask Google for suggestions and insights: YouTube creators and trends, geo target constants, Smart
campaign keyword themes and brands. All five only read and belong to the profile `read`. They add to the read
tools of the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | Account | What it does |
| --- | --- | --- | --- |
| `google.ads.content-creator-insights.generate-creator` | read | `customer_id` | insights for YouTube creators and channels |
| `google.ads.content-creator-insights.generate-trending` | read | `customer_id` | insights about trending YouTube content |
| `google.ads.brand-suggestions.suggest` | read | `customer_id` | verified brands for the prefix of a name |
| `google.ads.geo-target-constants.suggest` | read | none | geo target constants for place names or constants |
| `google.ads.keyword-theme-constants.suggest` | read | none | Smart campaign keyword themes for a text |

Each call costs one operation of the daily quota. No tool changes anything, offers `validate_only`, or needs a
confirmation.

## Which tools need an account

`geo-target-constants.suggest` and `keyword-theme-constants.suggest` need no account. Their paths
(`geoTargetConstants:suggest`, `keywordThemeConstants:suggest`) have no `customers/...` part. They take no
`customer_id`, send no `login-customer-id`, and are not bound to the targets of the connection, like
`fields.search`.

The creator insights and `brand-suggestions.suggest` have `customers/{customer_id}` in the path. Their
`customer_id` is a required argument and is checked against the targets before any secret is read or any request
is sent: it must be a target or lie below a manager target. They read no data of the account, so a manager account
needs no manager target here, unlike the tools that read billing data.

## Creator insights

Both methods are in `ContentCreatorInsightsService`. Google opens it to allowlisted customers only and describes
`GenerateCreatorInsights` as a private component: partners must pass a pre-screen assessment, follow data usage
rules and accept audits. A call from an account that is not allowlisted ends with `permission` (Google's
`CUSTOMER_NOT_ALLOWLISTED_FOR_THIS_FEATURE`). Each method is limited to 5 queries per second; the provider spaces
its requests more widely.

Every request names `customer_insights_group`, a value of your choice for the customer or brand planned for. Google
documents no format; Qatlas accepts 1 to 255 characters without control characters.

A resource of Google is written as the name the reference uses: a country or sub-country is a geo target constant,
`geoTargetConstants/ID`, which `geo-target-constants.suggest` finds.

An attribute (`audience_attributes`, and inside `audience_combinations` groups) is an object with exactly one member:

| Member | Value |
| --- | --- |
| `age_range` | `AGE_RANGE_18_24`, `_25_34`, `_35_44`, `_45_54`, `_55_64`, `_65_UP`, `AGE_RANGE_UNDETERMINED` |
| `gender` | `FEMALE`, `MALE`, `UNDETERMINED` |
| `parental_status` | `PARENT`, `NOT_A_PARENT`, `UNDETERMINED` |
| `income_range` | `INCOME_RANGE_0_50`, `_50_60`, `_60_70`, `_70_80`, `_80_90`, `_90_UP`, `INCOME_RANGE_UNDETERMINED` |
| `device` | `MOBILE`, `DESKTOP`, `TABLET`, `CONNECTED_TV`, `OTHER` (creator insights only) |
| `user_interest` | resource name `customers/CUSTOMER_ID/userInterests/ID` of the account of the call |
| `entity` | Knowledge Graph machine ID such as `/m/09l6s` |
| `category` | ID of a Product and Service category |

`audience_combinations` are groups with the logic AND of ORs: inside a group the attributes are OR, the groups
are AND. Google allows entities, categories and user interests either in `audience_attributes` or in the
groups, never in both, and the groups take only these three types. Qatlas refuses a request that breaks this
before sending it.

### generate-creator

`POST customers/ID:generateCreatorInsights`. Required: `customer_id`, `customer_insights_group` and
`country_locations` (1 to 25 countries). Name exactly one criterion:

- `search_attributes`: `audience_attributes` (up to 50), `audience_combinations` (up to 20 groups, each with up to 50
  attributes) and `creator_entities` (up to 20 machine IDs of entities that Google tags as creator attributes). At
  least one of the three is required.
- `search_channels`: `youtube_channel_ids` and `youtube_channel_handles`, each up to 50. A handle starts with `@`.
- `search_topics`: `entities`, 1 to 20 machine IDs of entities tagged for creator topic or trending insights. It
  allows one country in `country_locations`.

`sub_country_locations` (up to 50 geo target constants) work only with `search_attributes` and not together with
`supplemental_data` `LOCAL_CREATOR_DATA`, which fills `local_creator_insights` for a `search_topics` request.

### generate-trending

`POST customers/ID:generateTrendingInsights`. Required: `customer_id`, `customer_insights_group` and
`country_location` (one country). Name exactly one criterion:

- `search_audience`: `audience_attributes` (1 to 50, required) and `audience_combinations`. `device` is not an
  attribute here.
- `search_topics`: `entities`, 1 to 20 machine IDs of entities tagged for trending insights.

`sub_country_locations` work with `search_audience` only. `supplemental_data` takes `BRAND_SENTIMENT_DATA` (brand
sentiment for a topic with the brand capability) and `LOCAL_CREATOR_DATA`, each once; `LOCAL_CREATOR_DATA` is
refused with `sub_country_locations`.

### What is left out on purpose

- **`insights_application_info`** (application ID and instance of the calling application) is optional in both
  methods. It names the application toward Google and has no use for a user of this provider, so it is not offered.
- **Attribute types that neither method supports** as an audience attribute are not offered: `location`,
  `lineup`, `youtube_channel`, `youtube_video` and `user_list`. Google's text names `age_range`, `gender`,
  `user_interest`, `entity`, `category`, `device` (creator only), `parental_status` and `income_range`, and says that
  `location` is not supported. The sub-country list takes the place of a location attribute.
- **`search_brand`**, which Google removed in v25, does not exist.

## geo-target-constants.suggest

`POST geoTargetConstants:suggest`. Name exactly one of `location_names` (1 to 25 names, the bound Google documents)
and `geo_target_constants` (1 to 100 resource names `geoTargetConstants/ID` to filter by; Google documents no
bound, 100 is Qatlas's own). Optional: `country_code` (two capital letters, restricts the results) and `locale` (a
language tag such as `de-DE`; Google translates the names with it and falls back to `en`, and uses it as a hint).

The answer has `geo_target_constant_suggestions`. Each holds `locale`, `reach` (the approximate population, rounded
to the nearest 100), `search_term` (the name that matched), `geo_target_constant` and
`geo_target_constant_parents`. A constant holds `resource_name`, `id`, `name` (English), `country_code`,
`target_type`, `status` (`ENABLED` or `REMOVAL_PLANNED`), `canonical_name`, `parent_geo_target` and
`require_eu_political_advertising_declaration`.

## keyword-theme-constants.suggest

`POST keywordThemeConstants:suggest`. `query_text` (1 to 255 characters, such as `plumber`) is required by this tool;
Google's page does not say whether it is. Optional: `country_code` (two capital letters, Google defaults to US) and
`language_code` (two letters, default `en`). The answer has `keyword_theme_constants` with `resource_name`
(`keywordThemeConstants/ID~ID`), `country_code`, `language_code` and `display_name`.

## brand-suggestions.suggest

`POST customers/ID:suggestBrands`. Required: `customer_id` and `brand_prefix` (1 to 255 characters). Optional:
`selected_brands`, up to 100 brand IDs that Google leaves out of the answer; they are IDs, never names. Google
documents no bound, 100 is Qatlas's own. The answer has `brands` with `id` (the Knowledge Graph machine ID for a
verified or global brand), `name`, `urls` and `state` (`ENABLED`, `UNVERIFIED`, `APPROVED`, `REJECTED`,
`CANCELLED` or `DEPRECATED`).

## Answers

Everything in an answer is untrusted third-party content: names of creators and channels, descriptions, brand names,
addresses and summaries. Control characters become spaces and text is cut at 1,024 bytes. Member names are
written in snake_case; numbers Google sends as text (64-bit counts) stay text. Lists are cut at 500 entries, and
`truncated` says when one was. An answer that nests deeper than 16 levels or holds more than 200,000 values
(100,000 for the three suggestion tools) is refused as `invalid-provider-response`.

In the account tools, a resource name that begins with `customers/` must belong to the account of the call, such as
a user interest. Otherwise the answer is refused. Addresses in answers (channel, thumbnail and video links, brand
`urls`) are text: no tool fetches them. Non-public channel metrics and audience attributes come only for creators
that consented to share them, and Google marks that with `data_sharing_consent_given`.

Apart from the main list of each answer, an empty list is not shown; a missing member means empty.

## Boundary

Checked against the proto files of v26 (`content_creator_insights_service`, `audience_insights_attribute`,
`additional_application_info`, `geo_target_constant_service`, `geo_target_constant`,
`keyword_theme_constant_service`, `keyword_theme_constant`, `brand_suggestion_service`), the reference pages of
the services and of the request, enumeration and criterion types they link, and the creator insights guide:

- Google documents one bound only: at most 25 `location_names`. Every other list bound in this page is Qatlas's own.
- The guide sets no length or format for `customer_insights_group`.
- `insights_application_info` is optional, as is every field not named as required above.
- The method pages of the REST reference could not be read (they answer 404 under the paths tried). The RPC
  reference and the proto files, which carry the same HTTP paths and fields, were the source. The REST names of
  members are the camelCase form of the snake_case names above.

Not verified: that an allowlisted account is accepted, the exact shape of the answers beyond the proto fields, the
format Google accepts for handles and machine IDs beyond the examples, the role each call needs, and the behavior
of the suggestion calls for accounts without access. No live call has been made.

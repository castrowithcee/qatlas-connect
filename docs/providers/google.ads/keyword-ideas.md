---
description: >
  Describes the Google Ads keyword idea tools keyword-plan-ideas.generate-ideas, generate-historical-metrics, generate-ad-group-themes and generate-forecast-metrics in the profile read, the Explorer access level that blocks the service, the spacing of one request per second per customer ID, the arguments and their bounds, the checks before any request, the seeds that Google reads and Qatlas never fetches, the bounded answers, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: keyword ideas

These tools ask `KeywordPlanIdeaService` of Google Ads for keyword ideas, search metrics, ad group suggestions and
forecasts. They add to the read tools of the [Google Ads provider](README.md), whose configuration, targets,
quotas, and errors apply here too. They create and change nothing in the account.

| Tool | Google method | What it does |
| --- | --- | --- |
| `google.ads.keyword-plan-ideas.generate-ideas` | `GenerateKeywordIdeas` | ideas with their metrics from keywords, a page address, both, or a site |
| `google.ads.keyword-plan-ideas.generate-historical-metrics` | `GenerateKeywordHistoricalMetrics` | historical search metrics of 1 to 500 keywords |
| `google.ads.keyword-plan-ideas.generate-ad-group-themes` | `GenerateAdGroupThemes` | sorts keywords into ad groups of the account |
| `google.ads.keyword-plan-ideas.generate-forecast-metrics` | `GenerateKeywordForecastMetrics` | forecasts a keyword campaign described in the arguments |

All four belong to the profile `read` and need no confirmation. `customer_id` is a required argument and is checked
against the targets before a secret is read; an account below a manager target is proved with one read of the
hierarchy first, as for the other tools.

## Access level

Google blocks `KeywordPlanIdeaService` at the Explorer access level; the Cloud project of the credential needs
at least Basic access. At Explorer Google refuses the call. Qatlas maps that refusal like every refusal of the
service: an authorization or quota error with a documented code becomes `permission`, a 403 without one becomes
`permission` as well, and the message names the code. A project with test access reaches test accounts only and
ends with `CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION`. The descriptions of the tools say so.

## Spacing

Google limits `GenerateKeywordIdeas`, `GenerateKeywordHistoricalMetrics` and `GenerateKeywordForecastMetrics` to
one request per second per customer ID, counted as 60 requests per 60 seconds, and refuses more with
`RESOURCE_EXHAUSTED`. `GenerateAdGroupThemes` may take two per second. Qatlas spaces all four tools by one second
per customer ID within a process, whichever credential or connection sends them, in addition to the spacing per
credential (see [rate spacing](README.md#rate-spacing)). After a rate refusal the next request of that account
waits the delay Google names, at least one second and at most one minute. Processes do not share the spacing:
two processes may exceed the limit together, and Google then answers `RESOURCE_EXHAUSTED`, which is mapped as
`rate-limited` and not repeated. Every call costs one operation of the daily quota.

## Seeds and addresses

`seed_url` and `seed_site` are passed to Google as text. Qatlas never fetches them; Google reads the page. A
`seed_url` is a host name with an optional path, such as `www.example.com/cars`, or an `https` address, without
user info, port, spaces, backslashes or braces. A `seed_site` is a plain host name. Whether Google crawls the
address and what it finds there is not under the control of Qatlas.

## generate-ideas

Exactly one seed form, as in Google's `seed` oneof: `seed_keywords` (1 to 20 keywords), `seed_url`,
`seed_keywords` with `seed_url`, or `seed_site` alone. Further arguments: `language` (`languageConstants/ID`),
`geo_target_constants` (at most 10, `geoTargetConstants/ID`, each once), `include_adult_keywords`,
`keyword_plan_network` (`GOOGLE_SEARCH` or `GOOGLE_SEARCH_AND_PARTNERS`), `keyword_annotations`
(`KEYWORD_CONCEPT`), `aggregate_metric_types` (`DEVICE`), `historical_metrics_options`
(`year_month_range` with `start` and `end`, each a year and a month name, and `include_average_cpc`),
`page_token` and `page_size`.

Google pages the answer, 10000 rows at most. Qatlas sends `page_size` 100 when the argument is left out and takes
1 to 500, so that a page fits the 500 rows an answer holds. `next_page_token` goes into `page_token` of the next
call with every other argument unchanged. Google may answer more rows than asked: rows beyond 500 are cut and the
answer says `truncated`, and the cut rows cannot be read again.

Per idea the answer holds `text` (which may be the canonical form of several keywords), `keyword_idea_metrics`,
`keyword_annotations` (`name`, `concept_group_name`, `concept_group_type`) when requested, and `close_variants`.
`aggregate_metric_results.device_searches` holds `device` and `search_count` when `DEVICE` was requested.
`total_size` is Google's count of ideas.

## generate-historical-metrics

`keywords` (1 to 500), and the same targeting arguments as `generate-ideas`: `language`, `geo_target_constants`,
`include_adult_keywords`, `keyword_plan_network`, `aggregate_metric_types` and `historical_metrics_options`.
Google takes 10000 keywords. Qatlas takes 500, the most a result list holds, so that no result is cut: Google
merges near-identical keywords, so an answer never has more results than keywords. Of `car` and `cars` one text
comes back and `close_variants` names the merged ones.

The metrics are the same in both tools: `avg_monthly_searches`, `monthly_search_volumes` (`year`, `month`,
`monthly_searches`; a month without a volume has none), `competition`, `competition_index` (0 to 100),
`low_top_of_page_bid_micros`, `high_top_of_page_bid_micros` and `average_cpc_micros`. Numbers of 64 bits are
decimal strings, as in the other tools of this provider. Without a range Google answers the past 12 months and
offers the past four years; Qatlas checks the form and the order of the range and Google judges the rest.

## generate-ad-group-themes

`keywords` (1 to 200) and `ad_groups` (1 to 50, each `customers/CUSTOMER_ID/adGroups/ID` of the account of
`customer_id`, each once). A name of another account is refused as `invalid-request` before a secret is read. Google
names no bound for either list; the bounds are Qatlas's. The answer holds `ad_group_keyword_suggestions`
(`keyword_text`, `suggested_keyword_text`, `suggested_match_type`, `suggested_ad_group`, `suggested_campaign`) and
`unusable_ad_groups` (`ad_group`, `campaign`). An ad group is unusable when it is not enabled or its campaign is
neither enabled nor paused. Nothing is added to the ad groups. If an answer names an ad group or a campaign of
another account, or another collection, the whole answer is refused as `invalid-response`.

## generate-forecast-metrics

`campaign` (required) describes a campaign that does not exist: `language_constants` (at most 100),
`geo_target_constants` (at most 100, each once), `bidding_strategy` (required, exactly one of
`manual_cpc_bidding_strategy` with `max_cpc_bid_micros` and optional `daily_budget_micros`,
`maximize_clicks_bidding_strategy` with `daily_target_spend_micros` and optional `max_cpc_bid_ceiling_micros`, or
`maximize_conversions_bidding_strategy` with `daily_target_spend_micros`) and `ad_groups` (at most 200, each with at
least one keyword of `text` and `match_type`, at most 10000 keywords in all). Further arguments: `currency_code`
(three capital letters, the account currency when left out) and `forecast_period` (`start_date` and `end_date` as
`yyyy-mm-dd`). Google needs the start in the future and the end within one year of today, in the time zone of the
account, and uses the next Sunday to the following Saturday without a period. Qatlas checks the form and the
order of the dates only, because it does not know the time zone of the account. Google enforces a minimum budget
or spend for the currency and names it in its error.

The answer holds `campaign_forecast_metrics` with `average_cpc_micros` (only with clicks above zero), `clicks`
(manual CPC and maximize clicks), `cost_micros`, `conversions` (maximize conversions) and `average_cpa_micros`. A
member Google leaves out is left out. The tool takes no reference to a campaign or a plan of the account, and
neither does Google's method; this tool and `generate-historical-metrics` read no object of the account.

## Bounds and checks

Every argument is checked before a secret is read and before any request, and the refusal names the argument and
the rule, never the value. A keyword has at most 80 characters and 10 words, the bound Google documents for the
keywords of a plan. The other list bounds named above are Qatlas's own unless the text says Google's. `login_customer_id`,
paths, headers and `partial_failure` are no arguments. Everything Google returns is untrusted third-party text:
texts are cut to 1024 bytes with control characters replaced, lists to 500 entries, and a page token longer than
4096 bytes refuses the answer.

## Checked against Google's reference

The numbers above were checked on 2026-11-23 against the v26 definitions of `KeywordPlanIdeaService` and its common
messages in the `googleapis` repository (`keyword_plan_idea_service.proto`, `keyword_plan_common.proto`, `dates.proto`,
`criteria.proto`) and the quotas page of the Google Ads API (`/docs/best-practices/quotas`): at most 10 locations,
20 seed keywords, 10000 results per page, 10000 keywords for the metrics, the required members of the forecast
request, the forecast period rules, and one request per second per customer ID (two for the themes). Google's
`page_size` and `keywords` maxima exceed what Qatlas takes, as stated above.

Not verified: the exact error Google answers at the Explorer access level (the mapping above covers the documented
codes and any 403), the range of years `year_month_range` accepts, whether Google tolerates short bursts within
the 60 requests per 60 seconds that the spacing does not use, and what Google does with a seed address it cannot
read. No live call has been made.

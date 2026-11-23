---
description: >
  Describes the Google Ads reach planning tools reach-plans.list-locations, list-products, generate-forecast, generate-conversion-rates, list-user-lists and list-user-interests, all in the profile read: what each calls, which of them take customer_id and which are bound to no target, the arguments of the forecast with the checks made before any request, the bounded answers, the access level of Basic that Google requires, the planning rate of about one request per second and how the credential spacing relates to it, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: reach planning

These tools read from `ReachPlanService`: which locations, products, user lists and user interests reach planning
offers, which conversion rates it suggests, and a reach forecast for a product mix. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Calls | Takes `customer_id` |
| --- | --- | --- |
| `google.ads.reach-plans.list-locations` | `POST v26:listPlannableLocations` | no |
| `google.ads.reach-plans.list-products` | `POST v26:listPlannableProducts` | no |
| `google.ads.reach-plans.generate-forecast` | `POST v26/customers/{customer_id}:generateReachForecast` | yes |
| `google.ads.reach-plans.generate-conversion-rates` | `POST v26:generateConversionRates` | yes |
| `google.ads.reach-plans.list-user-lists` | `POST v26:listPlannableUserLists` | yes |
| `google.ads.reach-plans.list-user-interests` | `POST v26:listPlannableUserInterests` | yes |

All six have the effect `read` and belong to the profile `read`. Nothing is created, saved, bought or changed in an
account: a forecast describes a plan and Google keeps nothing of it. Therefore no tool of this group has
`validate_only`, `partial_failure`, a confirmation or a `tools` entry.

## Targets

The four tools with `customer_id` follow the rules of the [targets](README.md#targets): the account is checked
against the targets of the connection before a secret is read and before any request, an account below a manager
target is proved with one membership read, and the `login-customer-id` header goes along.

`list-locations` and `list-products` take no account in Google's reference. They follow `fields.get` and
`incentives.fetch`: they are bound to no target, send no `login-customer-id`, and a connection with targets may call
them. They still need the credential, so the Cloud project rules below apply to them as well.

## Arguments

Every documented request member is a structured argument in snake_case. `reach_application_info` (`application_id`,
`application_instance` `PRODUCTION` or `DEVELOPMENT_AND_TESTING`) and, where the reference has it,
`customer_reach_group` (a name of your choice, 1 to 200 characters) are optional on every tool that takes them.
`list-products` takes `plannable_location_id`. `list-user-interests` takes `name_query` and `path_query` (up to 200
characters each, a literal case-insensitive match) and `user_interest_taxonomy_types`, `AFFINITY` and `IN_MARKET`,
each once.

`generate-forecast` takes `campaign_duration`, `planned_products`, `targeting`, `cookie_frequency_cap_setting`,
`currency_code`, `effective_frequency_limit`, `min_effective_frequency`, `forecast_metric_options` and the two members
above. Before any secret is read Qatlas refuses a request that breaks what the reference states:

- `campaign_duration` names exactly one of `date_range` and `duration_in_days`. A date range holds `yyyy-mm-dd`
  dates, both inclusive, and spans at most 92 days. That the end lies less than a year ahead is left to Google.
- 1 to 15 planned products. Each has a `plannable_product_code` and `budget_micros` of at least 1 (an integer, kept
  digit for digit). `conversion_rate` lies between 0 and 1, both exclusive. That it is necessary for `DEMAND_GEN`
  and unsupported for other products is left to Google.
- `advanced_product_targeting` names exactly one of `youtube_select_settings`, `surface_targeting_settings` and
  `target_frequency_settings` (`WEEKLY` or `MONTHLY`).
- `min_effective_frequency` and `effective_frequency_limit` are 1 to 10 and are not combined.
- `targeting` takes the enum values of the reference only. Location IDs are numeric. That several locations share one
  parent country is left to Google.
- User lists and user interests in `targeting.audience_targeting` must be `customers/CUSTOMER_ID/userLists/ID` and
  `customers/CUSTOMER_ID/userInterests/ID` of the account of `customer_id`; a name of another account is refused as
  `invalid-request`.

A 400 of Google with a documented error code is returned as `provider-error` with the category and the code, as
Google's refusal of the input; nothing was changed.

## Answers

Every member of the documented answers is returned in the names of the reference in snake_case: the locations, the
products with their core attributes and allowed targeting, the user lists, the user interests, the conversion rate
suggestions, and for a forecast `reach_curve` with the forecast of each point and of each planned product, and
`on_target_audience_metrics`. A member that a later version adds is passed on as well. Everything in an answer is
untrusted third-party text:

- Control characters become spaces, and a text is cut at 1,024 bytes.
- A list is cut at 500 entries, also inside a forecast, and then `truncated` is set. The full interest taxonomy is
  larger than that, so narrow it with `name_query`, `path_query` or the types.
- An answer deeper than 16 levels or with more than 50,000 values is `invalid-provider-response`, as is a resource
  name of another account than the one asked for.
- Google leaves empty lists out, so a list that is missing is empty.

## Limits

- **Access level.** `ReachPlanService` is closed to Cloud projects with the access level Explorer. Basic or higher is
  needed, as for the keyword plan and audience insights services. How Google reports the refusal is not documented
  for this service; whatever error code arrives is mapped like every other one, see [errors](README.md#errors).
- **Planning rate.** Google lists planning at about one request per second. Qatlas spaces the requests of a credential
  by 250 ms only, see [rate spacing](README.md#rate-spacing), and builds no mechanism of its own for this limit. A
  second forecast within a second can therefore reach Google and may be refused as `rate-limited`. The credential then
  waits at least one second before the next request, and the failed request is not repeated.
- **Quota.** Each call is one request and one operation of the daily quota of the Cloud project, also when it fails;
  the membership read counts as well.

## Checked and not verified

Checked against the reference of v26: the six methods, their request and answer members, the enums, the REST paths
in the discovery document (four methods without an account in the path, the forecast below the customer), the limit
of 15 products, the span of 92 days, and the exclusive range of the conversion rate.

Not verified: the error code for an Explorer project, the behavior at the planning rate in practice, the size of the
real taxonomies, and which role an account user needs. No live call has been made.

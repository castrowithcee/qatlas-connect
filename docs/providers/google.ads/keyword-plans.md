---
description: >
  Describes the keyword plan tools of the Google Ads provider: keyword-plans.create, keyword-plans.update and keyword-plans.remove, and keyword-plan-campaigns.create, keyword-plan-campaigns.update and keyword-plan-campaigns.remove, and keyword-plan-campaign-keywords.create, keyword-plan-campaign-keywords.update and keyword-plan-campaign-keywords.remove, with the Mutate service and its operations list, the bound of 50 operations, the checks on name and forecast period against the v26 definitions, the field mask of update, the absence of response_content_type, validate_only, confirmation, how the tools are reached by effect, the plan limit of an account, the Explorer access level, the unknown outcome, the binding of resource names to the account, the manager account rule, and the one-campaign-per-plan rule, the bid, network, language and location checks of plan campaigns, the negative-only rule, text and match type checks, and the 1000-negative-keyword limit of plan campaign keywords, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: keyword plan tools

These tools create, change, and remove the keyword plans of a Google Ads account. A keyword plan is a saved Keyword
Planner plan, and a plan holds at most one campaign. The plan tools set the name and the forecast period of a plan, the
plan campaign tools the name, network, default bid, language, and locations of its campaign, the plan campaign keyword
tools the negative keywords of that campaign. They add to the read tools of
the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and they sit
beside the [keyword idea tools](keyword-ideas.md), which need no saved plan.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.keyword-plans.create` | create | creates one to 50 keyword plans |
| `google.ads.keyword-plans.update` | update | changes the name or the forecast period of existing plans |
| `google.ads.keyword-plans.remove` | delete | removes existing plans |
| `google.ads.keyword-plan-campaigns.create` | create | creates the campaign of one to 50 plans |
| `google.ads.keyword-plan-campaigns.update` | update | changes name, network, bid, language, or locations of existing plan campaigns |
| `google.ads.keyword-plan-campaigns.remove` | delete | removes existing plan campaigns |
| `google.ads.keyword-plan-campaign-keywords.create` | create | creates one to 50 negative keywords of plan campaigns |
| `google.ads.keyword-plan-campaign-keywords.update` | update | changes the text or match type of existing negative keywords |
| `google.ads.keyword-plan-campaign-keywords.remove` | delete | removes existing negative keywords of plan campaigns |

Reading plans, plan campaigns, and their keywords is not a tool of this page: run `reports.search` on the
`keyword_plan`, `keyword_plan_campaign`, and `keyword_plan_campaign_keyword` resources.

## How the tools are offered

- **By effect.** Each tool is offered by a connection whose effects include its own effect, and by a connection whose
  `tools` list names it. No tool belongs to a profile: the `read` profile holds none. A connection with a `tools` list
  gets exactly the tools the list names, whatever its effects are.
- **Confirmed per call.** Each tool needs the confirmation of that one call, `validate_only` included. The
  confirmation text names the account and, per entry, the name and forecast period or the ID, with texts cut and
  without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `KeywordPlanService.MutateKeywordPlans` with an `operations`
  list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50 entries. The 50 are
  Qatlas's own bound; the definition of v26 names none.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers errors
  only, so the answer then holds `validate_only: true` and an empty `results`. Validation needs the same
  confirmation and the same checks and costs the same operations as the change.
- **No `response_content_type`.** The request of v26 has no such member and a result holds the resource name only.
  A result is `keyword_plan_id` and `resource_name`.
- **Names come from checked IDs.** `update` and `remove` build `customers/ID/keywordPlans/PLAN_ID` from
  `customer_id` and `keyword_plan_id`. A caller never passes a resource name, a field mask, or an operation.
- **The field mask is built from the arguments.** `update` names exactly the fields given: `name`, `forecast_period`,
  or both. A `forecast_period` replaces the whole period of the plan, so switching between `date_interval` and
  `date_range` needs no second step.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or not
  readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the plans with
  `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole request and say
  so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/keywordPlans/N` of the `customer_id` of the call. For `update` and `remove` the results must be exactly
  the plans asked for, in the order asked for, and a `create` must answer one distinct plan per operation. Anything
  else is `invalid-provider-response` with the note about the unknown outcome, since the change may have been made.
  A `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused, for example by the plan limit, ends as `provider-error` with
  the error category and code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call may
  add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `keyword_plans` | 1 to 50 entries, each an object of `name` (required) and `forecast_period`, no other member |
| `update` | `keyword_plans` | 1 to 50 entries, each an object of `keyword_plan_id` (required, once per call) and at least one of `name` and `forecast_period` |
| `create`, `update` | `keyword_plans[].name` | 1 to 255 characters, no control characters |
| `update`, `remove` | `keyword_plan_id`, `keyword_plan_ids` | positive numbers as `reports.search` returns them; `remove` takes 1 to 50 distinct IDs |
| `create`, `update` | `keyword_plans[].forecast_period` | exactly one of `date_interval` and `date_range` |
| `create`, `update` | `forecast_period.date_interval` | `NEXT_WEEK`, `NEXT_MONTH`, or `NEXT_QUARTER` |
| `create`, `update` | `forecast_period.date_range` | `start_date` and `end_date`, both required, in the form `yyyy-mm-dd`, the start not after the end |
| all | `validate_only` | true validates only |

The forecast period is optional on a plan, as in the definition. `id` is output only and no argument.

### Qatlas bounds

- The 50 entries per call are Qatlas's own; Google documents no limit for this request.
- The 255 characters of the name are Qatlas's own, counted as characters, not bytes; the definition states no length
  and requires only a name that is not empty on create.
- Both dates of a `date_range` are required by the tool although the definition marks neither. The definition
  states the dates as inclusive, in the future, and a span of at most one year. Qatlas does not check the last two:
  Google checks them when a forecast runs, so a plan with a past range can be saved.

## Plan campaign tools

The sections above hold for the plan campaign tools too, with these differences: a call goes to
`KeywordPlanCampaignService.MutateKeywordPlanCampaigns`, resource names are
`customers/ID/keywordPlanCampaigns/N`, and a result is `keyword_plan_campaign_id` and `resource_name`. The request of
v26 has no `response_content_type` here either.

- **The plan is given as an ID.** `create` takes `keyword_plan_id` and builds `customers/ID/keywordPlans/PLAN_ID` from
  it and the checked `customer_id`. The plan of a campaign is not an argument of `update`.
- **One campaign per plan.** Google allows one campaign per plan. `create` refuses a call that names a plan twice,
  and Google refuses a plan that has a campaign already.
- **Update replaces lists.** `language_constants` and `geo_target_constants` given in `update` replace the whole
  list, and the field mask names exactly the fields given: `name`, `keyword_plan_network`, `cpc_bid_micros`,
  `language_constants`, `geo_targets`. An empty list is refused, since the definition does not document that it
  clears the field.

| Tool | Argument | Rule |
| --- | --- | --- |
| `create` | `keyword_plan_campaigns` | 1 to 50 entries for distinct plans, each an object of `keyword_plan_id`, `name`, `keyword_plan_network`, `cpc_bid_micros` (all required), `language_constants`, and `geo_target_constants` |
| `update` | `keyword_plan_campaigns` | 1 to 50 entries, each an object of `keyword_plan_campaign_id` (required, once per call) and at least one of the other four fields |
| `remove` | `keyword_plan_campaign_ids` | 1 to 50 distinct positive numbers |
| `create`, `update` | `name` | 1 to 255 characters, no control characters |
| `create`, `update` | `keyword_plan_network` | `GOOGLE_SEARCH` or `GOOGLE_SEARCH_AND_PARTNERS` |
| `create`, `update` | `cpc_bid_micros` | whole positive number of micros of the account currency that fits 64 bits, the default max CPC bid of the campaign |
| `create`, `update` | `language_constants` | one `languageConstants/ID` |
| `create`, `update` | `geo_target_constants` | 1 to 20 distinct `geoTargetConstants/ID` |

`id` is output only and `keyword_plan` and `resource_name` are no arguments of `update`.

### Plan campaign bounds

- Google's definition requires `name`, `keyword_plan_network`, and `cpc_bid_micros` on create, allows at most one
  language constant and at most 20 geo targets, and one campaign per plan. The tool checks all of these before a
  request.
- The 50 entries per call and the 255 characters of the name are Qatlas's own, as for plans.
- `UNSPECIFIED` and `UNKNOWN` of the network enum are not accepted.
- Google documents no minimum or multiple of the bid; the smallest unit of the currency is Google's to validate.
- The tool takes geo targets as a list of `geoTargetConstants/ID` strings and sends each as a `geoTargets` entry with
  its `geoTargetConstant`.

## Plan campaign keyword tools

The sections above hold for the plan campaign keyword tools too, with these differences: a call goes to
`KeywordPlanCampaignKeywordService.MutateKeywordPlanCampaignKeywords`, resource names are
`customers/ID/keywordPlanCampaignKeywords/N`, and a result is `keyword_plan_campaign_keyword_id` and `resource_name`.
The request of v26 has no `response_content_type` here either.

- **Negative keywords only.** Google supports only negative keywords on a plan campaign, and `negative` is immutable and
  must be true. The tools always send `negative: true` and have no argument for it, so no positive keyword can be
  made.
- **The campaign is given as an ID.** `create` takes `keyword_plan_campaign_id` and builds
  `customers/ID/keywordPlanCampaigns/CAMPAIGN_ID` from it and the checked `customer_id`. The campaign of a keyword is
  not an argument of `update`.
- **Update names its fields.** The field mask names exactly the fields given: `text`, `match_type`.
- **The keyword text is untrusted input.** It is checked before any request and shown quoted and cut in the
  confirmation.

| Tool | Argument | Rule |
| --- | --- | --- |
| `create` | `keyword_plan_campaign_keywords` | 1 to 50 entries, each an object of `keyword_plan_campaign_id`, `text`, and `match_type` (all required) |
| `update` | `keyword_plan_campaign_keywords` | 1 to 50 entries, each an object of `keyword_plan_campaign_keyword_id` (required, once per call) and at least one of `text` and `match_type` |
| `remove` | `keyword_plan_campaign_keyword_ids` | 1 to 50 distinct positive numbers |
| `create`, `update` | `text` | 1 to 80 characters, not blank, no control characters |
| `create`, `update` | `match_type` | `BROAD`, `EXACT`, or `PHRASE` |

`id` is output only and `negative`, `keyword_plan_campaign`, and `resource_name` are no arguments of `update`.

### Plan campaign keyword bounds

- **At most 1000 negative keywords per plan**, campaign keywords and ad group keywords together, as the service
  definition states. Google refuses a create beyond that with a resource count limit error. This replaces the 10000
  keywords per plan that the planning overview is summarized with.
- The 80 characters of the text and the 50 entries per call are Qatlas's own. Google's definition states no length or
  word count for the text, so Google may refuse a text that Qatlas accepts.
- `UNSPECIFIED` and `UNKNOWN` of the match type enum are not accepted.

## Limits of the account

- **At most 10000 saved plans** per account. Google refuses a create beyond that and allows removing plans when the
  limit is reached.
- **Explorer access level.** Google's planning overview that was read states no access level for
  `KeywordPlanService`. Other keyword planning services are closed to Cloud projects at Explorer, see
  [keyword ideas](keyword-ideas.md#access-level), so the descriptions say that the project may need Basic access. How
  Google reports a refusal is mapped like every other one, see [errors](README.md#errors).
- **Rate.** These calls are spaced per credential only, see [rate spacing](README.md#rate-spacing). Qatlas builds no
  per-account spacing for the planning rate; a request that Google refuses is `rate-limited` and is not repeated.

## Boundary

Left out on purpose:

- Reading plans.
- The ad groups and ad group keywords of a plan, positive keywords of a plan campaign, and running a forecast on a plan.
- `partial_failure`, `response_content_type`, any free operation, mask, or resource name, and
  `GoogleAdsService.Mutate`.

Checked against the v26 definitions of `KeywordPlan`, `KeywordPlanForecastPeriod`, `KeywordPlanService`,
`KeywordPlanOperation`, and `KeywordPlanForecastIntervalEnum.KeywordPlanForecastInterval`: the request holds
`customer_id`, `operations`, `partial_failure`, and `validate_only`, the operation holds `create`, `update`, `remove`,
and `update_mask`, the name is required on create, `id` is output only, `resource_name` is immutable, the period holds
one of the three intervals or a date range, and the limit of 10000 plans stands on the resource.

Checked against the v26 definitions of `KeywordPlanCampaign`, `KeywordPlanGeoTarget`,
`KeywordPlanCampaignOperation`, `KeywordPlanNetworkEnum.KeywordPlanNetwork`, and `KeywordPlanCampaignService`: the
request holds `customer_id`, `operations`, `partial_failure`, and `validate_only`, the result holds the resource name
only, `name`, `keyword_plan_network`, and `cpc_bid_micros` are required on create, `resource_name` is immutable, and
the resource states one campaign per plan, one language, and 20 geo targets. The definition marks `keyword_plan` as
no more than a reference, so whether Google refuses to change it is not stated; the tool offers no such change.

Checked against the v26 definitions of `KeywordPlanCampaignKeyword`, `KeywordPlanCampaignKeywordOperation`,
`KeywordMatchTypeEnum.KeywordMatchType`, and `KeywordPlanCampaignKeywordService`: the request holds `customer_id`,
`operations`, `partial_failure`, and `validate_only`, the result holds the resource name only, only negative keywords are
supported, `negative` and `resource_name` are immutable, `id` is output only, the match type is `BROAD`, `EXACT`, or
`PHRASE`, and a plan holds at most 1000 negative keywords. The definition marks no field as required, so the tool
requires `keyword_plan_campaign`, `text`, and `match_type` on create, since a keyword without them has no meaning.
The definition marks `keyword_plan_campaign` as no more than a reference, so whether Google refuses to change it is not
stated; the tool offers no such change.

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google counts the 255 characters of a name as characters, which names it refuses, and whether it refuses a
  second plan with the same name.
- Which error Google answers when a plan has a campaign already, and which language and location constants it
  refuses.
- Whether the Explorer access level blocks these calls, and the error then answered.
- The behavior at the planning rate. No live call has been made.
- For plan campaign keywords: the length and word count of a text and which symbols Google refuses, whether a second
  keyword with the same text and match type on a campaign is refused, and which error Google answers for a positive
  keyword (the tool never sends one).

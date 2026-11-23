---
description: >
  Describes the keyword plan tools of the Google Ads provider: keyword-plans.create, keyword-plans.update and keyword-plans.remove, with the Mutate service and its operations list, the bound of 50 operations, the checks on name and forecast period against the v26 definitions, the field mask of update, the absence of response_content_type, validate_only, confirmation, how the tools are reached by effect, the plan limit of an account, the Explorer access level, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: keyword plan tools

These tools create, change, and remove the keyword plans of a Google Ads account. A keyword plan is a saved Keyword
Planner plan. The tools of this page set the name and the forecast period of a plan only. They add to the read tools of
the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and they sit
beside the [keyword idea tools](keyword-ideas.md), which need no saved plan.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.keyword-plans.create` | create | creates one to 50 keyword plans |
| `google.ads.keyword-plans.update` | update | changes the name or the forecast period of existing plans |
| `google.ads.keyword-plans.remove` | delete | removes existing plans |

Reading plans is not a tool of this page: run `reports.search` on the `keyword_plan` resource.

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
- The campaigns, ad groups, and keywords of a plan, and running a forecast on a plan.
- `partial_failure`, `response_content_type`, any free operation, mask, or resource name, and
  `GoogleAdsService.Mutate`.

Checked against the v26 definitions of `KeywordPlan`, `KeywordPlanForecastPeriod`, `KeywordPlanService`,
`KeywordPlanOperation`, and `KeywordPlanForecastIntervalEnum.KeywordPlanForecastInterval`: the request holds
`customer_id`, `operations`, `partial_failure`, and `validate_only`, the operation holds `create`, `update`, `remove`,
and `update_mask`, the name is required on create, `id` is output only, `resource_name` is immutable, the period holds
one of the three intervals or a date range, and the limit of 10000 plans stands on the resource.

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google counts the 255 characters of a name as characters, which names it refuses, and whether it refuses a
  second plan with the same name.
- Whether the Explorer access level blocks these calls, and the error then answered.
- The behavior at the planning rate. No live call has been made.

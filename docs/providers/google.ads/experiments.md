---
description: >
  Describes the experiment tools of the Google Ads provider: experiments.create, experiments.update, experiments.remove, and experiments.list-async-errors, with the Mutate service and its bound of 10 operations, the fields and enums checked against the reference of v26, validate_only, confirmation, how create and update are reached by effect while remove needs the tools list, the read of asynchronous errors with paging and its bounds, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: experiment tools

These tools create, change, and remove the experiments of a Google Ads account, and list the errors of the last
asynchronous operation of an experiment. They add to the read tools of the [Google Ads provider](README.md),
whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.experiments.create` | create | creates one or more experiments |
| `google.ads.experiments.update` | update | changes name, description, suffix, status, dates, and goals of experiments |
| `google.ads.experiments.remove` | delete | removes existing experiments |
| `google.ads.experiments.list-async-errors` | read | lists the errors of the last scheduling or promotion of one experiment |

Reading experiments is not a tool of this page: run `reports.search` on the `experiment` resource, which also shows
the state of a scheduling or promotion. Scheduling, ending, graduating, and promoting an experiment are not offered
here. The arms of an experiment have their own [experiment arm tools](experiment-arms.md).

## How the tools are offered

- **`experiments.create` and `experiments.update`** are offered by a connection whose effects include `create` or
  `update`, and by a connection whose `tools` list names them. They belong to no profile.
- **`experiments.remove`** is offered only through the `tools` list. A connection with the `delete` effect and no
  `tools` list does not have it.
- **`experiments.list-async-errors`** reads only. It is part of the `read` profile and needs no confirmation.
- **Confirmed per call.** The three changing tools need the confirmation of that one call. The confirmation text
  names the account and the experiments, with the texts cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request, in all four tools. The `login_customer_id` of the service is never an argument.

## What every change does

- **One atomic request.** A call sends one request to `ExperimentService.MutateExperiments` with an `operations`
  list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 10 entries. The 10
  are Qatlas's own bound; the reference states none.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so
  the answer then holds `validate_only: true` and no `results`. Validation needs the same confirmation and the same
  checks, and costs the same operations as the change.
- **No `response_content_type`.** The request of this service has no such field in v26, so a result holds
  `experiment_id` and `resource_name` only.
- **Names come from checked IDs.** `update` and `remove` build `customers/ID/experiments/EXPERIMENT_ID` from
  `customer_id` and `experiment_id`. A caller never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or
  not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the
  experiments with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/experiments/N` of the `customer_id` of the call. For `update` and `remove` the results must be
  exactly the experiments asked for, in the order asked for; a `create` must answer one distinct experiment per
  operation. Anything else is `invalid-provider-response` with the note about the unknown outcome, since the change
  may have been made. A `partial_failure_error` in an answer is treated the same way.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `experiments` | 1 to 10 entries |
| `create` | `experiments[].name` | required, 1 to 1024 characters, no control characters; Google requires it to be unique under the account |
| `create` | `experiments[].type` | required, one of the types of `ExperimentType` except `UNSPECIFIED` and `UNKNOWN` |
| `create`, `update` | `experiments[].description` | 1 to 2048 characters, no control characters |
| `create`, `update` | `experiments[].suffix` | 1 to 255 characters, no control characters |
| `create`, `update` | `experiments[].status` | `ENABLED`, `HALTED`, or `SETUP` |
| `create`, `update` | `experiments[].start_date`, `end_date` | `YYYY-MM-DD`, in the time zone of the account; `end_date` not before `start_date` when both are given |
| `create`, `update` | `experiments[].goals` | 1 to 10 entries of `metric` and `direction`; on `update` the list replaces all goals |
| `create` | `experiments[].sync_enabled` | true syncs changes of the base campaign to the trial campaigns; settable only at creation |
| `create` | `experiments[].optimize_assets_experiment_subtype` | only with type `OPTIMIZE_ASSETS`: `ADD_ASSETS_TO_ASSETLESS_RETAIL`, `ADD_VIDEO_ASSETS_TO_VIDEOLESS`, `COMPARE_ASSETS` |
| `create` | `experiments[].video_experiment_subtype` | only with type `YOUTUBE_CUSTOM`: `ASSET`, `ASSET_UPLIFT`, `DEMAND_GEN_ASSET` |
| `update` | `experiments` | 1 to 10 entries, each `experiment_id` once, each with at least one field to change |
| `update` | `experiments[].experiment_id` | positive number, the `experiment.experiment_id` that `reports.search` returns |
| `remove` | `experiment_ids` | 1 to 10 distinct experiment IDs |
| `create`, `update`, `remove` | `validate_only` | true validates only |
| `list-async-errors` | `experiment_id` | required, positive number of an experiment of `customer_id` |
| `list-async-errors` | `page_size` | 1 to 1000; Google may return fewer |
| `list-async-errors` | `cursor` | the `next_cursor` of a previous page of the same call on the same connection |

The metrics of a goal are `AVERAGE_CPC`, `CLICKS`, `COMPLETED_VIDEO_VIEWS`, `CONVERSIONS`,
`CONVERSIONS_PER_INTERACTION_RATE`, `CONVERSIONS_VALUE_PER_COST`, `CONVERSION_VALUE`, `COST`, `COST_PER_CONVERSION`,
`CTR`, `CUSTOM_ALGORITHMS`, `IMPRESSIONS`, and `INCREMENTAL_CONVERSIONS`. Its directions are `DECREASE`,
`INCREASE`, `NO_CHANGE`, `NO_CHANGE_OR_DECREASE`, and `NO_CHANGE_OR_INCREASE`.

An `update` mask names exactly the fields given. `type`, `sync_enabled`, and the two subtypes cannot be changed.

The rules come from the reference pages of `ExperimentService`, `MutateExperiments`, `ListExperimentAsyncErrors`,
`Experiment`, `ExperimentOperation`, `MetricGoal`, and the enums for v26. They differ from the research behind
the tools in these points:

- **`response_content_type`.** Not part of the request in v26, so the tools offer none.
- **Operation bound.** The reference states no bound for `operations[]`. The 10 are Qatlas's own, as are the 10
  goals and the 255 characters of the suffix, for which the reference states no length.
- **Status.** The reference calls `status` the advertiser-chosen status. A tool takes `ENABLED`, `HALTED`, and
  `SETUP` only: `REMOVED` is what the remove operation does, so an update with it would bypass the `tools` list that
  `remove` needs; `PROMOTED` and `GRADUATED` belong to actions this provider does not offer here, and `INITIATED`
  is set by the system. Which of the three Google accepts in which state is Google's to validate.
- **Required fields.** Google marks `name` and `type` as required. Which further fields a type needs, for example
  a suffix for a system-managed experiment or a subtype for `OPTIMIZE_ASSETS`, is not stated on the pages read;
  use `validate_only`.

## Errors of the last asynchronous operation

`experiments.list-async-errors` sends one `GET customers/ID/experiments/EXPERIMENT_ID:listExperimentAsyncErrors`
and returns the errors Google recorded for the last scheduling or promotion of the experiment, one page per call.

- **Result.** `errors` holds `code` (the gRPC status code as a number) and `message` per status. `next_cursor` is
  present while more pages follow.
- **Untrusted data.** Messages are provider text: control characters become spaces and each message is cut to 1024
  bytes. The `details` of a status are free-form and are not returned.
- **Paging.** The cursor wraps the page token of Google, bound to the tool, the account, the experiment, and the
  connection. A cursor of another experiment or connection, a forged one, and one longer than the bound is refused
  as `invalid-request` before any request. A page token or a page of Google beyond the bounds (1000 bytes, 1000
  errors) is `invalid-provider-response`.
- **Quota.** One operation of the daily quota, plus the same checks of a manager target as for a change.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts the mask in camelCase and in the order Qatlas sends. No live call has been made.
- The fields and enum values of `OptimizeAssetsExperimentInfo` and `VideoExperimentInfo`: their own reference pages
  were not available; the names come from the field catalog of v26.

Left out on purpose:

- Scheduling, ending, graduating, and promoting experiments. The arms are in the [experiment arm tools](experiment-arms.md).
- `status` `REMOVED`, `PROMOTED`, `GRADUATED`, and `INITIATED`, `partial_failure`, and any free operation, mask, or
  resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the experiment family.

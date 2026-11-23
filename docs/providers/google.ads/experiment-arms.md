---
description: >
  Describes the experiment arm tools of the Google Ads provider: experiment-arms.create, experiment-arms.update, and experiment-arms.remove, with the Mutate service and its bound of 10 operations, the fields checked against the reference of v26, the binding of experiment, arm, campaign, asset group, ad, ad group, and asset to the account by ID, the Performance Max settings that set a budget and a bid, validate_only, confirmation, how update is reached by effect while create and remove need the tools list (create can set a budget and a bid), the unknown outcome, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: experiment arm tools

These tools create, change, and remove the arms of the experiments of a Google Ads account. An arm is one group of an
experiment, the control or a treatment. They add to the read tools of the [Google Ads provider](README.md), whose
configuration, targets, quotas, and errors apply here too, and to the [experiment tools](experiments.md).

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.experiment-arms.create` | create | creates one or more arms of existing experiments |
| `google.ads.experiment-arms.update` | update | changes name, control, traffic split, campaign, asset group, and asset testing of arms |
| `google.ads.experiment-arms.remove` | delete | removes existing arms |

Reading arms is not a tool of this page: run `reports.search` on the `experiment_arm` resource. Scheduling, ending,
graduating, and promoting experiments belong to the [experiment tools](experiments.md).

## How the tools are offered

- **`experiment-arms.update`** is offered by a connection whose effects include `update`, and by a connection whose
  `tools` list names it. It belongs to no profile.
- **`experiment-arms.create` and `experiment-arms.remove`** are offered only through the `tools` list. A connection
  with the `create` or `delete` effect and no `tools` list does not have them. `create` can set a budget and a bid,
  see below.
- **Confirmed per call.** All three tools need the confirmation of that one call. The confirmation text names the
  account, the experiments and arms, and the changed fields, with the texts cut and without control characters.
  For Performance Max settings it names the bidding strategy, the budget, the target CPA, and the target ROAS.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every change does

- **One atomic request.** A call sends one request to `ExperimentArmService.MutateExperimentArms` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 10
  entries. The 10 are Qatlas's own bound; the reference states none.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so
  the answer then holds `validate_only: true` and no `results`. Validation needs the same confirmation and costs the
  same operations as the change.
- **No `response_content_type`.** The tools never ask for the mutable resource, so a result holds `experiment_id`,
  `arm_id`, and `resource_name` only.
- **Objects are given by ID, never by name.** The experiment, the arm, the campaign, the asset group, the ad group,
  the ad, and the assets are IDs of `customer_id`. Qatlas builds `customers/ID/experiments/N`,
  `customers/ID/experimentArms/EXPERIMENT_ID~ARM_ID`, `customers/ID/campaigns/N`, `customers/ID/assetGroups/N`,
  `customers/ID/adGroups/N`, `customers/ID/ads/N`, and `customers/ID/assets/N` from `customer_id` and the checked
  numbers. A resource name, a field mask, or an operation is no argument, and an object of another account cannot
  be named.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not readable
  leaves open whether Google took the change, the error says that the outcome is unknown. Read the arms with
  `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole request and
  say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/experimentArms/N~M` of the `customer_id` of the call. For `update` and `remove` the results must be
  exactly the arms asked for, in the order asked for; a `create` must answer one distinct arm per operation.
  Anything else is `invalid-provider-response` with the note about the unknown outcome, since the change may have
  been made. A `partial_failure_error` in an answer is treated the same way.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`); a customer target that names it is refused as `invalid-request`.

## Arguments

All tools take `customer_id` (ten digits without hyphens), `experiment_arms` (1 to 10 entries), and
`validate_only`.

| Tool | Entry field | Rule |
| --- | --- | --- |
| `create` | `experiment_id` | required, positive number of an existing experiment; Google treats `experiment` as immutable |
| `update`, `remove` | `experiment_id`, `arm_id` | required, positive numbers, as `reports.search` returns them; each arm once per call |
| `create` | `name` | required, 1 to 1024 characters, no control characters; Google requires it to be unique under the experiment |
| `update` | `name` | the same rule, optional |
| `create`, `update` | `control` | true marks the arm the other arms are compared with |
| `create`, `update` | `traffic_split` | 1 to 100; Google requires the two trial arms to total 100, which it checks, so change both arms in one call |
| `create`, `update` | `campaign_id` | one campaign of the account; the reference allows at most one entry of `campaigns` |
| `create`, `update` | `asset_group` | one `asset_group_id`, with optional `asset_group_assets` of 1 to 50 entries of `asset_id` and `field_type`; the reference allows at most one entry of `asset_groups` |
| `create`, `update` | `asset_testing_info` | `asset_variation_infos`, 1 to 50 entries of `base_ad_id`, `base_ad_group_id`, and optional `asset_details` of 1 to 50 entries of `asset_id`, `asset_detail_operation` (`ADD` or `REMOVE`), and `field_type` |
| `create` | `performance_max_settings` | only for a `PMAX_REPLACEMENT_SHOPPING` experiment, immutable afterwards: `bidding_strategy_type`, `budget_amount_micros`, `target_cpa_micros`, `target_roas`, at least one of them |

`field_type` is a value of `AssetFieldType` except `UNSPECIFIED` and `UNKNOWN`. `bidding_strategy_type` is a value
of `BiddingStrategyType` except `UNSPECIFIED`, `UNKNOWN`, `INVALID`, and the deprecated `PAGE_ONE_PROMOTED` and
`TARGET_OUTRANK_SHARE`. The two micros fields are whole positive numbers that fit 64 bits; `target_roas` is above 0
and at most 1000. A `update` names exactly the given fields in its mask; each entry needs at least one.

## Budget and bid settings

`performance_max_settings` sets the budget amount, the target CPA, the target ROAS, and the bidding strategy type of
the experimental Performance Max campaign that Google creates when the experiment is set up. Those fields set a budget
and a bid directly, so `experiment-arms.create` is offered only through the `tools` list, like
`campaign-budgets.create`, and the confirmation of each call names these values. The fields are immutable at
Google, so `experiment-arms.update` offers no such setting and is offered by effect.

## Differences from the research and the reference

- **Operation bound.** The reference states no bound for `operations[]`. The 10 are Qatlas's own, as are the 50 of
  the three nested lists and the upper bound 1000 of `target_roas`.
- **Resource name in `ExperimentArmOperation`.** The page of the operation gives the form of `remove` as
  `customers/{customer_id}/experiments/{campaign_experiment_id}`. The page of `ExperimentArm` gives
  `customers/{customer_id}/experimentArms/{TrialArm.trial_id}~{TrialArm.trial_arm_id}`. The tools follow the page of
  the resource, which also matches the service name; the experiment ID is the `trial_id`.
- **Immutability.** The reference marks `experiment` and `performance_max_experiment_arm_info` immutable and
  no other field. `update` therefore offers the rest; which of them Google accepts after the experiment was scheduled
  is Google's to validate.
- **Required fields.** Only `name` is marked required. Which further fields an experiment type needs, such as an asset
  group for Optimize Assets, is not stated on the pages read; use `validate_only`.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts the mask in camelCase and in the order Qatlas sends. No live call has been made.
- The form of the resource names of `base_ad` (`customers/ID/ads/N`) and of the `asset` fields: the pages read give
  no form; Qatlas uses the form of the resources.
- The enum values and fields of the referenced messages come from their reference pages; the field catalog of v26
  was not compared.

Left out on purpose:

- `partial_failure`, `response_content_type`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the experiment arm family.

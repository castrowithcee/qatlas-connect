---
description: >
  Describes the campaign budget tools of the Google Ads provider that spend money: campaign-budgets.create, campaign-budgets.update, and campaign-budgets.remove, with the tools list requirement, the Mutate service and its operations list, the bound of 50 operations, the checks on amount_micros and total_amount_micros before any request, the rules for period, shared budgets, name, delivery method, type, and network settings against the reference of v26, validate_only, confirmation, the unknown outcome, the binding of resource names to the account, the manager account rule, the difference from account budgets, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-23
---

# Google Ads: campaign budget tools

These tools create, change, and remove the campaign budgets of a Google Ads account. A campaign budget is the
amount that a campaign, or several campaigns that share it, may spend. A budget decides how much money is spent,
so all three tools are offered only through the `tools` list. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-budgets.create` | create | creates one or more campaign budgets |
| `google.ads.campaign-budgets.update` | update | changes name, amount, delivery, alignment, or network settings of existing budgets |
| `google.ads.campaign-budgets.remove` | delete | removes existing budgets |

Reading budgets is not a tool of this page: run `reports.search` on `campaign_budget`. Attaching a budget to a
campaign is done with `campaign_budget` of the [campaign tools](campaigns.md).

Account budgets are another thing. They are the spending limits of a billing setup and only read (see the
[billing tools](billing.md)). No tool of this page creates, changes, or removes an account budget or a proposal
for one, and none of the rules of account budgets applies here.

## How the tools are offered

- **Only through the `tools` list.** A connection gets these tools only when its `tools` list names them, whatever
  its effects are. They belong to no profile.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account, the budgets, and the amounts, with the texts cut and without control characters, and says that the
  budgets decide how much the campaigns using them may spend.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignBudgetService.MutateCampaignBudgets` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50
  entries. The 50 are Qatlas's own bound; the reference of the method states none.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so
  the answer then holds `validate_only: true` and an empty `results`. Validation needs the same confirmation, the
  same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the budget after the change to
  each result. Google returns the mutable members then: `name`, `period`, `type`, `amount_micros`,
  `total_amount_micros`, `delivery_method`, `explicitly_shared`, `aligned_bidding_strategy_id`, and
  `budget_network_settings`. Qatlas also shows `id`, `status`, `reference_count`, `has_recommended_budget`, and
  `recommended_budget_amount_micros` when Google adds them. Without it a result holds `budget_id` and
  `resource_name` only. `remove` has no such argument.
- **Names come from checked IDs.** `update` and `remove` build `customers/ID/campaignBudgets/BUDGET_ID` from
  `customer_id` and `budget_id`. A caller never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the budgets with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/campaignBudgets/N` of the `customer_id` of the call. For `update` and `remove` the results must
  be exactly the budgets asked for, in the order asked for; a `create` must answer one distinct budget per
  operation. A returned budget must have the resource name of its result and, when it has an `id`, the same
  number. Anything else is `invalid-provider-response` with the note about the unknown outcome, since the change
  may have been made. A `partial_failure_error` in an answer is treated the same way, as the request asked for
  none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and code
  in the message, such as `campaignBudgetError`, and the field path in the original. The other codes of the README
  apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Amounts

`amount_micros` and `total_amount_micros` are checked before the secret is read and before any request:

- They must be JSON integers, a whole number of micros of the currency of the account: 1,000,000 micros are one
  unit, so `5000000` is 5 units. A decimal, an exponent, a negative number, a string, and a value above
  9,223,372,036,854,775,807 (the largest `int64`) are refused. So is 0: a budget takes at least 1 micro.
- Qatlas sends the number as the decimal string that the JSON mapping of an `int64` uses, so no digit is lost.
- The reference states neither a minimum nor a multiple of a smallest currency unit. Qatlas invents none, so an
  amount that the currency cannot take is Google's to refuse (use `validate_only` to see it).
- `amount_micros` is the average daily amount of a `DAILY` budget; Google caps the spend of a month at 30.4 times
  it. `total_amount_micros` is the amount over the whole duration of a `CUSTOM_PERIOD` budget. The two exclude
  each other.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `campaign_budgets` | 1 to 50 entries |
| `create` | `campaign_budgets[].name` | 1 to 255 bytes of UTF-8 without the blanks around it, no control characters; required unless `explicitly_shared` is `false` |
| `create` | `campaign_budgets[].period` | `DAILY` (default) or `CUSTOM_PERIOD`; cannot be changed afterwards |
| `create` | `campaign_budgets[].amount_micros` | required for `DAILY`, refused for `CUSTOM_PERIOD` |
| `create` | `campaign_budgets[].total_amount_micros` | required for `CUSTOM_PERIOD`, refused for `DAILY` |
| `create` | `campaign_budgets[].explicitly_shared` | boolean; Google defaults it to `true` |
| `create` | `campaign_budgets[].type` | `STANDARD`, `FIXED_CPA`, `SMART_CAMPAIGN`, or `LOCAL_SERVICES`; cannot be changed afterwards |
| `create`, `update` | `campaign_budgets[].delivery_method` | `STANDARD` (Google's default on create) or `ACCELERATED` |
| `create`, `update` | `campaign_budgets[].aligned_bidding_strategy_id` | positive number, the ID of a portfolio bidding strategy |
| `create`, `update` | `campaign_budgets[].budget_network_settings` | 1 to 12 entries as in `customer-budget-network-settings.create`: one per advertising channel type, with caps of 0 to 100,000 milli-percent and priority tiers |
| `update` | `campaign_budgets` | 1 to 50 entries, each `budget_id` once |
| `update` | `campaign_budgets[].budget_id` | positive number, the `campaign_budget.id` that `reports.search` returns |
| `update` | `campaign_budgets[].name` | 1 to 255 bytes of UTF-8 |
| `update` | `campaign_budgets[].amount_micros`, `total_amount_micros` | at most one of them per entry |
| `update` | `campaign_budgets[].explicitly_shared` | only `true`, and only together with `name` |
| `remove` | `budget_ids` | 1 to 50 distinct budget IDs |
| `create`, `update`, `remove` | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

An `update` entry needs at least one member besides `budget_id`, and the mask Qatlas sends names exactly those:
`name`, `amountMicros`, `totalAmountMicros`, `deliveryMethod`, `explicitlyShared`, `alignedBiddingStrategyId`,
`budgetNetworkSettings`. `budget_network_settings` replaces the whole message: entries that are left out are gone
afterwards, and a budget without settings uses the defaults of
[customer-budget-network-settings](settings.md).

A budget that is explicitly shared needs a name. A non-shared budget can become shared, in an operation that also
names it, but a shared budget never becomes non-shared, so `update` takes `explicitly_shared` only as `true`.
Google syncs the name and the status of a non-shared budget with its campaign. The `period` and the `type` are
immutable, so `update` takes neither. `id`, `status`, `reference_count`, `has_recommended_budget`, the
`recommended_budget_*` members, and `budget_network_eligibility` are output only, so no tool takes them.

The limits are those of the reference pages of `CampaignBudget`, `CampaignBudgetOperation`,
`MutateCampaignBudgetsRequest`, and the enums for v26 at
`https://developers.google.com/google-ads/api/reference/rpc/v26/CampaignBudget` and the pages that it links.
The name limit counts bytes, as the reference states.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts the mask in camelCase and in the order Qatlas sends, and what a budget without
  `delivery_method` or `explicitly_shared` gets beyond the defaults the reference names. No live call has been made.
- A minimum amount, or a multiple of the smallest unit of a currency, for `amount_micros`. The reference states
  none, so Google answers it with a `campaignBudgetError` when it applies.
- Which `type` fits which campaign. Google names `FIXED_CPA` for Display campaigns with Target CPA, `SMART_CAMPAIGN`
  for Smart campaigns, and `LOCAL_SERVICES` for Local Services campaigns; Google validates the pairing when a
  campaign uses the budget, not Qatlas. Google no longer creates Smart campaigns.
- Whether Google refuses to remove a budget that campaigns still use, and what happens to them.
- The number of budgets an account may hold, which the pages read do not state.
- The members of `budget_network_settings` that Google refuses for a channel type, and the account allowlist for
  network settings.

Left out on purpose:

- `budget_network_eligibility`, and the `recommended_budget_estimated_change_*` members: output only, read them
  with `reports.search` on `campaign_budget`.
- Account budgets and their proposals, which are only read, see the [billing tools](billing.md).
- `partial_failure`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the campaign budget family.

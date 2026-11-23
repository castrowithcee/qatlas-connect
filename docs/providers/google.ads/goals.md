---
description: >
  Describes the goal tools of the Google Ads provider: goals.create and update and campaign-goal-configs.create, update and remove, with the tools list requirement and confirmation, up to 10 entries in one atomic request without partial_failure, validate_only, the goal types and settings that are checked, the resource names built from customer_id and numeric IDs, the effect on bidding, why remove needs the tools list, the manager account rule, the unknown outcome, and where the v26 reference is silent and what is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: goal tools

These tools create and change the customer lifecycle goals of a Google Ads account (retention, loyalty retention,
new customer acquisition) and attach them to, change them on, and detach them from campaigns. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too. They follow the
rules of the [conversion action tools](conversions.md) and the [settings tools](settings.md): a connection offers
them only when its `tools` list names each one, every call needs a confirmation, the request is sent once and
never repeated, and an answer or a failure that leaves open whether Google took the change says that the outcome
is unknown. This page names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.goals.create` | create | creates one to 10 goals |
| `google.ads.goals.update` | update | replaces the value settings of one to 10 goals |
| `google.ads.campaign-goal-configs.create` | create | attaches one to 10 goals to campaigns |
| `google.ads.campaign-goal-configs.update` | update | changes the settings of one to 10 campaign goal configs |
| `google.ads.campaign-goal-configs.remove` | delete | detaches one to 10 goals from campaigns |

None belongs to a profile. Why all five need the `tools` list, as the conversion action tools do: a goal and its
assignment to a campaign decide what the campaign optimizes and bids for. The value settings change the
conversion value that bidding sees for customers of a lifecycle, and an assignment or a removal changes the
objective of a running campaign, so each can move spend. The effect profile alone is too broad for that.
`remove` is the destructive one: the campaign stops optimizing for the goal, and a recreated config starts again
from its settings. The goal itself stays. Goals have no remove method in the API.

Goals and configs are read with `reports.search` on `goal` and `campaign_goal_config`; no list tool exists.

## Request

`goals.*` call `GoalService.MutateGoals`, the others `CampaignGoalConfigService.MutateCampaignGoalConfigs`, once,
with `operations[]` and never with `partial_failure`: Google applies all operations or none. The reference of v26
lists `partial_failure` for both services, so the atomic form is a choice of Qatlas here, not a limit of Google.
At most 10 entries go in one call (a limit of Qatlas; the reference names none). `validate_only` is an argument
of all five: Google then validates and answers errors only, and the answer has an empty `results`. The
confirmation is required also then. Neither request has `response_content_type`, and a result holds the resource
name only, so the tools return `resource_name` and the numeric IDs and no object.

`customer_id` is checked against the targets before a secret is read. On a manager account a manager target must
name the account; the rule and its extra read are those of the [user access tools](user-access.md). Resource
names are built from the checked `customer_id` and numeric IDs: `campaign_id`, `goal_id`, and `goal_id` of an
update are numbers, and no argument takes a resource name. A campaign or a goal of another account cannot be
named, which includes a goal owned by a manager account and used by a client account (`Goal.owner_customer`
names an owner, and the reference does not say whether a config may point to a goal of another account). Every
name in an answer must be of the account and the one asked for at the same position; otherwise the call ends as
`invalid-provider-response` with the note that the outcome is unknown.

## Goals

`goal_type` is `CUSTOMER_RETENTION`, `LOYALTY_RETENTION`, or `NEW_CUSTOMER_ACQUISITION`. The type of a goal is
output only in the API and follows from the settings member that carries it, so the tool maps `goal_type` to
`retention_goal_settings`, `loyalty_retention_goal_settings`, or `new_customer_acquisition_goal_settings`. It
cannot change later. `create` needs `goal_type` and may carry `value_settings`; `update` needs `goal_id`,
`goal_type`, and `value_settings`, replaces `<settings>.value_settings` as a whole (mask of that one path), and
never clears it. Each ID may appear once per call.

`value_settings` (`CustomerLifecycleOptimizationValueSettings`) holds two pairs, each with at most one member:

| Pair | Members |
| --- | --- |
| value adjustment for customers of the lifecycle | `additional_value` or `value_multiplier` |
| value adjustment for customers with high lifetime value | `additional_high_lifetime_value` or `high_lifetime_value_multiplier` |

At least one member is needed. The numbers must be finite and 0 or more: the reference gives no range, so the
non-negative bound is Qatlas's own. The reference says the high lifetime value must be greater than the value
when both are set; Qatlas does not compare them (the members may differ in kind) and leaves it to Google.

## Campaign goal configs

`create` needs `campaign_id`, `goal_id`, and `goal_type` (the type of that goal, to choose the settings member:
`campaign_retention_settings`, `campaign_loyalty_retention_settings`, or
`campaign_new_customer_acquisition_settings`), and may carry settings. `update` needs the three and at least one
setting; its mask holds exactly the given settings, prefixed with the settings member. `remove` takes the pair
`campaign_id` and `goal_id` only and sends `customers/ID/campaignGoalConfigs/CAMPAIGN~GOAL`. A pair may appear once per call. The
campaign and the goal are immutable.

| Setting | Goal types | Check in Qatlas |
| --- | --- | --- |
| `target_option` | `CUSTOMER_RETENTION`, `NEW_CUSTOMER_ACQUISITION` | `TARGET_ALL` or `TARGET_SPECIFIC`; Google lets only allowlisted accounts set it for the retention goal |
| `enable_bid_adjustments_for_loyalty_members` | `LOYALTY_RETENTION` | boolean |
| `show_targeted_loyalty_member_benefits_in_pla` | `LOYALTY_RETENTION` | boolean |
| `value_settings_override` | all | as `value_settings` above, replaced as a whole |

Settings that do not fit the `goal_type` are refused before any request. Settings are set, never cleared.

## Boundary

Checked against the v26 pages of both services, their request and operation messages, `Goal`, `CampaignGoalConfig`,
the settings messages, and the enums for the goal type and the lifecycle optimization mode. The reference marks no
field as required and gives no ranges. The values `UNSPECIFIED` and `UNKNOWN` are never sent. Output-only fields
(`goal_id`, `goal_type`, `optimization_eligibility`, `owner_customer`, and the `goal_type` of a config) are not
arguments; `optimization_eligibility` is readable with `reports.search`.

Not verified, and no live call has been made:

- Which accounts may use these services. They are new in recent API versions, and the reference says nothing about
  an allowlist except for `target_option` of the retention settings; other `GoalError` and
  `CampaignGoalConfigError` outcomes reach the caller as provider errors.
- Which campaign types accept which goal, how many goals a campaign or an account may hold, and what Google does with
  a goal whose `optimization_eligibility` is `INELIGIBLE`.
- Whether Google answers `validate_only` always without results; if it sends results, they are checked like real ones.
- Whether Google accepts the camelCase mask paths and an empty settings message on create.
- The role an account needs to change goals.

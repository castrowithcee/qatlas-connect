---
description: >
  Describes the custom conversion goal tools of the Google Ads provider: custom-conversion-goals.create, update and remove, with the tools list requirement and confirmation, up to 10 entries in one atomic request without partial_failure, validate_only, response_content_type for create and update, the name and conversion_actions settings and their checks, conversion actions that must belong to the account, the status that is not offered, the effect on bidding, the manager account rule, the unknown outcome, and what the v26 reference does not state.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: custom conversion goal tools

These tools create, change, and remove the custom conversion goals of a Google Ads account. A custom conversion
goal bundles conversion actions. They add to the [Google Ads provider](README.md), whose configuration, targets,
quotas, and errors apply here too, and follow the rules of the [conversion action tools](conversions.md): a
connection offers them only when its `tools` list names each one, every call needs a confirmation, the request is
sent once and never repeated, and an answer or a failure that leaves open whether Google took the change says
that the outcome is unknown. This page names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.custom-conversion-goals.create` | create | creates one to 10 custom conversion goals |
| `google.ads.custom-conversion-goals.update` | update | changes name or conversion actions of one to 10 goals |
| `google.ads.custom-conversion-goals.remove` | delete | removes one to 10 goals |

None belongs to a profile, for the reason of the conversion actions: a custom goal decides what the campaigns that
use it optimize for, so even a rename-looking `update` that swaps its conversion actions can move spend, and
`remove` takes the goal away as a bidding signal from every campaign that uses it. `remove` is destructive.
Custom conversion goals are read with `reports.search` on `custom_conversion_goal`; no list tool exists.

## Request

All three call `CustomConversionGoalService.MutateCustomConversionGoals` once with `operations[]`. The service has
no `partial_failure` field in v26, so the request is atomic by nature and the tool sends none. At most 10 entries
go in one call (a limit of Qatlas, not of Google), and the schema and the handler refuse more. `validate_only`
is an argument of all three: Google then validates and answers errors only, and `results` is empty. The
confirmation is required also then. `create` and `update` take `response_content_type`; with `MUTABLE_RESOURCE`
each result holds the goal as Google returns it. `remove` takes none because its answer is the resource name only.

`customer_id` is checked against the targets before a secret is read. On a manager account a manager target must
name the account; the rule and its extra read are those of the [user access tools](user-access.md). Resource
names of goals are built from the checked `customer_id` and numeric IDs. Every name in an answer must be of the
account and, for `update` and `remove`, the one asked for at the same position; otherwise the call ends as
`invalid-provider-response` with the note that the outcome is unknown.

## Settings

`create` needs `name` and `conversion_actions` per goal. `update` needs `custom_conversion_goal_id` and at least
one of them; its mask holds exactly the given settings. Each ID may appear once per call.

| Setting | Check in Qatlas |
| --- | --- |
| `name` | 1 to 200 characters, not blank, no control characters; Google checks duplicates and its own rules |
| `conversion_actions` | one to 50 resource names `customers/ID/conversionActions/ID`, each once, all of the checked `customer_id`; on an update they replace the whole list |

A conversion action of another account is refused before any request. Whether an action may be part of a goal
(its category, its origin) is Google's to validate.

## Left out on purpose

- **`status`.** `ENABLED` is the only value worth sending and `REMOVED` is what `remove` does, so no argument
  offers it. It is returned in `MUTABLE_RESOURCE` answers.
- `partial_failure` (the service has none), resource names of goals as arguments, and `login_customer_id` as an
  argument.
- The assignment of goals to campaigns and customers (`campaign-conversion-goals`, `customer-conversion-goals`).

## Where the reference differs

The v26 reference of `CustomConversionGoalService`, `CustomConversionGoal`, and the mutate messages was read:

- The mutate request has `customer_id`, `operations[]`, `response_content_type`, and `validate_only`, and no
  `partial_failure`. Both the matrix of the survey and the reference agree on that.
- The resource has `resource_name`, `id` (immutable), `name`, `status`, and `conversion_actions[]`. The reference
  marks no field as required and names no limit of operations, of name length, or of conversion actions per goal;
  the bounds of 10, 200, and 50 are chosen here.
- The status enum has `ENABLED` and `REMOVED`.

## Boundary

Not verified:

- The maximum length of a name, the number of conversion actions of a goal, and the number of goals of an account.
- Whether Google refuses to remove a goal that a campaign still uses.
- Whether Google accepts a goal whose conversion actions have different categories.
- The role an account needs to change custom conversion goals. No live call has been made.

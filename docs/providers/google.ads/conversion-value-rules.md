---
description: >
  Describes the conversion value rule tools of the Google Ads provider: conversion-value-rules.create, update and remove, with the tools list requirement and confirmation, up to 10 entries in one atomic request without partial_failure, validate_only, the action and the four conditions that can be set, how resource names in the conditions are bound to the account, why updates replace whole conditions, the effect on bidding, the manager account rule, the unknown outcome, and what the reference of v26 does not state and is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: conversion value rule tools

These tools create, change, and remove the conversion value rules of a Google Ads account. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too. They follow the
rules of the [conversion action tools](conversions.md) and the [settings tools](settings.md): a connection offers
them only when its `tools` list names each one, every call needs a confirmation, the request is sent once and never
repeated, and an answer or a failure that leaves open whether Google took the change says that the outcome is
unknown. This page names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.conversion-value-rules.create` | create | creates one to 10 conversion value rules |
| `google.ads.conversion-value-rules.update` | update | changes settings of one to 10 conversion value rules |
| `google.ads.conversion-value-rules.remove` | delete | removes one to 10 conversion value rules |

None belongs to a profile. A rule adds a value to, multiplies, or sets the value of a conversion when its
conditions hold, and Smart Bidding optimizes for that value, so any of the three tools, `remove` and a harmless
looking `update` of `status` included, can move spend. For this reason all three are offered by the `tools` list
only, as the conversion action tools are. `remove` deletes for good: Google marks the rule `REMOVED`, it cannot be
applied or enabled again, and conversions are valued without it.

Rules are read with `reports.search` on `conversion_value_rule`; no list tool exists.

## Request

All three call `ConversionValueRuleService.MutateConversionValueRules` once, with `operations[]`, never with
`partial_failure`: Google applies all operations or none. At most 10 entries go in one call (a limit of Qatlas,
not of Google), and the schema and the handler refuse more. `validate_only` is an argument of all three: Google
then validates and answers errors only, and the answer has an empty `results`. The confirmation is required also
then. `create` and `update` take `response_content_type`; with `MUTABLE_RESOURCE` each result holds the rule as
Google returns it. `remove` answers resource names only and takes none.

`customer_id` is checked against the targets before a secret is read. On a manager account a manager target must
name the account (rules can belong to a manager account and are inherited by its clients); the rule and its extra
read are those of the [user access tools](user-access.md). Rule names are built from the checked `customer_id` and
numeric IDs; no argument takes the resource name of a rule. Every name in an answer must be of the account and,
for `update` and `remove`, the one asked for at the same position; otherwise the call ends as
`invalid-provider-response` with the note that the outcome is unknown. The same holds for `owner_customer` and
for every user list, user interest, and geo target constant inside a returned rule.

## Settings

`create` needs an `action` per rule. `update` needs `conversion_value_rule_id` and at least one setting; its mask
holds exactly the given top-level settings. Each ID may appear once per call. A setting is set, never cleared.

| Setting | Check in Qatlas |
| --- | --- |
| `action` | `operation` `ADD`, `MULTIPLY`, or `SET` and a finite number `value`; both are required, and an update replaces the whole action |
| `status` | `ENABLED` or `PAUSED`; `REMOVED` only through `remove` |
| `geo_location_condition` | `geo_target_constants` and `excluded_geo_target_constants` as `geoTargetConstants/N`, each up to 100 and without repeats, `geo_match_type` and `excluded_geo_match_type` of `ANY` or `LOCATION_OF_PRESENCE` |
| `device_condition` | `device_types` of `MOBILE`, `DESKTOP`, `TABLET` |
| `audience_condition` | `user_lists` as `customers/ID/userLists/N` and `user_interests` as `customers/ID/userInterests/N`, each up to 100 and without repeats |
| `itinerary_condition` | `advance_booking_window` (`min_days`, `max_days`), `travel_length` (`min_nights`, `max_nights`), `travel_start_day` (seven booleans), whole numbers of 0 to 2147483647 |

A condition that is given must name at least one setting and replaces the whole condition of the rule; it cannot
be cleared. Resource names in the conditions must belong to `customer_id`, which Qatlas checks before any
request. Geo target constants are constants of Google and are checked for their form only. Which conditions a
rule must carry, which accounts may use the itinerary condition (a travel feature), and whether a combination
is valid is Google's to decide.

## Left out on purpose

- **Read-only output fields.** `id` and `owner_customer` are returned when Google sends them; no argument sets
  them, and `resource_name` is not an argument.
- `partial_failure`, free operations, and `login_customer_id` as an argument.
- Rule sets, which attach rules to campaigns or the account, belong to other tools.

## Where the reference differs

Checked against the v26 proto of `ConversionValueRule`, `ConversionValueRuleService`, and the enums
`ConversionValueRuleStatus`, `ValueRuleOperation`, `ValueRuleDeviceType`, and `ValueRuleGeoLocationMatchType`:

- The enum values and fields above are the ones of the reference. The status enum also has `REMOVED`, which only
  `remove` reaches.
- The reference states no range for `value`, no limit for the lists of a condition, no limit of operations per
  request for this service, and no range for the itinerary numbers. The caps of 10 entries and 100 names per list
  and the non-negative itinerary numbers are chosen here and are narrower than the reference.
- The proto was read from the public googleapis repository, not from the rendered reference pages, which show the
  fields but not all nested values.

## Boundary

Not verified:

- Whether Google accepts the mask paths in camelCase, and whether a value of 0 or a negative `ADD` value is valid
  for each operation.
- Which role an account needs to change rules, and whether a client account may change a rule inherited from a
  manager. No live call has been made.
- Whether a `validate_only` answer is always empty; if Google sends results, they are checked like real ones.

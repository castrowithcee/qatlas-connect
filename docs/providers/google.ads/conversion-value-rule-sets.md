---
description: >
  Describes the conversion value rule set tools of the Google Ads provider: conversion-value-rule-sets.create, update and remove, with the tools list requirement and confirmation, up to 10 entries in one atomic request without partial_failure, validate_only, attachment to the account or one campaign, the immutable attachment type and categories, why status cannot be given, how rules and campaign are bound to the account, the effect on bidding, the manager account rule, the unknown outcome, and what the reference of v26 does not state and is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: conversion value rule set tools

These tools create, change, and remove the conversion value rule sets of a Google Ads account. A set attaches
[conversion value rules](conversion-value-rules.md) to the account or to one campaign. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and follow the
rules of the [conversion value rule tools](conversion-value-rules.md): a connection offers them only when its
`tools` list names each one, every call needs a confirmation, the request is sent once and never repeated, and an
answer or a failure that leaves open whether Google took the change says that the outcome is unknown. This page
names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.conversion-value-rule-sets.create` | create | creates one to 10 conversion value rule sets |
| `google.ads.conversion-value-rule-sets.update` | update | changes settings of one to 10 conversion value rule sets |
| `google.ads.conversion-value-rule-sets.remove` | delete | removes one to 10 conversion value rule sets |

None belongs to a profile. A set decides which rules apply, and Smart Bidding optimizes for the adjusted value, so
any of the three tools can move spend. All three are offered by the `tools` list only. `remove` deletes for good.

Sets are read with `reports.search` on `conversion_value_rule_set`; no list tool exists.

## Request

All three call `ConversionValueRuleSetService.MutateConversionValueRuleSets` once, with `operations[]`, never with
`partial_failure`: Google applies all operations or none. At most 10 entries go in one call (a limit of Qatlas,
not of Google). `validate_only` is an argument of all three; the answer then has an empty `results`, and the
confirmation is required also then. `create` and `update` take `response_content_type`; with `MUTABLE_RESOURCE`
each result holds the set as Google returns it. `remove` answers resource names only and takes none.

`customer_id` is checked against the targets before a secret is read. On a manager account a manager target must
name the account (sets can belong to a manager account and are inherited by its clients). Set names are built from
the checked `customer_id` and numeric IDs. Every rule and the campaign in an argument must be a resource of
`customer_id`, or the call is refused before any request. Every name in an answer must be of the account and,
for `update` and `remove`, the one asked for at the same position; otherwise the call ends as
`invalid-provider-response` with the note that the outcome is unknown. The same holds for `owner_customer`, the
campaign, and every rule inside a returned set.

## Settings

| Setting | Tool | Check in Qatlas |
| --- | --- | --- |
| `attachment_type` | create, required | `CUSTOMER` (the account) or `CAMPAIGN`; immutable |
| `campaign` | create, update | `customers/ID/campaigns/N`; needed with `CAMPAIGN` and refused with `CUSTOMER` on create |
| `conversion_action_categories` | create | categories of the conversion actions, without repeats; immutable |
| `conversion_value_rules` | create, update | `customers/ID/conversionValueRules/N`, up to 100, without repeats |
| `dimensions` | create, update | `AUDIENCE`, `DEVICE`, `GEO_LOCATION`, `ITINERARY`, `NO_CONDITION`, without repeats; the first is the primary dimension |

`update` needs `conversion_value_rule_set_id` and at least one of `campaign`, `conversion_value_rules`,
`dimensions`; its mask holds exactly the given settings. Each ID may appear once per call. A list that is given
replaces the whole list, and a setting is set, never cleared. Whether the rules fit the dimensions, and which
lists a set must carry, is Google's to decide.

## Left out on purpose

- **`status`.** The v26 reference marks it output only, so no tool takes it; a set cannot be paused or enabled
  through these tools, and is removed only with `remove`. It is returned when Google sends it.
- `attachment_type` and `conversion_action_categories` in `update`, because the reference marks both immutable.
- `id`, `owner_customer`, and `resource_name` as arguments, `partial_failure`, free operations, and
  `login_customer_id` as an argument.

## Where the reference differs

Checked against the rendered v26 pages of `ConversionValueRuleSet`, `MutateConversionValueRuleSetsRequest`,
`ConversionValueRuleSetOperation`, and `MutateConversionValueRuleSetResult`:

- The fields, the enums `ValueRuleSetAttachmentType` (`CUSTOMER`, `CAMPAIGN`), `ValueRuleSetDimension` and the
  immutability of `attachment_type` and `conversion_action_categories` are those of the reference. `UNKNOWN` and
  `UNSPECIFIED` are not offered.
- No field is marked required, and the reference states no limit for the lists or the operations per request.
  The cap of 10 entries, the cap of 100 rules, the required `attachment_type`, and the pairing of `campaign` with
  `CAMPAIGN` are chosen here and are narrower than the reference.
- The `ConversionValueRuleSetStatus` enum has `REMOVED`, which only `remove` reaches.

## Boundary

Not verified:

- Whether Google accepts the mask paths in camelCase, and whether `campaign` can be changed after creation.
- Which role an account needs to change sets, and whether a client account may change a set inherited from a
  manager. No live call has been made.
- Whether a `validate_only` answer is always empty; if Google sends results, they are checked like real ones.

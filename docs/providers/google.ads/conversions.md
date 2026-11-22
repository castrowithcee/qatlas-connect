---
description: >
  Describes the conversion action tools of the Google Ads provider: conversion-actions.create, update and remove, with the tools list requirement and confirmation, up to 20 operations in one atomic request without partial_failure, validate_only, the settings and value ranges that are checked, the conversion types that are offered and the ones left out, the fields that are read-only or not offered, the effect on bidding, the manager account rule, the unknown outcome, and where the reference of v26 differs from the summaries and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: conversion action tools

These tools create, change, and remove the conversion actions of a Google Ads account. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too. They follow the
rules of the [settings tools](settings.md): a connection offers them only when its `tools` list names each one,
every call needs a confirmation, the request is sent once and never repeated, and an answer or a failure that
leaves open whether Google took the change says that the outcome is unknown. This page names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.conversion-actions.create` | create | creates one to 20 conversion actions |
| `google.ads.conversion-actions.update` | update | changes settings of one to 20 conversion actions |
| `google.ads.conversion-actions.remove` | delete | removes one to 20 conversion actions |

None belongs to a profile. A conversion action decides what campaigns optimize for: one that is primary for its
goal feeds the bidding of every campaign that uses the goal, so even a harmless-looking `update` can move spend.
The confirmation names the account, the actions, and, for an update, `primary_for_goal` and `status`.

Conversion actions are read with `reports.search` on `conversion_action`; no list tool exists.

## Request

All three call `ConversionActionService.MutateConversionActions` once, with `operations[]`, never with
`partial_failure`: Google applies all operations or none. At most 20 operations go in one call (a limit of
Qatlas, not of Google, which allows many more), and the schema and the handler refuse more. `validate_only` is an
argument of all three: Google then validates and answers errors only, and the answer has an empty `results`. The
confirmation is required also then. `create` and `update` take `response_content_type`; with `MUTABLE_RESOURCE`
each result holds the conversion action as Google returns it. `remove` answers resource names only and takes no
`response_content_type`.

`customer_id` is checked against the targets before a secret is read. On a manager account a manager target must
name the account (conversion actions can belong to a manager account); the rule and its extra read are those of
the [user access tools](user-access.md). Resource names are built from the checked `customer_id` and numeric
IDs. No argument takes a resource name. Every name in an answer must be of the account and, for `update` and
`remove`, the one asked for at the same position; otherwise the call ends as `invalid-provider-response` with
the note that the outcome is unknown.

## Settings

`create` needs `name` and `type` per action. `update` needs `conversion_action_id` and at least one setting; its
mask holds exactly the given settings, nested ones by path (`valueSettings.defaultValue`). A setting is set,
never cleared. Each ID may appear once per call.

| Setting | Check in Qatlas |
| --- | --- |
| `name` | 1 to 200 characters, not blank, no control characters; Google checks duplicates and its own length limit |
| `type` (create only, immutable) | `WEBPAGE`, `UPLOAD_CLICKS`, `UPLOAD_CALLS`, `AD_CALL`, `WEBSITE_CALL` |
| `status` | `ENABLED` or `HIDDEN`; `REMOVED` only through `remove` |
| `category` | a `ConversionActionCategory` of v26 without `UNSPECIFIED` and `UNKNOWN` |
| `primary_for_goal` | boolean; `false` makes the action non-biddable for the goals of all campaigns (not for custom conversion goals) |
| `click_through_lookback_window_days` | 1 to 90, 1 to 60 for `AD_CALL` and `WEBSITE_CALL` (checked on create) |
| `view_through_lookback_window_days` | 1 to 30, not for `AD_CALL` and `WEBSITE_CALL` (checked on create) |
| `value_settings` | `default_value` of 0 or more, `default_currency_code` as three capital letters, `always_use_default_value` |
| `counting_type` | `ONE_PER_CLICK` or `MANY_PER_CLICK` |
| `attribution_model` | `GOOGLE_ADS_LAST_CLICK` or `GOOGLE_SEARCH_ATTRIBUTION_DATA_DRIVEN` |
| `phone_call_duration_seconds` | 0 to 10000 |

On `update` the type of the action is not known to Qatlas, so Google validates the lookback windows of call
types there.

## Left out on purpose

- **Types that bind the account to an app or a foreign analytics platform.** The types for Firebase, third-party
  app analytics, Google Play, and other app conversions are not offered, and neither is `app_id`: they link
  the account to a third party, and the policy of the provider is that no tool opens such a link. The same holds
  for `STORE_SALES_DIRECT_UPLOAD` (allowlisted store data upload), `LOCAL_SERVICES_ADS`, the deprecated
  `SALESFORCE`, and Universal Analytics. Types Google documents as read-only (Google Analytics 4, Floodlight,
  Search Ads 360, lead form, store visits, smart campaign, codeless web, and the others) are read-only here too.
  Only the types of the first table are created.
- **Read-only output fields.** `id`, `origin`, `owner_customer`, `mobile_app_vendor`, `tag_snippets`,
  `data_driven_model_status`, and the Firebase, third-party app analytics, and Google Analytics 4 settings are
  output only. They are returned as Google sends them (the Mutate answer holds mutable fields only, so they
  appear rarely there); no argument sets them. `include_in_conversions_metric` is read-only in practice, see
  below, and is not an argument either.
- **Rule-based and external attribution models.** First click, linear, time decay, position based, and
  `EXTERNAL` are not offered.
- `partial_failure`, free operations, resource names as arguments, and `login_customer_id` as an argument.
- Uploads of conversions and everything else of the conversion groups (value rules, goals, custom variables);
  they belong to other tools.

## Where the reference differs

Checked against the v26 reference of `ConversionAction` and the page on creating conversion actions:

- **`include_in_conversions_metric`.** The v26 resource does not mark it output only, but the guide says that
  setting it in a create or update fails with `FieldError.IMMUTABLE_FIELD`; `primary_for_goal` is the way to
  take an action out of bidding. The tool does not offer the field. The summary that rated it as a setting
  does not hold.
- **Lookback windows.** The resource gives no range. The guide gives 1 to 90 days (1 to 60 for call types) and
  1 to 30 days for view-through (unset for call types, else `VALUE_MUST_BE_UNSET`). Qatlas applies these.
- **Attribution models.** The guide names the data-driven and the last-click model and says that rule-based ones
  end as `CANNOT_SET_RULE_BASED_ATTRIBUTION_MODELS`. Only those two are offered.
- **`primary_for_goal` on create.** The proto comment still says that it can be set to false only after
  creation (a statement for v9); the field is `optional` in v26. Qatlas sends it on create and leaves the answer
  to Google.
- **Mutate limit.** The reference names no limit of operations per request for this service; the cap of 20
  is chosen here.

## Boundary

Not verified:

- Which of the types of the first table an account may create, and whether `UPLOAD_CLICKS` and `UPLOAD_CALLS`
  stay creatable for projects without earlier upload use; Google restricts the upload services for new
  projects, and the reference does not say whether the action itself is affected.
- The maximum length of a name, the number of conversion actions an account may hold
  (`ResourceCountLimitExceededError` is possible), and the rule for a negative `default_value`: Qatlas
  refuses a negative one, which is narrower than the reference.
- Whether a `validate_only` answer is always empty; if Google sends results, they are checked like real ones.
- The role an account needs to change conversion actions. No live call has been made.
- Whether Google accepts the mask paths in camelCase.

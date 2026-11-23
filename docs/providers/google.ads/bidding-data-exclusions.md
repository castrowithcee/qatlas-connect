---
description: >
  Describes the bidding data exclusion tools of the Google Ads provider: bidding-data-exclusions.create, update and remove, all offered only through the tools list, with BiddingDataExclusionService.MutateBiddingDataExclusions, the bound of 10 operations, the rules for scope, campaigns and channel types, the time format and the 14 day interval, devices, the effect on automated bidding, validate_only, confirmation, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-23
---

# Google Ads: bidding data exclusion tools

These tools create, change, and remove the bidding data exclusions of a Google Ads account. An exclusion tells the
automated bidding strategies to ignore the data of a past period, such as a tracking outage, when they learn. It
changes how those strategies bid and so how an account spends. The tools add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.bidding-data-exclusions.create` | create | creates one or more exclusions |
| `google.ads.bidding-data-exclusions.update` | update | changes fields of existing exclusions |
| `google.ads.bidding-data-exclusions.remove` | delete | removes existing exclusions |

Reading exclusions is not a tool of this page: run `reports.search` on `bidding_data_exclusion`. Seasonality
adjustments are a different resource and not part of these tools.

## How the tools are offered

- **Only through the `tools` list.** A connection offers all three tools only when its `tools` list names them,
  whatever its effects are. They belong to no profile: the `read` profile holds none of them.
- **Confirmed per call.** Every call needs the confirmation of that one call. The confirmation text names the
  account, the scope, the period, the devices, and the campaigns or channels, with texts cut and without control
  characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `BiddingDataExclusionService.MutateBiddingDataExclusions`
  with an `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1
  to 10 entries. The 10 are Qatlas's own bound; Google accepts up to 10,000 mutate operations per request.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and an empty `results`. Validation needs the same
  confirmation, the same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the exclusion after the change to
  each result. Without it a result holds `data_exclusion_id` and `resource_name` only. `remove` has no such
  argument.
- **Names come from checked IDs.** `update` and `remove` build `customers/ID/biddingDataExclusions/N` from
  `customer_id` and `data_exclusion_id`. Campaigns are given as `campaign_ids`, and Qatlas builds
  `customers/ID/campaigns/N` from the checked `customer_id`. A caller never passes a resource name, a field mask,
  or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or
  not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the
  exclusions with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/biddingDataExclusions/N` of the `customer_id` of the call. For `update` and `remove` the results
  must be exactly the exclusions asked for, in the order asked for; a `create` must answer one distinct exclusion
  per operation. A returned exclusion must have the resource name of its result, when it has an ID the same
  number, and every campaign it names must be a campaign of the account. Anything else is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made. A
  `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target,
  are refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create`, `update` | `data_exclusions` | 1 to 10 entries |
| `remove` | `data_exclusion_ids` | 1 to 10 distinct IDs of existing exclusions |
| `update` | `data_exclusions[].data_exclusion_id` | positive number, each ID once, as `reports.search` returns it |
| `create` | `data_exclusions[].scope` | required: `CUSTOMER`, `CAMPAIGN`, or `CHANNEL` |
| `create` | `data_exclusions[].start_date_time`, `end_date_time` | required, `yyyy-MM-dd HH:mm:ss` |
| `create`, `update` | `data_exclusions[].name` | 1 to 255 characters |
| `create`, `update` | `data_exclusions[].description` | at most 2048 characters; for `update`, an empty text clears it |
| `create`, `update` | `data_exclusions[].devices` | `MOBILE`, `TABLET`, `DESKTOP`, `CONNECTED_TV`, `OTHER`, each once; none means all devices; for `update`, an empty list clears it |
| `create`, `update` | `data_exclusions[].campaign_ids` | 1 to 2000 distinct campaign IDs of the account, for the scope `CAMPAIGN` |
| `create`, `update` | `data_exclusions[].advertising_channel_types` | `DISPLAY`, `SEARCH`, or `SHOPPING`, each once, for the scope `CHANNEL` |
| all | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

The limits are those of the reference of `BiddingDataExclusion` and `BiddingDataExclusionService` for v26.

- **Period.** An exclusion looks backward: it is for events that started in the past and end in the past or the
  future. The start is inclusive and the end is exclusive. The interval from start to end must be longer than zero
  and at most 14 days. Qatlas checks the format and, when a call gives both times, the interval, counting the
  wall-clock difference of the two values; the values are in the time zone of the account. An `update` that gives
  only one of the times leaves the interval to Google, which checks it against the stored other time. Whether
  Google accepts a start that lies in the future is not stated; Google decides.
- **Scope, campaigns, and channels.** `campaign_ids` apply for the scope `CAMPAIGN`, `advertising_channel_types`
  for the scope `CHANNEL`, and Google does not support both in one exclusion. Qatlas refuses both together, a
  list that does not fit the scope, and on `create` a scope `CAMPAIGN` without campaigns or a scope `CHANNEL`
  without channel types. Only `DISPLAY`, `SEARCH`, and `SHOPPING` are supported channel types, so Qatlas offers
  no other. A scope is required on `create` because an unspecified scope names nothing; the reference does not
  mark it required.
- **Status and name.** The `status` of an exclusion is output only in the reference, so no tool takes it; a
  removed exclusion shows `REMOVED`. The resource name is immutable and is never an argument.
- **Update.** The mask Qatlas sends names exactly the fields given, in camelCase. An `update` needs at least one
  field besides `data_exclusion_id`, and a list of campaigns or channel types that is given must not be empty. An
  `update` that changes the scope does not clear the list of the old scope; whether Google does is not stated.

Text must not hold control characters. Whether the length is counted in characters or bytes is not stated;
Qatlas counts characters (code points), which cannot refuse a text Google accepts. Which exclusions Google
accepts beyond these limits, for example an overlap with another exclusion or a campaign of a channel type that
supports no exclusion, is Google's to validate. Use `validate_only` to see it.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts the mask in camelCase and in the order Qatlas sends, an update that sets an empty
  description or an empty devices list, and a start that lies in the future. No live call has been made.
- The limit on the number of exclusions of an account, which the pages read do not state.
- How the interval is counted across a change of daylight saving time in the time zone of the account.

Left out on purpose:

- Seasonality adjustments (`BiddingSeasonalityAdjustment`) and reading exclusions.
- `status` of an exclusion, `partial_failure`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the exclusion family.

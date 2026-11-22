---
description: >
  Describes the bidding seasonality adjustment tools of the Google Ads provider: bidding-seasonality-adjustments.create, update, and remove for up to ten adjustments per call, with the tools list requirement, the effect on Smart Bidding and spend, the members offered and left out on purpose, the checks on scope, period, conversion rate modifier, campaigns, channel types, and devices against the reference of v26, field masks and clearing, validate_only, confirmation, the atomic request, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: bidding seasonality adjustment tools

These tools create, change, and remove the bidding seasonality adjustments of a Google Ads account. A seasonality
adjustment tells Smart Bidding that the conversion rate changes for a short, known event, such as a sale. During
its period, the bids of the campaigns it covers move with the `conversion_rate_modifier`, so the spend moves with
it. They add to the read tools of the [Google Ads provider](README.md), whose configuration, targets, quotas, and
errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.bidding-seasonality-adjustments.create` | create | creates up to 10 adjustments |
| `google.ads.bidding-seasonality-adjustments.update` | update | changes members of existing adjustments |
| `google.ads.bidding-seasonality-adjustments.remove` | delete | removes existing adjustments |

Reading adjustments is not a tool of this page: run `reports.search` on `bidding_seasonality_adjustment`.

## How the tools are offered

- **Only through the `tools` list.** A connection offers all three only when its `tools` list names them, whatever
  its effects are. They belong to no profile.
- **Confirmed per call.** The confirmation names the account and, per adjustment, name, scope, period, modifier,
  and the number of campaigns or the channel types, and says that Smart Bidding changes bids and spend.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to
  `BiddingSeasonalityAdjustmentService.MutateBiddingSeasonalityAdjustments` with an `operations` list and without
  `partial_failure`: Google applies all operations or none. A list holds 1 to 10 entries. The 10 are Qatlas's own
  bound; Google accepts up to 10,000 mutate operations per request.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so the
  answer then holds `validate_only: true` and an empty `results`.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the adjustment after the change to each
  result, with the members of the tool plus `seasonality_adjustment_id` and `status`. Without it a result holds the
  `resource_name` only. `remove` has no such argument.
- **Names are checked.** `update` and `remove` take `customers/ID/biddingSeasonalityAdjustments/N` of the
  `customer_id` of the call. A resource name of another account, of another collection, or with a temporary
  (negative) ID is refused. Every campaign in `campaigns` must be `customers/ID/campaigns/N` of that account.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not readable leaves
  open whether Google took the change, the error says that the outcome is unknown. Read the adjustments with
  `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole request.
- **Resource names stay with the account.** Every `resource_name` of a result must be of the `customer_id` of the
  call, distinct, and for `update` and `remove` exactly the adjustment asked for, in the order asked for. A returned
  adjustment must carry the resource name of its result, and a campaign in it must belong to the account. Anything
  else, and any `partial_failure_error`, is `invalid-provider-response` with the note about the unknown outcome.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and code
  in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is
  no manager.
- **Manager accounts.** Google documents that the resource cannot be used in manager accounts. A connection with
  targets refuses a manager account that no manager target names before the change; a manager target that names it
  is passed on, and Google answers with its own error.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create`, `update` | `adjustments` | 1 to 10 entries; for `update`, each `resource_name` once |
| `create` | `scope` | required, `CAMPAIGN` or `CHANNEL` |
| `create` | `start_date_time`, `end_date_time` | required, `yyyy-MM-dd HH:mm:ss` in the time zone of the account; the start is inclusive, the end exclusive |
| `create`, `update` | `name` | 1 to 255 characters; an empty text clears it on `update` |
| `create`, `update` | `description` | 1 to 2048 characters; an empty text clears it on `update` |
| `create`, `update` | `conversion_rate_modifier` | 0.1 to 10.0; 1.0 or unset applies no adjustment |
| `create`, `update` | `devices` | `MOBILE`, `DESKTOP`, `TABLET`, `CONNECTED_TV`, `OTHER` as the catalog of v26 lists them; unset or, on `update`, an empty list means all devices |
| `create`, `update` | `campaigns` | up to 2000 campaigns of the account, with the scope `CAMPAIGN` |
| `create`, `update` | `advertising_channel_types` | `DISPLAY`, `SEARCH`, `SHOPPING`, with the scope `CHANNEL` |
| `remove` | `resource_names` | 1 to 10 distinct adjustments of the account |
| all | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

Checked before any request, besides the forms above:

- The two times are real dates. The interval from the start to the end is longer than 0 and at most 14 days. On an
  `update` that gives only one of the two, the other is the one stored, which only Google can check.
- `campaigns` and `advertising_channel_types` are never both given with entries. The scope `CAMPAIGN` takes no channel
  types, and the scope `CHANNEL` takes no campaigns.
- `scope` is never `CUSTOMER`: Google's reference says that value cannot be used in mutate requests.

An `update` entry names exactly the members given in its field mask, in camelCase. A list or text member replaces
the whole value. The ID and the status are output only and no tool takes them.

The limits are those of the reference pages of `BiddingSeasonalityAdjustment`, `SeasonalityEventScope`, and
`MutateBiddingSeasonalityAdjustments` for v26, checked on 2026-11-22. The overall 10,000 operations come from the
quotas page. Qatlas requires `scope` on `create`, although the reference does not mark it required, so that the
scope is never guessed. The reference counts name and description in characters; Qatlas counts characters (code
points). Whether the account time zone changes the length of an interval across a clock change is not stated;
Qatlas compares the two times as written.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google documents it per call nowhere.
- Which members of an existing adjustment Google accepts to change, for instance the scope or the start of an
  event that has begun. The reference marks only `resource_name` as immutable. No live call has been made.
- Whether Google requires the list that matches the scope, or accepts an adjustment with the scope `CAMPAIGN` and no
  campaigns. Use `validate_only` to see it.
- Whether the start must lie in the future. The reference says the adjustment is forward looking and should be used
  for events that start and end in the future; Qatlas does not enforce it.
- The supported advertising channel types and campaign limit are as the reference states them; Google may refuse
  a campaign whose channel does not support seasonality adjustments.

Left out on purpose:

- `status` and `seasonality_adjustment_id`, `partial_failure`, and any free operation, mask, or resource name.
- The scope `CUSTOMER`.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the adjustment family.

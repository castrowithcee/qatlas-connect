---
description: >
  Describes the bidding strategy tools of the Google Ads provider that spend money: bidding-strategies.create, bidding-strategies.update and bidding-strategies.remove for up to ten portfolio strategies per call, with the tools list requirement, the six schemes and their members in micros, the required members, the left out enhanced_cpc and deprecated members, the immutable currency, the checks on resource names, field masks, validate_only, the atomic request, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: bidding strategy tools

These tools create, change, and remove portfolio bidding strategies of a Google Ads account. A portfolio strategy
sets the bids of every campaign attached to it, so its scheme, target, and bid limits decide how much those
campaigns spend. The tools add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and
errors apply here too, and follow the rules of the [settings tools](settings.md) (tools list, confirmation per
call, one request sent once, unknown outcome, `validate_only`, `response_content_type`, the manager account rule)
and of the [campaign tools](campaigns.md) (resource names, field masks, answer binding); this page names what
differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.bidding-strategies.create` | create | creates up to 10 strategies |
| `google.ads.bidding-strategies.update` | update | changes the given members of up to 10 strategies |
| `google.ads.bidding-strategies.remove` | delete | removes up to 10 strategies |

No profile offers them: a connection has a tool only when its `tools` list names it.

## What differs from the campaign tools

- **One atomic request.** The tools call `BiddingStrategyService.MutateBiddingStrategies` with up to 10
  operations and without `partial_failure`: Google applies all of them or none. Ten is the bound of Qatlas;
  Google's reference names none for this call. The answer holds one entry per strategy, in the order given, each
  once.
- **Resource names stay with the account.** `update` and `remove` name a strategy as
  `customers/ID/biddingStrategies/N` of the `customer_id` of the call, with a positive ID, checked before a secret
  is read. A result of another account or strategy is `invalid-provider-response` with the note about the
  unknown outcome.
- **A manager account has strategies of its own.** They are reached only through a manager target that names the
  account, like every change on a manager account.
- **The mask names what was given.** `update` sends a field mask with the path of each member given, such as
  `targetCpa.targetCpaMicros`, or the scheme itself when it is given without members.
- **`remove` has no `response_content_type`.** A removed strategy has no mutable resource to show. Google
  decides what happens to campaigns still attached to it.
- **The answer with `MUTABLE_RESOURCE`.** Each entry then holds the strategy with the members the tools offer, its
  resource name, and the read-only members `id`, `status`, `type`, `effective_currency_code`, `campaign_count`,
  and `non_removed_campaign_count`, mapped and bounded like every Google answer.

## bidding-strategies.create and bidding-strategies.update

The members are those of the `BiddingStrategy` resource of v26 in snake case:

- `name`: 1 to 255 bytes after trimming, unique in the account (Google checks the uniqueness).
- `aligned_campaign_budget_id`: the budget of the account the strategy is aligned with; campaigns added later
  must use that budget.
- `currency_code`: an ISO 4217 code, accepted on `create` only because the currency is immutable. Only a strategy
  of a manager account takes it; a serving account always uses its own currency, which Google enforces.
- One scheme, exactly one on `create` and at most one on `update`:

| Scheme | Members |
| --- | --- |
| `maximize_conversions` | `target_cpa_micros`, `cpc_bid_ceiling_micros`, `cpc_bid_floor_micros` |
| `maximize_conversion_value` | `target_roas`, `cpc_bid_ceiling_micros`, `cpc_bid_floor_micros`, `target_roas_tolerance_percent_millis` |
| `target_cpa` | `target_cpa_micros`, `cpc_bid_ceiling_micros`, `cpc_bid_floor_micros` |
| `target_impression_share` | `location`, `location_fraction_micros` (0 to 1,000,000, so 10,000 is 1 percent), `cpc_bid_ceiling_micros` (required) |
| `target_roas` | `target_roas` (required, 0.01 to 1000), `cpc_bid_ceiling_micros`, `cpc_bid_floor_micros`, `target_roas_tolerance_percent_millis` |
| `target_spend` | `cpc_bid_ceiling_micros` |

The members of a scheme are the ones the campaign tools offer for a campaign's own strategy, so both are checked
alike. A scheme without required members may be given empty, such as `{}` for `maximize_conversions`, which aims
for the most conversions without a target.

Amounts in micros are whole numbers of at least 0 in the currency of the strategy, one unit being 1,000,000
micros. Qatlas refuses a negative or fractional amount, and a `target_roas` or a share outside its range, before
anything is sent. Which amounts are above the minimum billable unit of a currency is Google's to validate: use
`validate_only`.

**Left out on purpose.**

- `enhanced_cpc`: Google no longer lets it be created. `manual_cpc` with `enhanced_cpc_enabled` on a campaign
  replaces it, see the [campaign tools](campaigns.md).
- `target_spend.target_spend_micros`, which is deprecated.
- The members Google fills itself and the type of the strategy, which `reports.search` reads. Changing the scheme
  of an existing strategy is giving the member of the new scheme; whether Google accepts a change of the type is
  its to decide.

## bidding-strategies.remove

Calls `MutateBiddingStrategies` with remove operations for strategies named by `resource_names`, each once. A
removed strategy cannot be used again.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds, in particular for a scheme given
  without members. No live call has been made.
- Whether `MutateBiddingStrategies` has an upper limit of operations; Qatlas keeps its own.
- Which schemes each campaign channel type accepts, and whether an update may change the scheme: Google's
  validation.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- Attaching a strategy to a campaign: that is the `bidding_strategy` member of the [campaign tools](campaigns.md).

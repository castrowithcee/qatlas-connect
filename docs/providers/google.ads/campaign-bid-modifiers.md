---
description: >
  Describes the campaign bid modifier tools of the Google Ads provider that change what a campaign spends: campaign-bid-modifiers.create, campaign-bid-modifiers.update, and campaign-bid-modifiers.remove for up to ten modifiers per call, with the tools list requirement, the one criterion Google documents, the meaning of the factor and the checks on it, how resource names are built from campaign and criterion IDs, validate_only, confirmation, the atomic request, the unknown outcome, the manager account rule, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: campaign bid modifier tools

These tools create, change, and remove the bid modifiers of the campaigns of a Google Ads account. A campaign bid
modifier is a factor that multiplies the bids of one campaign when its criterion matches. A change moves the spend
of a running campaign. They add to the read tools of the [Google Ads provider](README.md), whose configuration,
targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-bid-modifiers.create` | create | creates modifiers for campaigns |
| `google.ads.campaign-bid-modifiers.update` | update | changes the factor of existing modifiers |
| `google.ads.campaign-bid-modifiers.remove` | delete | removes existing modifiers |

Reading modifiers is not a tool of this page: run `reports.search` on `campaign_bid_modifier`, which returns
`campaign`, `criterion_id`, `interaction_type.type`, and `bid_modifier`. Bid modifiers of ad groups, and the bid
modifiers of campaign criteria such as location or device, belong to other tools.

## How the tools are offered

- **Only through the `tools` list.** A connection offers all three only when its `tools` list names them. They
  belong to no profile, and no effect alone offers them: a connection with the `create`, `update`, or `delete`
  effect and no `tools` list does not have them.
- **Confirmed per call.** All three need the confirmation of that one call. The text names the account, the
  campaigns, the criterion IDs, and the factors, and says that the call changes what the campaigns spend.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignBidModifierService.MutateCampaignBidModifiers`
  with an `operations` list and without `partial_failure`: Google applies all operations or none. A list holds
  1 to 10 entries. The 10 are Qatlas's own bound; the reference page of the request states no limit.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers errors
  only, so the answer then holds `validate_only: true` and an empty `results`. Validation needs the same
  confirmation and the same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the modifier after the change to
  each result: `resource_name`, `campaign`, `criterion_id`, `interaction_type`, `bid_modifier`. Without it a result
  holds `campaign_id`, `criterion_id`, and `resource_name` only. `remove` has no such argument.
- **Names come from checked IDs.** `update` and `remove` build
  `customers/ID/campaignBidModifiers/CAMPAIGN_ID~CRITERION_ID` from `customer_id`, `campaign_id`, and
  `criterion_id`. `create` builds `customers/ID/campaigns/CAMPAIGN_ID` from `customer_id` and `campaign_id`. A caller
  never passes a resource name, a field mask, or an operation. Both IDs are positive numbers.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or
  not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the
  modifiers with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/campaignBidModifiers/CAMPAIGN_ID~CRITERION_ID` of the `customer_id` of the call. For `update` and
  `remove` the results must be exactly the modifiers asked for, in the order asked for. For `create`, one distinct
  modifier of the campaign asked for must be answered per entry, in order. A returned modifier must have the
  resource name of its result, and its `campaign` and `criterion_id`, when present, must be those of the name.
  Anything else is `invalid-provider-response` with the note about the unknown outcome, since the change may have
  been made. A `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message, such as `criterionError`, and the field path in the original. The other codes of the README
  apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is
  no manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target,
  are refused as `invalid-request`. Campaigns live in customer accounts, so a call on a manager account is
  rarely useful.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| all | `campaign_bid_modifiers` | 1 to 10 entries; the same modifier (campaign and interaction type for `create`, campaign and criterion for `update` and `remove`) appears once |
| all | `campaign_bid_modifiers[].campaign_id` | positive number, the ID of an existing campaign of the account |
| `create` | `campaign_bid_modifiers[].interaction_type` | required, `CALLS` |
| `create`, `update` | `campaign_bid_modifiers[].bid_modifier` | required, a number of at least 0 |
| `update`, `remove` | `campaign_bid_modifiers[].criterion_id` | positive number, the `criterion_id` that `reports.search` or `create` returns |
| all | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

`update` changes the factor only, with the mask `bidModifier`: the criterion of a modifier is immutable in the
reference, and the campaign is output only.

**The factor.** `bid_modifier` multiplies the bid. Google documents 1.2 as a raise of 20 percent and 0.8 as a cut of
20 percent, and 0.0 as opting the criterion out, the equivalent of a -100 percent adjustment in the Google Ads
interface. When several modifiers apply to one impression they multiply. With smart bidding a modifier can behave
differently or be ignored. The reference pages of the campaign bid modifier state no range of the factor, so Qatlas
refuses only what is no factor: a negative number. Which upper bound Google applies, and whether it accepts 0 for
the interaction type, is Google's to validate; use `validate_only` to see it.

**The criterion.** The reference of v26 knows one criterion for a campaign bid modifier, the interaction type, with
one value, `CALLS`, supported only for Search campaigns. Any other value is refused by Google as a criterion error,
and Qatlas offers only `CALLS`. The text of the bid modifier guide calls the value `CALL`; the enum of the reference
and the field catalog say `CALLS`, which Qatlas sends.

The limits are those of the reference pages of `CampaignBidModifier`, `InteractionTypeInfo`,
`MutateCampaignBidModifiersRequest`, `CampaignBidModifierOperation`, and of the v26 proto files, checked on
2026-11-22.

## Boundary

Not verified:

- How a create names its campaign. The reference marks `campaign` of the modifier as output only and says that no
  resource name is expected on a create, yet states no other way to name the campaign. The create sends the
  campaign as `customers/ID/campaigns/CAMPAIGN_ID` in the `campaign` member, as the earlier reference did. If Google
  refuses it, the refusal is a `provider-error` of the field and nothing was changed; `validate_only` shows it
  before any change. No live call has been made.
- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Which criterion ID a new modifier gets. Read it from the answer or from `reports.search`.
- Whether a campaign that is not a Search campaign is refused at validation or at creation.
- What a removal does to bids that were already placed. The pages read do not say.

Left out on purpose:

- Bid modifiers of ad groups, and the `bid_modifier` of campaign criteria (device, location, ad schedule, and the
  rest).
- `partial_failure`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the campaign bid modifier family.

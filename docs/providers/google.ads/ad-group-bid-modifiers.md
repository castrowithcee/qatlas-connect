---
description: >
  Describes the ad group bid modifier tools of the Google Ads provider that spend money: ad-group-bid-modifiers.create, ad-group-bid-modifiers.update and ad-group-bid-modifiers.remove for up to ten bid modifiers per call, with the tools list requirement, the range of bid_modifier, the six criteria of which exactly one is required, what only creation accepts, the checks on resource names, field masks, validate_only, the atomic request, the answer with MUTABLE_RESOURCE, where Qatlas differs from the reference of Google, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: ad group bid modifier tools

These tools create, change, and remove the bid modifiers of ad groups of a Google Ads account. A bid modifier
raises or lowers the bids of an ad group where its criterion applies (a device, or a property of a hotel
booking), so every call changes how much the ad group costs. The tools add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and follow
the rules of the [settings tools](settings.md) (tools list, confirmation per call, one request sent once,
unknown outcome, `validate_only`, `response_content_type`, the manager account rule) and of the
[ad group tools](ad-groups.md), whose field tree and checks they share; this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.ad-group-bid-modifiers.create` | create | creates up to 10 bid modifiers |
| `google.ads.ad-group-bid-modifiers.update` | update | changes `bid_modifier` of up to 10 bid modifiers |
| `google.ads.ad-group-bid-modifiers.remove` | delete | removes up to 10 bid modifiers |

No profile offers them: a connection has a tool only when its `tools` list names it. The confirmation text
names the account, the bid modifiers with their value and criterion, and that the change affects spend.

## What the tools check

- **One atomic request.** The tools call `AdGroupBidModifierService.MutateAdGroupBidModifiers` with up to 10
  operations and without `partial_failure`: Google applies all of them or none. The answer holds one entry per
  bid modifier, in the order given.
- **Range of `bid_modifier`.** From 0.1 to 10.0, as the reference states. The value 0 opts out of a device:
  `create` accepts it only together with the `device` criterion. On `update` the criterion of the bid modifier
  is not known to Qatlas, so 0 passes there and Google refuses it where it does not apply.
- **Exactly one criterion on `create`.** The reference of v26 lists these members of the criterion:
  `device` (`type`), `advance_booking_window` (`min_days`, `max_days`), `length_of_booking` (`min_days`,
  `max_days`), `start_date_range` (`start_date`, `end_date`, as YYYY-MM-DD), `start_day_of_week`
  (`day_of_week`), and `user_set_dates` (`type`). The reference says a criterion is required on creation, and
  Qatlas requires exactly one of the six. `create` requires `ad_group`, `bid_modifier`, and the criterion.
  Enum values are those of the field reference of v26 without the unspecified and unknown values.
- **Resource names stay with the account.** `ad_group` must be `customers/ID/adGroups/N` of the `customer_id`
  of the call, and the bid modifiers of `update` and `remove` must be
  `customers/ID/adGroupBidModifiers/AdGroupID~CriterionID` of it, both IDs positive. A temporary (negative) ID
  is refused. This is checked before a secret is read. In the answer every resource name must belong to the
  account and, for `update` and `remove`, be the one asked for; for `create` it must belong to the ad group
  asked for, at the same position. The ad group and the base ad group inside a returned bid modifier must
  belong to the account as well. Anything else is `invalid-provider-response` with the note about the unknown
  outcome.
- **Creation-only members.** `ad_group` and the criterion are arguments of `create` only, as the reference
  marks them immutable. `update` offers `bid_modifier` alone, and its field mask is `bidModifier`. To change
  the criterion, remove the bid modifier and create another one.
- **`remove` has no `response_content_type`.** A removed bid modifier has no mutable resource to show.
- **The answer with `MUTABLE_RESOURCE`.** Each entry then holds the bid modifier with the members the tools
  offer and the ones Google fills itself: `criterion_id`, `base_ad_group`, `bid_modifier_source`, and the
  resource name. Members the tools do not know are dropped.

## Where Qatlas differs from the reference

- Google names no limit of operations per call in the reference of the method; Qatlas allows 10.
- `min_days` and `max_days` have no documented range; Qatlas accepts whole numbers from 0, and Google decides
  the rest (including whether the low end is below the high end).
- The dates of `start_date_range` must be real calendar dates in the form YYYY-MM-DD; the reference names only
  the form.
- The reference does not say which device values or criteria suit which campaign type, and whether a
  hotel criterion needs a Hotel campaign. All values of the enums are offered, and Google validates the
  combination: use `validate_only`.
- Qatlas never sends `partial_failure`; the reference offers it.

## Left out on purpose

- `criterion_id`, `base_ad_group`, and `bid_modifier_source` as arguments: Google fills them. The resource has no
  deprecated member. They appear in the answer.
- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- The bid modifiers of campaigns and the criteria of ad groups: they have their own tools or none.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the path Qatlas builds. No live call has been made.
- Which criterion and device values each campaign type accepts: the reference states none, and the rest is
  Google's validation.
- Whether `MutateAdGroupBidModifiers` has an upper limit of operations; Qatlas keeps its own.

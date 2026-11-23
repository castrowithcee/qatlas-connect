---
description: >
  Describes the recommendation subscription tools of the Google Ads provider: recommendation-subscriptions.create and recommendation-subscriptions.update, that only the tools list offers, the ongoing effect of an enabled subscription that Google applies recommendations automatically, the type and status arguments, the bound of 10 entries, the atomic request without partial_failure, validate_only, response_content_type, the unknown outcome, the binding of resource names to the account and the type, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: recommendation subscription tools

These tools create and change the subscriptions of a Google Ads account to automatic recommendations. With an
`ENABLED` subscription for a recommendation type, Google applies the recommendations of that type to the account
by itself, without a further call and without a confirmation, until the subscription is paused. They add to the
read tools of the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here
too. The one-time tools are described in [recommendation tools](recommendations.md).

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.recommendation-subscriptions.create` | create | subscribes the account to 1 to 10 recommendation types, each `ENABLED` or `PAUSED` |
| `google.ads.recommendation-subscriptions.update` | update | switches 1 to 10 existing subscriptions between `ENABLED` and `PAUSED` |

## An enabled subscription keeps acting

A confirmed call to these tools is not a one-time change. After it, Google applies recommendations of the
subscribed types on its own schedule. Depending on the type, that changes budgets, bids, targets, keywords, ads,
assets, and the networks of campaigns, and therefore what the account spends.

- **No parameters.** A subscription holds only the type and the status. Google chooses the values it applies, and
  Qatlas Connect can neither see nor limit them.
- **Never deleted.** A subscription cannot be removed, only switched between `ENABLED` and `PAUSED`. `PAUSED`
  stops further automatic applications and does not undo what was already applied.
- **What is applied** is read afterwards, for example with `reports.search` on `change_event` and `recommendation`.
  The subscriptions themselves are read with `reports.search` on `recommendation_subscription`.
- **Types with outward reach.** The list of types is the one Google defines, including types that create ads,
  assets, or lead forms or change final URL expansion. The tools offer no way to exclude a type from Google's
  application other than pausing its subscription.

## How the tools are offered

- Both tools are offered only through the `tools` list of a connection. A connection with the effects `create`
  and `update` and no `tools` list does not have them, and they belong to no profile.
- **Confirmed per call**, also with `validate_only`. The confirmation names the account, the types, and the
  statuses, and says that an enabled subscription applies recommendations automatically.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. `login_customer_id` is never an argument.

## What every call does

- **One atomic request.** A call sends one request to
  `RecommendationSubscriptionService.MutateRecommendationSubscription` with an `operations` list and without
  `partial_failure`: Google applies all operations or none. A list holds 1 to 10 entries. The 10 are Qatlas's own
  bound; the reference states none.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only,
  so the answer holds `validate_only: true` and an empty `results`. Validation needs the same confirmation and
  costs the same operations as the change.
- **`response_content_type`.** `MUTABLE_RESOURCE` adds the subscription after the change to each result
  (`resource_name`, `type`, `status`, `create_date_time`, `modify_date_time`, bounded text). Without it a result
  holds `type` and `resource_name` only.
- **Names come from checked values.** The resource name `customers/ID/recommendationSubscriptions/TYPE` is built
  from `customer_id` and `type`. A caller never passes a resource name, a field mask, or an operation. An update
  sends the mask `status` and nothing else, because the type of a subscription cannot change.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not readable
  leaves open whether Google took the change, the error says that the outcome is unknown. Read the subscriptions
  with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole
  request and say so.
- **Resource names stay with the account.** The results must be exactly the entries asked for, in the order asked
  for, each with the resource name of `customer_id` and its type. A returned subscription must carry the name of
  its result and, when it states a type, the same type. Anything else, and any `partial_failure_error`, is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made.
- **Quota.** Each entry counts as one operation of the daily quota, in a refused request too, plus one for the
  membership read below a manager target and one for the check that the account is no manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it.

## Arguments

| Argument | Rule |
| --- | --- |
| `customer_id` | ten digits without hyphens |
| `recommendation_subscriptions` | 1 to 10 entries, each `type` once |
| `recommendation_subscriptions[].type` | required, a `RecommendationType` of v26 (for example `KEYWORD`, `CAMPAIGN_BUDGET`), without `UNSPECIFIED` and `UNKNOWN` |
| `recommendation_subscriptions[].status` | required, `ENABLED` or `PAUSED` |
| `validate_only` | true validates only |
| `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

## What was checked against Google

Checked on the reference pages of `RecommendationSubscriptionService`, `MutateRecommendationSubscription`,
`RecommendationSubscription`, `RecommendationSubscriptionStatus`, and `RecommendationType` for v26 on 2026-11-23:

- The request holds `customer_id`, `operations`, `partial_failure`, `validate_only`, and `response_content_type`.
  An operation is a `create` or an `update` with an `update_mask`; there is no `remove`.
- The resource has `resource_name` (immutable), `type` (required, immutable), `status` (required),
  `create_date_time` and `modify_date_time` (output only). The statuses are `ENABLED` and `PAUSED`, and the
  page says a subscription cannot be deleted. Both date fields are returned as Google writes them.
- The reference states no limit on the number of operations. The research behind the tools found none either, so
  the 10 per call are Qatlas's own and lower than the 10,000 operations that Google accepts per mutate request.
- The `RecommendationType` list has 60 values; 58 are offered, without the two placeholders. The reference does
  not say which types accept a subscription. Google refuses the others, and `validate_only` shows it.
- The service page lists the errors `RecommendationError`, `RecommendationSubscriptionError`, and
  `UrlFieldError` among others. The enum of `RecommendationSubscriptionError` lists no value beyond the
  placeholders, so a refusal arrives as the generic code plus the message of Google.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Which types Google accepts for a subscription, and what Google applies for each type and when.
- Whether a `create` for a type that is already subscribed is refused or answers with the existing subscription.
- Whether Google accepts the mask in camelCase as Qatlas sends it. No live call has been made.

Left out on purpose:

- `partial_failure`, and any free operation, mask, or resource name.
- Parameters of a subscription: v26 has none.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the recommendation subscription family.

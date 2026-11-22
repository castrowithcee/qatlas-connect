---
description: >
  Describes the ad group tools of the Google Ads provider that spend money: ad-groups.create, ad-groups.update and ad-groups.remove for up to ten ad groups per call, with the tools list requirement, the required name, campaign and status, the members offered and left out on purpose, the checks on resource names and addresses, field masks and clearing, validate_only, the atomic request, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: ad group tools

These tools create, change, and remove ad groups of a Google Ads account. An ad group that is enabled serves in
its campaign and bids within the budget of the campaign, and its bids decide how much a click or a view costs.
The tools add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply
here too, and follow the rules of the [settings tools](settings.md) (tools list, confirmation per call, one
request sent once, unknown outcome, `validate_only`, `response_content_type`, the manager account rule) and of
the [campaign tools](campaigns.md), whose field tree they share; this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.ad-groups.create` | create | creates up to 10 ad groups |
| `google.ads.ad-groups.update` | update | changes the given members of up to 10 ad groups |
| `google.ads.ad-groups.remove` | delete | removes up to 10 ad groups |

No profile offers them: a connection has a tool only when its `tools` list names it.

## What differs from the campaign tools

- **A list of ad groups, one atomic request.** The tools call `AdGroupService.MutateAdGroups` with up to 10
  operations and without `partial_failure`: Google applies all of them or none. Ten is the bound of Qatlas;
  the reference of v26 names none for this call. The answer holds one entry per ad group, in the order given.
- **Required members.** `create` requires `name`, `campaign`, and `status` (`ENABLED` or `PAUSED`). Google
  documents only the name as required, and its default status is `ENABLED`: Qatlas asks for the status so that
  no ad group starts serving by a default, and for the campaign because an ad group belongs to one and the
  reference calls it immutable. `update` takes `ENABLED` or `PAUSED`; an ad group is removed only with
  `ad-groups.remove`, and a removed ad group cannot be enabled again.
- **Resource names stay with the account.** The `campaign` of an ad group in an argument must be
  `customers/ID/campaigns/N` of the `customer_id` of the call, with a positive ID, and the ad groups of `update`
  and `remove` must be `customers/ID/adGroups/N` of it. A temporary (negative) ID is refused. This is checked
  before a secret is read. The resource names in the answer, the campaign inside a returned ad group included,
  must belong to the account and, for `update` and `remove`, be the ad groups asked for; anything else is
  `invalid-provider-response` with the note about the unknown outcome.
- **Creation-only members.** `campaign`, `type`, and `audience_setting` are arguments of `create` only, as the
  reference marks them immutable; the schema of `update` does not offer them.
- **The mask names what was given.** `update` sends a field mask with the path of each member given. A list
  member (custom parameters, target restrictions, excluded asset types) replaces the whole list. An empty text
  for `tracking_url_template` or `final_url_suffix` clears the member: it is in the mask and has no value.
- **`remove` has no `response_content_type`.** A removed ad group has no mutable resource to show.
- **The answer with `MUTABLE_RESOURCE`.** Each entry then holds the ad group with the members the tools offer,
  its resource name, and `fixed_cpm_micros` if Google returns it; members the tools do not offer are dropped,
  and a member without a value is left out.

## ad-groups.create and ad-groups.update

The members are those of the `AdGroup` resource of v26, nested as the resource nests them, with the names in
snake case: name, status, campaign, type, ad rotation mode, the bids (`cpc_bid_micros`, `cpm_bid_micros`,
`cpv_bid_micros`, `percent_cpc_bid_micros`), the targets (`target_cpa_micros`, `target_cpc_micros`,
`target_cpm_micros`, `target_cpv_micros`, `target_roas`), the Display bid dimension, optimized targeting, the
excluded parent asset types, tracking template, final URL suffix, custom parameters, the targeting setting, the
audience setting, AI Max, the vertical ads format setting, the Demand Gen channel controls, and the video ad
sequence step. The descriptor lists every member with its type, enum values, and bounds. Enum values are those
of the field reference of v26 without the unspecified and unknown values. Amounts are in micros of the account
currency.

Qatlas checks the form of an argument and the rules Google documents:

- **Name:** fewer than 255 characters, a full-width character counting twice, and no null, line feed, carriage
  return, or other control character. Google's limit counts display width; Qatlas counts a character from U+1100 on as
  full-width, and Google's own check decides the rest.
- **`percent_cpc_bid_micros`:** from 0 up to, not including, 1 (stored as millionths), so 0 to 999999.
- **Excluded parent asset set types:** only `LOCATION_SYNC`, the one value Google supports.
- **Types:** `SHOPPING_SMART_ADS` is not offered, because Google documents that no new Smart Shopping ad groups
  can be created.

The rules that depend on the campaign are Google's to validate, so use `validate_only`: that `cpc_bid_micros` is
ignored under a portfolio bidding strategy, that `target_roas` is refused for a portfolio strategy, that
`target_cpa_micros` overrides the campaign only for a target CPA strategy or maximize conversions with a target,
that `exclude_demographic_expansion` is ignored without `optimized_targeting_enabled`, and that the vertical ads
format setting needs AI Max and a travel feed on the campaign.

**Addresses.** The tracking template and the final URL suffix can pass click data to a third party. They follow
the checks of [customers.update](settings.md#customersupdate): a template is an `https` address without user info
and with a plain host name, a suffix is parameters and no address. Custom parameters carry only a key and a
value, and their values are text. The ad group has no final URLs of its own: they belong to the ads.

**Left out on purpose.**

- Members Google fills itself (output only), which `reports.search` reads: `id`, `base_ad_group`, `labels`, the
  effective bids and their sources, `primary_status`, and its reasons. The resource has no deprecated member.
- `fixed_cpm_micros`: the price of a reservation campaign, which is booked through a billing action that this
  provider only reads. An answer shows it when Google returns it.
- The type `SHOPPING_SMART_ADS`, see above.

## ad-groups.remove

Calls `MutateAdGroups` with remove operations for ad groups named by `resource_names`. An ad group appears once.
Removing stops serving at once. The history of a removed ad group stays readable with `reports.search`.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds, in particular for messages
  without members. No live call has been made.
- Which members each ad group type accepts: the reference states some of it and the rest is Google's validation.
- Whether `MutateAdGroups` has an upper limit of operations; Qatlas keeps its own.
- Whether `video_ad_group_settings.video_ad_sequence.step_id` is accepted on an update: the reference does not
  mark it, and Google validates it.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- Criteria have the [ad group criterion tools](ad-group-criteria.md). Bid modifiers, labels on ad groups, and ads
  have their own tools or none.

---
description: >
  Describes the campaign tools of the Google Ads provider that spend money: campaigns.create, campaigns.update and campaigns.remove for up to ten campaigns per call, and campaigns.enable-pmax-brand-guidelines for one Performance Max campaign, with the tools list requirement, the explicit status, the members offered and left out on purpose, the checks on resource names, temporary IDs and addresses, field masks and clearing, validate_only and its absence for brand guidelines, the atomic request, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: campaign tools

These tools create, change, remove, and prepare campaigns of a Google Ads account. A campaign that is enabled
serves ads and spends money within its budget, and its bidding, budget, and networks decide how much. The tools
add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and
follow the rules of the [settings tools](settings.md) (tools list, confirmation per call, one request sent once,
unknown outcome, `validate_only`, `response_content_type`, the manager account rule); this page names what
differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaigns.create` | create | creates up to 10 campaigns |
| `google.ads.campaigns.update` | update | changes the given members of up to 10 campaigns |
| `google.ads.campaigns.remove` | delete | removes up to 10 campaigns |
| `google.ads.campaigns.enable-pmax-brand-guidelines` | update | enables brand guidelines for one Performance Max campaign |

No profile offers them: a connection has a tool only when its `tools` list names it.

## What differs from the settings tools

- **A list of campaigns, one atomic request.** The tools call `CampaignService.MutateCampaigns` with up to 10
  operations and without `partial_failure`: Google applies all of them or none. Ten is the bound of Qatlas;
  Google's reference names none for this call. The answer holds one entry per campaign, in the order given.
- **No temporary IDs.** A resource name with a negative ID is refused. A campaign refers only to objects that
  exist: create the budget, the bidding strategy, or the assets first, then name them in a later call.
- **Resource names stay with the account.** A budget, bidding strategy, campaign group, hotel property asset
  set, conversion action, or campaign in an argument must be `customers/ID/<collection>/N` of the `customer_id`
  of the call, with a positive ID. This is checked before a secret is read. The resource names in the answer
  must belong to the account and, for `update` and `remove`, be the campaigns asked for; anything else is
  `invalid-provider-response` with the note about the unknown outcome.
- **The status is explicit.** `create` requires `status`, `ENABLED` or `PAUSED`, although Google defaults to
  `ENABLED`: no campaign starts serving by a default. `update` takes `ENABLED` or `PAUSED`; a campaign is removed
  only with `campaigns.remove`, and a removed campaign cannot be enabled again.
- **Creation-only members.** `advertising_channel_type` and the other members Google fixes at creation are
  arguments of `create` only, and the schema of `update` does not offer them.
- **The mask names what was given.** `update` sends a field mask with the path of each member given: the
  members of a message that was given, or the message itself when it has no members, such as `manual_cpm`. A
  list member (frequency caps, custom parameters, and the others) replaces the whole list. An empty text for
  `end_date_time`, `tracking_url_template`, or `final_url_suffix` clears the member: it is in the mask and has
  no value. Changing the bidding strategy is giving the member of the new one, at most one of the bidding
  members per campaign.
- **`remove` has no `response_content_type`.** A removed campaign has no mutable resource to show.
- **The answer with `MUTABLE_RESOURCE`.** Each entry then holds the campaign with the members the tools offer
  and its resource name, mapped and bounded like every Google answer. Members the tools do not offer are
  dropped, a resource name of another account fails the call, and a member without a value is left out.

## campaigns.create and campaigns.update

The members are those of the `Campaign` resource of v26, nested as the resource nests them, with the names in
snake case: name, status, channel type and sub type, budget, campaign group, the bidding members, networks,
geo target type, start and end, tracking template, final URL suffix, custom parameters, frequency caps, the
declarations and optimization settings, the brand and text guidelines, asset automation, the channel specific
settings (shopping, dynamic search ads, local, app, hotel, travel, Demand Gen, Performance Max, Local Services,
video), and the targeting setting. The descriptor lists every member with its type, enum values, and bounds. Enum
values are those of the field reference of v26 without the unspecified and unknown values.

Qatlas checks the form of an argument and the rules Google documents: required members, the exclusive group of
the bidding members, the range of `target_roas` (0.01 to 1000), `campaign_priority` (0 to 2), the hex colors and
the font list of the brand guidelines, and the form of a date time, `yyyy-MM-dd HH:mm:ss` in the time zone of the
account. The bounds of text lengths and list sizes that Google does not document are Qatlas's own. Every other
rule, such as which members a channel type accepts, is Google's to validate: use `validate_only`.

**Addresses.** The tracking template and the final URL suffix can pass click data to a third party, and the
address of an automated video crawl is fetched by Google. They follow the checks of
[customers.update](settings.md#customersupdate): a template is an `https` address without user info and with a
plain host name, a suffix is parameters and no address, and a crawl address is an `https` address. Custom
parameters carry only a key and a value, and their values are text.

**Left out on purpose.**

- Members Google fills itself (output only), which `reports.search` reads, and the deprecated members:
  `target_spend.target_spend_micros` and `shopping_setting.use_vehicle_inventory`.
- `fixed_cpm` and `video_campaign_settings.reservation_ad_category_self_disclosure`: they belong to reservation
  campaigns, which are booked through a billing action that this provider only reads.
- `third_party_integration_partners` and `shopping_setting.advertising_partner_ids`: they hand the data of the
  campaign to partners and accounts outside the account.
- The phone numbers of the Local Services settings, which are personal data of the provider.
- `targeting_setting.target_restriction_operations`, the delta form of `target_restrictions`, which replaces
  the list.

## campaigns.remove

Calls `MutateCampaigns` with remove operations for campaigns named by `resource_names`. A campaign appears once.
Removing stops serving at once. The history of a removed campaign stays readable with `reports.search`.

## campaigns.enable-pmax-brand-guidelines

Calls `CampaignService.EnablePMaxBrandGuidelines` for one Performance Max campaign. The brand assets are
filled by Google (`auto_populate_brand_assets`) or named: a business name asset and square logos, optionally
landscape logos, all assets of the account. The final URI domain, the two colors, which are given together, and
the font family are optional.

- **No `validate_only`.** The request of Google has no such member, so the tool has none and the call changes
  the campaign.
- **One campaign per call.** Google answers a campaign that it could not enable inside an answer that is
  otherwise successful, so a call with several campaigns would not be all or nothing. The tool takes one
  campaign, and a failure Google reports for it ends the call with the error classes of the README.
- **No `partial_failure`, no list.** Google's request has neither, and the bound of ten operations of the call is
  not used.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds, in particular for messages
  without members and for the members of a message of a one-of group. No live call has been made.
- Which members each channel type accepts and which bidding strategies each channel type allows: the reference of
  the resource states some of it and the rest is Google's validation.
- Whether `MutateCampaigns` has an upper limit of operations; Qatlas keeps its own.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- The booking and quoting of reservation campaigns and every other billing action.
- Smart campaigns and the settings that belong to other services: budgets, bidding strategies, ad groups,
  criteria, and assets have their own tools or none.

---
description: >
  Describes the ad content tools of the Google Ads provider: ads.update for the content of existing ads (the tools list requirement, the members offered and the immutable ones left out, final URLs and tracking, field masks, validate_only as the ad policy check, policy_validation_parameter) and ad-parameters.create, ad-parameters.update and ad-parameters.remove for the numbers in the placeholders of keyword ads (the effect profile of create and update, the tools list for remove, the keyword and the index 1 or 2, the form of the insertion text), with the checks on resource names, the atomic request, the answer with MUTABLE_RESOURCE, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: ad content tools

These tools change the content of ads that exist and manage the ad parameters of keywords. A changed ad goes
through the policy review again and serves with the new content once it passes, and an ad parameter puts a number
into the text of the ads of a keyword; both are public once the ads serve. The tools add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and follow the
rules of the [settings tools](settings.md) (confirmation per call, one request sent once, unknown outcome,
`validate_only`, `response_content_type`, the manager account rule), of the [ad tools](ads.md) (the members of an
ad, the checks on addresses, the ad policy check) and of the [ad group tools](ad-groups.md) (a list of up to 10
entries in one atomic request, resource names that stay with the account); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.ads.update` | update | changes the content of up to 10 existing ads |
| `google.ads.ad-parameters.create` | create | creates up to 10 ad parameters |
| `google.ads.ad-parameters.update` | update | changes the insertion text of up to 10 ad parameters |
| `google.ads.ad-parameters.remove` | delete | removes up to 10 ad parameters |

No profile offers them. `ads.update` and `ad-parameters.remove` are offered only to a connection whose `tools`
list names them. `ad-parameters.create` and `ad-parameters.update` are offered by the effects `create` and
`update` like the [audience tools](audiences.md), without a `tools` list. Every call of the four is confirmed, and
the confirmation names the account, the ads or ad parameters, and what happens: a changed ad is reviewed again and
serves with the new content, an ad parameter shows its number in the text of the ads of the keyword. Read ads and
ad parameters with `reports.search` on `ad` and `ad_parameter`.

## ads.update

Calls `AdService.MutateAds`, which has update operations only, with up to 10 operations and without
`partial_failure`: Google applies all of them or none. Ten is the bound of Qatlas; the reference of v26 names none.
Each entry of `ads` names the ad by `resource_name`, `customers/ID/ads/AD_ID`, and the members to change. At least
one member is required, and the request carries a field mask that names exactly the members given. A member that
is an empty text or an empty list is cleared and is in the mask.

**Offered.** The members of an ad that the [ad tools](ads.md) offer on creation: `final_urls`, `final_mobile_urls`,
`tracking_url_template`, `final_url_suffix`, `url_custom_parameters`, `url_collections`, `final_app_urls`,
`display_url`, `device_preference`, `synthetic_content_info`, and the members of the one ad type the ad has,
nested as the `Ad` resource of v26 nests them, with the same counts. A list replaces the whole list. For the
members of an ad type the field mask names the path of each member, such as `responsive_search_ad.headlines`, so a
change of the headlines leaves the descriptions as they are; an ad type with no member named is refused. The ad
types of one call need not match: each entry names the type its ad has, and Google refuses a type the ad does not
have. `policy_validation_parameter` is an argument of each entry, as for the ad tools.

**Left out on purpose.**

- Members the reference marks immutable: `resource_name` is the name of the ad and no member to change, `name`,
  and the types `text_ad`, `expanded_dynamic_search_ad`, and `image_ad`. The reference also states that
  `MutateAds` does not update `TextAd`, `ExpandedDynamicSearchAd`, `GmailAd`, and `ImageAd`: such an ad is replaced,
  not changed. The image bytes of an image ad are therefore no argument here either.
- The types Google marks deprecated or legacy, as for [ad creation](ads.md#ad-types), and the members Google fills
  itself (output only).
- `status`, `start_date_time`, `end_date_time`, and the asset automation settings: they belong to the ad group ad
  and are changed with [ad-group-ads.update](ads.md).
- `partial_failure`, temporary IDs, and `GoogleAdsService.Mutate`.

**Checks before anything is sent.**

- An ad and every asset named inside an ad belong to the `customer_id` of the call, with a positive ID. A
  temporary (negative) ID is refused. An ad appears once per call.
- Final URLs, tracking templates, the final URL suffix, and the members of `url_collections` follow the
  [checks of the ad tools](ads.md#final-urls-and-tracking): `https` addresses without user info, a suffix that is
  parameters, not an address. An update does not require a final URL: whether the ad still has one is Google's
  to say.
- `path2` can be set without `path1` in an update only if the ad already has `path1`; Qatlas does not read the ad
  and leaves this to Google, use `validate_only`.

**The answer.** One entry per ad, in the order given. The result must be the ad asked for, and an ad returned with
`MUTABLE_RESOURCE` must name that ad and assets of the account; anything else is `invalid-provider-response`
with the note about the unknown outcome. With `validate_only` Google answers without a result.

**The ad policy check.** As for the ad tools, `validate_only` shows whether the changed ad would pass the review:
a rejection names the policy findings as topics and policy names in the error. An ad that is saved with a policy
exemption cannot serve.

## Ad parameters

An ad parameter is a number, such as a price or a stock level, that Google puts into the placeholder
`{param1:default}` or `{param2:default}` in the text lines of the ads of one keyword. The reference states that
there are at most two ad parameters per ad group criterion, one with index 1 and one with index 2, and Google
refuses a criterion that is not a keyword (`AD_GROUP_CRITERION_MUST_BE_KEYWORD`). The tools call
`AdParameterService.MutateAdParameters` with up to 10 operations (the bound of Qatlas; the reference names none)
and without `partial_failure`.

- **`ad-parameters.create`.** Each entry of `ad_parameters` needs `ad_group_criterion`
  (`customers/ID/adGroupCriteria/AD_GROUP_ID~CRITERION_ID`), `parameter_index` (1 or 2), and `insertion_text`.
  Google requires none of the three in the schema; Qatlas asks for all of them, because an ad parameter without
  a keyword, an index, or a number has no use. A keyword and an index appear once per call. Whether the keyword
  already has a parameter with that index is Google's to say.
- **`ad-parameters.update`.** An entry names the ad parameter by `resource_name`
  (`customers/ID/adParameters/AD_GROUP_ID~CRITERION_ID~INDEX`, the index 1 or 2) and gives `insertion_text`, the one
  member that can change. The keyword and the index are immutable and no argument. The mask is `insertion_text`.
- **`ad-parameters.remove`.** Names the ad parameters by `resource_names`. It has no `response_content_type`: a
  removed ad parameter has no mutable resource to show. The ads of the keyword no longer get the value in their
  placeholder; the ad parameter can be created again.
- **The insertion text** is the number as Google describes it: a comma or a period as separator with an optional
  period or comma for fractions (`1,000,000.00`, `2.000.000,10`), a currency symbol or a currency code before or
  after it (`$99.99`, `99.99USD`, `EUR200`), `%`, a plus or minus sign (`-10.99`, `25+`), or `/` between two
  numbers (`4/1`, `0.95/0.45`). Qatlas checks only what the description cannot allow, so that a text that is
  plainly no number never leaves: 1 to 50 bytes (the bound is Qatlas's own), at least one digit, besides the digits
  only `.`, `,`, `%`, `+`, `-`, `/`, a currency symbol, and one currency code of three capital letters, and no
  space. Whether the form is right is Google's to say; it answers `INVALID_INSERTION_TEXT_FORMAT`.
- **Resource names stay with the account.** `ad_group_criterion`, `resource_name`, and `resource_names` must be of
  the `customer_id` of the call, with positive IDs and without a temporary ID. The ad parameters of a call are
  named once. This is checked before a secret is read.
- **The answer.** One entry per ad parameter, in the order given. The result must be the ad parameter asked for;
  for a create it must be the one that follows from the keyword and the index
  (`customers/ID/adParameters/AD_GROUP_ID~CRITERION_ID~INDEX`). An ad parameter returned with `MUTABLE_RESOURCE`
  must agree with its name and hold a keyword of the account. Anything else is `invalid-provider-response` with the
  note about the unknown outcome.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds, in particular the paths inside an
  ad type such as `responsiveSearchAd.headlines`, and `insertionText` for an ad parameter. No live call has been
  made.
- Which ads and which members `MutateAds` accepts beyond the four types the reference names: the reference states
  that `synthetic_content_info` can be updated only for the ad types `HTML5_UPLOAD_AD`, `DYNAMIC_HTML5_AD`, and
  `IMAGE_AD`, and the rest is Google's validation. Use `validate_only`.
- Whether `MutateAds` and `MutateAdParameters` have an upper limit of operations; Qatlas keeps its own.
- Which campaign types and ad types use ad parameters: the reference of the resource does not say.
- The length limits of the texts of an ad and the length limit of the insertion text.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- The change of the status, the dates, or the asset automation of an ad ([ad tools](ads.md)), the creation of ads,
  and labels on ads.

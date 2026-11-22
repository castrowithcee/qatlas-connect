---
description: >
  Describes the asset tools of the Google Ads provider: assets.create and assets.update for the asset types of the API with a structured argument per member, image and media bundle bytes through local_path or content_base64 under the files release, campaign-automatically-created-assets.remove, with the tools list requirement, confirmation, the operations list of at most 10, validate_only, the atomic single request, the unknown outcome, the checks on addresses and resource names, the size limits, the members left out on purpose, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: asset tools

These tools create and change the assets of a Google Ads account and remove automatically created assets from
campaigns. They add to the read tools of the [Google Ads provider](README.md), whose configuration, targets,
quotas, and errors apply here too. What every changing tool does (tools list, confirmation, no repetition,
unknown outcome, resource names that stay with the account) is described for the
[settings tools](settings.md); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.assets.create` | create | creates one to 10 assets of any offered type |
| `google.ads.assets.update` | update | changes members of one to 10 existing assets |
| `google.ads.campaign-automatically-created-assets.remove` | delete | ends the use of automatically created assets by campaigns |

All three belong to no profile: a connection offers them only when its `tools` list names each one. `create`
needs in addition a directory released for reading under `files` on the connection, since it can read the bytes
of an image from a local file; without that release the connection does not offer it, and `update` and `remove`
are unaffected. Read assets with `reports.search` on `asset`, `campaign_asset`, and the other asset resources.

## Operations list

`create` and `update` take `assets`, a list of one to 10 entries. All entries go in one request to
`AssetService.MutateAssets`, atomic and without `partial_failure`: Google applies all or none. The bound of 10
is Qatlas's own; the reference of the method documents none. `validate_only` validates the whole list without
making it; Google then answers errors only and no results. `response_content_type` `MUTABLE_RESOURCE` adds the
mutable members of each asset to the answer. `remove` takes `removals`, one to 10 entries, and offers neither
`validate_only` nor `response_content_type`, because
`RemoveCampaignAutomaticallyCreatedAssetRequest` has neither; it sends `partial_failure` as `false`, which that
request requires.

The answer of `create` and `update` holds `results`, one entry per asset in the order of the call, each with
the resource name `customers/ID/assets/N`. A result or asset that names another account, another number of
results than assets, or any asset other than the one named by an update is `invalid-provider-response` with the
note that the outcome is unknown.

## assets.create

Each entry holds the common members and exactly one type member:

| Member | Rule |
| --- | --- |
| `name` | text |
| `final_urls`, `final_mobile_urls` | up to 20 `https` addresses without user info |
| `tracking_url_template` | an `https` address without user info, checked like the one of `customers.update` |
| `final_url_suffix` | parameters such as `utm_source=x`, no address |
| `url_custom_parameters` | up to 20 entries with `key` and `value` |
| `synthetic_content_info` | the `advertiser_attestation` with `status` and `source` |
| type member | one of the types below, named as in the API in snake case |

Types: `text_asset`, `image_asset`, `media_bundle_asset`, `youtube_video_asset`, `youtube_video_list_asset`,
`sitelink_asset`, `callout_asset`, `structured_snippet_asset`, `call_asset`, `mobile_app_asset`,
`hotel_callout_asset`, `page_feed_asset`, `price_asset`, `promotion_asset`, `lead_form_asset`,
`book_on_google_asset`, `call_to_action_asset`, `business_message_asset`, `app_deep_link_asset`,
`demand_gen_carousel_card_asset`, `hotel_property_asset`, and the dynamic feed types `dynamic_education_asset`,
`dynamic_real_estate_asset`, `dynamic_custom_asset`, `dynamic_hotels_and_rentals_asset`,
`dynamic_flights_asset`, `dynamic_travel_asset`, `dynamic_local_asset`, `dynamic_jobs_asset`. The members of a
type are those of the proto message of v26, with the members that Google requires marked as required in the
input schema and checked before a request. Oneof members, such as the discount of a promotion, exclude each
other. A value of an enum is its name, such as `MONDAY`; Qatlas checks its form, never its value. Lengths,
characters, and combinations are Google's to validate: use `validate_only`.

**Images and bundles.** `image_asset` and `media_bundle_asset` take exactly one of `local_path`, a file inside a
directory the connection releases for reading, and `content_base64`. A file is 1 byte to 5120 KiB, which is the
size Google's help pages give for image assets; the API reference states no size, so this is a Qatlas limit
and Google may refuse sooner. Inline content is limited to 4 MiB like in every file tool, and one call carries
at most 10 MiB of bytes. The account is checked against the targets first, then the file is read, and a path outside the released
directories refused, before a secret is read and before any request. Paths are only read. Image bytes are never part of an answer.

## assets.update

An entry holds `resource_name`, the asset as `customers/ID/assets/N`, and the members to change. The field mask
names exactly the members given: top-level members and, inside a type member, its members. An empty text or an
empty list clears a member. Only the mutable types are offered: the immutable ones (`text_asset`, `image_asset`,
`media_bundle_asset`, `youtube_video_asset`, `youtube_video_list_asset`, `book_on_google_asset`,
`call_to_action_asset`, `app_deep_link_asset`, `demand_gen_carousel_card_asset`, `hotel_property_asset`) and the
members set at creation, such as `external_redeem_info` of a promotion, are not. The questions of an existing
lead form can be reordered but not added or removed, as Google documents. An asset cannot be removed; remove it
from what uses it.

## Checks on addresses and resource names

`final_urls`, `final_mobile_urls`, `tracking_url_template`, and every other address member (the privacy policy of
a lead form, the page of a page feed, the images of the dynamic types) must be `https` without user info,
before a secret is read. The template and the suffix send click data to the address they name, so they follow the
rules of [customers.update](settings.md#customersupdate), and the tools are offered only through the `tools`
list. App links of the dynamic types are free text, not addresses.

A reference to another object, such as `background_image_asset` or `call_conversion_action`, must belong to the
`customer_id` of the call. So must the `resource_name` of an update and the `campaign` and `asset` of a removal.
A temporary negative ID is refused. The same holds for every resource name Google returns: one of another
account makes the answer invalid.

## campaign-automatically-created-assets.remove

Calls `AutomaticallyCreatedAssetRemovalService.RemoveCampaignAutomaticallyCreatedAsset`. Each removal names a
`campaign`, an `asset`, and the `field_type`, an `AssetFieldType` name such as `HEADLINE`. The asset stays in
the account; only its use by the campaign for that field type ends. Google answers nothing but a possible error,
so the answer lists the removals as asked.

## Left out on purpose

- The delivery of lead form data to a webhook (`delivery_methods` with `advertiser_webhook_url` and
  `google_secret`): it cannot be given and is never returned.
- `location_asset`, which Google only returns.
- `partial_failure`, and `validate_only` where Google offers none.
- Output-only members in the answer of a change: the policy summaries and the image data come with
  `reports.search`, and image bytes and `data` never.

## Boundary

Not verified:

- That `MutateAssets` accepts every type above. The description of the service names the image, the media
  bundle, and the YouTube video as the types to create with it and says text assets are created with an ad;
  the resource defines the others, and the types of the dynamic feeds depend on the account.
- A size limit for the image and bundle bytes, a bound for the number of operations, and the lengths of the
  members, which the reference does not state.
- The roles an account needs, the answer to `MUTABLE_RESOURCE` for each type, and the field mask with nested
  members in camel case. No live call has been made.

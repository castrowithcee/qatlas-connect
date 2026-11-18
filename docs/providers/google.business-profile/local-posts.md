---
description: >
  Describes the local post tools of the Google Business Profile provider: listing and reading posts, creating posts of the types STANDARD, EVENT, OFFER, and ALERT, changing them with a required update mask, and deleting them, the confirmation text with its 500-character limit, the checks per post type, the https-only links, the bounds on untrusted post text, and what the v4 documentation leaves unverified.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: local posts

The local post tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The tools use the My Business API v4 at
`https://mybusiness.googleapis.com/v4`, like the [review tools](reviews.md#the-v4-client). Google documents
the methods for a **verified** location.

A local post is **public**: it appears on the Google profile of the business and in Google Search, in the name
of the business. The parameters and answer members come from the reference at
`developers.google.com/my-business/reference/rest/v4` (the LocalPost resource with its nested types and enums,
the methods `localPosts.list`, `get`, `create`, `patch`, `delete`, and the MediaItem resource), read on
2026-11-18; no live call was made.

## The v4 paths

A post is named `accounts/ACCOUNT/locations/LOCATION/localPosts/POST`. The tools check every path against its
form and the target before a secret is read, and prove the location for an account target, exactly as the
review tools do: see [the v4 client](reviews.md#the-v4-client). A refusal never names the other object. The
tools are offered on every target.

## Reading

`local-posts.list` and `local-posts.get` belong to the profile `read` and are safe to repeat.

- `local-posts.list`: `location` as `accounts/ACCOUNT/locations/LOCATION`, `page_size` 1 to 100 (Google's
  default is 20), and `cursor`. The result holds `localPosts` and `next_cursor`. A cursor belongs to the
  location it came from.
- `local-posts.get`: `local_post` as `accounts/ACCOUNT/locations/LOCATION/localPosts/POST`.

A post carries `name`, `languageCode`, `summary`, `callToAction` (`actionType`, `url`), `createTime`,
`updateTime`, `event` (`title`, `schedule` with `startDate`, `startTime`, `endDate`, `endTime`), `state`, `media`,
`searchUrl`, `topicType`, `alertType`, and `offer` (`couponCode`, `redeemOnlineUrl`, `termsConditions`).
Google may reject a post that violates its policies, and the `state` tells: `LIVE`, `PROCESSING` (not yet in
Search), or `REJECTED`. A media item shows `name`, `mediaFormat`, `sourceUrl`, `googleUrl`, `thumbnailUrl`,
`createTime`, `dimensions`, and `description`; its `insights` and `attribution` (name and photo of a
contributor) are not answered, and its location association does not apply to a post.

### Untrusted text

Texts, titles, codes, terms, and every link of a post (action, redemption, media, search) are written by whoever
edits the profile. They are returned unchanged as data: texts are cut at 4000 characters (titles and codes at
500, links at 2048, enumerations at 100) and lists at 250 entries, members that are not documented are dropped,
and nothing in a result is followed. No link is ever fetched.

## Creating

`local-posts.create` (`POST .../localPosts`) publishes a post and belongs to the profile `write`. It needs
confirmation. It is **not idempotent** and is never sent twice: after an error that says the outcome is
unknown, read the posts with `local-posts.list` before creating again. The result is the post as Google holds
it, with its `name`, `state`, and `searchUrl`.

| Argument | Meaning |
| --- | --- |
| `location` | required, `accounts/ACCOUNT/locations/LOCATION` |
| `topic_type` | required, `STANDARD`, `EVENT`, `OFFER`, or `ALERT` |
| `language_code` | language of the post, such as `en` or `de` |
| `summary` | the text, plain text |
| `call_to_action` | `action_type` (`BOOK`, `ORDER`, `SHOP`, `LEARN_MORE`, `SIGN_UP`, `CALL`) and `url`, required unless `CALL` and not allowed with `CALL` |
| `event` | `title` and `schedule`: `start_date` and `end_date` as `year`, `month`, `day`, `start_time` and `end_time` as `hours`, `minutes`, and optionally `seconds`, `nanos`; all four are required |
| `offer` | `coupon_code`, `redeem_online_url`, `terms_conditions`; at least one |
| `alert_type` | `COVID_19` |
| `media` | up to 10 items of `source_url` and optionally `media_format` `PHOTO` |

`GET_OFFER` is deprecated by Google and not offered. The reference names no recurrence field, so none is
offered.

### Checks per type

Before anything is sent, the tool refuses what Google documents as invalid or ignored for the type, so that a
person never confirms a post that would look different:

| Type | Needs | Refused |
| --- | --- | --- |
| `STANDARD` | nothing | `event`, `offer`, `alert_type` |
| `EVENT` | `event` | `offer`, `alert_type` |
| `OFFER` | `event` | `alert_type`, `call_to_action` (Google ignores it) |
| `ALERT` | `alert_type` | `event`, `offer`, `media` (Google supports only summary and call to action) |

Google marks `ALERT` as not always available for authoring and is disabling new COVID-19 alerts, so Google
may refuse it.

### Links and texts

Every link (`call_to_action.url`, `offer.redeem_online_url`, `media.source_url`) must be a plain `https`
address: ASCII without spaces, control characters, backslashes, or user information, at most 2000 bytes. The
tool never calls it; for a media item, Google fetches the photo from `source_url`. Texts must be valid UTF-8
without control, invisible formatting, or separator characters other than line breaks and tabs.

### Confirmation

The confirmation shows the location, the type, the whole text, the language, the button and its address, the
event with its schedule, the offer with code, redemption address, and terms, the alert type, every media
source, and every URL found inside the texts. The core cuts a confirmation at 500 characters, which would hide
what a person confirms, so a post whose confirmation does not fit is refused before anything is sent; it is
about 400 characters of text with typical context, and every link counts. Shorten the text or send fewer links.

## Changing

`local-posts.update` (`PATCH .../localPosts/POST?updateMask=...`) changes fields of a post; it belongs to the
profile `write` and needs confirmation.

| Argument | Meaning |
| --- | --- |
| `local_post` | required, the post path |
| `topic_type` | required, the type the post has; used for the checks and the confirmation and never changed |
| `update_mask` | required, the fields to change, from `languageCode`, `summary`, `callToAction`, `event`, `media`, `offer`, `alertType` |
| content | the same members as `create`, only those named in the mask |

The mask must name exactly the fields given with a value: a field in the mask without a value would be cleared
and a value outside the mask ignored, so both are refused, and no tool clears a field. Only whole top-level
fields can be named: a named field is replaced as a whole. The same type checks and the same confirmation limit
apply, and the confirmation shows the post path, the type, and the new values. The tool reads nothing first and
shows no previous values.

## Deleting

`local-posts.delete` (`DELETE .../localPosts/POST`) removes the post from the profile and from Google Search.
It needs confirmation, belongs to the profile `write`, and cannot be undone. The result is `local_post` and
`deleted: true`.

**Spacing.** Creating, changing, and deleting count against the 10 changes per minute and profile; Qatlas
spaces them by 6 seconds per location, like the other changes. A change whose outcome is unclear (timeout,
`5xx`, unreadable answer) is reported as such and never repeated.

## Not verified

- No live call was made. The reference states no length for a summary, a title, a code, the terms, or the
  number of media items, and none for a post ID: Qatlas accepts post IDs of up to 256 letters, digits, `_`, and
  `-`, and 10 media items.
- The reference says that only `sourceUrl` is supported for the media of a post, and that `PHOTO` is the only
  supported media format. Qatlas sends `mediaFormat` only when it is given.
- Whether `topicType` may be sent with a `PATCH` whose mask does not name it. Qatlas always sends it, as the
  reference marks it required.
- The reference gives no rules for the time of day beyond the ranges: Qatlas refuses the hour 24 and a leap
  second, and an event that ends before it starts.
- `localPosts.reportInsights` is not part of these tools.

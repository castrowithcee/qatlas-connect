---
description: >
  Describes the media tools of the Google Business Profile provider: listing and reading the photos and videos of a location, adding one from a public https address, changing its category, and deleting it, with the confirmation, the limits of what Google lets a change do, and why no file is uploaded.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: media

The media tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there; the My Business API v4 client and the form of a v4 path, which these
tools share, are described in [reviews](reviews.md#the-v4-client). Parameters and answer members come from the
reference at `developers.google.com/my-business/reference/rest/v4/accounts.locations.media` (the MediaItem
resource and the methods `list`, `get`, `create`, `patch`, and `delete`), read on 2026-11-18; no live call was
made. Google documents the methods only for a **verified** location.

Media is public: every visitor of the business profile sees it, so an image that is pushed in is a damage to the
reputation of the business. The three changing tools therefore belong to the profile `write` and each needs the
confirmation of a person.

## Reading

`media.list` and `media.get` belong to the profile `read`, are safe to repeat, and return personal data: the
`attribution` of a photo that a customer uploaded names the customer.

- `media.list`: `location` as `accounts/ACCOUNT/locations/LOCATION`, `page_size` 1 to 250 (Google's default is
  100, its maximum 2500; Qatlas lists are cut at 250 entries, so it offers no larger page), and `cursor`. The
  result holds `mediaItems`, `totalMediaItemCount` of the whole location, and `next_cursor`.
- `media.get`: `media_item` as `accounts/ACCOUNT/locations/LOCATION/media/MEDIA`.

A media item carries `name` (a profile photo has the key `profile`), `mediaFormat` (`PHOTO` or `VIDEO`),
`locationAssociation` (`category` or `priceListItemId`; missing for media that does not belong to the location
itself, such as the photo of a local post), `googleUrl` (a Google-hosted address that may change, for a video a
preview image), `thumbnailUrl`, `createTime`, `dimensions` (`widthPixels`, `heightPixels`), `insights`
(`viewCount`), `attribution` (`profileName`, `profilePhotoUrl`, `profileUrl`, `takedownUrl`), and `description`.
For a profile photo Google answers the epoch as `createTime` and no dimensions.

Descriptions and the names, photo links, and profile links of contributors are written by anyone. They are
returned unchanged as data and cut like every text of the provider, and no address is ever fetched. `attribution`
is returned as Google sends it, because Google requires that it be shown unaltered.

The members `sourceUrl` and `dataRef` are input only and not answered: Google does not keep the address of a
created item, and an address that was handed over for fetching could carry a token.

## Adding by address

`media.create` (`POST .../media`) adds a photo or video that Google fetches itself from a public address.
Qatlas never fetches the address, and no tool uploads a file, reads a local file, or calls `startUpload`: that
needs access to the file system, for which no contract exists.

| Argument | Meaning |
| --- | --- |
| `location` | required, the v4 location path |
| `media_format` | required, `PHOTO` or `VIDEO`; Google documents only photos for this version |
| `source_url` | required, an `https` address of at most 2000 bytes, ASCII without spaces, control characters, or user information |
| `location_association` | required, exactly one of `category` (`COVER`, `PROFILE`, `EXTERIOR`, `INTERIOR`, `PRODUCT`, `AT_WORK`, `FOOD_AND_DRINK`, `MENU`, `COMMON_AREA`, `ROOMS`, `TEAMS`, `ADDITIONAL`) or `price_list_item_id` |
| `description` | at most 500 characters, without control or invisible formatting characters; Google accepts it only for an item that is not a `COVER` photo |

Google requires photos other than `PROFILE` and `COVER` to be at least 250 pixels on the short edge and 10240
bytes. Qatlas cannot check this without fetching the address, so Google's answer decides; a request Google
rejects is reported as `invalid-provider-response`, since Qatlas checked its form. Google documents one cover and one
profile photo per location but not what a second one does.

The confirmation shows the location, the format, the category or price list item, the **whole** address, and the
description. The core cuts a confirmation at 500 characters, so a call whose confirmation would not fit with that
context (an address of more than about 350 characters, less with a description) is refused before anything is sent.
Use a shorter address or description.

A create is not idempotent. It is sent once and never repeated; after a timeout, a `5xx`, or an unreadable answer
the result is reported as unknown, so read the list before adding again.

## Changing the category

`media.update` (`PATCH .../media/MEDIA`) changes the category of an item. Google lets a change set only the
category, and not to `COVER` or `PROFILE`; format, source, description, and price list item cannot be changed, so
a different image is a new item.

| Argument | Meaning |
| --- | --- |
| `media_item` | required, the media path |
| `location_association` | required, an object with `category`, one of the categories above except `COVER` and `PROFILE` |
| `update_mask` | required, exactly `["locationAssociation.category"]` |

Google treats a missing mask as a full update of every editable field, so the tool refuses a call without the
mask. The confirmation shows the item and the new category. The request carries no previous values: Qatlas does
not read the item first.

## Deleting

`media.delete` (`DELETE .../media/MEDIA`) removes an item from the public profile. It needs confirmation, shows
the item, and keeps no copy. It belongs to the profile `write`; a connection that must not allow it names its
tools in a `tools` list instead of the profile.

**Spacing.** Create, update, and delete count against the 10 changes per minute and profile; Qatlas spaces them
by 6 seconds per location, like the other changes.

## Not verified

- No live call was made. The reference gives no limits for the length of an ID, so Qatlas accepts media keys of up
  to 256 letters, digits, `_`, and `-`, which includes the key `profile`.
- The reference does not list the values of `updateMask`. Qatlas sends `locationAssociation.category`, the
  camelCase path of the only editable field, and nothing else.
- The format of a price list item ID and whether Google accepts an item that does not exist; Qatlas accepts up to
  128 printable ASCII characters without spaces.
- Which address forms Google accepts as `sourceUrl`, and how long it takes to fetch one.
- Whether `VIDEO` is accepted: the reference lists it as a format but states that only photos are supported.

## Left out

- `media.startUpload` and the `dataRef` member: the upload of a local file, see [Adding by address](#adding-by-address).
- `sourceUrl` and `dataRef` in answers, see [Reading](#reading).
- The deprecated category `LOGO`, which Google maps to `PROFILE`: use `PROFILE`.

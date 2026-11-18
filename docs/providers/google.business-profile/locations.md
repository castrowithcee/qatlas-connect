---
description: >
  Describes the locations tools of the Google Business Profile provider: listing and reading locations with the required read mask, reading several locations at once through the v4 batch method, reading what Google changed, creating, changing, and deleting a location, the writable fields, the previous values that a change compares, the review by Google, the listed-only deletion, and how account and location targets narrow and prove them.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Business Profile: locations

The location tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. `list`, `get`, `batch-get`, and `get-google-updated` read and are safe to repeat.
`create`, `update`, and `delete` change public profile data and need confirmation.

## Read mask

Both tools need `read_mask`: the fields of a location to return, each once, from the top-level fields of the
discovery document: `name`, `languageCode`, `storeCode`, `title`, `phoneNumbers`, `categories`,
`storefrontAddress`, `websiteUri`, `regularHours`, `specialHours`, `serviceArea`, `labels`,
`adWordsLocationExtensions`, `latlng`, `openInfo`, `metadata`, `profile`, `relationshipData`, `moreHours`, and
`serviceItems`. There is no default and no wildcard, so the size and content of an answer are always chosen. A
missing, empty, or unknown mask is refused before anything is sent.

Locations hold the public contact data of a business, such as its address and phone numbers, and the website,
description, and labels whoever edits the profile entered. `metadata` holds Google's own state: the place ID,
links to Google Maps and to writing a review, a possible duplicate location, and flags such as pending edits.

## google.business-profile.locations.list

`GET https://mybusinessbusinessinformation.googleapis.com/v1/{account}/locations`. Lists the locations of one
account. For a personal account Google lists only the locations it owns directly; for other types every
location the account reaches. `accounts/-` names the consenting account and also lists locations it reaches
through groups; only an unbound connection takes it.

- `filter` is Google's location filter, applied by Google; `totalSize` then holds Google's approximate count.
- `order_by` sorts by `title` or `store_code`, each optionally descending.
- `page_size` asks for 1 to 100 locations per page; Google's default is 10.

`next_cursor` reads on and is bound to the account, the read mask, the filter, and the sort order, not to the
page size. An empty list is no error.

A connection bound to an account lists that account. A connection bound to a location answers with that
location alone, read with `locations.get`; it refuses `account`, `filter`, and a cursor, since a filter Google
would apply cannot be applied to a single location.

## google.business-profile.locations.get

`GET https://mybusinessbusinessinformation.googleapis.com/v1/{location}`. Reads one location as the business
last set it; what Google Maps shows may differ, for example after an update by Google.

A connection bound to a location reads only that location, and `location` may be left out. A connection bound
to an account first proves that the location is in the account's location list, as described under
[Target](README.md#target), and refuses a location the list does not hold without reading it.

## google.business-profile.locations.batch-get

`POST https://mybusiness.googleapis.com/v4/{account}/locations:batchGet` (My Business API v4
`accounts.locations.batchGet`). Reads several locations of one account in one call. `locations.get` stays the
main way to read a location, with the fields of the business information API and a read mask; this tool is for
many locations at once. Parameters and answer members come from the v4 reference at
`developers.google.com/my-business/reference/rest/v4` (the method, the Location resource, Category, PostalAddress,
and Date), read on 2026-11-18; no live call was made.

- `account` is `accounts/ACCOUNT`. It may be left out on a connection bound to an account and is required on one
  bound to a location.
- `location_names` holds 1 to 50 locations as `accounts/ACCOUNT/locations/LOCATION`, each below `account`, none
  twice. The reference states no maximum, so 50 is Qatlas's own bound, the same as for `reviews.batch-get`. An
  empty list and a longer one are refused before anything is sent.

The answer holds `locations`, the v4 Location of each location Google returned: `name`, `languageCode`,
`storeCode`, `locationName`, `primaryPhone`, `additionalPhones`, `address`, `primaryCategory`,
`additionalCategories`, `websiteUrl`, `regularHours`, `specialHours`, `serviceArea`, `locationKey`, `labels`,
`adWordsLocationExtensions`, `latlng`, `openInfo`, `locationState`, `attributes`, `metadata` (`mapsUrl` and
`newReviewUrl`), `profile`, `relationshipData`, and `moreHours`. The v4 fields differ from those of
`locations.get`: the business name is `locationName`, the address is `address`, hours use `hh:mm` text, and the
state flags are in `locationState`. The reference marks `priceLists` and `metadata.duplicate` as deprecated, so
they are never answered. An attribute value of a kind other than string or boolean is dropped. There is no read
mask: Google answers the whole location. Text is untrusted content, cut like in the other tools and never
followed; links are never fetched.

The reference documents no per-location error. A failure of the request fails the call. A location that Google
leaves out of its answer is missing from the result, and an entry for a location that was not asked for is
dropped.

Every name is checked against its form and the target before a secret is read, so one name outside the binding
fails the whole call and names nothing. On a connection bound to an account the locations are then proved with
one pass over the account's location list, as for `locations.get`, before the batch is sent. A connection bound
to a location compares only the location ID of each name.

## google.business-profile.locations.get-google-updated

`GET https://mybusinessbusinessinformation.googleapis.com/v1/{location}:getGoogleUpdated`. Reads the location as
it appears live on Google Maps and Search, with the fields of `read_mask`, together with `pendingMask` (edits
Google has not applied yet) and `diffMask` (fields that differ from what the business set). It is part of the
profile `read`, takes `location` and `read_mask` like `locations.get`, and proves the location first on a
connection bound to an account.

## Writing a location

The three changing tools write only the fields the discovery document marks as input: `storeCode`, `title`,
`phoneNumbers`, `categories`, `storefrontAddress`, `websiteUri`, `regularHours`, `specialHours`, `serviceArea`,
`labels`, `adWordsLocationExtensions`, `latlng`, `openInfo`, `profile`, `relationshipData`, `moreHours`, and
`serviceItems`, plus `languageCode` on creation only, since Google never changes it afterwards. `name` and
`metadata` are Google's and not arguments, and so are the members of a category and of `openInfo` that Google
only answers (`displayName`, service types, `canReopen`). A field is a structured argument of the same name and
shape as the read answers it. Values are public as soon as Google accepts them, and no value, such as a website,
is ever fetched.

`title`, an address, and a category can lead Google to verify or review the location again, or to suspend its
profile. That effect can only be observed live. Google counts at most 10 changes per minute and profile, which
the tools wait for like the admin tools.

The confirmation of a change shows what the contract asks for in full, the title of a create and the old and
new value of every changed field of an update, and the confirmation of a create may hold 2048 characters, that of an update 16384. A call
whose confirmation would be longer is refused before anything is sent, with a notice instead of an excerpt, so
split a large change into several calls.

### google.business-profile.locations.create

`POST https://mybusinessbusinessinformation.googleapis.com/v1/{account}/locations`. Needs `title` and
`categories` with a `primaryCategory`; a call without them is refused before anything is sent. `account` is
optional on a connection bound to an account; a connection bound to a location does not offer the tool. The
call is not idempotent. `request_id` lets Google detect a repeated call; without it the tool generates a UUID,
and the result names the ID in `request_id`. After an unclear outcome check `locations.list` before creating
again. A create counts against the daily quota of the account. Part of the profile `write`.

### google.business-profile.locations.update

`PATCH https://mybusinessbusinessinformation.googleapis.com/v1/{location}?updateMask=...`. `update_mask` is
required and names top-level fields; each needs its new value as an argument, and a field outside the mask is
refused, so nothing unnamed is sent. An empty text or an empty list clears a field; an object field cannot be
cleared. `previous_values` is required: an object with exactly the fields of the mask, each as `locations.get`
answers it. It is only compared and never sent. The tool reads the location first and changes nothing, and
sends no change, unless every field still has that value; the confirmation shows the old and the new values.
Output-only members and empty values are ignored in the comparison. The tool refuses a call whose new values
all equal the previous ones. Part of the profile `write`.

### google.business-profile.locations.delete

`DELETE https://mybusinessbusinessinformation.googleapis.com/v1/{location}`. Listed-only: it is in no profile and
no effect list offers it, only a `tools` list that names it, and every call needs confirmation. Whether a
deleted location can be restored is not documented, so the tool is described as irreversible. Google refuses a
location it marks as not deletable through the API. Moving a location to another account (`locations.transfer`)
is not a tool.

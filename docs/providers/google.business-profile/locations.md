---
description: >
  Describes the locations tools of the Google Business Profile provider: listing the locations of an account with the required read mask, filter, sort order, page size and cursor, reading one location, the Google methods behind them, the location fields, and how account and location targets narrow and prove them.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Business Profile: locations

The location tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. Both tools read and are safe to repeat.

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

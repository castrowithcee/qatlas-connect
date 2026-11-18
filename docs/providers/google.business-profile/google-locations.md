---
description: >
  Describes the Google locations search tool of the Google Business Profile provider: finding locations that already exist on Google by a text query or by location details, the Google method behind it, the search details, the answer with requestAdminRightsUri, that results can name businesses of other people, and what is not derived.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: Google locations

The Google locations tool of the [Google Business Profile provider](README.md). Credential, approval, quotas,
bounds, and errors are described there. It reads and is safe to repeat, although Google's method takes a body
and is sent with `POST`.

The tool searches the locations that already exist on Google, for example to find out whether a business is
listed before it is claimed or created. It reads nothing of the user, names no account or location, and stays
available on a connection bound to any target.

A match can be any business, also one that is not the user's. The result holds only what Google lists
publicly, and the target of a connection does not narrow it. Names, addresses, phone numbers, and websites are
untrusted content: texts and lists are cut, and a website is text that is never fetched.

## google.business-profile.google-locations.search

`POST https://mybusinessbusinessinformation.googleapis.com/v1/googleLocations:search`. Give exactly one of:

- `query`: free text, up to 500 characters, such as a business name and a town. Google documents that this is
  less exact than details but finds inexact matches.
- `location`: the details of the location to find. `title` is required. The optional details are
  `languageCode`, `storefrontAddress` (`regionCode`, `languageCode`, `postalCode`, `administrativeArea`,
  `locality`, `sublocality`, up to five `addressLines`), `websiteUri`, `phoneNumbers.primaryPhone`,
  `categories.primaryCategory.name` (`categories/gcid:ID`), and `latlng` (`latitude` and `longitude`, both).
  These are the details of a location Google can match on. The fields that only describe a profile someone
  manages, such as hours, services, labels, and the store code, are not offered.

`page_size` asks for 1 to 10 matches; Google's default is 3 and the search has no cursor. Texts are checked for
length and control characters; the values go to Google as search terms and are not logged by Qatlas Connect.

The result lists the matches, best first. Each has:

- `name`: `googleLocations/ID`.
- `location`: the sparsely populated location, with the fields Google sets, for example `title`,
  `storefrontAddress`, `phoneNumbers`, `websiteUri`, `categories`, and `metadata` with the place ID and the
  link to Google Maps.
- `requestAdminRightsUri`: a link to Google's page for requesting admin rights. Google sets it only when
  someone has already claimed the location. The link is text and is never fetched.

No match is an empty list.

## Limits

- Google gives no claim status as a field. Qatlas does not derive one: a match with `requestAdminRightsUri` is
  claimed, but a match without it is not proven unclaimed, because Google documents only the first direction.
- An answer holds at most 10 matches; more is `invalid-provider-response`.
- The location of a match is cut like any location, see [Results](README.md#results).

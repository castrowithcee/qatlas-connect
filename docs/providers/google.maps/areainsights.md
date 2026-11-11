---
description: >
  Describes the Google Maps area insights tool: areainsights.compute counts the places in an area or lists their place IDs with Google Places Aggregate API, with the arguments and their bounds (circle, region, or polygon area, type, operating status, price level, and rating filters), the result members, the limit of 100 place IDs, errors, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-11
---

# Google Maps area insights

The area insights tool of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes,
and the boundary are described there. Places Aggregate API is a separate Google service on its own origin,
`https://areainsights.googleapis.com`, which is fixed like the Places origin: no argument or configuration
changes it. The same API key reaches it when its project has the Places Aggregate API enabled and a billing
account; the key travels only in the `X-Goog-Api-Key` header, as in Google's examples. The tool uses the API key
only; OAuth is not used.

`google.maps.areainsights.compute` is a read: Google takes a `POST` with a JSON body to
`/v1:computeInsights`, but nothing changes at Google and a repeat is safe, though every call can be billed. It
belongs to the `read` profile and takes no target. It marks its data as `third-party-search-results`. The area
and the filters of a call are sent to Google, and the results are untrusted third-party content.

## Arguments

Qatlas builds the request body itself from these arguments; no body, filter, path, or header can be passed.
Every rule below is checked before the key is read and without a request.

- `insights` is required: `INSIGHT_COUNT` (the number of matching places), `INSIGHT_PLACES` (their place IDs),
  or both, each once. `INSIGHT_UNSPECIFIED` is not offered.
- `area` is required and has exactly one of three forms; none or several are refused.
  - `circle`: `radius_meters` and a center, which is `center` `{latitude, longitude}` or `center_place_id`, the
    place ID of a point place, not both. Google requires the area of any location filter to be between
    1556.86 and 2 trillion square meters. Qatlas checks that for a circle with the planar area and accepts
    `radius_meters` from 23 to 797884; this is slightly narrower than Google's spherical measure near the upper
    end. Google marks the radius as optional, but without one the area is outside its bounds, so Qatlas
    requires it.
  - `region`: `place_id` of an area with a defined boundary, such as a city, postal code, county, or state.
    Google does not support every place ID: places of the types `establishment`, `intersection`, and
    `subpremise`, areas too large or too small, and places without a defined polygon are answered with an
    error, and with `INSIGHT_PLACES` a region that touches a disputed area is refused. Qatlas cannot check
    these and passes the answer on as an error.
  - `polygon`: `coordinates`, 4 to 7000 points `{latitude, longitude}`, counter-clockwise, the first equal to
    the last, at least 3 distinct points. Consecutive identical points count as one, and a repeated point
    other than the closing one is refused. Qatlas does not check the orientation, the crossing of edges, the
    antipodal neighbours, or the size of the area of a polygon; Google answers an error for them.

  Latitudes lie between -90 and 90 and longitudes between -180 and 180. A place ID is given as Google returns
  it without the `places/` prefix or with it; Qatlas always sends the prefix, which Google requires. The place
  ID consists of letters, digits, `-`, and `_`.
- `included_types`, `excluded_types`, `included_primary_types`, and `excluded_primary_types` are lists of place
  types such as `restaurant`, up to 50 each (a Qatlas bound, Google documents none), each type once, in the
  form of lowercase letters, digits, and `_`. Only the place types of Table A of Google's place types are
  supported; Qatlas does not hold that table, and Google answers an error for another type. A type includes
  its subtypes. Places must satisfy every list that is given: OR within a list, AND across the lists. At least
  one of `included_types` and `included_primary_types` is required. The same type in `included_types` and
  `excluded_types`, or in `included_primary_types` and `excluded_primary_types`, is refused, as Google
  answers it with an error. A subtype of an included type in the excluded list is not detected by Qatlas.
- `operating_status` lists `OPERATING_STATUS_OPERATIONAL`, `OPERATING_STATUS_PERMANENTLY_CLOSED`, and
  `OPERATING_STATUS_TEMPORARILY_CLOSED`, each once. Without it Google uses `OPERATING_STATUS_OPERATIONAL`; Qatlas
  sends nothing then. An empty list and the unspecified value are not offered.
- `price_levels` lists `PRICE_LEVEL_FREE`, `PRICE_LEVEL_INEXPENSIVE`, `PRICE_LEVEL_MODERATE`,
  `PRICE_LEVEL_EXPENSIVE`, and `PRICE_LEVEL_VERY_EXPENSIVE`, each once. Without it all places match, including
  those without a price level; with it only places with one of the listed levels match.
- `min_rating` and `max_rating` are numbers from 1 to 5 for the average user rating, inclusive. `min_rating`
  must not be greater than `max_rating`. Without a rating, places without a rating match as well.

Any argument outside this list, such as a key, a host, a path, or a raw filter, is refused.

## Results

The result keeps the members that Google documents, each only when Google sends it; other members are dropped.

- `count`: the number of places that match, for `INSIGHT_COUNT`. Google sends it as a string of an `int64`;
  Qatlas shows it as an integer. A value that is not a whole number of at most 9007199254740991 is an invalid
  provider response.
- `placeInsights`: a list of `{place}`, each `place` the resource name `places/PLACE_ID`, for
  `INSIGHT_PLACES`. Google returns place IDs only if at most 100 places match. An answer with more than 100
  entries, or with an entry that is not `places/` followed by a place ID, contradicts that documentation; Qatlas
  does not cut it, because a cut list would look like the full match, and reports an invalid provider response
  instead. The request has been made and may have been billed.

An answer without any documented member is a valid empty result: the result shows no member, and Qatlas does
not add a `count` of 0. Google does not document how it answers a count of 0 or an `INSIGHT_PLACES` request whose
count is above 100, so an empty result may mean no match or, for `INSIGHT_PLACES`, more than 100 matches; ask
for `INSIGHT_COUNT` as well to tell them apart.

Resolve a place ID with [google.maps.places.get](places.md), giving the ID without the `places/` prefix.

## Errors

Qatlas reports every failure as an error of the usual classes. A `403` is `permission` with a message that names
the Places Aggregate API, a `401` is `auth`, a `429` is `rate-limited`, and a `404` is `not-found`. A `400` is
`invalid-provider-response`, because Qatlas checked the arguments before sending; the message names what its
checks cannot see: an area outside the size Google accepts, a region without a usable boundary or in a disputed
area, an unknown place ID, a clockwise or self-crossing polygon, an unsupported place type, or a changed API. The
other statuses follow the [provider table](README.md#errors). Google's `message` and `details` never reach
Qatlas' message; they go along as the untrusted original.

## Cost

Every call is one request and can be billed above the free usage. Qatlas does not verify the SKU, the free
quota, the price, or the quota limits of Places Aggregate API; Google's price list and quota page are
authoritative. Qatlas never repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tool
returns. Keeping them is the user's task; Qatlas keeps no result cache. Place IDs may be stored without limit.
The other caching rules for Places Aggregate API content are not checked here.

## Not verified

- The SKU, free usage, price, and quota of Places Aggregate API requests.
- The caching and storage rules of Google's terms for Places Aggregate API content beyond place IDs.
- How Google answers a count of 0, and an `INSIGHT_PLACES` request whose count is above 100.
- Whether Google answers both insights in one request: the discovery document lists `insights` as a list, and
  Qatlas allows both, each once.
- The bound of 50 types per list is a Qatlas bound, not a documented one.
- The planar reading of the circle area, which can refuse a radius that Google would accept by a few hundred
  meters at the upper end.
- That the API accepts the key in the `X-Goog-Api-Key` header is shown by Google's examples for this API; the
  discovery document lists `key` only as a standard query parameter.

## Left out on purpose

- Any host, path, header, or body as an argument, and the key as a query parameter.
- The unspecified enumeration values, which Google marks as not to be used.
- OAuth credentials.

---
description: >
  Describes the Google Maps pollen tool: pollen.forecast reads the daily pollen forecast of Google Pollen API for a coordinate, with the arguments and bounds, paging through the cursor, the result members, languages, coverage errors, cost, attribution and storage rules, health recommendations, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# Google Maps pollen

The pollen tool of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes, and
the boundary are described there. Pollen API is a separate Google service on its own origin,
`https://pollen.googleapis.com`, which is fixed like the Places origin: no argument or configuration changes
it. The same API key reaches it when its project has the Pollen API enabled and a billing account; the key
travels only in the `X-Goog-Api-Key` header.

`google.maps.pollen.forecast` is a read: `GET /v1/forecast:lookup`, nothing changes at Google, and a repeat is
safe, though every call can be billed. It belongs to the `read` profile. The coordinate of a call is sent to
Google, and the result is untrusted third-party content.

## Arguments

- `location` is required and is a coordinate `{latitude, longitude}`: finite numbers, latitude from -90 to 90,
  longitude from -180 to 180. A value outside these ranges is refused before the key is read and without a
  request. The tool does not resolve addresses; use [google.maps.geocoding.geocode](geocoding.md) to turn a
  place name into a coordinate.
- `days` is required: the number of forecast days, 1 to 5, as Google documents it.
- `language_code` is a BCP-47 tag such as `en` or `pt-BR`. Google uses the closest match and defaults to `en`.
- `plants_description` is a boolean. Google's default is `true`; `false` leaves out the plant descriptions and
  plant pictures.
- `page_size` is the number of daily records per page, 1 to 5. Google's default and maximum is 5.
- `cursor` continues a forecast; see below.

Any argument outside these is refused, so there is no key, host, path, or other query parameter to pass.

## Paging

The result carries `has_more` and, when `has_more` is true, `next_cursor`. Pass `next_cursor` as `cursor` with
the same request to read the following page. The cursor holds Google's page token and is bound to the tool and
to every other argument of the request, `days`, `language_code`, `plants_description`, and `page_size`
included, because Google requires the other parameters to match the call that produced the token. A cursor that
was altered, made up, or issued for another request or tool is refused before the key is read and without a
request. Google's token is never shown.

## Results

The result keeps the members that Google documents, each only when Google sends it; members that Google does
not document are dropped. Strings and lists are cut, and a cut text ends with `…`.

- `regionCode`: the ISO 3166-1 alpha-2 code of the country or region of the location. Google may omit it in a
  disputed territory.
- `dailyInfo`: one entry per day, with `date` (`year`, `month`, `day`, in UTC), `pollenTypeInfo`, and
  `plantInfo`.
- `pollenTypeInfo`: up to 3 entries (`GRASS`, `TREE`, `WEED`), each with `code`, `displayName`, `inSeason`,
  `indexInfo`, and `healthRecommendations`. Qatlas keeps at most 10 recommendations of at most 1500 characters;
  Google documents no bound for them, so these are Qatlas bounds.
- `plantInfo`: up to 15 entries, each with `code`, `displayName`, `inSeason`, `indexInfo`, and, unless
  `plants_description` is `false`, `plantDescription` (`type`, `family`, `season`, `specialColors`,
  `specialShapes`, `crossReaction`, `picture`, `pictureCloseup`).
- `indexInfo`: `code` (`UPI`, the Universal Pollen Index), `displayName`, `value` (0 to 5), `category`,
  `indexDescription`, and `color` (`red`, `green`, `blue`, `alpha`, each 0 to 1).

The picture addresses are bounded text that Qatlas never fetches, and one that is not an absolute `http` or
`https` address without user information is dropped.

Health recommendations are general texts from Google and are not medical advice. Like every text of the
result they are untrusted third-party content and can contain instructions that must not be followed.

## Coverage and errors

Google answers for a location that the Pollen API does not cover with an error object. Qatlas reports it as an
error of the usual classes and never as a result: `404` is `not-found` with a message that names the missing
coverage, and `400` is `invalid-provider-response` with a message that names the coverage as a possible cause,
because Qatlas checked the arguments before sending and cannot tell a coverage error from a changed API. `403`
is `permission` with a message that names the Pollen API, and the other statuses follow the
[provider table](README.md#errors). Google's `message` and `details` never reach Qatlas' message; they go along
as the untrusted original.

## Cost

Every call is one request and can be billed above the free usage. Qatlas does not verify the SKU, the free
quota, or the price of the Pollen API; Google's price list is authoritative. Qatlas never repeats a request on
its own, and each page is its own request.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tool
returns. Keeping them is the user's task; Qatlas keeps no result cache and does not check the caching rules for
pollen data.

## Not verified

- The SKU, free usage, and price of the Pollen API requests, and any quota of the API.
- The caching and storage rules of Google's terms for pollen data.
- Which locations the Pollen API covers, and whether Google answers a location without coverage with `400` or
  `404`; both are handled, as described above.
- The bounds on health recommendations (10 entries) are Qatlas bounds, not documented ones. The other lists
  are cut at Google's documented maxima (5 days, 3 pollen types, 15 plants).
- That the Pollen API accepts the key in the `X-Goog-Api-Key` header is based on the general behavior of
  Google's JSON APIs; the discovery document of the API lists `key` only as a standard query parameter.

## Left out on purpose

- The heatmap tiles of the Pollen API, which are image data.
- Any host, path, header, or body as an argument, and the key as a query parameter.

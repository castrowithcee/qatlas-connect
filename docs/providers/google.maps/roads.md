---
description: >
  Describes the Google Maps road tools: roads.snap snaps a path of coordinates to the roads, roads.nearest finds the nearest road of independent coordinates, and roads.speed-limits reads posted speed limits for a path or place IDs from Google Roads API, with the arguments and bounds, the result members, the key as a query parameter, movement data, cost, attribution and storage rules, the access note for speed limits, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-11
---

# Google Maps roads

The road tools of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes, and
the boundary are described there. Roads API is a separate Google service on its own origin,
`https://roads.googleapis.com`, which is fixed like the Places origin: no argument or configuration changes it.
The same API key reaches it when its project has the Roads API enabled and a billing account.

All three tools are reads: nothing changes at Google and a repeat is safe, though every call can be billed. They
belong to the `read` profile. The results are untrusted third-party content.

| Tool | Endpoint | Returns |
| --- | --- | --- |
| `google.maps.roads.snap` | `GET /v1/snapToRoads` | The points of a path snapped to the roads, optionally with the road geometry between them |
| `google.maps.roads.nearest` | `GET /v1/nearestRoads` | The nearest road segment of each of up to 100 independent points |
| `google.maps.roads.speed-limits` | `GET /v1/speedLimits` | The posted speed limit of road segments, for a path or for place IDs |

## Endpoint and key

Google's reference and discovery document for Roads API name the key only as the `key` query parameter. Qatlas
therefore sends it that way, as the geocoding tools do, and not in the `X-Goog-Api-Key` header. The URL never
leaves the request function: no message, log entry, or result carries it, a transport error is reduced to a
class without the error text, and the key is registered with the redactor. The host and the paths are fixed, a
redirect is never followed, and a request is sent once and never repeated.

## Movement data

A path or a set of points describes where a vehicle or a person was and when. The points are sent to Google,
and the results repeat them. The tools therefore mark their data as `google-maps-movement-data`, the class of
the [route tools](routes.md#movement-data). The invocation log records metadata such as the operation, the
connection, the result code, and the sizes of arguments and result, never their content.

## Arguments

Points are structured coordinates `{latitude, longitude}`: latitude from -90 to 90, longitude from -180 to 180.
Qatlas formats them itself, so no raw string reaches the URL. A list takes 1 to 100 points, Google's limit per
request. The tools do not resolve addresses; use [google.maps.geocoding.geocode](geocoding.md) to turn a place
name into a coordinate. Every value outside these ranges, and any argument outside the lists below, is refused
before the key is read and without a request, so there is no key, host, path, or other query parameter to pass.

`google.maps.roads.snap`:

- `path` is required: the points in travel order. Google works best when consecutive points are within 300 m of
  each other and may add a warning for a sparse path.
- `interpolate` `true` adds points that follow the full road geometry, so the result has more points than the
  request. Google's default is `false`.
- `travel_mode` is `DRIVING`, `CYCLING`, or `WALKING` and constrains the roads. The unspecified value is not
  offered, because Google treats it like a left-out parameter.
- `asset_id` names the asset of the path for abuse detection at Google for clients with asset-based SKUs. It is a
  text of at most 128 characters without control characters; the length is a Qatlas bound.

`google.maps.roads.nearest`:

- `points` is required: independent points that need not form a path.
- `travel_mode` as for `snap`.

`google.maps.roads.speed-limits`:

- `path` or `place_ids`, exactly one. The reference asks for "either a path or a placeId"; it does not say what
  happens with both, so Qatlas refuses both together and neither.
- `path` is snapped by Google first, as `snap` does, and the speed limits of those segments are returned with
  the snapped points.
- `place_ids` are 1 to 100 road segment IDs from `snap` or `nearest`, of letters, digits, `-`, and `_`, at most
  256 characters. Google snaps nothing, and a place ID that is not a road segment yields no speed limit.
- `units` is `KPH` (Google's default) or `MPH`.

`travel_mode` and `asset_id` are listed in the discovery document of Roads API v1, not in the reference pages
of the tools; the discovery document is the source for them.

## Results

The result keeps the members that Google documents, each only when Google sends it; members that Google does
not document are dropped. None of the documented members is deprecated, and all are kept.

- `snappedPoints` (`snap`, `nearest`, and `speed-limits` with `path`): each entry holds `location`
  `{latitude, longitude}`, `originalIndex`, and `placeId`. `originalIndex` is the 0-based index of the request
  point and is missing for an interpolated point; `nearest` may return several points for one coordinate with
  different locations or place IDs.
- `warningMessage` (`snap` and `speed-limits`): Google's user-visible warning, such as a sparse path.
- `speedLimits` (`speed-limits`): each entry holds `placeId`, `speedLimit`, and `units` (`KPH` or `MPH`). A road
  segment without a known speed limit has no entry. For a segment with variable limits Google returns the
  maximum for cars.

An empty answer, which Google sends as `{}` when nothing is found, is an empty list. A place ID is shown whole or
not at all: one longer than 256 characters is left out. Strings are cut at the usual bounds and end with `…`.
The lists are cut at 2000 snapped points and 500 speed limits, which are Qatlas bounds, not documented ones.
Google says speed limit data is not real-time and may be estimated, inaccurate, incomplete, or outdated.

## Errors

Google's error object is mapped as in the [provider table](README.md#errors), with messages that name the Roads
API. Google's `message` and `details` never reach Qatlas' message; they go along as the untrusted original, with
the key masked.

## Cost

Every call is one request and can be billed above the free usage. Google's price list shows the Roads SKUs in
the Pro group: `Roads - Route Traveled` for `snap`, `Roads - Nearest Road`, and `Roads - Speed Limits`, which
Google's usage page bills per speed limit returned, so a request with more points or place IDs costs more. Qatlas
does not verify the free usage, the price, or which SKU a call is billed under; Google's price list is
authoritative. Google limits the requests of a project per minute across the Roads endpoints. Qatlas never
repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tools
return. Google's policy page for Roads API allows the results to be shown on Google Maps and prohibits showing
them on the map of another provider. Place IDs may be stored without limit. Keeping the rules is the user's
task; Qatlas keeps no result cache and does not check the other caching rules.

## Speed limit access

Google's page for the speed limit service says it is available to customers with an Asset Tracking license, and
its price list marks the SKU as limited access. Whether the project of a given key has that access cannot be
checked by Qatlas; a refusal from Google is reported as `permission`.

## Not verified

- Which SKU each call is billed under, the free usage, the prices, and the per-minute quota of Roads API.
- Whether a project needs the Asset Tracking license or other access for the speed limit service.
- The caching and storage rules of Google's terms beyond the policy page of Roads API.
- That the Roads API accepts the key in the `X-Goog-Api-Key` header; the tools do not use it.
- The bounds on `asset_id` and on the result lists are Qatlas bounds. The discovery document lists the speed
  limit and nearest road methods as `POST`; the reference pages and Qatlas use `GET`.
- Whether Google accepts both `path` and `place_ids` of a speed limit request.
- OAuth tokens: Google's pages mention them for Roads API, but a connection holds an API key only.

## Left out on purpose

- Traffic speeds (`trafficSpeeds`, tile requests) of the discovery document: no reference page describes them.
- Any host, path, header, or body as an argument, and the key as an argument.
- The unspecified enumeration values.

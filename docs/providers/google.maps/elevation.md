---
description: >
  Describes the Google Maps elevation tool: elevation.get reads the elevation of up to 512 coordinates or of equidistant samples along a path from the Google Elevation API, with the argument forms and bounds, encoded polylines, the result members, the key as a query parameter, the status forms and their classes, location data, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-11
---

# Google Maps elevation

The elevation tool of the [Google Maps provider](README.md): `google.maps.elevation.get`. It reads and changes
nothing, a repeat is safe, and the profile `read` holds it. The key must belong to a Cloud project with the
Elevation API enabled and a billing account; the connection test of the provider proves only Places API (New).
Configuration, rate spacing, and the boundary are described in the provider page.

## Endpoint and key

The tool calls the legacy web service `GET https://maps.googleapis.com/maps/api/elevation/json`. The host and the
path are fixed; no argument and no configured `base_url` can change them, and a redirect is never followed.
Google's reference documents the key for this service only as the `key` query parameter, so Qatlas sends it
that way, as it does for [geocoding](geocoding.md#endpoint-and-key), and not in a header. The URL never leaves
the request function: no message, log entry, or result carries it, a transport error is reduced to a class
without the error text, and the key is registered with the redactor. A request is sent once and never repeated.
The output format is always JSON.

## Arguments

Google takes either `locations` or `path`. Qatlas takes exactly one of four arguments and refuses none, two, or
more, before the key is read and without a request:

- `locations`: 1 to 512 points `{latitude, longitude}`, latitude from -90 to 90, longitude from -180 to 180.
- `locations_polyline`: the same points as an encoded polyline (Google's `enc:` form, precision 5) of at most
  8192 characters.
- `path`: 2 to 512 ordered points. Google returns elevations at `samples` equidistant points along it.
- `path_polyline`: the path as an encoded polyline of 2 to 512 points.
- `samples`: 2 to 512, the number of points along the path, endpoints included. It is required with `path` and
  `path_polyline` and refused with `locations`. Google documents no maximum; 512, the number of results one
  answer can hold, and the minimum of 2 are Qatlas bounds.

Points are formatted by Qatlas, so no raw string reaches the URL. A polyline is decoded and every point is
range-checked; a text that does not decode cleanly, holds more than 512 points, or has a point outside the ranges
is refused. A request whose URL would exceed Google's limit of 16,384 characters is refused as well, which can
happen with full-precision coordinates; give fewer points or a polyline. The tool does not resolve addresses;
use [google.maps.geocoding.geocode](geocoding.md) for that.

## Results

- `status`: `OK`, or `DATA_NOT_AVAILABLE` or `ZERO_RESULTS` when Google has no data; `results` is then empty.
  Google's Elevation page lists `DATA_NOT_AVAILABLE`; `ZERO_RESULTS` is the value of the other legacy services
  and is treated the same.
- `results`: each entry holds `elevation` in meters, `location` `{lat, lng}` (for a path, the sampled point), and
  `resolution`, the largest distance in meters between the data points Google interpolated. `resolution` is
  missing when Google does not know it.

Google's `error_message` appears only with a status other than `OK`; it is untrusted provider text and goes only
along as the original of an error, never in a result. Members that Google does not document are dropped. The
list is cut at 512 entries. Single-point queries are the most accurate; Google smooths multi-point answers.

## Errors

Google reports an error as a `status` in an HTTP 200 answer. The classes are those of the
[legacy form](README.md#errors): `OVER_QUERY_LIMIT` is `rate-limited`, `OVER_DAILY_LIMIT` is `permission`,
`REQUEST_DENIED` is `auth`, `INVALID_REQUEST` and an unknown or missing status are `invalid-provider-response`,
and `UNKNOWN_ERROR` is `provider-error`. An HTTP error status is mapped by the HTTP status with a message that
names the Elevation API. Provider text goes along as the untrusted original with the key masked.

## Location data

The coordinates sent to Google tie a person or device to a place. The tool marks its data as
`google-maps-movement-data`, the class of the [route tools](routes.md#movement-data), because a path describes
movement. The invocation log records metadata such as the operation, the connection, the result code, and the
sizes of arguments and result, never their content.

## Cost

Every call is one request and can incur charges above the free usage Google grants. Google's price list shows
the Elevation SKU in the Pro group with 5,000 free events per month and billing account; prices and caps change
and Google's price list is authoritative. How Google counts the locations of one request is not stated in the
sources used. Google limits the requests of a project per minute (6,000 on Google's usage page). Qatlas never
repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tool
returns; keeping them is the user's task. Qatlas keeps no result cache and does not check the other caching
rules.

## Not verified

- The free usage, the prices, and the way Google counts the locations of a request.
- The caching and storage rules of Google's terms.
- That the Elevation API accepts the key in a header; the tool does not use one.
- The minimum of 2 for `samples` and the maximum of 512 are Qatlas bounds, as are the 8192 characters of a
  polyline.
- OAuth tokens are not used; a connection holds an API key only.

## Left out on purpose

- The XML output format.
- Any host, path, header, or body as an argument, and the key as an argument.

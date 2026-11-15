---
description: >
  Describes the Google Maps geolocation tool: geolocation.locate estimates a position and its accuracy from IP address, cell towers, and Wi-Fi access points with the Google Geolocation API, with the argument forms and bounds, the key as a query parameter on the shared Google APIs host, the Google error form and its classes, personal radio data, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-11
---

# Google Maps geolocation

The geolocation tool of the [Google Maps provider](README.md): `google.maps.geolocation.locate`. The call is a
`POST`, but it changes nothing at Google and a repeat is safe, so it is a read; the profile `read` holds it. The
key must belong to a Cloud project with the Geolocation API enabled and a billing account; the connection test of
the provider proves only Places API (New).

## Endpoint and key

The tool calls `POST https://www.googleapis.com/geolocation/v1/geolocate`. The host is shared by other Google
APIs, so only this fixed path is reachable: no argument and no configured `base_url` can change the host or the
path, the body is built by Qatlas from the checked arguments, and a redirect is never followed. Google's
reference documents the key for this API only as the `key` query parameter, so Qatlas sends it that way and not in
a header. Google's billing page also names OAuth tokens in general terms; the tool uses an API key only and does
not claim that a token is accepted. The URL never leaves the request function: no message, log entry, or result
carries it, a transport error is reduced to a class without the error text, and the key is registered with the
redactor. A request is sent once and never repeated.

## Arguments

Every argument is optional. With none, Google locates the connection by its IP address; `consider_ip: false`
forbids that fallback. All are checked before the key is read and without a request.

- `consider_ip`: Google's default is `true`.
- `radio_type`: `gsm` (Google's default), `cdma`, `wcdma`, `lte`, or `nr`. Google says to send it whenever it is
  known, because a wrong default can give invalid or zero results.
- `home_mobile_country_code` (0 to 999, not for `cdma`) and `home_mobile_network_code` (0 to 999; for `cdma` the
  SID, 0 to 32767).
- `carrier`: the carrier name, at most 100 characters without control characters (a Qatlas bound).
- `cell_towers`: 1 to 32 towers (a Qatlas bound; Google names no maximum). Each holds `mobile_network_code`
  (required) and, as Google's table requires for the `radio_type`: `cell_id` (0 to 65535 for `gsm` and `cdma`,
  0 to 268435455 for `wcdma` and `lte`; refused for `nr`), `new_radio_cell_id` (0 to 68719476735; required for
  `nr`, refused otherwise), `location_area_code` (0 to 65535, for `nr` up to 16777215; required for `gsm` and
  `cdma`), `mobile_country_code` (0 to 999; required except for `cdma`, where it is refused), and optionally
  `age` (milliseconds), `signal_strength` (dBm), and `timing_advance`. Qatlas applies these rules with `gsm`
  when `radio_type` is missing, as Google does. The bounds of `age`, `signal_strength`, and `timing_advance` are
  Qatlas bounds.
- `wifi_access_points`: 2 to 100 access points (the minimum is Google's, the maximum a Qatlas bound). Each holds
  `mac_address` (required, six colon-separated hex pairs) and optionally `signal_strength` (dBm, -128 to -10, as
  documented), `age` (milliseconds), `channel`, and `signal_to_noise_ratio` (dB). Qatlas refuses the broadcast
  address and the IANA-reserved range `00:00:5E:00:00:00` to `00:00:5E:FF:FF:FF`, which Google tells callers to
  remove. Google silently drops locally administered addresses; Qatlas does not check for them.

## Results

- `location`: the estimated position `{lat, lng}` in degrees.
- `accuracy`: the radius in meters. Google gives about 20 m with two or more access points, hundreds of meters
  with macro cells, and thousands with an IP address.

Members that Google does not document are dropped.

## Errors

Google answers a failure with an HTTP status and an object `{"error":{"errors":[{"domain","reason","message"}],
"code","message"}}`. The reason decides within a status, and only the documented reasons shape a message:

| Status and reason | Class |
| --- | --- |
| 404 `notFound` (any 404) | `not-found`: Google could not determine a position; this is no error of the key |
| 400 `keyInvalid` | `auth` |
| 400 `parseError`, any other 400 | `invalid-provider-response` (Qatlas built the body from checked arguments) |
| 403 `dailyLimitExceeded`, `userRateLimitExceeded` | `rate-limited` (quota or a request cap of the project) |
| any other 403 | `permission` |
| 429 | `rate-limited` |
| 401 | `auth` |
| other statuses | by HTTP status as in the [provider table](README.md#errors) |

Google's `message` and each entry's `reason` and `message` go along as the untrusted original with the key
masked, never in Qatlas' message. A 404 occurs, for example, when fewer than two usable access points remain and
no other data locates the device.

## Personal data

MAC addresses of access points, cell identities, and the resulting position are personal location data and are
sent to Google, which compares them with its own database. The tool marks its data as
`google-maps-movement-data`, the class of the [route tools](routes.md#movement-data). The invocation log records
metadata such as the operation, the connection, the result code, and the sizes of arguments and result, never
their content. Whether the access points belong to the person asking is the caller's responsibility.

## Cost

Every call is one request and can incur charges above the free usage Google grants. Google's price list shows
the Geolocation SKU in the Essentials group with 10,000 free events per month and billing account; prices and
caps change and Google's price list is authoritative. Qatlas never repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tool
returns; keeping them is the user's task. Qatlas keeps no result cache and does not check the other caching
rules.

## Not verified

- The free usage and the prices, and the caching and storage rules of Google's terms.
- That the Geolocation API accepts the key in a header, or an OAuth token; the tool uses neither.
- Google documents no maximum number of cell towers or access points, no range for `age`, `timing_advance`, or
  `channel`, and no outcome for `consider_ip: false` without usable data; the bounds are Qatlas bounds, and a
  404 is the expected answer in the last case.
- The structure of an error body is taken from Google's error page; a body of another form is classified by
  its HTTP status.

## Left out on purpose

- Any host, path, header, or body as an argument, and the key as an argument.
- Locally administered address detection, which Google handles itself.

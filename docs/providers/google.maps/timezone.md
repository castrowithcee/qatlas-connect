---
description: >
  Describes the Google Maps time zone tool: timezone.get reads the time zone of a coordinate at a timestamp from the Google Time Zone API, with the required timestamp, the language, the result members, the key as a query parameter, the status forms and their classes, location data, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-11
---

# Google Maps time zone

The time zone tool of the [Google Maps provider](README.md): `google.maps.timezone.get`. It reads and changes
nothing, a repeat is safe, and the profile `read` holds it. The key must belong to a Cloud project with the Time
Zone API enabled and a billing account; the connection test of the provider proves only Places API (New).
Configuration, rate spacing, and the boundary are described in the provider page.

## Endpoint and key

The tool calls the legacy web service `GET https://maps.googleapis.com/maps/api/timezone/json`. The host and the
path are fixed; no argument and no configured `base_url` can change them, and a redirect is never followed.
Google's reference shows the key only as the `key` query parameter in its examples, so Qatlas sends it that way,
as it does for [geocoding](geocoding.md#endpoint-and-key), and not in a header. The URL never leaves the request
function: no message, log entry, or result carries it, a transport error is reduced to a class without the error
text, and the key is registered with the redactor. A request is sent once and never repeated. The output format
is always JSON.

## Arguments

- `location` is required: a point `{latitude, longitude}`, latitude from -90 to 90, longitude from -180 to 180.
  Data may be unavailable over water.
- `timestamp` is required: whole seconds since 1970-01-01 UTC. It decides whether daylight saving applies; Google
  ignores historical changes of a zone. A missing timestamp is refused before the key is read. Qatlas accepts the
  years 1 to 9999, a Qatlas bound.
- `language` is a language tag such as `en` or `pt-BR`; it localizes `timeZoneName`. Google documents that it uses
  the `Accept-Language` header of the request when `language` is missing; Qatlas sends no such header, so Google's
  default applies.

## Results

- `status`: `OK`, or `ZERO_RESULTS` when Google has no zone for the position or time; the zone members are then
  missing. That is no error.
- `timeZoneId`: the CLDR identifier, such as `America/Los_Angeles`; the canonical one when a zone has several.
- `timeZoneName`: the long name, such as "Pacific Daylight Time", localized with `language`. Untrusted text.
- `rawOffset`: the offset from UTC in seconds without daylight saving.
- `dstOffset`: the daylight saving offset in seconds at the timestamp, 0 when it is not in effect.

The local time is `timestamp + dstOffset + rawOffset`. Google's `errorMessage` appears only with a status other
than `OK`; it is untrusted provider text and goes only along as the original of an error, never in a result.
Members that Google does not document are dropped.

## Errors

Google reports an error as a `status` in an HTTP 200 answer. The classes are those of the
[legacy form](README.md#errors): `OVER_QUERY_LIMIT` is `rate-limited`, `OVER_DAILY_LIMIT` is `permission`,
`REQUEST_DENIED` is `auth`, `INVALID_REQUEST` and an unknown or missing status are `invalid-provider-response`,
and `UNKNOWN_ERROR` is `provider-error`. An HTTP error status is mapped by the HTTP status with a message that
names the Time Zone API. Provider text goes along as the untrusted original with the key masked.

## Location data

The coordinate sent to Google ties a person or device to a place. The tool marks its data as
`google-maps-movement-data`, the class of the [route tools](routes.md#movement-data). The invocation log
records metadata such as the operation, the connection, the result code, and the sizes of arguments and result,
never their content.

## Cost

Every call is one request and can incur charges above the free usage Google grants. Google's price list shows
the Time Zone SKU in the Essentials group with 10,000 free events per month and billing account; prices and caps
change and Google's price list is authoritative. Qatlas never repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tool
returns; keeping them is the user's task. Qatlas keeps no result cache and does not check the other caching
rules.

## Not verified

- The free usage and the prices, and the caching and storage rules of Google's terms.
- That the Time Zone API accepts the key in a header; the tool does not use one.
- The range of `timestamp` is a Qatlas bound.
- OAuth tokens are not used; a connection holds an API key only.

## Left out on purpose

- The XML output format, whose member names differ.
- Any host, path, header, or body as an argument, and the key as an argument.

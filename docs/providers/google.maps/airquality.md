---
description: >
  Describes the Google Maps air quality tools: airquality.current, airquality.forecast, and airquality.history read the current air quality, the hourly forecast, and the hourly history of Google Air Quality API for a coordinate, with the arguments and bounds, the extra computations, custom local indexes, time selectors, paging through the cursor, the result members, coverage errors, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# Google Maps air quality

The air quality tools of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes,
and the boundary are described there. Air Quality API is a separate Google service on its own origin,
`https://airquality.googleapis.com`, which is fixed like the Places origin: no argument or configuration
changes it. The same API key reaches it when its project has the Air Quality API enabled and a billing
account; the key travels only in the `X-Goog-Api-Key` header.

All three tools are reads: Google takes a `POST` with a JSON body, but nothing changes at Google and a repeat
is safe, though every call can be billed. They belong to the `read` profile. The coordinate of a call is sent
to Google, and the results are untrusted third-party content. The tools mark their data as
`third-party-search-results`: the result is environmental data about a place, and the only personal input is
the coordinate the caller chooses.

| Tool | Endpoint | Returns |
| --- | --- | --- |
| `google.maps.airquality.current` | `POST /v1/currentConditions:lookup` | The current air quality |
| `google.maps.airquality.forecast` | `POST /v1/forecast:lookup` | Hourly forecast records |
| `google.maps.airquality.history` | `POST /v1/history:lookup` | Hourly history records, up to 720 hours back |

## Arguments

- `location` is required and is a coordinate `{latitude, longitude}`: finite numbers, latitude from -90 to 90,
  longitude from -180 to 180. A value outside these ranges is refused before the key is read and without a
  request. The tools do not resolve addresses; use [google.maps.geocoding.geocode](geocoding.md) to turn a
  place name into a coordinate.
- `extra_computations` lists extra data, each value at most once: `LOCAL_AQI` (the national index of the
  country), `HEALTH_RECOMMENDATIONS` (advice for seven population groups), `POLLUTANT_ADDITIONAL_INFO` (sources
  and health effects), `DOMINANT_POLLUTANT_CONCENTRATION` (concentration of the dominant pollutants), and
  `POLLUTANT_CONCENTRATION` (concentration of all pollutants with measurements). Without it, the result has
  only the indexes. `EXTRA_COMPUTATION_UNSPECIFIED`, which Google ignores, is not offered.
- `universal_aqi` is a boolean. Google includes the Universal AQI by default; `false` leaves it out.
- `uaqi_color_palette` is `RED_GREEN`, `INDIGO_PERSIAN_DARK`, or `INDIGO_PERSIAN_LIGHT` and sets the colors of the
  Universal AQI only; local indexes have fixed colors. `COLOR_PALETTE_UNSPECIFIED` is not offered.
- `language_code` is a BCP-47 tag such as `en` or `pt-BR`. Google uses the closest match and defaults to `en`.
- `custom_local_aqis` is a list of at most 20 pairs `{region_code, aqi}`. `region_code` is a two-letter ISO
  3166-1 alpha-2 code, `aqi` is the code of a local index from Google's list, such as `usa_epa`, and the pairs
  choose a non-default local index for a country, for example the US EPA index for Canada. A country may appear
  once. Google documents no bound for the number of pairs; 20 is a Qatlas bound, and the form of `aqi` (lower
  case letters, digits, and `_`, at most 64 characters) is a Qatlas reading of "snake case".
- `date_time` (`forecast`, `history`) is one point in time as an RFC 3339 timestamp in UTC with a trailing `Z`,
  which Google rounds down to the hour and answers with a single hourly record.
- `period` (`forecast`, `history`) is `{start_time, end_time}`, each an RFC 3339 timestamp in UTC with a
  trailing `Z`; `start_time` is inclusive, `end_time` exclusive, at least one is needed, and `start_time` must
  not be after `end_time`.
- `hours` (`history`) is 1 to 720 and reads that many hours back from now.
- `page_size` (`forecast`, `history`) is 1 to 168 records per page. Google's default is 24 for the forecast and
  72 for the history, and documents the maximum of 168 for the history only; for the forecast 168 is a Qatlas
  bound.
- `cursor` (`forecast`, `history`) continues a list; see below.

`date_time`, `period`, and `hours` exclude each other. Google's discovery document describes them as separate
members without saying that they exclude each other, and Qatlas follows the narrower reading and refuses a
combination before the key is read. Without any of them Qatlas sends none and leaves the range to Google. Any
argument outside the list, such as a key, a host, or a page token, is refused.

## Paging

The result of `forecast` and `history` carries `has_more` and, when `has_more` is true, `next_cursor`. Pass
`next_cursor` as `cursor` with the same request to read the following page. The cursor holds Google's page token
and is bound to the tool and to every other argument of the request, `page_size` included, because Google
requires the other parameters to match the call that produced the token. A cursor that was altered, made up, or
issued for another request or tool is refused before the key is read and without a request. Google's token is
never shown and is sent only in the request body as `pageToken`.

## Results

Each result keeps the members that Google documents, each only when Google sends it; members that Google does
not document are dropped. Strings and lists are cut, and a cut text ends with `…`. Health recommendations and
the pollutant texts are cut at 1500 characters; lists are cut at 10 indexes, 20 pollutants, and 168 hourly
records.

- `current`: `dateTime` (rounded down to the hour), `regionCode`, `indexes`, `pollutants`, and
  `healthRecommendations`.
- `forecast`: `regionCode` and `hourlyForecasts`, each with `dateTime`, `indexes`, `pollutants`, and
  `healthRecommendations`.
- `history`: `regionCode` and `hoursInfo`, with the same members per hour.

An index has `code`, `displayName`, `aqi`, `aqiDisplay`, `category`, `dominantPollutant`, and `color`
(`red`, `green`, `blue`, `alpha`, each 0 to 1). Use `aqi` for calculations and `aqiDisplay`, which may carry a
prefix or suffix such as `100+`, for display; non-numeric indexes have no `aqi`. A pollutant has `code`,
`displayName`, `fullName`, `concentration` (`value`, `units` as `PARTS_PER_BILLION` or
`MICROGRAMS_PER_CUBIC_METER`), and `additionalInfo` (`sources`, `effects`). `pollutants` appears only with a
pollutant extra computation, and `healthRecommendations` only with `HEALTH_RECOMMENDATIONS`. Its members are
`generalPopulation`, `elderly`, `lungDiseasePopulation`, `heartDiseasePopulation`, `athletes`, `pregnantWomen`,
and `children`. They are general texts and no medical advice. `regionCode` may be missing in a disputed
territory.

Google's `nextPageToken` becomes `next_cursor` and is never shown as such.

## Coverage and errors

Google's discovery document does not describe the answer for a location outside the Air Quality API's coverage.
Qatlas reports every failure as an error of the usual classes and never as a result: a `404` is `not-found` with a
message that names missing coverage, a `400` is `invalid-provider-response` with a message that names coverage
as a possible cause, because Qatlas checked the arguments before sending and cannot tell a coverage error from a
changed API, and `403` is `permission` with a message that names the Air Quality API. The other statuses follow
the [provider table](README.md#errors). Google's `message` and `details` never reach Qatlas' message; they go
along as the untrusted original. An answer without any documented member is `invalid-provider-response`.

## Cost

Every call is one request and can be billed above the free usage, and each page of a forecast or history is its
own request. Qatlas does not verify the SKU, the free quota, or the price of the Air Quality API; Google's price
list is authoritative. Qatlas never repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tools
return. Keeping them is the user's task; Qatlas keeps no result cache and does not check the caching rules for
air quality data.

## Not verified

- The SKU, free usage, price, and quota of the Air Quality API requests.
- The caching and storage rules of Google's terms for air quality data.
- Which locations the Air Quality API covers, and whether Google answers a location without coverage with `400`
  or `404`; both are handled, as described above.
- That `date_time`, `period`, and `hours` exclude each other, and that the forecast works without any of them.
- The bound of 168 records per page of the forecast, the bound of 20 pairs of `custom_local_aqis`, and the form
  of the index code are Qatlas bounds, not documented ones.
- That the Air Quality API accepts the key in the `X-Goog-Api-Key` header is based on the general behavior of
  Google's JSON APIs; the discovery document of the API lists `key` only as a standard query parameter.

## Left out on purpose

- Heatmap tiles (`mapTypes.heatmapTiles.lookupHeatmapTile`): the endpoint returns image data, which a tool
  result cannot carry. This is a boundary of the tool set, not a security exclusion.
- The unspecified enumeration values, which Google ignores.
- Any host, path, header, or body as an argument, a page token as an argument, and the key as a query
  parameter.

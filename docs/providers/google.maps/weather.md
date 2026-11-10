---
description: >
  Describes the Google Maps weather tools: weather.current, weather.minutes, weather.hours, and weather.days read the current conditions, the minute-level precipitation nowcast, and the hourly and daily forecasts of Google Weather API for a coordinate, with the arguments and bounds, paging through the cursor, the result members, units, languages, coverage errors, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# Google Maps weather

The weather tools of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes, and
the boundary are described there. Weather API is a separate Google service on its own origin,
`https://weather.googleapis.com`, which is fixed like the Places origin: no argument or configuration changes
it. The same API key reaches it when its project has the Weather API enabled and a billing account; the key
travels only in the `X-Goog-Api-Key` header.

All four tools are reads: `GET`, nothing changes at Google, and a repeat is safe, though every call can be
billed. They belong to the `read` profile. The coordinate of a call is sent to Google, and the results are
untrusted third-party content.

| Tool | Endpoint | Returns |
| --- | --- | --- |
| `google.maps.weather.current` | `/v1/currentConditions:lookup` | The current conditions |
| `google.maps.weather.minutes` | `/v1/forecast/minutes:lookup` | The minute-level precipitation nowcast for up to 6 hours |
| `google.maps.weather.hours` | `/v1/forecast/hours:lookup` | Up to 240 hours of hourly forecast, from the current hour |
| `google.maps.weather.days` | `/v1/forecast/days:lookup` | Up to 10 days of daily forecast, from the current day |

## Arguments

- `location` is required and is a coordinate `{latitude, longitude}`: finite numbers, latitude from -90 to 90,
  longitude from -180 to 180. A value outside these ranges is refused before the key is read and without a
  request. The tools do not resolve addresses; use [google.maps.geocoding.geocode](geocoding.md) to turn a
  place name into a coordinate.
- `units_system` is `METRIC` (Google's default), `IMPERIAL`, or `UNITS_SYSTEM_UNSPECIFIED`, which Google treats
  like leaving it out. Every quantity of the result carries its unit, for example `CELSIUS` or `FAHRENHEIT`.
- `language_code` is a BCP-47 tag such as `en` or `pt-BR`. Google uses the closest match and defaults to `en`.
  `current`, `hours`, and `days` take it; `minutes` does not, because Google documents no language option there.
- `hours` (`hours` tool) limits the total to 1 to 240, and `days` (`days` tool) to 1 to 10. Google's default is
  the maximum. For locations in Japan and South Korea, Google offers at most 72 hours.
- `page_size` is the number of records per page: 1 to 24 for `hours` (Google's default is 24), 1 to 10 for
  `days` (Google's default is 5), and 1 to 360 for `minutes`. Google documents no bound for `minutes`; 360 is
  a Qatlas bound. `current` has no paging.
- `cursor` continues a forecast; see below.

Any argument outside these is refused, so there is no key, host, path, or other query parameter to pass.

## Paging

A forecast longer than one page needs the next pages. The result of a paged tool carries `has_more` and, when
`has_more` is true, `next_cursor`. Pass `next_cursor` as `cursor` with the same request to read the following
page. The cursor holds Google's page token and is bound to the tool and to every other argument of the request,
`hours`, `days`, and `page_size` included, because Google requires the other parameters to match the call that
produced the token. A cursor that was altered, made up, or issued for another request is refused before the key
is read and without a request. Google's token is never shown. A `days` request without `page_size` returns
Google's default page of 5 days; set `page_size` or follow the cursor for more.

## Results

Each result keeps the members that Google documents and each only when Google sends it; members that Google
does not document are dropped. Strings and lists are cut, and a cut text ends with `…`.

- `current`: `timeZone` (`id`, `version`), `currentTime`, `isDaytime`, `weatherCondition`, `temperature`,
  `feelsLikeTemperature`, `dewPoint`, `heatIndex`, `windChill`, `relativeHumidity`, `uvIndex`,
  `thunderstormProbability`, `cloudCover`, `precipitation`, `airPressure`, `wind`, `visibility`, and
  `currentConditionsHistory`, the change over the last 24 hours.
- `minutes`: `timeZone`, `overallPredictionTimeframe`, and `segments`, each with `timeFrame`, `probability`,
  `type`, `intensity`, `qpf`, and `snowfallAmount`.
- `hours`: `timeZone` and `forecastHours`, each hour with `interval`, `displayDateTime`, `isDaytime`,
  `weatherCondition`, the temperatures (`temperature`, `feelsLikeTemperature`, `dewPoint`, `heatIndex`,
  `windChill`, `wetBulbTemperature`), `precipitation`, `airPressure`, `wind`, `visibility`, `iceThickness`,
  `cloudCover`, `relativeHumidity`, `uvIndex`, and `thunderstormProbability`.
- `days`: `timeZone` and `forecastDays`, each day with `interval`, `displayDate`, `maxTemperature`,
  `minTemperature`, `feelsLikeMaxTemperature`, `feelsLikeMinTemperature`, `maxHeatIndex`, `sunEvents`,
  `moonEvents` (`moonPhase`, `moonriseTimes`, `moonsetTimes`), and the three parts `daytimeForecast`,
  `nighttimeForecast`, and `fullDayForecast`, each with `interval`, `weatherCondition`, `precipitation`,
  `relativeHumidity`, `uvIndex`, `thunderstormProbability`, `cloudCover`, `wind`, and `iceThickness`.

`weatherCondition` has `type`, `description` (`text`, `languageCode`), and `iconBaseUri`. The icon address is
bounded text that Qatlas never fetches, and one that is not an absolute `http` or `https` address without user
information is dropped. Google may add or change condition codes and icons.

Wind speeds, precipitation amounts, ice thickness, and visibility are `{value or quantity, unit}` pairs in the
units of the chosen system. The air pressure is `meanSeaLevelMillibars`, in millibars.

## Coverage and errors

Google answers for a location that the Weather API does not cover with an error object. Qatlas reports it as an
error of the usual classes and never as a result: `404` is `not-found` with a message that names the missing
coverage, and `400` is `invalid-provider-response` with a message that names the coverage as a possible cause,
because Qatlas checked the arguments before sending and cannot tell a coverage error from a changed API. `403`
is `permission` with a message that names the Weather API, and the other statuses follow the
[provider table](README.md#errors). Google's `message` and `details` never reach Qatlas' message; they go along
as the untrusted original.

## Cost

Every call is one request and can be billed above the free usage. Qatlas does not verify the SKU, the free
quota, or the price of the Weather API; Google's price list is authoritative. Qatlas never repeats a request on
its own, and each page of a forecast is its own request.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tools
return. Keeping them is the user's task; Qatlas keeps no result cache and does not check the caching rules for
weather data.

## Not verified

- The SKU, free usage, and price of the Weather API requests, and any quota of the API.
- The caching and storage rules of Google's terms for weather data.
- Which locations the Weather API covers, and whether Google answers a location without coverage with `400` or
  `404`; both are handled, as described above.
- The bound of 360 records per page of `minutes` is a Qatlas bound, not a documented one. The result lists
  are cut at the documented page maxima (24 hours, 10 days) and at 360 minute segments.
- That the Weather API accepts the key in the `X-Goog-Api-Key` header is based on the general behavior of
  Google's JSON APIs; the discovery document of the API lists `key` only as a standard query parameter.

## Left out on purpose

- Weather history, public weather alerts, and weather map tiles, which the Weather API offers on other endpoints.
- A minute count for `minutes`: the discovery document of the API has no such parameter.
- Any host, path, header, or body as an argument, and the key as a query parameter.

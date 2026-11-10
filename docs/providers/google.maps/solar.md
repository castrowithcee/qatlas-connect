---
description: >
  Describes the Google Maps solar tools: solar.building-insights reads the solar potential of the building closest to a coordinate and solar.data-layers reads the addresses of the solar data layers around a coordinate from Google Solar API, with the arguments and bounds, the radius rules, the result members, how data layer addresses are shown and never fetched, coverage errors, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# Google Maps solar

The solar tools of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes, and
the boundary are described there. Solar API is a separate Google service on its own origin,
`https://solar.googleapis.com`, which is fixed like the Places origin: no argument or configuration changes it.
The same API key reaches it when its project has the Solar API enabled and a billing account; the key travels
only in the `X-Goog-Api-Key` header.

Both tools are reads: nothing changes at Google and a repeat is safe, though every call can be billed. They
belong to the `read` profile. The coordinate of a call is sent to Google, and the results are untrusted
third-party content. The tools mark their data as `third-party-search-results`: the result describes a
building or a region at a coordinate the caller chooses.

| Tool | Endpoint | Returns |
| --- | --- | --- |
| `google.maps.solar.building-insights` | `GET /v1/buildingInsights:findClosest` | The solar potential of the building closest to the coordinate |
| `google.maps.solar.data-layers` | `GET /v1/dataLayers:get` | Imagery dates and quality and the addresses of the GeoTIFF data layers of a region |

## Arguments

Both tools:

- `location` is required and is a coordinate `{latitude, longitude}`: finite numbers, latitude from -90 to 90,
  longitude from -180 to 180. The tools do not resolve addresses; use
  [google.maps.geocoding.geocode](geocoding.md) to turn a place name into a coordinate.
- `required_quality` is the minimum imagery quality: `HIGH`, `MEDIUM`, `LOW`, or `BASE`. Google restricts the
  result to `HIGH` when it is left out.
- `exact_quality_required` `true` makes `required_quality` the exact quality instead of the minimum. Google's
  default is `false`.
- `experiments` enables pre-GA features, each at most once. Google classes such requests as a pre-GA offering
  under the Google Maps Platform Service Specific Terms.

`google.maps.solar.building-insights` also takes:

- `additional_insights`, each at most once: `ROOF_GEOMETRY` adds a polygon to each roof segment and the
  obstacles on the roof, `DETECTED_ARRAYS` adds the solar arrays detected on the building.
- `experiments` takes `EXPANDED_COVERAGE` and `ROOF_GEOMETRY_INSIGHTS`. Google documents that
  `ROOF_GEOMETRY_INSIGHTS` needs `ROOF_GEOMETRY` in `additional_insights`, so Qatlas refuses it without.

`google.maps.solar.data-layers` also takes:

- `radius_meters` is required: the radius of the region around the coordinate, from 0. Google documents three
  rules, which Qatlas checks before sending: any value up to 100 is allowed; above 100 the radius must not
  exceed `pixel_size_meters` × 1000; above 175 the view must not include monthly flux or hourly shade, so it
  must be `DSM_LAYER`, `IMAGERY_LAYERS`, or `IMAGERY_AND_ANNUAL_FLUX_LAYERS`. A left-out view counts as
  `FULL_LAYERS` at Google and is refused above 175.
- `view` chooses the layers: `DSM_LAYER` (surface model only), `IMAGERY_LAYERS` (adds the image and the building
  mask), `IMAGERY_AND_ANNUAL_FLUX_LAYERS` (adds annual flux), `IMAGERY_AND_ALL_FLUX_LAYERS` (adds monthly flux),
  or `FULL_LAYERS` (adds hourly shade).
- `pixel_size_meters` is the minimum scale of the data: `0.1` (Google's default), `0.25`, `0.5`, or `1`.
- `experiments` takes `EXPANDED_COVERAGE` only. Google documents `ROOF_GEOMETRY_INSIGHTS` as an error for this
  method, so it is not offered.

The unspecified values of Google's enumerations are not offered, because Google treats them like a left-out
parameter. Every value outside these ranges, and any argument outside these, is refused before the key is read
and without a request, so there is no key, host, path, or other query parameter to pass.

## Results

The result keeps the members that Google documents, each only when Google sends it; members that Google does
not document are dropped. The discovery document marks none of the members as deprecated, and all of them are
kept. Strings are cut at 200 characters and end with `…`. Money amounts are `{currencyCode, units, nanos}` with
`units` as a string, dates are `{year, month, day}`, and polygons are GeoJSON `{type, coordinates}`.

`google.maps.solar.building-insights` returns `name` (`buildings/{place_id}`), `center`, `boundingBox`,
`imageryDate`, `imageryProcessedDate`, `postalCode`, `administrativeArea`, `statisticalArea`, `regionCode`,
`imageryQuality`, `solarPotential`, and, with `DETECTED_ARRAYS`, `detectedArrays`. `solarPotential` holds the
maximum array, sunshine hours, the carbon offset factor, roof and building statistics with sunshine quantiles,
the roof segments, the panel placements, the panel configurations with their roof segment summaries, the
financial analyses per monthly bill (financial details, lease, cash purchase, financed purchase), the panel
model, and, with `ROOF_GEOMETRY`, the obstacles. The tool descriptor lists the members in full.

A large building can have thousands of panels, and Google documents no bound on any list of this answer, so
Qatlas cuts them: 500 panels, 500 panel configurations, 50 roof segment summaries per configuration, 100 roof
segments, 200 obstacles, 50 financial analyses, 100 detected arrays, 50 sunshine quantiles, and polygons to 10
loops of 200 positions of at most 3 numbers. A cut polygon loop is no longer closed. Counts such as
`panelsCount` and `maxArrayPanelsCount` stay Google's values. `name` is never cut; it is left out when its
place ID is longer than 256 characters.

`google.maps.solar.data-layers` returns `imageryDate`, `imageryProcessedDate`, `imageryQuality`, and the
addresses of the GeoTIFF files the view selects: `dsmUrl`, `rgbUrl`, `maskUrl`, `annualFluxUrl`,
`monthlyFluxUrl`, and `hourlyShadeUrls`, twelve addresses for January to December. Google states that the
addresses are valid for a few hours.

### Data layer addresses

Qatlas never fetches a data layer address and offers no tool that does. It shows an address as text only when
all of these hold, and withholds it otherwise:

- It starts with `https://solar.googleapis.com/`, has no user information, port, whitespace, or backslash, and
  is at most 2048 bytes long. An address of another host is not shown, although Google documents no host for
  these addresses, because a caller who fetches it would be led to a server outside the Solar API.
- Its query does not contain `;`. A server may split a query there, so such a query could hide a key that a
  rewrite at `&` misses.
- Every query parameter name can be decoded.

From a shown address, Qatlas removes the fragment and every query parameter whose name stands for an API key
(`key`, `apiKey`, `api_key`, `api-key`, and the like, in any case, percent-encoded, with `[]`, and as often as
it occurs). In `hourlyShadeUrls` a withheld address stays as an empty string, so the position still names the
month. Google's discovery document does not say whether the addresses carry a key; the removal guards against
it. The key value itself is masked in every output as well.

## Coverage and errors

Google documents `NOT_FOUND` for a location outside the coverage of the data layers and for a coordinate with
no building within about 50 m. Qatlas reports `404` as `not-found` with a message that names the missing
coverage. `400` is `invalid-provider-response` with a message that names the coverage as a possible cause,
because Qatlas checked the arguments before sending and cannot tell a coverage error from a changed API. `403`
is `permission` with a message that names the Solar API, and the other statuses follow the
[provider table](README.md#errors). Google's `message` and `details` never reach Qatlas' message; they go along
as the untrusted original. An answer without any documented member is `invalid-provider-response`.

## Cost

Every call is one request and can be billed above the free usage. Google prices Building Insights by its own
tariff and Data Layers under an Enterprise SKU. Qatlas does not verify the SKUs, the free quota, or the prices
of the Solar API; Google's price list is authoritative. Qatlas never repeats a request on its own.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tools
return, and requests with `experiments` fall under the terms for pre-GA offerings. Keeping them is the user's
task; Qatlas keeps no result cache and does not check the caching rules for solar data.

## Not verified

- The SKUs, free usage, and prices of the Solar API requests, and any quota of the API.
- The caching and storage rules of Google's terms for solar data.
- Which locations the Solar API covers, and whether Google answers a location without coverage with `400` or
  `404`; both are handled, as described above.
- Whether the data layer addresses carry a key and on which host they lie; the discovery document states
  neither, so the rules above are a narrower reading.
- The list bounds of the building insights are Qatlas bounds, not documented ones. Only the twelve hourly shade
  addresses are documented.
- Whether the answer for a very large building stays within the response limit of 2 MiB; a larger answer is
  `invalid-provider-response`, and the request may still be billed.
- That the Solar API accepts the key in the `X-Goog-Api-Key` header is based on the general behavior of
  Google's JSON APIs; the discovery document of the API lists `key` only as a standard query parameter.

## Left out on purpose

- The GeoTIFF download of the Solar API (`geoTiff.get`) and fetching any data layer address: both return image
  data.
- `ROOF_GEOMETRY_INSIGHTS` for the data layers and the unspecified enumeration values, as described above.
- Any host, path, header, or body as an argument, and the key as a query parameter.

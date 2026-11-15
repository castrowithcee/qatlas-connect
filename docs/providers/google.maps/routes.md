---
description: >
  Describes the Google Maps route tools: routes.compute computes a route with legs, steps, polylines, tolls, and transit details, and routes.matrix computes distances and durations for every origin and destination pair with Google Routes API v2, with the waypoint forms, the settings and their documented combinations, the request limits, the mandatory field mask, per-element matrix status, cut texts and whole-or-nothing polylines, errors, the SKUs, attribution and storage rules, movement data, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# Google Maps routes

The route tools of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes, and
the boundary are described there. Routes API is a separate Google service on its own origin,
`https://routes.googleapis.com`, which is fixed like the Places origin: no argument or configuration changes it.
The same API key reaches it when its project has the Routes API enabled and a billing account; the key travels
only in the `X-Goog-Api-Key` header.

Both tools are reads: Google takes a `POST` with a JSON body, but nothing changes at Google and a repeat is safe,
though every call is billed. They belong to the `read` profile.

| Tool | Endpoint | Returns |
| --- | --- | --- |
| `google.maps.routes.compute` | `POST /directions/v2:computeRoutes` | One route, alternatives, or reference routes |
| `google.maps.routes.matrix` | `POST /distanceMatrix/v2:computeRouteMatrix` | One element per origin and destination pair |

## Movement data

Waypoints, departure and arrival times, and vehicle data describe where a person goes and when. They are sent to
Google, and the results repeat them. The tools therefore mark their data as `google-maps-movement-data`, a class
of their own: neither `third-party-search-results` nor the address class of geocoding says that a trip is tied
to a person. The invocation log records metadata such as the operation, the connection, the result code, and the
sizes of arguments and result, never their content.

## Arguments

The argument names are the snake case forms of Google's members; the tool contract lists each with its values.

### Waypoints

A waypoint has exactly one form:

- `address`: text or a plus code, 1 to 1000 characters without control characters.
- `location`: `{latitude, longitude}` with an optional `heading` from 0 to 360, the direction of traffic at the
  point; `heading` only for `DRIVE` and `TWO_WHEELER`.
- `place_id`: a place ID such as one of the Places tools returns.
- `navigation_point_token`: a token of Google's Geocoding destination search, 1 to 1024 visible ASCII
  characters.

It may add `vehicle_stopover` and `side_of_road`, which Google applies to `DRIVE` and `TWO_WHEELER`, and, on an
intermediate of `routes.compute` only, `via`: the route passes through without a stop and gets no leg there.
Google fails a request with `via` on any other waypoint, and Qatlas refuses it before the key is read.

### Settings both tools take

- `travel_mode`: `DRIVE` (Google's default), `BICYCLE`, `WALK`, `TWO_WHEELER`, `TRUCK`, or `TRANSIT`. `TRUCK`
  needs `route_modifiers.vehicle_info` with `total_height_mm`, `total_length_mm`, `total_width_mm`, and
  `total_weight_kg`; on a matrix, each origin needs them.
- `routing_preference`: `TRAFFIC_UNAWARE`, `TRAFFIC_AWARE`, or `TRAFFIC_AWARE_OPTIMAL`, for `DRIVE` and
  `TWO_WHEELER` only.
- `departure_time` or `arrival_time`, never both, as RFC 3339 timestamps with `Z` or an offset. `arrival_time` is
  for `TRANSIT` only. Google allows a past departure only for `TRANSIT` and transit trips from 7 days back to
  100 days ahead; Qatlas leaves these windows to Google, because they depend on Google's clock.
- `language_code`, `region_code` (a two-letter country code top-level domain), and `units` (`METRIC` or
  `IMPERIAL`, for the display texts only).
- `extra_computations`: each value once. A route takes `TOLLS`, `FUEL_CONSUMPTION`, `TRAFFIC_ON_POLYLINE`,
  `HTML_FORMATTED_NAVIGATION_INSTRUCTIONS`, and the polyline details `FLYOVER_INFO_ON_POLYLINE`,
  `NARROW_ROAD_INFO_ON_POLYLINE`, `TUNNEL_INFO_ON_POLYLINE`, `BRIDGE_INFO_ON_POLYLINE`, and
  `SKYWAY_INFO_ON_POLYLINE`; a matrix takes only `TOLLS`. Their fields come back only when the field mask
  names them.
- `traffic_model`: `BEST_GUESS`, `PESSIMISTIC`, or `OPTIMISTIC`, only with `DRIVE` and `TRAFFIC_AWARE_OPTIMAL`.
- `transit_preferences`: `allowed_travel_modes` (`BUS`, `SUBWAY`, `TRAIN`, `LIGHT_RAIL`, `RAIL`) and
  `routing_preference` (`LESS_WALKING`, `FEWER_TRANSFERS`), only with `TRANSIT`.

### Route modifiers

`route_modifiers` holds `avoid_tolls`, `avoid_highways`, `avoid_ferries`, `avoid_indoor`, `toll_passes` (Google's
toll pass names, each once), and `vehicle_info` with `emission_type`, the four totals, `trailer_info` (up to 10
trailers with `height_mm`, `length_mm`, `width_mm`, `weight_kg`), and `hazardous_goods_types`. Sizes and weights
are whole numbers from 1 to 1,000,000,000; Qatlas sends them as the strings Google expects. `trailer_info` needs
`TRAFFIC_AWARE_OPTIMAL`. A route also takes `avoid_tunnels`, which needs `TRAFFIC_AWARE_OPTIMAL`; a matrix does not
take it, and with `DRIVE` it takes neither the vehicle sizes nor trailers.

### routes.compute

- `origin` and `destination` are required; `intermediates` are up to 25 waypoints.
- `polyline_quality` (`HIGH_QUALITY`, `OVERVIEW`) and `polyline_encoding` (`ENCODED_POLYLINE`,
  `GEO_JSON_LINESTRING`).
- `compute_alternative_routes`; Google returns no alternatives for a request with intermediates.
- `optimize_waypoint_order` reorders the intermediates. It needs `routes.optimizedIntermediateWaypointIndex` in
  the field mask and excludes `via`.
- `requested_reference_routes`: `FUEL_EFFICIENT` and `SHORTER_DISTANCE`. Google marks `SHORTER_DISTANCE` as
  experimental; it needs `DRIVE`, `BICYCLE`, or `TWO_WHEELER` and works with neither `via` nor
  `optimize_waypoint_order`.

### routes.matrix

`origins` is a list of `{waypoint, route_modifiers}`, `destinations` a list of `{waypoint}`. Google's limits per
request apply before the key is read: at most 625 elements (origins times destinations), at most 100 with
`TRAFFIC_AWARE_OPTIMAL` or `TRANSIT`, and at most 50 origins and destinations together given by `address` or
`place_id`.

Qatlas refuses, with a message that names the rule, before the key is read and without a request, every
combination the reference describes as failing, as not supported, or as needing a condition, such as those above.
Options the reference only describes as applying to some travel modes, such as `avoid_tolls`, `avoid_indoor`,
`emission_type`, `vehicle_stopover`, and `side_of_road`, are passed on. Any argument outside the contract, such
as a key or a host, is refused.

## Field mask

`field_mask` is required and has no default. It is a list or one comma-separated string of Google's response
field paths in camel case with dots, such as `routes.duration`, `routes.legs.steps.navigationInstruction`, or,
for the matrix, `localizedValues.distance.text`; a path may name a member with all its nested members. Qatlas
checks every path against the documented members of the answer before the key is read and sends the mask in the
`X-Goog-FieldMask` header. It refuses a missing or empty mask, `*` and any path with `*`, an unknown path, a path
into a GeoJSON line or into the `details` of a status, and the bare `routes`, which Google documents as selecting
every field of a route.

The matrix mask always gets `originIndex`, `destinationIndex`, and `status`: without the indexes an element
cannot be placed, and without `status` Google shows every element as successful. Google bills the matrix per
element, not per field.

## Results

The result keeps the members Google documents, each only when Google sends it; others are dropped. Google omits
a member that holds its default value, such as a distance of 0.

- `routes.compute`: `routes`, `fallbackInfo`, and `geocodingResults` (status, types, partial match, and place
  ID of each address waypoint). An answer without routes means Google found none and is a result, not an
  error.
- `routes.matrix`: `elements`, one per pair in Google's order, each with `originIndex` and `destinationIndex`
  (zero-based positions in the request; Qatlas fills in 0 where Google omits it), `status` (`code` and
  `message`, absent when the pair succeeded), `condition`, and the members the mask names. A pair that failed
  is reported in its element, and the call succeeds. Google's REST answer is one JSON array; Qatlas reads it
  whole within the 2 MiB limit. A cut or unfinished array, an element that is not an object, or a number of
  elements other than origins times destinations is `invalid-provider-response`.

Bounds: texts such as navigation instructions, warnings, and status messages are cut at 1500 characters, names
at 300, other strings at 200, and a cut text ends with `…`. Lists are cut at 10 routes, 26 legs, 1000 steps per
leg, 200 multi-modal segments, 2000 speed reading intervals, 1000 entries per polyline detail, and 10 to 25
entries elsewhere. Links of transit lines and agencies are text that is never fetched, and one that is not an absolute
`http` or `https` address is dropped. A polyline is kept whole or left out, because a cut line would draw a wrong
route: an encoded polyline over 256 KiB or with characters outside the polyline alphabet, and a GeoJSON line with
more than 20,000 points or a malformed point, are left out. `routeToken` is kept whole up to 8 KiB, a place ID up
to 256 characters.

## Errors

Every failure of the whole request is an error of the usual classes, see the
[provider table](README.md#errors). A `403` is `permission` with a message that names the Routes API. A `400` is
`invalid-provider-response`, because Qatlas checked the arguments before sending; its message names what Qatlas
cannot check, such as an unknown or unroutable waypoint or an unsupported combination. A `404` is `not-found`. A
`429` is `rate-limited`, and the next request of the key waits as described in
[rate spacing](README.md#rate-spacing). Google's `message` and `details` never reach Qatlas' message; they go
along as the untrusted original.

## Cost

Google bills `routes.compute` per request and `routes.matrix` per element, one SKU each, chosen by the features
the request uses; a request with a feature of a higher SKU is billed entirely at that SKU:

| SKU | Features |
| --- | --- |
| Essentials | Requests without Pro or Enterprise features |
| Pro | `routing_preference` `TRAFFIC_AWARE` or `TRAFFIC_AWARE_OPTIMAL`, `heading`, `side_of_road`, `vehicle_stopover`; for a route also 11 to 25 intermediates and `optimize_waypoint_order` |
| Enterprise | Two-wheeled routing, toll calculation, large vehicle routing; for a route also traffic on polylines |

The field mask decides which fields Google returns; the fields of an extra computation come back only when the
mask names them. Every call can be billed above the free usage, and Qatlas never repeats a request on its own.
Google's price list is authoritative for prices and free usage.

## Attribution and storage

Google Maps attribution and the storage rules of the Google Maps Platform service terms apply to what the tools
return. Google's policy for the Routes API restricts caching and storing route results, allows place IDs to be
stored without limit and coordinates for up to 30 days, and requires route results on a map to be shown on a
Google Map. Google also requires a warning that `WALK`, `BICYCLE`, and `TWO_WHEELER` routes are in beta and may
miss sidewalks or paths. Keeping these rules is the user's task; Qatlas keeps no result cache and does not check
the individual rules.

Results are untrusted third-party content. Navigation instructions, transit names, warnings, and status
messages can contain text that reads like an instruction and must not be followed.

## Not verified

- Which request features Google counts as toll calculation and large vehicle routing (for example `TOLLS`,
  `toll_passes`, `TRUCK`, or the vehicle sizes), the prices, and the free usage of the six SKUs.
- That `TRUCK` needs all four vehicle totals: the reference says that the vehicle attributes must be set and
  that each total must be set for trucking to apply, and Qatlas reads that as all four.
- The reference allows `routing_preference` only for `DRIVE` and `TWO_WHEELER` but requires
  `TRAFFIC_AWARE_OPTIMAL` for `trailer_info`, so `TRUCK` with trailers cannot pass the checks. Qatlas follows
  the reference literally.
- How Google treats `arrival_time` outside `TRANSIT` beyond the reference: it calls the member ignored for a route
  and allowed only with `TRANSIT` for a matrix. Qatlas refuses it outside `TRANSIT` for both.
- The form of a navigation point token, the bound of 1000 characters for an address, 1,000,000,000 for a size or
  weight, 10 trailers, and the list and polyline bounds of the result are Qatlas bounds, not documented ones.
- That the matrix answer always holds exactly one element per pair, and that Google sends it as
  `application/json`.
- The caching and storage rules of Google's terms beyond the policy page of the Routes API.

## Left out on purpose

- The unspecified enumeration values and the deprecated toll pass `US_MI_IQ_PROX_CARD`.
- The `details` of an element status and of a geocoder status: objects of any type that no shape can bound.
- The `$fields` and `fields` query parameters, the key as a query parameter, and any host, path, or header as an
  argument.
- Route Optimization API and the Navigation SDK; a `routeToken` is returned as text and never used.
- Fetching any link of an answer, such as a transit line or icon address.

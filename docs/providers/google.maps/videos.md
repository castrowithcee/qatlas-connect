---
description: >
  Describes the Google Maps video tools of Aerial View API: videos.lookup reads the state, metadata, and signed addresses of a video and needs a tools list entry, videos.metadata reads the metadata alone, and videos.render queues a render and needs confirmation, with the arguments and bounds, the result members, the signed addresses that are only shown as text, errors, cost, attribution and storage rules, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-11
---

# Google Maps videos

The Aerial View tools of the [Google Maps provider](README.md). Configuration, rate spacing, the error classes,
and the boundary are described there. Aerial View API is a separate Google service on its own origin,
`https://aerialview.googleapis.com`, which is fixed like the Places origin: no argument or configuration
changes it. The same API key reaches it when its project has the Aerial View API enabled; the key travels only
in the `X-Goog-Api-Key` header. A passing connection test does not show that: it only reads Places API (New).

The service covers US addresses only. An address is personal data: the tools send it to Google and mark their
data as `google-maps-address-data`.

| Tool | Endpoint | Effect | Returns |
| --- | --- | --- | --- |
| `google.maps.videos.lookup` | `GET /v1/videos:lookupVideo` | read | State, metadata, and signed addresses of a video |
| `google.maps.videos.metadata` | `GET /v1/videos:lookupVideoMetadata` | read | The metadata of a video, without addresses |
| `google.maps.videos.render` | `POST /v1/videos:renderVideo` | create | State and metadata of a video that was queued or already exists |

## Arguments

`lookup` and `metadata` take exactly one of:

- `video_id`, the ID of a video as returned by `render` or in the metadata of a lookup.
- `address`, a US postal address as one line of text.

Both or neither is refused before the key is read and without a request. Each is at most 256 characters
(`video_id`) or 280 characters (`address`), not blank, and without control characters; Google documents no
length, so these are Qatlas bounds.

`render` takes `address`, which is required, with the same rules.

There is no argument for the key, a path, a header, a URL, or a body.

## Results

`lookup` returns `state` and, once the video is `ACTIVE`, `metadata` and `uris`. `metadata` returns `videoId`,
`captureDate` (`{year, month}`, month granularity), `duration` (Google's text, such as `43.5s`), and `state`. `render`
returns `state` and `metadata`, and no addresses.

`state` is `PROCESSING` (still rendering), `ACTIVE` (ready), or `FAILED`. Google may add values; an unknown
value is shown as text and not refused. Members that Google does not document are dropped; none of the
documented members is deprecated. Strings are cut at 200 characters and end with `…`. A `videoId` of more than
1024 bytes is left out.

`uris` maps a media format to `{landscapeUri, portraitUri}`. Google documents the formats `IMAGE`, `MP4_HIGH`,
`MP4_MEDIUM`, `MP4_LOW`, `DASH`, and `HLS`. A format name that is not documented is passed on when it consists
of capital letters, digits, and `_` and is at most 64 characters, in sorted order and up to 12 formats.

### Signed addresses

The addresses are signed and short-lived and grant access to the video without a sign-in. Because of that,
`lookup` is in no profile and a connection offers it only when its `tools` list names it. Qatlas never fetches
an address, never loads or stores a video, and writes no file; the result is text only. It shows an address
when it is an `https` address of at most 4096 bytes without user information, whitespace, control characters,
or backslash, and withholds it otherwise. The host is not restricted, because Google documents none; treat each
address as untrusted third-party content. The query is kept whole, because it holds the signature.

Use `videos.metadata` to follow a render: it returns the state without addresses.

## Profiles and effects

`metadata` is in the recommended `read` profile. `lookup` is a read that no effect-based connection offers; name
it in `tools`. `render` is in no profile: it is not a read, so the effect `create` has to be allowed on the
connection, and every call asks for confirmation.

## Render and the uncertain outcome

`render` adds the address to the render queue, or, when a video exists, returns its metadata. It is one
request and Qatlas never repeats it. If the request times out, is cut off, fails in the connection after it
started, or Google answers with a `5xx`, a redirect, or an answer that cannot be read, Qatlas cannot know
whether Google queued the render. The error then names the outcome as uncertain, and `videos.metadata` for the
same address shows what exists. A refusal that proves nothing was queued (`400`, `401`, `403`, `429`, a
connection that was never made) carries no such note.

## Errors

`404` is `not-found`: Google documents it for a video that does not exist, and Qatlas names that `render` queues
one. `400` is `invalid-provider-response`, because Qatlas checked the arguments before sending; the message
names a non-US or unresolvable address as a possible cause. `403` is `permission` with a message that names the
Aerial View API, and the other statuses follow the [provider table](README.md#errors). Google's `message` and
`details` never reach Qatlas' message; they go along as the untrusted original. An answer without any
documented member is `invalid-provider-response`.

## Cost

Aerial View is one pay-as-you-go SKU, `Aerial View` in the Pro category. Google calls retrieving a video a
billable event, so every `lookup` call that Google answers can be billed, also while the video is `PROCESSING`.
Google documents `metadata` as the free alternative; Qatlas does not verify the SKU's price or free usage, and
Google's price list is authoritative. Qatlas never repeats a request on its own.

## Attribution and storage

Google Maps attribution is required wherever the video is shown: the Google Maps logo, or the text
"Google Maps" where space is short, unchanged and not hidden. Videos must not be stored or redistributed.
Google exempts only the video ID from the caching restrictions. Keeping to these terms is the user's task;
Qatlas keeps no result cache.

## Not verified

- That the service accepts the API key alone. Google's documentation requires an API key or an OAuth token and
  an anonymous request was refused for lack of a credential, which supports only that the key is the expected
  kind; a live test has to confirm it. Qatlas sends only the key.
- Quota errors. The documentation says a project that reaches its quota stops being served, but gives no
  error form, `QuotaFailure` or `Retry-After` behavior; they are handled by HTTP status only.
- The SKU price and free usage, and which calls are billed beyond the documented lookup.
- The host and lifetime of the signed addresses, and the format of a video ID, which Google does not state.
- The length bounds of `address` and `video_id`.
- That Google keeps answering `404` for an unknown video; the page documents no other error code.

## Left out on purpose

- Fetching, playing, or saving any video or address.
- Any host, path, header, or body as an argument, and the key as a query parameter.
- The unspecified values of Google's enumerations (`STATE_UNSPECIFIED`, `MEDIA_FORMAT_UNSPECIFIED`) are not
  offered or special-cased; they would show as text.

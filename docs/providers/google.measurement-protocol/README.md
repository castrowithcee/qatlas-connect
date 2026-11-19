---
description: >
  Describes the Google Measurement Protocol provider: the stream secret and the measurement_id option, the one listed-only tool events.send, the checks before sending, the limits of 25 events and 130 kB, why a result says sent and never accepted, the local-only connection test and what it does not prove, the effect on measurement data, and the start without a profile.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-19
---

# Google Measurement Protocol

Google Measurement Protocol is a provider that sends events to one Google Analytics 4 web data stream. It has
one tool, `google.measurement-protocol.events.send`. Only web data streams with a measurement ID (`G-...`) are
supported; app streams (`firebase_app_id`, `app_instance_id`) are not. The connection takes no target: its
reach is the one data stream of its measurement ID.

[Set up Google Measurement Protocol](setup.md) leads through creating the stream secret.

## Configuration

The service is always the official origin `https://www.google-analytics.com`, which applies when `base_url` is
left out. Qatlas refuses any other URL, and the regional EU host is not offered. The path, the host, and the
parameters of the request are fixed: a caller cannot pass a URL, a path, a header, or a query parameter.

| Setting | Where | Meaning |
| --- | --- | --- |
| `api_secret` | credential, the only secret role | The Measurement Protocol API secret of the data stream |
| `measurement_id` | `options` of the service, not secret | The measurement ID of the web data stream, `G-` followed by upper-case letters and digits |

Qatlas checks the form of `measurement_id` before it reads the secret. Google takes both values as query
parameters. That URL is built inside the provider, is never shown, and never appears in a message, a log, a
result, or an error. The secret is masked in every output.

## Listed-only and start without a profile

`events.send` is offered only by a connection whose `tools` list names it. Without that entry the tool is
invisible and cannot be called, even when the connection permits the `create` effect. Because every tool of this
provider is listed-only, the provider declares no tool profile: a new connection starts without any preselected
tool, and you add `google.measurement-protocol.events.send` to its `tools` list yourself. The tool also needs
confirmation on every call.

## events.send

Arguments:

| Argument | Required | Meaning |
| --- | --- | --- |
| `client_id` | yes | Identifier of the client the events belong to |
| `events` | yes | 1 to 25 events, each `{name, params}` |
| `user_id` | no | Your own identifier of a signed-in user |
| `timestamp_micros` | no | Microseconds since the epoch, only for past events, at most 72 hours back |
| `non_personalized_ads` | no | Deprecated by Google in favor of `consent` |
| `consent` | no | `ad_user_data` and `ad_personalization`, each `GRANTED` or `DENIED` |
| `user_properties` | no | Up to 25 entries of name and scalar value |

Qatlas never sets `consent` itself. Pass it only as the user decided it. `client_id`, `user_id`,
`user_properties`, and event parameters can be personal data; they go to Google, and Qatlas does not log
them. The invoke log records the operation, the connection, and the sizes, never the arguments.

### Checks before sending

Google answers every request with a 2xx status, also for events it drops, so nothing would tell you about a
mistake afterwards. Qatlas therefore checks the rules of the
[Measurement Protocol reference](https://developers.google.com/analytics/devguides/collection/protocol/ga4/reference)
before it sends anything or reads the secret:

- At most 25 events per request, at most 25 parameters per event, at most 25 user properties.
- The JSON body stays under 130 kB. Qatlas reads this as 130000 bytes.
- Event, parameter, and user property names use letters, digits, and underscores and start with a letter.
  Event and parameter names have at most 40 characters, user property names at most 24.
- Google's reserved event names are refused, and so are `screen_view`, `ad_impression`, and `in_app_purchase`,
  which Google allows only for app streams.
- Parameter and user property names must not start with `firebase_`, `ga_`, or `google_`, and the parameter
  name `firebase_conversion` and the reserved user property names are refused.
- Parameter values are strings, numbers, or booleans. Google drops a string above 100 characters on a standard
  property without an error, and allows 500 on an Analytics 360 property. Qatlas refuses above 500 and cannot
  tell which kind of property the stream belongs to, so keep values to 100 characters.
- User property values are at most 36 characters.
- `timestamp_micros` is a time in microseconds within the past 72 hours.
- Nested values and the `items` list of ecommerce events are not supported.

Two bounds are Qatlas's own, because Google names none: `client_id` and `user_id` have at most 256 characters.

### The result says sent, never accepted

The result is `sent: true`, the number of events, and a note. Google does not report whether it accepted,
dropped, or counted the events, so Qatlas never says so. A timeout, a lost connection, or a 5xx answer is an
error, and its message says that the events may still have reached Google. Qatlas sends a request once and
never repeats it, so check the property before you send the same events again.

## Effect on measurement data

Sending creates events in the Google Analytics property. They can distort reports, audiences, and conversions,
and Qatlas cannot take them back. Use a separate test property when you try the tool out.

## Check the connection

The connection test runs only on your machine. It checks the form of `measurement_id` and that the secret is
present, and it sends nothing, because `/mp/collect` always answers with success and would record real data.

**A passing test proves neither the secret nor the data stream.** It cannot tell a wrong secret or a wrong
measurement ID from a right one.

| Result | Meaning |
| --- | --- |
| `ok` | The measurement ID has the right form and a secret is present |
| `provider-error` | The measurement ID is missing or malformed, or the connection has a target or a foreign origin |
| `auth` | The secret has an unusable form |

## Rate spacing

Requests to one data stream are spaced by at least 500 milliseconds. This is a cautious start and not a limit
that Google documents. Google states a ceiling of 100 million non-conversion requests per hour and property.

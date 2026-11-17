---
description: >
  Describes the Google Analytics data stream tools: data-streams.list and data-streams.get read the web, Android, and iOS streams of a property, and data-streams.create and data-streams.update create and change one, with the type data of each stream type, which members can change, how targets restrict each tool and prove the account of a property, the checked values, the result members, the measurement ID rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics data streams

The data stream tools of the [Google Analytics provider](README.md). A data stream is the source of the data of a
property: a website, an Android app, or an iOS app. `data-streams.list` and `data-streams.get` read and change
nothing, a repeat is safe, and the profile `read` holds both. They need the scope `analytics.readonly`. The two
tools under [Changing data streams](#changing-data-streams) need `analytics.edit`; see
[setup](setup.md#rights-and-scopes). Configuration, targets, paging, and the boundary are described in the
provider page. Find property names with [accounts.list-summaries](accounts.md) and stream names with
`data-streams.list`. Deleting a stream and the Measurement Protocol secrets have no tool yet.

## Result members

A stream has `name` (`properties/ID/dataStreams/ID`), `type` (`WEB_DATA_STREAM`, `ANDROID_APP_DATA_STREAM`, or
`IOS_APP_DATA_STREAM`), `displayName`, and `createTime` and `updateTime` exactly as Google sends them. Its type
decides the data it carries:

- `webStreamData`: `measurementId`, `firebaseAppId`, and `defaultUri`.
- `androidAppStreamData`: `packageName` and `firebaseAppId`.
- `iosAppStreamData`: `bundleId` and `firebaseAppId`.

A member Google does not send is left out. The `measurementId` is returned only by the two reading tools; the
results of `data-streams.create` and `data-streams.update` leave it out, and `data-streams.get` returns it. It is
not a secret, but no tool of this provider hands it on as part of anything else.

## google.analytics.data-streams.list

Reads `properties.dataStreams.list`. `property` (`properties/ID`) is required. `page_size` and `cursor` work as in
the provider page; the cursor fits only the same connection and property. A stream in the answer that does not
carry a valid name of the asked property is left out. An empty answer is an empty list.

## google.analytics.data-streams.get

Reads `properties.dataStreams.get`: one stream with all members above. `name` (`properties/ID/dataStreams/ID`) is
required. An answer for another stream than the one asked for is refused. Google answers a stream that is missing
like one the credential cannot see; both are `not-found`.

## Changing data streams

Both tools change Analytics, need the confirmation of a person for each call, and are one request without a
retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the stream is read before it is changed again. A refusal, a `403`, a `404`, and a
`429` mean the change was not made. A `429` holds the next request of the credential for one second, or the
seconds of `Retry-After` up to one minute, and nothing repeats the failed one.

The values are checked before any request or secret access, and a refusal names the argument and the rule, never
the value:

- `display_name` is non-blank text without control characters, at most 255 UTF-16 code units (Google's limit).
- `default_uri` is an `http` or `https` address with a host, without user info, blanks, or control characters, at
  most 2,048 bytes. Qatlas checks its form and never fetches it.
- `package_name` is an Android package name of at least two dotted parts, such as `com.example.app`.
- `bundle_id` is an Apple bundle ID of letters, digits, dots, and hyphens, such as `com.example.app`.

Qatlas checks only the form of these values; Google decides whether it accepts them.

### google.analytics.data-streams.create

Uses `properties.dataStreams.create`. Required: `property`, `type`, `display_name`, and the data of the type:
`default_uri` for a web stream, `package_name` for an Android stream, `bundle_id` for an iOS stream. A call
without the data of its type is refused, and so is data of another type. The type cannot be changed later. The
answer is the new stream without its `measurementId`. The call is not idempotent: a repeat creates a second
stream.

### google.analytics.data-streams.update

Uses `properties.dataStreams.patch`. `name` is required, and at least one of `display_name` and `default_uri`
must be set. Qatlas sends the update mask from exactly the arguments that are set (`display_name` and
`web_stream_data.default_uri`) and no others. `default_uri` applies to a web stream only; Google refuses it on
another type. The type, the package name, and the bundle ID are immutable at Google, so there are no arguments
for them, and a member cannot be cleared this way. The answer is the stream after the change, without its
`measurementId`.

### Targets and proof

The property in the name decides, like the property tools: a bound property is used at once, a property of a bound
account only after one read that shows its account, and any other property is refused without a request or, after
the proof read, without the call. A connection bound to properties only refuses every other property before any
request. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Provider content

Display names, package names, bundle IDs, and addresses are untrusted data. Names are cut at 300 characters and
addresses at 2,048, and a cut text ends with `…`. Qatlas never fetches an address from a result.

## Not verified

- The sets of values Google accepts for addresses, package names, and bundle IDs are Google's; Qatlas checks only
  the forms above.
- The members of the type data were read from the Discovery document `analyticsadmin` v1beta, revision 20261115,
  and Google may add members.

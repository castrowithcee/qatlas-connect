---
description: >
  Describes the Google Route Optimization provider: the token credential and its fixed origin, the project targets and how the name of an operation is bound to them, the profile read, the tool operations.get with its bounded and untrusted result, the Google error form and its classes, rate spacing, the connection test and its limit, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-19
---

# Google Route Optimization

Google Route Optimization is a provider for the Google Route Optimization API with one credential that issues
access tokens. It reads the state of an operation of the API: it sends no optimization and costs nothing.

[Set up Google Route Optimization](setup.md) leads through the credential and the rights it needs. The fields of
a tool are in its descriptor; this page holds what the descriptor does not say.

## Configuration

The service is always the official origin `https://routeoptimization.googleapis.com`, which applies when
`base_url` is left out. Qatlas refuses any other URL before a secret is read. The origin cannot be configured, a
redirect is never followed, and a tool never takes an origin, a path, a header, or a body.

The credential takes the roles of any token credential and picks its procedure from the roles that hold a value:
a person's consent (`client_id`, optionally `client_secret`, and the `refresh_token` that
`qconnect credential consent` stores), a service account (`service_account_key`), or the impersonation of a
service account that the connection names with `service_account` (the consent of a person who may create
tokens for it). The connection holds no scope. Qatlas derives it from the tools it offers.

The only scope the reference of the API lists is `cloud-platform`, also for `operations.get`. Google lists no
narrower or read-only scope. The token therefore reaches more than the methods Qatlas wires, in the same way as
for the profile `read-iam` of Google Cloud: no tool of this provider changes anything, but the token itself
may change what its account may in every Google API. Give the account only the roles it needs, and prefer a
service account of its own over the consent of a person. The access token travels in the `Authorization` header
only, is never an argument or part of a result, and is masked in every output. A 401 renews the token once, and
nothing else repeats a request.

## Targets

A connection may be bound to projects (`projects/PROJECT`, by project ID or project number), several at once.
Targets are optional: without them the connection reaches what its credential reaches, and the credential stays
the upper bound either way. There are no folders, organizations, or billing accounts, because the API needs no
Resource Manager access and proving that a project belongs to a folder would ask for more rights.

A project ID is not resolved to a project number. A name matches a target only when both spell the project the
same way: a target `projects/my-project-123` does not release a name that holds the project number, and the other
way round.

## Profiles

The profile `read` is the recommended and only one. It holds `google.route-optimization.operations.get`, which
asks for `cloud-platform`. A tool that runs an optimization is billed and never belongs to the recommended
profile.

## Tools

### operations.get

`operations.get` reads `GET /v1/{name}` with `name` of the form
`projects/PROJECT/locations/LOCATION/operations/ID`, as an operation returned it. Google offers no cancel,
delete, or list for these operations, and neither does this provider.

- Before any request, and before a secret is read, Qatlas checks the form of the name (project, location, and
  ID, no other characters and no further segments) and, for a connection with targets, that the project of the
  name is one of them. A name outside the targets ends as `invalid-request` without quoting the value. The
  location is checked for its shape only (letters, digits, and `-`), because Google assigns it in the answer of
  an optimization.
- The answer must name the operation that was asked for: the same location and ID, and the same project or the
  same project by the other spelling (ID or number). Anything else is `invalid-provider-response`.
- The result holds `name`, `done`, `metadata`, and, once `done` is true, `response`. `metadata` of an
  optimization operation has no fields beyond its `@type`; `response` of a successful one is an
  `OptimizeToursResponse`. `response` is a bounded open object without a typed mapping.
- A done operation that carries an `error` is a provider error, mapped from the `google.rpc.Code`: 5 `not-found`,
  7 `permission`, 8 `rate-limited`, 16 `auth`, any other `provider-error`. The message of that error and its
  details are Google's text and go along only as the original. An `error` on an operation that is not done is not
  an outcome and is ignored.
- Locations, vehicles, and routes are personal data. The tool carries the data sensitivity
  `location-and-route-data`, and the invoke log holds no arguments.
- `metadata` and `response` are untrusted third-party data and are cut: text at 1024 bytes, objects and lists at
  100 entries, nesting at six levels, and the whole value at 2000 values. A large response is therefore not
  complete. An answer of more than 2 MiB is refused as `invalid-provider-response`.

## Errors

Qatlas' message never carries Google's text, because `message` and `details` may echo request values. Google's
own answer goes along as the original in the `provider` field of the error detail (untrusted, redacted, and
bounded). Google answers with `{"error":{"code","message","status","details":[...]}}`; only values it documents
decide beyond the HTTP status.

| Answer | Class |
| --- | --- |
| 401 with `CREDENTIALS_MISSING`, `ACCESS_TOKEN_EXPIRED`, or status `UNAUTHENTICATED` | `auth` |
| 403 with reason `SERVICE_DISABLED` | `permission`, the message names `routeoptimization.googleapis.com` as the API to enable |
| 403 with reason `ACCESS_TOKEN_SCOPE_INSUFFICIENT` | `scope-missing` |
| 403 with status `PERMISSION_DENIED` | `permission` |
| 404 with status `NOT_FOUND` | `not-found` |
| 400 | `invalid-provider-response` (Qatlas checked the name before sending) |
| 429 | `rate-limited` |
| any other 401, 403, 404, or 4xx | `invalid-provider-response` |
| 5xx | by HTTP status |

A failed token request ends as `auth`, `consent-required`, or `consent-expired`, as the core decides. An
unreadable, oversized, or non-JSON answer is `invalid-provider-response`. The timeout is 30 seconds, below the
60 seconds an invoke may take. A call is at most one request and is never repeated, apart from the single
renewal of the token after a 401.

## Rate spacing

Requests of one credential are spaced by at least 500 ms within a process. This is a cautious start of Qatlas,
not a Google limit. On a 429 the next request of the credential waits at least one second, or the seconds of
`Retry-After` up to one minute; a missing `Retry-After` never leads to a loop, and the failed request is not
repeated.

## Connection test

The connection test (`t` on a connection in `qconnect tui`) obtains an access token and checks that the consent holds the scope. For each
project target it then reads one operation that does not exist,
`projects/PROJECT/locations/global/operations/qatlas-connection-test`. An answer of `404` with status
`NOT_FOUND` passes, because the API is then enabled in the project and the credential could ask. A `403` ends as
`permission`, or as `scope-missing` when Google says the scope is insufficient, and a `401` as `auth`. If the
API is not enabled in the Cloud project of the credential, the test ends as `permission` with a message that
says so. The test never sends an optimization and costs nothing.

A connection without a project target proves only the token and its scope. It does not prove the API or the
right, because an operation name always holds a project and there is nothing to read without one.

## Boundary

Not verified:

- Google does not document whether it checks the right to read an operation before it looks the operation up.
  The test assumes that a `404` is only answered to an account that holds `routeoptimization.operations.get`
  and that otherwise a `403` comes. If Google answers `404` regardless, a passed test does not prove the right,
  only that the API is enabled and reached.
- Whether Google accepts the location `global` of the probe is not verified. Another answer for it would end the
  test as `invalid-provider-response` or `not-found` for a reason that is not a missing operation.
- The exact answer of the API for a disabled service (`SERVICE_DISABLED`) was not seen.
- The project of an answer is compared with the one asked for as described under `operations.get`; a project ID is
  never resolved to its number.

Left out on purpose:

- Cancel, delete, and list of operations, which the API does not offer.
- The Cloud Storage methods (`optimizeToursUri`, `batchOptimizeTours`).
- A free path, a `$fields` parameter, and any base URL other than the official origin.

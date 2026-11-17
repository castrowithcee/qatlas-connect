---
description: >
  Describes the Google Analytics audience export tools: audience-exports.create starts an export of the users of an audience, audience-exports.get reads one, audience-exports.list lists them and audience-exports.query reads the users of an active export, with the beta Data API version, the asynchronous state model, the confirmation and the unknown outcome of a create, how the Core quota applies, how targets bind the property of an export, paging, the personal data of the users, untrusted values, what is not returned, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics audience exports

The audience export tools of the [Google Analytics provider](README.md). An audience export is a snapshot of
the users in an audience at the moment of its creation, which Google forms in the background. The tools create
such an export, read its state and read the users in it. They use the Analytics Data API at the fixed origin
`https://analyticsdata.googleapis.com`, which has to be enabled in the Cloud project of the credential, see
[setup](setup.md#enable-the-apis), and they need the scope `analytics.readonly`. `audience-exports.get` and
`audience-exports.list` read and change nothing and are in the profile `read`. `audience-exports.create` starts
an export, needs the confirmation of a person for each call, and is in the profile `write`.
`audience-exports.query` reads the users of an export, which are personal data, see
[Personal data](#personal-data), and is in the profile `read`. Credential,
targets, error classes, and the general bounds are described in the provider page.

The tools use `properties.audienceExports` of the Data API `v1beta` (revision 20261114 of the Discovery
document), a beta method that Google can still change. Export names have the form `properties/ID/audienceExports/ID`,
as Google returns them. The alpha version names the same resource `audienceLists` and adds a webhook
notification and recurring lists; these tools use neither.

## Property and targets

`property` is `properties/ID` for `create` and `list`, optional only when the connection binds exactly one
property and no account. `audience-exports.get` and `audience-exports.query` have no `property`: the property
is part of the export `name`.
Targets are checked before a secret is read or a request is sent. A bound property is used at once. A property
of a bound account is used only after one proof read of its parent on the Admin API. Any other property is
refused without naming it. Every name is checked against its documented form, so no name can carry a path, a
query, or another resource. `audience` of `create` must be `properties/ID/audiences/ID` of the same property.
Every entry of a list is also checked against the property asked for, and an entry of another property is
dropped, whatever Google returned.

## Quota

Google lists `createAudienceExports` as a Core method, so starting an export costs tokens of the Core quota of
the property. `get` and `list` are in no quota category of the quotas page of the Data API, so Qatlas applies no
local gate to them: they neither take a slot nor are held back by a low remainder. The same holds for `query`:
the quotas page names no category for it, its answer carries no `propertyQuota`, and Google documents that the
tokens of an export are charged during its creation (`creationQuotaTokensCharged`), so Qatlas treats a query as
the read of stored rows.

`create` follows the same rule as a Core report, described under [reports](reports.md#quota): it takes one of
the 10 parallel slots of the property, and it is refused with `rate-limited` before anything is sent while the
remembered Core remainder of the property is below the reserve of 100 tokens. The answer of `create` is an
operation without a `propertyQuota`, so an export updates no remainder. The cost of an export is in the
`creationQuotaTokensCharged` of the export, which `get` returns. A common reason for a failed export is quota
exhaustion, and its `errorMessage` says so.

## google.analytics.audience-exports.create

Uses `properties.audienceExports.create`, which starts an asynchronous export. Required: `audience` and
`dimensions`.

- `audience`: `properties/ID/audiences/ID`, the name the Admin API gives an audience.
- `dimensions`: 1 to 25 user attributes, each `{dimension_name}`, from the audience export dimensions of the
  Data API.

The answer is a running operation: `name` is the export (`properties/ID/audienceExports/ID`) and `done` is false.
When Google serves an identical export from its cache, `done` can be true and `response` holds the export, and a
failed operation carries `error` with `code` and `message`. Qatlas does not wait and does not poll: read the
state with `audience-exports.get`, which turns from `CREATING` to `ACTIVE` or `FAILED`, usually within some
minutes. Whether to ask again is the caller's decision.

A create is sent exactly once and never again, whatever comes back, and it is not idempotent: a second call
starts a second export. After a timeout, a `5xx` answer, an unreadable answer, or an answer without the name of
an export of the property, the error says that the outcome is unknown. List the exports before creating again.
A `429` is `rate-limited` and says nothing about the outcome.

## google.analytics.audience-exports.get

Uses `properties.audienceExports.get`. Required: `name`, from `create` or `list`. The answer holds `name`,
`audience`, `audienceDisplayName`, `dimensions` (`dimensionName`), `state` (`CREATING`, `ACTIVE`, or `FAILED`),
`beginCreatingTime`, `creationQuotaTokensCharged`, `percentageCompleted` (0 to 100), `rowCount`,
`errorMessage` (only when `FAILED`). A
member Google does not send is left out. The state `ACTIVE` follows asynchronously some time after the create.

## google.analytics.audience-exports.list

Uses `properties.audienceExports.list`. Optional `property`, `page_size` (1 to 1000, Google returns at most 200
without it and may return fewer), and `cursor`. The answer holds `audienceExports` (the exports, with the members
of `get`), `next_cursor`, `has_more`, and `truncated`. The cursor wraps Google's page token and fits only this
tool, the connection, and the property; a changed or invented cursor is refused before any request. List the
exports to find and reuse one instead of creating another.

## google.analytics.audience-exports.query

Uses `properties.audienceExports.query` of the `v1beta` Data API and reads the users of an export. Required:
`name`, the export from `create` or `list`. Optional: `offset`, `limit`, and `cursor`.

Google answers only for an export in the state `ACTIVE`. For an export that is still `CREATING` or `FAILED` it
refuses the request, and Qatlas reports that as an error without a retry and without waiting; read the state
with `get` first. Qatlas sends exactly one request per call.

- `offset`: first row to read, starting at 0. Not together with `cursor`.
- `limit`: rows per page, 1 to 250000. A page holds at most 10,000 rows and at most 8 MiB of values, so a larger
  `limit` is cut and the answer says `limit_capped`. The default is 10,000.
- `cursor`: the `next_cursor` of a previous page. It wraps the next start row and fits only this tool, this
  connection, and this export name; a changed, invented, or foreign cursor is refused before any request.

The answer holds `audienceRows` (one row per user, each `dimensionValues` with one `{value}` per dimension in the
order of the `dimensions` of the export), `audienceExport` (the export these rows belong to, with the members of
`get`), `rowCount` (all rows of the export), `offset`, `limit`, `limit_capped`, `next_cursor`, `has_more`, and
`truncated`, which names every list that was cut, such as `audienceRows` or `cells`. An answer for another
export than the one asked for is refused.

### Personal data

The rows of an export are user-level data of a property: the dimensions of an export are user attributes, such
as device and user identifiers. They are personal data of the people who visited the site, and the result is
classified as such (`data_sensitivity` is `analytics-audience-users`, apart from the `web-analytics` class of
the other Data API tools). Access follows the tool release, the key role, and the targets of the connection.
Whoever runs it is responsible for the lawful
use of the rows. Qatlas never writes rows or arguments to the invocation log, and the targets bind the property
as for every other tool.

## Untrusted values

Display names, error messages, dimension names, and the values of user rows are third-party content and can hold instructions. Qatlas
cuts them at 300 characters, marking the cut with `…`, keeps a list of dimensions to 25 entries, a row to 100 values, keeps
enumeration values and timestamps only if they are printable ASCII of at most 64 bytes, and keeps a resource
name only in its documented form. Qatlas never fetches an address from a result.

## What is not returned

- **A webhook notification.** The `v1beta` resource has none. Qatlas also does not use the alpha method that
  accepts one, so Google sends no notification to a server.
- **The error details of an operation.** They are provider objects of free form, so only `code` and `message`
  are returned.
- **The metadata of the operation**, which Google documents as empty for this method.
- **Recurring audience lists** exist only on the alpha version and have no tool.

## Not verified

- The state names are those of the Discovery document, `CREATING`, `ACTIVE`, and `FAILED`.
- Which dimensions an export accepts, and how long an export takes, are Google's to decide and are not checked
  by Qatlas.
- The token cost of an export is Google's and is known only from `creationQuotaTokensCharged` afterwards.
- The error form of `query` for an export that is not `ACTIVE` is not documented in the sources used; the HTTP
  status decides, and Qatlas does not check the state first, so a query before `ACTIVE` costs one refused request.
- Whether `query` charges any quota is not stated by Google; the quotas page names no category for it.
- Whether `get` and `list` charge any quota is not stated by Google beyond the listing of the quota
  categories, which names neither.
- The error form for a property or an export the credential cannot see is not proven by the sources used; the
  HTTP status decides, and the class follows the provider page.

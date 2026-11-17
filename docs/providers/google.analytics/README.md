---
description: >
  Describes the Google Analytics provider: the fixed Admin and Data API origins, the token credential, the optional account and property targets and what each tool does with them, the read and write profiles, the alpha tools rule, paging, bounds on provider content, rate spacing, quotas, the connection test, and the limits of what is verified; the account, property, report, audience export, and report task tools have their own pages.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-17
---

# Google Analytics

Google Analytics is a provider for Google Analytics 4 on the Analytics Admin API and the Analytics Data API. It
lists the accounts and properties a credential can see, with their configuration, reads one account or
property, renames an account, moves an account to the trash, creates, changes, and trashes properties, and
reads, creates, and changes the data streams of a property, and reads and changes their enhanced measurement and
data redaction settings and reads their site tag. It also runs reports, pivot reports, batches of up
to five of them, realtime reports, and an alpha funnel report, reads the metadata of the dimensions and metrics a
property accepts, creates and reads audience exports, and reads the users of an export, which are personal data.
It also starts asynchronous report tasks (alpha) and reads their state and rows. The reading tools change
nothing. The account, property, and data stream tools read no report data and use no reporting quota; the report
tools do, see [reports](reports.md#quota), and so does starting an audience export, see
[audience exports](audience-exports.md#quota), and starting a report task, see
[report tasks](report-tasks.md#quota). Every tool that changes something needs confirmation.

[Set up Google Analytics](setup.md) leads through the credential. The tools are described in
[accounts](accounts.md), [properties](properties.md), [data streams](data-streams.md), [reports](reports.md),
[audience exports](audience-exports.md), and [report tasks](report-tasks.md). The funnel report and the report
tasks use the alpha version of the Data API, which Google can change without notice; their descriptions say so.
The audience exports use the beta version. Tools on the alpha version of the Admin API follow the
[alpha tools](#alpha-tools) rule.

## Configuration

A service is always the official origin `https://analyticsadmin.googleapis.com`, which applies when `base_url`
is left out. The report tools use the second fixed origin `https://analyticsdata.googleapis.com` of the same
service; it has no setting. Qatlas refuses any other `base_url` before a secret is read, because the access
token belongs to these services.

The credential is a token credential: it does not hold a static key but issues short-lived access tokens, from
a person's consent, a service account key, or the impersonation of a service account. Its secret roles, the
consent, the storage of a renewed refresh token, and the rule that no token ever appears in a message, a result,
or the invocation log are those of every token provider; [setup](setup.md#credential) shows the steps for
Google. A credential belongs to one provider, so a Google Analytics credential is not shared with another Google
product. The tools send the access token only in the `Authorization` header.

The provider takes no further configuration and no exclusions.

## Alpha tools

A tool on the alpha version of the Admin API (`v1alpha`) calls a fixed `/v1alpha/` path and ends its description
with an `ALPHA:` note. Google can change or remove such a method without notice, and then the arguments and
the answer of the tool change with it. Every other Admin tool uses `v1beta`. The tool groups that use `v1alpha`
say so on their page; the [data streams](data-streams.md#stream-settings-alpha) settings tools are the first.

## Targets

A connection may be bound to targets of two kinds, and several of them (at most 100):

- `accounts/ID` binds an Analytics account with all its properties.
- `properties/ID` binds one property, also when the credential sees the property without its account.

The ID is the number Google shows, without a leading zero. Without a target the connection reaches what the
credential reaches. Every check below happens before a secret is read or a request is sent, and a refusal does
not name the resource.

| Tool | With targets |
| --- | --- |
| `accounts.list-summaries` | Shows a bound account with all its properties, and any other account only with its bound properties |
| `accounts.list` | Shows the bound accounts only; a connection bound to properties only is refused |
| `accounts.get`, `accounts.get-data-sharing-settings` | Read a bound account; a connection bound to properties only is refused |
| `accounts.update` | Changes a bound account, like `accounts.get` |
| `accounts.delete` | Only on a connection that binds exactly this account and no property; see [accounts](accounts.md#googleanalyticsaccountsdelete) |
| `properties.list` | Lists the properties of a bound account; see [properties](properties.md#googleanalyticspropertieslist) |
| `properties.get` | Reads a bound property, or a property of a bound account after one proof read of its parent |
| `properties.create` | Creates a property only in a bound account; a connection bound to properties only is refused |
| `properties.update`, `properties.delete` | Change or trash a bound property, or a property of a bound account after one proof read of its parent |
| `data-streams.list`, `data-streams.get`, `data-streams.create`, `data-streams.update` | Use the streams of a bound property, or of a property of a bound account after one proof read of its parent; see [data streams](data-streams.md#targets-and-proof) |
| `data-streams.get-enhanced-measurement`, `data-streams.update-enhanced-measurement`, `data-streams.get-data-redaction`, `data-streams.update-data-redaction`, `data-streams.get-site-tag` | The property in the stream name follows the same rule as the stream tools |
| `reports.run`, `reports.run-pivot`, `reports.batch-run`, `reports.batch-run-pivot`, `reports.run-realtime`, `reports.check-compatibility` | Use a bound property, or a property of a bound account after one proof read of its parent |
| `reports.get-metadata` | Like `reports.run`; `properties/0` needs no target, see [reports](reports.md#property-and-targets) |
| `reports.run-funnel` | Like `reports.run` |
| `audience-exports.create`, `audience-exports.list` | Like `reports.run`, on a bound property or a property of a bound account after one proof read |
| `audience-exports.get` | The property in the export name follows the same rule; see [audience exports](audience-exports.md#property-and-targets) |
| `audience-exports.query` | Like `audience-exports.get`: the property in the export name follows the same rule |
| `report-tasks.create`, `report-tasks.list` | Like `reports.run`, on a bound property or a property of a bound account after one proof read |
| `report-tasks.get`, `report-tasks.query` | The property in the task name follows the same rule; see [report tasks](report-tasks.md#property-and-targets) |

A property of another account is refused, also when it is named correctly.

## Profiles

The profile `read` is the recommended one and holds `google.analytics.accounts.list-summaries`,
`google.analytics.accounts.list`, `google.analytics.accounts.get`,
`google.analytics.accounts.get-data-sharing-settings`, `google.analytics.properties.list`, and
`google.analytics.properties.get`, `google.analytics.reports.run`, `google.analytics.reports.get-metadata`,
`google.analytics.reports.check-compatibility`, `google.analytics.data-streams.list`,
`google.analytics.data-streams.get`, `google.analytics.reports.run-funnel`,
`google.analytics.audience-exports.get`, `google.analytics.audience-exports.list`,
`google.analytics.reports.run-pivot`, `google.analytics.reports.batch-run`,
`google.analytics.reports.batch-run-pivot`, `google.analytics.reports.run-realtime`,
`google.analytics.report-tasks.get`, `google.analytics.report-tasks.list`,
`google.analytics.report-tasks.query`, `google.analytics.audience-exports.query`,
`google.analytics.data-streams.get-enhanced-measurement`, `google.analytics.data-streams.get-data-redaction`, and
`google.analytics.data-streams.get-site-tag`. The profile `write` reads
like `read`, changes accounts, properties, data streams, and their settings, and starts audience exports and report tasks, each
change with its own confirmation: it adds `google.analytics.accounts.update`, `google.analytics.properties.create`,
`google.analytics.properties.update`, `google.analytics.data-streams.create`,
`google.analytics.data-streams.update`, `google.analytics.data-streams.update-enhanced-measurement`,
`google.analytics.data-streams.update-data-redaction`, `google.analytics.audience-exports.create`,
and `google.analytics.report-tasks.create`. The users of an export are personal data; see
[audience exports](audience-exports.md#personal-data). Deleting is in no profile: a
connection offers `google.analytics.accounts.delete` and `google.analytics.properties.delete` only when its
`tools` list names them, and each needs the confirmation of a person. `accounts.delete` also runs only on a
connection that binds exactly that account and no property. Which scope each tool needs is listed in
[setup](setup.md#rights-and-scopes).

## Paging

A list returns `next_cursor` and `has_more` when Google reports a further page; reports continue by row offset,
see [reports](reports.md#rows-and-cursor), funnel, pivot, batch, and realtime reports have no cursor,
`audience-exports.list` and `report-tasks.list` page by cursor, see
[audience exports](audience-exports.md#googleanalyticsaudience-exportslist), and the rows of
`audience-exports.query` and `report-tasks.query` continue by row offset like reports. The cursor wraps Google's page
token and fits only the tool, the connection, and the arguments that Google requires to stay the same, such as
the `filter`; a changed or invented cursor is refused before any request. `page_size` may change between
pages. Google may return fewer entries than asked for and a page can be empty while `has_more` is true.

## Provider content

Everything Google returns is untrusted third-party content, including display names. Names are cut at 300
characters and a cut text ends with `…`. Enumeration values and timestamps are kept exactly as Google sends
them, and a value that is not printable ASCII or too long is left out. A resource name that is not
`accounts/ID`, `properties/ID`, `accountSummaries/ID`, or `properties/ID/dataStreams/ID` is left out. Lists are
cut at 200 entries per page and 2,000 properties per account summary. The bounds of report values and lists are
in [reports](reports.md#untrusted-values). Qatlas never fetches an address from a result.

## Rate spacing and quotas

Requests of one credential are spaced by at least 100 ms within a process. This is a cautious start, not a
Google limit. Google's Admin API quotas are set per Cloud project and per user, and the Cloud console shows
them. On a `429` the next request of the credential waits one second, or the seconds of `Retry-After` up to one
minute. The failed request is never repeated, and a missing `Retry-After` causes no retry loop. The account and
property tools use no Data API quota. The report tools spend the token quota of the property and follow a
reserve and a parallel limit of their own, see [reports](reports.md#quota).

## Connection test

`qconnect connection test` obtains an access token and lists one account summary. This reads no report data, so
it uses no reporting quota. Success proves the token, an enabled Analytics Admin API in the Cloud project of
the credential, and at least one visible account. A connection bound to properties is also tested by reading
each of them. No visible account is `not-found`. The test does not prove the Analytics Data API, which the report
tools need and which has its own activation.

## Errors

Qatlas' message never carries Google's text, because `message` and `details` may echo request values. Google's
answer goes along as the untrusted, redacted, and bounded original in the `provider` field of the error detail.
The class follows the HTTP status: 401 is `auth`, 403 `permission`, 404 `not-found`, 429 `rate-limited`, and
other failures `provider-error` or a transport class. A 401 renews the access token once and the request is
sent once more; a second 401 is an error, never a loop. A 403 with the reason `SERVICE_DISABLED` says that the
Admin API, or for a report tool the Data API, is not enabled in the Cloud project. Google answers a property
or account the credential cannot see like one that does not exist, so `not-found` and `permission` can both mean
missing access. Symptoms and their fixes are in [setup](setup.md#fixing-errors).

## Boundary

The provider reaches only the endpoints of the tool pages, each with a fixed path on one of its two origins. It offers no passthrough of
method, path, header, query, or body.

Not verified:

- The quota figures of the Admin and Data APIs are not checked by Qatlas and may change.
- The error form for a property the credential cannot see is not documented in the sources used; the HTTP
  status decides.
- The Analytics Data API is not covered by the connection test; a report tool is the first call that shows a
  missing activation.

Changes are sent once and never repeated; after a timeout, a `5xx` answer, or an unreadable answer the error
says that the outcome is unknown. A change is confirmed only by the answer of Google, and between the proof
read of a property and its change the property may be moved; Qatlas does not close that window.

Left out on purpose: the deprecated members of the Admin API, the creation of an account
(`provisionAccountTicket`), and the methods named in [properties](properties.md#left-out-on-purpose).

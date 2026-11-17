---
description: >
  Describes the Google Analytics report tools: reports.run runs one report, reports.get-metadata lists the dimensions and metrics a property accepts, and reports.check-compatibility tells which fields fit a report, with the Data API quota model (reserve, parallel limit, return_property_quota), sampling and thresholding, rows and the cursor, the filter forms, how targets apply, untrusted report values, the methods left out, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics reports

The report tools of the [Google Analytics provider](README.md). They read and change nothing, a repeat is
safe although Google takes `runReport` and `checkCompatibility` as a POST, the profile `read` holds them, and
they need the scope `analytics.readonly`. They use the Analytics Data API at the fixed origin
`https://analyticsdata.googleapis.com`, which has to be enabled in the Cloud project of the credential, see
[setup](setup.md#enable-the-apis). Credential, targets, error classes, and the general bounds are described in
the provider page. Find property names with [accounts.list-summaries](accounts.md).

## Property and targets

`property` is `properties/ID`, optional only when the connection binds exactly one property and no account.
Targets are checked before a secret is read or a request is sent. A bound property is used at once. A property
of a bound account is used only after one proof read of its parent on the Admin API, which costs no reporting
quota. Any other property is refused without naming it.

`reports.get-metadata` also takes `properties/0`. It names no property, so it needs no target and no proof
read, and returns only the dimensions and metrics common to all properties, without custom ones. `reports.run`
and `reports.check-compatibility` refuse it.

## Quota

Every report costs tokens of the property quota, counted per day, per hour, and per Cloud project and hour.
Google also limits the requests of a property that run at the same time. The quotas page of the Data API lists
the figures, which depend on the service level of the property and may change.

- **`return_property_quota`** is true unless the caller sets it to false. `propertyQuota` then shows the
  remaining and consumed amount of every bucket after the report. Switching it off is possible only
  explicitly; Qatlas then learns no remainder from the answer.
- **Reserve.** Qatlas remembers the remaining tokens of the hourly, daily, and project buckets from each
  `propertyQuota`, per property and credential, for one hour. While a remembered bucket is below the reserve
  of 100 tokens, a Data API request of that property is refused with `rate-limited` before anything is sent,
  and the message names the bucket and its remainder. The reserve is about ten times the usual cost of one
  report. After an hour the memory lapses and the next request goes to Google, which still refuses what is
  really exhausted.
- **Parallel limit.** At most 10 requests of one property run at once in a process, the figure of a standard
  property. A property of Analytics 360 allows more, but the level is not known before the request, so 10
  applies to all. The eleventh request is refused with `rate-limited` and is not queued.
- **`429`** is `rate-limited` like every other provider answer: the next request of the credential waits one
  second, or the seconds of `Retry-After` up to one minute, and nothing repeats the failed one.

Google lists `getMetadata` and `checkCompatibility` as Core methods, which charge the Core quotas, but names
no token cost for them and returns no `propertyQuota` for them. They therefore follow the parallel limit and the
reserve like a report, and they update no remainder.

## google.analytics.reports.run

Uses `properties.runReport`. Required: `metrics`. Required unless `cohort_spec` is set, which excludes it:
`date_ranges`. Qatlas builds the body from structured arguments; a body, path, or header of the caller is never
passed on. The arguments, their forms, and the answer members are listed by `qconnect describe`.

Checks before any request: the schema of every argument, that each filter expression and each filter sets
exactly one alternative, that `date_ranges` and `cohort_spec` are not both set, and that `offset` and `cursor`
are not both set. Filter expressions nest `and_group`, `or_group`, and `not_expression` up to 2 levels.
Whether a field name or a combination is valid is Google's to decide; use `reports.check-compatibility` to
find out first.

### Rows and cursor

`limit` is the number of rows of one page, default 10000. Google allows up to 250000, but Qatlas asks for and
shows at most 10000 rows and 8 MiB of values per page. A larger `limit` is capped and the result says
`limit_capped`. `rowCount` is the number of all rows of the query. While more rows follow, `has_more` is true
and `next_cursor` continues at the first row not shown, also when a page was cut by the size bound; `truncated`
names every list that was cut. The cursor wraps the next `offset` and fits only the tool, the connection, the
property, and the same report arguments. `limit` may change between pages. A changed or invented cursor is
refused before any request. An `offset` can instead start anywhere.

### Sampling, thresholding, and other data limits

`metadata` is returned as Google sends it and decides how far the figures can be trusted:

- `samplingMetadatas` tells that the report is sampled and from how many events, per date range.
- `subjectToThresholding` tells that Google hid data to protect anonymity, so figures can be lower than the
  true ones. Requests that may be thresholded have their own hourly quota in `propertyQuota`.
- `dataLossFromOtherRow` tells that rows were rolled into an `(other)` row.
- `emptyReason`, `schemaRestrictionResponse` (metrics blocked for this property, for example revenue), and
  `dataTruncationReasons` explain empty or incomplete results.

An empty report has no rows and, where Google gives one, an `emptyReason`.

### Untrusted values

Dimension values such as page titles, search terms, and referrers are third-party content and can hold
instructions. Qatlas cuts them at 300 characters, marking the cut with `…`, bounds every list, and never
fetches an address from a result. Metric values and enumerations are kept as Google sends them if they are
printable ASCII of at most 64 bytes.

## google.analytics.reports.get-metadata

Uses `properties.getMetadata`, a `GET`. It lists the dimensions and metrics a property accepts, with API name,
UI name, description, category, `customDefinition`, and `deprecatedApiNames`, and for metrics also `type`,
`expression`, and `blockedReasons`, and the saved comparisons. The list of a property includes its custom
dimensions and metrics. Use the `apiName` values in `reports.run`. Lists are cut at 1000 entries (500
comparisons), descriptions at 1000 characters, and `truncated` says so.

## google.analytics.reports.check-compatibility

Uses `properties.checkCompatibility`. It takes the `dimensions`, `metrics`, `dimension_filter`, and
`metric_filter` of a planned report in the form of `reports.run`, and an optional `compatibility_filter` of
`COMPATIBLE` or `INCOMPATIBLE`. It returns every dimension and metric of the property with its metadata and
whether it can be added to the report. It runs no report.

## Left out on purpose

`OrderBy.pivot` and `cohortReportSettings.accumulate` are not arguments: Google documents them for pivot
reports or as not supported in `runReport`. Batch, pivot, realtime, funnel, and audience export methods have no
tool in this group.

## Not verified

- The error form for a property without access and for an exhausted token quota (`RESOURCE_EXHAUSTED`) is not
  proven by the sources used; the HTTP status decides, and the class follows the provider page.
- The quota figures and the token cost per method are Google's and may change; the reserve and the parallel
  limit are fixed values of Qatlas.
- The memory of the remainder lives in the process: another process, or another user of the same project, can
  use the quota unseen.
- The field lists were read from the Discovery document `analyticsdata` v1beta, revision 20261114.

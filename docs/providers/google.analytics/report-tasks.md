---
description: >
  Describes the Google Analytics report task tools: report-tasks.create starts an asynchronous report, report-tasks.get reads one, report-tasks.list lists them and report-tasks.query reads the rows of an active task, with the alpha Data API version, the asynchronous state model, the report definition, the confirmation and the unknown outcome of a create, how the Core quota applies, how targets bind the property of a task, paging, untrusted values, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics report tasks

The report task tools of the [Google Analytics provider](README.md). A report task is a report that Google forms
in the background and stores, for reports too large for [`reports.run`](reports.md#googleanalyticsreportsrun).
The tools start a task, read its state, list the tasks of a property, and read the rows of a finished task. They
use the Analytics Data API at the fixed origin `https://analyticsdata.googleapis.com`, which has to be enabled
in the Cloud project of the credential, see [setup](setup.md#enable-the-apis), and they need the scope
`analytics.readonly`. `report-tasks.get`, `report-tasks.list`, and `report-tasks.query` read and change nothing
and are in the profile `read`. `report-tasks.create` starts a task, needs the confirmation of a person for each
call, and is in the profile `write`. Credential, targets, error classes, and the general bounds are described in
the provider page.

**Alpha.** The tools use `properties.reportTasks` of the Data API `v1alpha` (revision 20261114 of the Discovery
document). Google can change or remove these methods without notice, and so can the arguments and answers of
these tools. The descriptions of the tools say so. Task names have the form `properties/ID/reportTasks/ID`, as
Google returns them.

## Property and targets

`property` is `properties/ID` for `create` and `list`, optional only when the connection binds exactly one
property and no account. `get` and `query` have no `property`: the property is part of the task `name`. Targets
are checked before a secret is read or a request is sent. A bound property is used at once. A property of a
bound account is used only after one proof read of its parent on the Admin API. Any other property is refused
without naming it. Every name is checked against its documented form, so no name can carry a path, a query, or
another resource. Every entry of a list is also checked against the property asked for, and an entry of another
property is dropped, whatever Google returned.

## Quota

Starting a task charges tokens of the property quota at creation, and the task reports them as
`creationQuotaTokensCharged`. The quotas page of the Data API names no category for the report task methods.
Qatlas counts `create` with the Core category, like `audience-exports.create` and a Core report, because the
task runs a report on the same buckets as a Core report. It follows the rule described under
[reports](reports.md#quota): it takes one of the 10 parallel slots of the property, and it is refused with
`rate-limited` before anything is sent while the remembered Core remainder of the property is below the reserve
of 100 tokens. The answer of `create` is an operation without a `propertyQuota`, so a task updates no remainder.

`get`, `list`, and `query` read stored state or rows. The quotas page names no category for them, their answers
carry no quota state, and Google documents that the tokens of a task are charged during its creation, so Qatlas
applies no local gate to them. Qatlas never waits for a task: a task in the state `CREATING` is reported as it
is, and whether to ask again is the caller's decision.

## google.analytics.report-tasks.create

Uses `properties.reportTasks.create`, which starts an asynchronous report. Required: `report_definition`.
Optional: `property`.

`report_definition` is the report, built from the same parts as `reports.run`:

- `dimensions` (up to 9), `metrics` (1 to 10), `dimension_filter`, `metric_filter`, `order_bys`,
  `metric_aggregations`, `currency_code`, and `keep_empty_rows`, in the forms described under
  [reports](reports.md#googleanalyticsreportsrun).
- `date_ranges` (1 to 4) or `cohort_spec`: exactly one of the two is required.
- `offset` and `limit` (1 to 250000): the rows of Analytics storage that the task holds, for example the first
  100,000. Another task with a later `offset` holds the following rows. The `totalRowCount` of a finished task
  says how many rows storage holds.
- `sampling_level`: `LOW`, `MEDIUM`, or `UNSAMPLED`. The last two are for Analytics 360 properties.

`comparisons` are not part of a task definition. The checks of `reports.run` apply before any request: the schema
of every argument, that each filter sets exactly one alternative, and that `date_ranges` and `cohort_spec` are
not both set. Whether a field name or a combination is valid is Google's to decide.

The answer is a running operation: `name` is the task (`properties/ID/reportTasks/ID`) and `done` is false. When
the operation already holds an outcome, `response` holds the task, and a failed operation carries `error` with
`code` and `message`. Qatlas does not wait and does not poll: read the state with `report-tasks.get`, which turns
from `CREATING` to `ACTIVE` or `FAILED`, then read the rows with `report-tasks.query`.

A create is sent exactly once and never again, whatever comes back, and it is not idempotent: a second call
starts a second task. After a timeout, a `5xx` answer, an unreadable answer, or an answer without the name of a
task of the property, the error says that the outcome is unknown. List the tasks before creating again. A
`429` is `rate-limited` and says nothing about the outcome.

## google.analytics.report-tasks.get

Uses `properties.reportTasks.get`. Required: `name`, from `create` or `list`. The answer holds `name`,
`reportDefinition`, and `reportMetadata` with `state` (`CREATING`, `ACTIVE`, or `FAILED`), `beginCreatingTime`,
`creationQuotaTokensCharged`, `taskRowCount` (the rows of this report, which bound `query`), `totalRowCount`, and
`errorMessage` (only when `FAILED`). A member Google does not send is left out. `reportDefinition` has the
members and names of the Google resource, such as `dateRanges`, `dimensionFilter`, and `samplingLevel`, and a
64-bit integer such as `limit` is a string. An answer for another task than the one asked for is refused.

## google.analytics.report-tasks.list

Uses `properties.reportTasks.list`. Optional `property`, `page_size` (1 to 1000), and `cursor`. The answer holds
`reportTasks` (with the members of `get`), `next_cursor`, `has_more`, and `truncated`. The cursor wraps Google's
page token and fits only this tool, the connection, and the property; a changed or invented cursor is refused
before any request. List the tasks to find and reuse one instead of creating another.

## google.analytics.report-tasks.query

Uses `properties.reportTasks.query`. Required: `name`. Optional: `offset`, `limit`, and `cursor`, which work as
for [`reports.run`](reports.md#rows-and-cursor): a page holds at most 10,000 rows and 8 MiB of values, a larger
`limit` is cut and the answer says `limit_capped`, and the cursor wraps the next start row and fits only this
tool, this connection, and this task name. A task holds at most the rows of its own `limit`, so a query past
them returns none.

Google answers only for a task in the state `ACTIVE`. For another state it refuses the request, and Qatlas
reports that as an error without a retry; read the state with `get` first.

The answer holds `dimensionHeaders`, `metricHeaders`, `rows`, the requested `totals`, `maximums`, and `minimums`,
`rowCount`, `metadata` (sampling, thresholding, empty reason, time zone, currency, schema restriction, the data
lost into the `(other)` row, truncation reasons, and `section`), `offset`, `limit`, `limit_capped`, `next_cursor`,
`has_more`, and `truncated`. Report values can be personal or sensitive web analytics data of the property; the
result is classified like the other report tools (`web-analytics`).

## Untrusted values

Task names, error messages, report values, and every string of a report definition are third-party content and
can hold instructions: a definition may have been created by anyone who can use the property. Qatlas cuts
strings at 300 characters, marking the cut with `…`, keeps a list of a definition to 200 entries and its nesting
to 16 levels, leaves out a member whose name is not made of letters, digits, and underscores, keeps enumeration
values and timestamps only if they are printable ASCII of at most 64 bytes, and keeps a resource name only in
its documented form. `truncated` names every list that was cut, such as `reportDefinition`. Qatlas never
fetches an address from a result.

## What is not returned

- **The error details of an operation.** They are provider objects of free form, so only `code` and `message`
  are returned.
- **The metadata of the operation**, which Google documents as empty for this method.
- **Quota state.** The task methods return no `propertyQuota`, and Qatlas offers no switch for it.

## Not verified

- The state names are those of the Discovery document, `CREATING`, `ACTIVE`, and `FAILED`. The alpha version
  can change them.
- The quota category of the task methods is not named by Google. The Core category for `create` is Qatlas'
  assumption; whether `get`, `list`, or `query` charge any quota is not stated.
- How long a task takes, and which combinations of fields a task accepts, are Google's to decide and are not
  checked by Qatlas.
- The error form for a task that is not `ACTIVE`, or one the credential cannot see, is not proven by the
  sources used; the HTTP status decides, and the class follows the provider page.

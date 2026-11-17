---
description: >
  Describes the Google Analytics access report tools: accounts.run-access-report and properties.run-access-report run a Data Access Report that records who read the reporting data of an account or property and when, with the Admin API v1beta methods, the personal data of the records, the report arguments, rows and cursor, the quota that is only returned, how targets bind the entity, untrusted values, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics access reports

The access report tools of the [Google Analytics provider](README.md). A Data Access Report lists each time a
user read Analytics reporting data: in the Analytics interface, in explorations, through the Data API, and
through linked products such as Firebase. The tools use `accounts.runAccessReport` and
`properties.runAccessReport` of the Analytics Admin API `v1beta` (revision 20261115 of the Discovery document) at
the fixed origin `https://analyticsadmin.googleapis.com`. Both methods are `POST` requests that only read. They
accept the scope `analytics.readonly` (and `analytics.edit`, which Qatlas does not ask for), so both tools are in
the profile `read`. Google allows the report only to administrators and keeps the records for up to 2 years.

## Personal data

The records name the email addresses of users and when they read which data. They are personal data and
describe the access patterns of people. The result is classified as such (`data_sensitivity` is
`analytics-access-records`, apart from `analytics-configuration` of the account tools and `web-analytics` of the
reports), and the description of both tools carries a warning. Read only what the task needs, filter to the
smallest range of dates and users, and do not pass the rows on. Whoever runs the tools is responsible for the
lawful use of the rows. Qatlas never writes arguments or rows to the invocation log. The profile `read` is the
recommended profile and includes these tools, so a connection with it can read these records; leave the tools out
with a `tools` list when that is not wanted.

## Entity and targets

`entity` is required. It is `accounts/ID` for `accounts.run-access-report` and `properties/ID` for
`properties.run-access-report`. Targets are checked before a secret is read or a request is sent.

- An account is used on a connection without targets or one that binds the account itself, like the other
  account tools. A connection bound to properties only is refused.
- A property is used when it is bound, or after one proof read of its parent on the Admin API when its account
  is bound. Any other property is refused without naming it.

Every name is checked against its documented form, so no name can carry a path or a query. For a report on an
account, Google includes the access to all properties of the account.

## Arguments

Every argument is a structured member. Qatlas renames them to the documented ones and sends nothing else.

- `dimensions`: up to 9 `{name}`, and `metrics`: up to 10 `{name}`, from the Data Access Schema of Google, such
  as `userEmail` and `accessCount`. Google allows dimensions that are not related to quota only on Analytics 360
  properties.
- `date_ranges`: up to 2 `{start_date, end_date}` as `YYYY-MM-DD`, `today`, `yesterday`, or `NdaysAgo`. With two
  ranges each row carries the zero-based index of its range.
- `dimension_filter` and `metric_filter`: a filter expression, nested up to 2 levels. An expression sets exactly
  one of `filter`, `and_group {expressions}`, `or_group {expressions}`, and `not_expression`. A `filter` is
  `{field_name}` with exactly one of `string_filter {value, match_type, case_sensitive}`,
  `in_list_filter {values, case_sensitive}`, `numeric_filter {operation, value}`, and
  `between_filter {from_value, to_value}`; a numeric value is `{int64_value}` or `{double_value}`. Unlike the
  Data API, an access filter has no empty filter. Qatlas sends a `filter` as `accessFilter`, the name Google
  uses here. A dimension filter holds only dimensions and a metric filter only metrics; Google checks that.
- `order_bys`: up to 20 `{desc, metric {metric_name}}` or `{desc, dimension {dimension_name, order_type}}`.
- `time_zone`: an IANA name such as `America/New_York` that reads the dates; the property time zone otherwise.
- `include_all_users`: also returns users who never made an API call. `expand_groups` returns the users within
  user groups and works only with `include_all_users` true; Google decides what it does otherwise.
- `return_entity_quota`: see [Quota](#quota).
- `offset`, `limit`, `cursor`: see [Rows and cursor](#rows-and-cursor).

## Rows and cursor

`limit` is 1 to 100000, and a page holds at most 10,000 rows and 8 MiB of values, so a larger `limit` is cut and
the answer says `limit_capped`. The default is 10,000. `offset` is the first row, starting at 0. A page that
leaves rows behind returns `next_cursor` and `has_more` true. The cursor wraps the next start row and fits only
the tool, the connection, the entity, and the report arguments except `limit`; a changed, invented, or foreign
cursor is refused before any request, as is `offset` together with `cursor`. `truncated` names every list that
was cut, such as `rows`, `cells`, or the headers.

## Result

`dimensionHeaders` (`dimensionName`), `metricHeaders` (`metricName`), `rows` (each `dimensionValues` and
`metricValues` with one `{value}` per header), `rowCount` (all rows of the query, whatever `offset` and `limit`
are), `quota`, `offset`, `limit`, `limit_capped`, `next_cursor`, `has_more`, and `truncated`. Every documented
member of the response is returned.

## Quota

The access report has a quota of its own: `tokensPerDay`, `tokensPerHour`, `tokensPerProjectPerHour`,
`concurrentRequests`, and `serverErrorsPerProjectPerHour`, each with `consumed` and `remaining`. It is not the
property quota of the Data API, so the reports of the [report tools](reports.md#quota) and these tools do not
share a remainder, and Qatlas applies no local gate to the access report: it takes no slot and never refuses on
a remembered remainder. Google decides, and its `429` is `rate-limited`.

- `properties.run-access-report` asks Google for the state and returns it in `quota`. `return_entity_quota` is
  true unless you set it to false.
- `accounts.run-access-report` always sends `returnEntityQuota` false, because the Discovery document says it
  must be false for an account-level request and that `quota` does not work there. The argument is accepted for
  symmetry, and true is refused before any request.

## Untrusted values

Email addresses, dimension values, and metric values are third-party content and can hold instructions. Qatlas
cuts values at 300 characters, marking the cut with `…`, keeps at most 100 headers and 100 values per row,
keeps a metric value only if it is printable ASCII of at most 64 bytes, and returns no member of the answer that
the type does not list. Qatlas never fetches an address from a result. A request is sent exactly once and never
again, whatever comes back.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.
- Which dimensions and metrics exist, which need an Analytics 360 property, and what the report returns for a
  user without administrator rights are Google's to decide. The error form for a refused entity is not proven;
  the HTTP status decides, and the class follows the [provider page](README.md).
- The scope `analytics.readonly` is the one the Discovery document lists first among two accepted scopes. Whether
  Google accepts it for every use of the report is not proven by a live call.
- Whether an account-level report also holds the access to properties the credential cannot see is not stated by
  Google.

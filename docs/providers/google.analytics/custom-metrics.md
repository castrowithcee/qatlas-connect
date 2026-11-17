---
description: >
  Describes the Google Analytics custom metric tools: custom-metrics.list and custom-metrics.get read the event-scoped custom metrics of a property, and custom-metrics.create, custom-metrics.update, and custom-metrics.archive create, change, and archive one, with the measurement units and restricted metric types, which members can change, how targets restrict each tool and prove the account of a property, the checked values, the result members, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics custom metrics

The custom metric tools of the [Google Analytics provider](README.md). A custom metric is a number a property
collects from an event parameter, such as a load time or a margin. `custom-metrics.list` and
`custom-metrics.get` read and change nothing, a repeat is safe, and the profile `read` holds both. They need the
scope `analytics.readonly`. The three tools under [Changing custom metrics](#changing-custom-metrics) need
`analytics.edit` and are in the profile `write`; see [setup](setup.md#rights-and-scopes). Configuration,
targets, paging, and the boundary are described in the provider page. Find property names with
[accounts.list-summaries](accounts.md) and metric names with `custom-metrics.list`.

## Result members

A metric has `name` (`properties/ID/customMetrics/ID`), `parameterName` (the event parameter it reads),
`displayName`, `description`, `measurementUnit`, `scope`, and `restrictedMetricType`. A member Google does not
send is left out.

- `measurementUnit` is one of `STANDARD`, `CURRENCY`, `FEET`, `METERS`, `KILOMETERS`, `MILES`, `MILLISECONDS`,
  `SECONDS`, `MINUTES`, or `HOURS`.
- `scope` is `EVENT`, the only scope Google documents.
- `restrictedMetricType` lists `COST_DATA` and `REVENUE_DATA`. Google requires it for `CURRENCY` metrics and
  allows it for no other unit.

## google.analytics.custom-metrics.list

Reads `properties.customMetrics.list`. `property` (`properties/ID`) is required. `page_size` (1 to 200) and
`cursor` work as in the provider page; the cursor fits only the same connection and property. A metric in the
answer that does not carry a valid name of the asked property is left out. An empty answer is an empty list.

## google.analytics.custom-metrics.get

Reads `properties.customMetrics.get`: one metric with all members above. `name`
(`properties/ID/customMetrics/ID`) is required. An answer for another metric than the one asked for is refused.
Google answers a metric that is missing like one the credential cannot see; both are `not-found`.

## Changing custom metrics

All three tools change Analytics, need the confirmation of a person for each call, and are one request without a
retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the metric is read before it is changed again. A refusal, a `403`, a `404`, and a
`429` mean the change was not made. A `429` holds the next request of the credential for one second, or the
seconds of `Retry-After` up to one minute, and nothing repeats the failed one.

The values are checked before any request or secret access, and a refusal names the argument and the rule, never
the value:

- `parameter_name` is letters, digits, and underscores, starts with a letter, and has at most 40 characters.
- `display_name` is letters, digits, blanks, and underscores, starts with a letter, and has at most 82
  characters.
- `description` has at most 150 characters and no control characters.
- `measurement_unit` and `scope` are the values above; the `UNSPECIFIED` values are not offered.
- `restricted_metric_type` holds `COST_DATA` and `REVENUE_DATA` at most once each.

Google decides everything beyond these forms, including the limit of custom metrics per property and the error
it answers when the limit is exceeded; that error is not verified live.

### google.analytics.custom-metrics.create

Uses `properties.customMetrics.create`. Required: `property`, `parameter_name`, `display_name`,
`measurement_unit`, and `scope`. `description` is optional. A `CURRENCY` metric needs `restricted_metric_type`
with at least one entry, and any other unit with entries is refused before a request. The parameter name and the
scope cannot be changed later. The call is not idempotent. The answer is the new metric.

### google.analytics.custom-metrics.update

Uses `properties.customMetrics.patch`. `name` is required, and at least one of `display_name`, `description`,
`measurement_unit`, and `restricted_metric_type` must be set. Qatlas sends the update mask from exactly the
arguments that are set and no others. An empty `description` or an empty `restricted_metric_type` list clears
that member. Qatlas does not check the unit against the types of the metric; Google refuses a combination that
breaks the `CURRENCY` rule. The parameter name and the scope are immutable at Google, so there are no arguments
for them. The answer is the metric after the change.

### google.analytics.custom-metrics.archive

Uses `properties.customMetrics.archive`. `name` is required. The call archives the metric: it no longer collects
data and no longer appears in the list, and no tool of this provider restores it. The answer is `name` and
`archived: true`, which only says that Google accepted the call. The tool changes one metric and is in the profile
`write`, unlike the tools that delete accounts and properties.

### Targets and proof

The property in the name decides, like the property tools: a bound property is used at once, a property of a bound
account only after one read that shows its account, and any other property is refused without a request or, after
the proof read, without the call. A connection bound to properties only refuses every other property before any
request. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Provider content

Display names, descriptions, and parameter names are untrusted data. They are cut at 300 characters, and a cut
text ends with `…`.

## Not verified

- The members, units, and limits were read from the Discovery document `analyticsadmin` v1beta, revision
  20261115, and Google may change them.
- The limit of custom metrics per property, the error at the limit, and whether archiving an archived metric
  succeeds are not verified live.

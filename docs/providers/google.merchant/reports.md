---
description: >
  Describes the Google Merchant tools for reports, quotas, and limits: the structured report query and its fixed field catalog of ten tables, the rules each table sets, result bounds and paging, the quota and limit tools, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant reports, quotas, and limits

Four read tools of [Google Merchant](README.md) read what Merchant Center reports and how much of the API an
account may still use. All of them are reads without confirmation, belong to the profile `read`, take the
`account` argument that the [targets](README.md#targets) check before any secret is read, and show every text
of Google as untrusted content.

| Tool | What it reads |
| --- | --- |
| `google.merchant.reports.search` | one report of an account, from a structured query |
| `google.merchant.quotas.list` | the method groups of the API with daily limit, usage, and per-minute limit |
| `google.merchant.limits.list` | the limits of one type that apply to an account |
| `google.merchant.limits.get` | one limit |

## Reports

`reports.search` sends one `POST` to `reports/v1/accounts/ACCOUNT/reports:search` with the query, the page size,
and the page token. The call reads and changes nothing, so it is sent once more after a `401` renewed the token,
like any read. Qatlas builds the query of the Merchant Center Query Language (MCQL) itself. The agent passes
no query text: it names a table, fields, conditions, a date range, and an order, and every name and operator is
checked against a fixed field catalog. A name that is not in the catalog is refused before a secret is read.

| Argument | Meaning |
| --- | --- |
| `table` | one of the ten tables below |
| `fields` | 1 to 40 fields of the table to select |
| `conditions` | up to 10 conditions joined by `AND`: `field`, `operator`, and `values` |
| `date_range` | `window` (a documented date function) or `start` and `end` as `YYYY-MM-DD`, applied to `date` |
| `order_by` | up to 5 selected fields, `ASC` (default) or `DESC` |
| `limit` | rows per call, 1 to 1000, default 100 |
| `cursor` | `next_cursor` of the previous call with the same account, table, and query |

The tables are `product_performance_view`, `non_product_performance_view`, `product_view`,
`price_competitiveness_product_view`, `price_insights_product_view`, `best_sellers_product_cluster_view`,
`best_sellers_brand_view`, `competitive_visibility_competitor_view`, `competitive_visibility_top_merchant_view`,
and `competitive_visibility_benchmark_view`. The MCQL names of fields are the snake case of the members of the
Merchant API answer (`clickThroughRate` is `click_through_rate`); the rows keep the member names of the answer.
The field catalog is the contract of the tool and changes with a new version of it. The tool description and the
input schema list every field.

### Rules of the tables

- The tool adds the fields every query of a table must select, such as `id` in `product_view` and the price
  tables, or `report_date`, `report_granularity`, `report_country_code`, and `report_category_id` in the best
  sellers tables.
- A performance table (`product_performance_view`, `non_product_performance_view`) needs a metric among the
  fields and a `date_range`. A condition on a metric needs that metric among the fields.
- The best sellers tables need conditions on `report_granularity` and `report_country_code`. The competitive
  visibility tables need a `date_range` and conditions on `report_country_code` and `report_category_id`;
  `competitive_visibility_top_merchant_view` cannot select `date`.
- `reporting_context` and `item_issues.severity.aggregated_severity` of `product_view` can be filtered but not
  selected. Fields the documentation excludes from filtering or sorting, such as the ranks and rates of the
  competitive tables or `status_per_reporting_context`, are refused in a condition or in `order_by`. Prices,
  timestamps, and booleans take no condition, and prices, lists, and nested values no ordering.
- At most one condition per field, and `date` only through `date_range`. Use `BETWEEN` for a range.

Operators by field: text accepts `=`, `!=`, `<>`, `IN`, `NOT IN`, `LIKE`, `NOT LIKE`, `REGEXP_MATCH`, and
`NOT REGEXP_MATCH`; enumerations the first five; numbers and integers the comparisons, `IN`, `NOT IN`, and
`BETWEEN`; dates the comparisons and `BETWEEN`; lists of texts `CONTAINS ANY`, `CONTAINS ALL`, and `CONTAINS NONE`.
`BETWEEN` takes two values, the list operators 1 to 20, every other operator one. A text value has 1 to 200
characters, a pattern at most 100 and valid RE2. A value must not contain quotes or control characters or end in a
backslash, because the documentation names no escape rule for a string. Enumeration values are upper case with
underscores; Qatlas checks their form, Google their meaning. The windows are the twelve date functions of the
query language, from `LAST_7_DAYS` to `YESTERDAY`.

### Results

The result holds `table`, `rows`, `has_more`, and `next_cursor`. A row has the selected members of the view in
the form of the answer; a member that is not in the catalog or has another type is dropped. Texts are cut at 300
characters, thumbnail addresses at 2048, and identifiers and enumerations at 100. Nested values (prices, dates,
issues, statuses) keep their structure with at most 100 entries per list, 50 members per object, and 6 levels.
Addresses are never fetched. Domains in the competitive tables are market data of third parties.

Google returns no total. `has_more` is true when Google names a next page, and a full page alone does not mean
there is one. A cursor is tied to the tool, the account, the query, and the targets, so another query refuses
it; the page size may change between calls. A call reads at most 1000 rows, and the answer read from Google
is capped at 4 MiB: a table with wide rows, such as `product_view` with issues, needs a smaller `limit`.

### Access

Reports need the Merchant Center role Performance and insights or Admin. Google does not offer every table to
every account, in particular the best sellers and competitive visibility tables; a refusal ends as
`permission` or another class of the [errors](README.md#errors). Every call counts against the quota of the
account, a failed one too, and Qatlas does not repeat it after `429`.

## Quotas and limits

`quotas.list` takes `page_size` from 1 to 1000 (Google sends 500 without it) and shows the groups sorted by
usage with `name`, `quotaUsage`, `quotaLimit`, `quotaMinuteLimit`, and `methodDetails` (`version`, `method`,
`subapi`, `path`, at most 100 per group). The daily values reset at 12:00 UTC. The call counts against its own
group, so the tool is not meant for polling. The group part of a name has no guaranteed format.

`limits.list` needs `type`, whose only documented value is `products`, and takes `page_size` from 1 to 100.
`limits.get` takes the limit part of a name as `limit`, such as `products~ADS_NON_EEA`, as `limits.list` shows
it. A limit holds `name` and `products` with `scope` (`ADS_NON_EEA` or `ADS_EEA`) and `limit`, the number of
products the account may hold. Numbers that Google sends as strings stay strings.

## Boundary

Checked against the reference pages of the Merchant API v1 (reports, quota, and accounts limits) and its guides
for the query language and the quotas:

- Google accepts a report `pageSize` up to 100000 and sends 1000 without it. The tool offers at most 1000 and
  sends 100 without it. The page size of `quotas.list` is 500 by default and 1000 at most, that of
  `limits.list` 100 and 100, and `limits.list` requires its `type` filter.
- The query language documents twelve date functions, which the tool offers all. It documents no maximum for
  `LIMIT`, so the tool sends no `LIMIT` and pages with `pageSize`.
- The guide shows 1000 calls a day and 10 a minute for the group of `quotas.list` only in an example answer;
  `quotas.list` shows the real values of an account.

Not verified:

- No live call was made. Field names, types, and the rules of each table come from the reference and the guides.
  The reference does not label segments and metrics outside the performance tables, and gives MCQL names only
  through the examples of the guides, so the snake case of the answer members is derived.
- Which nested members besides `item_issues.severity.aggregated_severity` MCQL accepts in a condition, whether a
  boolean or a price can be filtered, and how a string is escaped. The tool offers none of them.
- The enumeration values of `reporting_context`, `network`, and the traffic source beyond the documented lists,
  and whether a role below Performance and insights sees a table.
- Whether an account has the best sellers and competitive visibility tables.

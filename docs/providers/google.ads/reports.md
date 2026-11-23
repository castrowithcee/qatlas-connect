---
description: >
  Describes the report tools of the Google Ads provider: reports.search with its structured query, the field catalog of API version v26 and how it is renewed, the checks of fields, operators and values, the row limits and has_more, the fields whose values are never returned, the personal data a report can hold, target rules for resources that list accounts, and fields.search and fields.get for the field reference, with the numbers checked against the reference pages and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: report tools

These tools read data of Google Ads accounts and the field reference of the query language. They add to the
[Google Ads provider](README.md), whose configuration, targets, rate spacing, and errors apply here too. Their
arguments and results are in the descriptors; this page holds what the descriptors do not say.

| Tool | What it does |
| --- | --- |
| `google.ads.reports.search` | rows of one resource from a structured query on an account |
| `google.ads.fields.search` | fields, segments, metrics, and resources of the query language, filtered and paged |
| `google.ads.fields.get` | one field or resource by name |

All three are in the profile `read`. They only read, and they need no confirmation. The scope `adwords` also allows
changes, but no tool of this group changes anything.

## reports.search

Every resource of Google Ads is read through the query language GAQL; the API has no list or get call per
resource. This tool takes no query string. It takes the resource, the fields, conditions, sort keys, and a limit
as arguments, checks each of them, and builds the GAQL string itself, which it returns as `query`.

### What is checked, and when

Everything below ends the call as `invalid-request` before a secret is read and before any request. A message
names the argument and its position, never the value.

- `resource` must be one of the resources of the field catalog.
- Every field of `fields`, `conditions`, and `orderings` must be in the field catalog, must fit the resource, and
  must be selectable (`fields`), filterable (`conditions`), or sortable (`orderings`). A field fits a resource
  when it belongs to the resource, to a resource that Google lists as attributed to it, or is a segment or metric
  that Google lets a query on the resource use. A field that exists but does not fit is told apart from one
  without an entry, and `fields.get` on the resource names what fits. Whether Google accepts a combination of
  segments and metrics beyond that, or requires a condition such as a date range for a resource, only Google
  decides. A refused query is `provider-error` with the code in the original (`queryError`).
- `fields` holds 1 to 100 distinct names, `conditions` at most 50 entries joined by `AND`, `orderings` at most 10.
  There are no wildcards and no `OR`.
- The operator must fit the type of the field: `=` and `!=` on scalars, `>`, `>=`, `<`, `<=`, and `BETWEEN` on
  numbers and dates, `IN` and `NOT IN` on everything but booleans, `LIKE`, `NOT LIKE`, `REGEXP_MATCH`, and
  `NOT REGEXP_MATCH` on strings, `CONTAINS ANY`, `CONTAINS ALL`, and `CONTAINS NONE` on repeated fields only,
  `DURING` on dates with one of the twelve date functions of the grammar, and `IS NULL` and `IS NOT NULL` anywhere.
  Repeated fields take only the `CONTAINS` operators and the null checks.
- Values are typed by the catalog. Integers and doubles are plain numbers, or strings of digits, without exponent.
  Dates are `YYYY-MM-DD`, and fields that hold a date and time take `YYYY-MM-DD HH:MM:SS`. Booleans are `true` or
  `false`. A list holds 1 to 1,000 values and `BETWEEN` exactly two.
- Enum values must be values of the enum of the catalog. `UNSPECIFIED` and `UNKNOWN` are placeholders of the API
  and are never valid in a condition, even though `fields.get` lists them in `enum_values`.
- Strings must not hold a quote, a double quote, a backslash, or a control character. Qatlas does not escape GAQL
  strings; it refuses such a value instead. This also holds for the patterns of `LIKE` and `REGEXP_MATCH`, so a
  regular expression with an escape such as `\d` cannot be written; use `[0-9]` and `[.]`. A regular expression
  must be valid RE2, which Go checks before the request. The wildcards `%` and `_` of `LIKE` are the caller's, and
  a literal `[`, `]`, `%`, or `_` is written in brackets, as the grammar says.

### Rows, limits, and has_more

`limit` is 1 to 1,000 and 100 when left out. These two numbers are Qatlas's own; Google documents no row limit
for a search besides its fixed page of up to 10,000 rows. Qatlas asks for one row more than `limit`. When Google
returns it, the extra row is dropped and `has_more` is true. `has_more` is also true when the total count that
Google returns (`total_results_count`, the number of matching rows ignoring the limit) is above `limit`, and when
the rows were cut at the output limit of 1 MiB, in which case `truncated` is true as well.

GAQL has no offset, and Qatlas does not hand out Google's page tokens, so there is no cursor. To go on after a
cut, narrow the conditions, for example by an ID greater than the last row, with `orderings` on that ID. Total
count and rows come from one request that costs one operation of the daily quota. Whether asking for the total
count raises the resources of a query is not verified; `query_resource_consumption` shows what Google counted.

Every row is an object with the selected fields by their full names. Google writes 64-bit integers as strings;
money is in micros. A field Google leaves out is null, or missing from the result, since the core leaves empty
members out. Only the selected fields are returned, no matter what else Google puts in a row. Messages and lists
keep Google's structure; they are cut at 8 levels, 200 members, and 500 entries, and text at 4096 bytes.
Control characters in text become spaces. Everything Google returns is untrusted third-party content.

The remaining arguments are the request options of the search. `summary_row` asks for the summary row of the
metrics, `omit_results` leaves the rows out and answers the total count, `validate_only` lets Google check the
query without running it and returns no rows, and `omit_unselected_resource_names` sets the query parameter of
that name. The last one is documented by the MCP server of Google, not by the grammar page, and is off by default.
`field_mask` and `metric_attributes` are returned as Google gives them.

### Fields whose values are never returned

These values are never returned. The fields cannot be selected, filtered, or sorted either, since a condition
would reveal the value. `fields.get` marks each of them with `value_withheld`.

| Field | Why |
| --- | --- |
| `customer_user_access.passkey_enabled` | security state of other people |
| `third_party_app_analytics_link.shareable_link_id` | credential that gives a third party access |
| `asset.lead_form_asset.delivery_methods` | holds the webhook address and the shared secret of the lead form delivery |
| `android_privacy_shared_key_google_ad_group.shared_ad_group_key`, `..._campaign.shared_campaign_key`, `..._network_type.shared_network_type_key` | shared keys of no documented kind, treated as credentials |

Where a field is a message that holds one of these, as the new access entry of a multi-party authorization review,
the member is dropped from the message: `passkeyEnabled`, `shareableLinkId`, `webhook`, `googleSecret`,
`advertiserWebhookUrl`, and the three shared keys.

Resources such as `click_view`, `search_term_view`, `campaign_search_term_insight`, `lead_form_submission_data`,
`local_services_lead`, and `local_services_lead_conversation` can hold personal data of third parties, such as
search terms, click IDs, form answers, and contact details. They are returned as Google gives them, and the
descriptor says so. `customer_user_access` returns the e-mail addresses of the users of an account.

### Targets

`customer_id` is required, ten digits without hyphens. It is checked against the targets of the connection, with
the membership read of the README for an account below a manager target, before a secret is read. The
resources `customer_client`, `customer_manager_link`, and `customer_client_link` list accounts other than the one
queried, and a row does not say under which manager an account sits. They are read only on a connection without
targets, on a manager target, or below one; on a customer target they are refused as `invalid-request`. The
manager of the header `login-customer-id` is the setting `login_customer_id` of the service and never an argument.

## fields.search and fields.get

These two tools return the field reference of the query language as the service for it, `GoogleAdsFieldService`,
gives it, for the fixed API version v26. They need a credential, since they call Google, but no account, so they
take no `customer_id`, no target applies, and no `login-customer-id` header is sent. The result of a field holds
all documented members: name, resource name, category, data type, whether it is selectable, filterable, or
sortable, whether it is repeated, the fields it can be selected with, for a resource its attributed resources,
metrics, and segments, the enum values, and the URL of the type. Lists are cut at 5,000 entries, and
`lists_truncated` says so.

`fields.search` takes filters instead of a query: a name prefix, part of a name, a category, and the capabilities.
They are joined by `AND`. `page_size` is 1 to 200 and 25 when left out, since the lists of a field can be long;
Google documents neither a default nor a maximum and may return fewer. A page is cut at 1 MiB, then without
cursor and with `truncated`. The cursor holds Google's page token, bound to the filters, the page size, the
targets, and `login_customer_id`; it fails before any request when one of them changed. Google lets a page token
expire after about two hours and demands the same query for the next page.

`fields.get` takes the name of a field or resource, such as `campaign` or `campaign.status`, and asks
`GET /v26/googleAdsFields/NAME`. The answer must name the field that was asked for.

## The field catalog

A query is built only from names the field catalog knows. The catalog is the field reference of the Google Ads API
v26, as it was generated into a file of the provider (`catalog_gen.go`) with its generator under `gen/`: every
resource with its fields, their capabilities and enum values, and which segments and metrics a query on the
resource may use. Neither the build nor a call needs the network or a credential for it. The catalog stays as it
is until someone runs `go generate` in the package, which reads the reference again; raising the API version is a
change of the provider. A field Google adds to v26 later is not in the catalog and is refused until then, while
`fields.search` and `fields.get` show what Google has now.

The catalog holds 190 resources, the number the field reference of v26 lists. Other counts, such as 189 queryable
resources counted from the proto files, differ by one; the catalog follows the field reference.

## Boundary

Checked against the reference pages of v26:

- `GoogleAdsService.Search` returns pages of up to 10,000 rows with `next_page_token`, not for the last page. The
  request takes `customer_id` and `query` as required, and `page_token`, `search_settings`, and `validate_only`;
  `page_size` is deprecated and answers `PAGE_SIZE_NOT_SUPPORTED`. The response has `results`, `next_page_token`,
  `total_results_count` (the count ignoring `LIMIT`), `field_mask`, `summary_row`, `query_resource_consumption`,
  and `metric_attributes`. `search_settings` holds `omit_results`, `return_summary_row`, and
  `return_total_results_count`, all off by default. The numbers 100 and 1,000 are not in Google's pages;
  they are limits of Qatlas.
- `SearchStream` has batches of up to 10,000 rows and no stated maximum for the whole result. It is not offered,
  because it has no bound.
- `SearchGoogleAdsFields` requires `query`; `page_size` and `page_token` are optional and the server may return
  fewer rows than asked. `GetGoogleAdsField` requires `resource_name`, in the form `googleAdsFields/NAME`.
- The grammar page says that `FROM` is required for the search service and must be left out for the field service,
  that `LIMIT` takes a positive integer, that conditions join only with `AND`, and that `REGEXP_MATCH` uses RE2.
  It also documents the escapes `\\`, `\'`, `\"`, `\n`, `\r`, and `\t` in strings. Qatlas still refuses
  quotes and backslashes instead of escaping them, so that no value can change the structure of a query.

Not verified:

- No live call was made. Fixtures stand for the answers of Google.
- The query grammar of the field service: that `name LIKE`, `category =`, and the capability members work in a
  `WHERE` clause, and how `_` in a `LIKE` pattern behaves there. The pages read show only the `WHERE name =` form.
  `fields.search` does not use `ORDER BY` or `LIMIT` for that reason.
- The format of date literals; the grammar page does not state it. Qatlas writes `YYYY-MM-DD`.
- Whether Google accepts the parameter `omit_unselected_resource_names`.
- Which combinations of segments, metrics, and resources Google refuses beyond what the catalog says, and what
  each one costs.
- Whether a message field of the catalog, selected as a whole, can hold more members than the ones named above
  that are credentials. The member names are those of the reference of the resources.

Left out on purpose:

- A free query string, `page_token`, `SearchStream`, and the argument `login_customer_id`.
- The values of the withheld fields above.

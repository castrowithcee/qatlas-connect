---
description: >
  Describes the Google Analytics BigQuery link tools (alpha): bigquery-links.list and bigquery-links.get read the BigQuery links of a property with the Admin API v1alpha methods, the export settings in the result, how targets bind the property, untrusted values and their bounds, and the limits of what is verified; creating, changing, and deleting a link has no tool.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics BigQuery links

The BigQuery link tools of the [Google Analytics provider](README.md). A BigQuery link joins a property to a
Google Cloud project and sets which data Analytics exports to it. Both tools use the alpha version of the Admin
API (`v1alpha`, Discovery document `analyticsadmin`, revision 20261115) and follow the
[alpha tools](README.md#alpha-tools) rule. They change nothing, a repeat is safe, they need the scope
`analytics.readonly`, and the profile `read` holds them; see [setup](setup.md#rights-and-scopes). They read no
report data and use no reporting quota. Creating, changing, and deleting a link has no tool.

| Tool | Google method |
| --- | --- |
| `bigquery-links.list` | `properties.bigQueryLinks.list` |
| `bigquery-links.get` | `properties.bigQueryLinks.get` |

## Result members

A link has `name` (`properties/ID/bigQueryLinks/ID`), `project` (`projects/NUMBER` of the Google Cloud project),
`datasetLocation`, `dailyExportEnabled`, `freshDailyExportEnabled`, `streamingExportEnabled`,
`includeAdvertisingId` (advertising IDs of mobile app streams), `exportStreams` (data stream names of the
property), `excludedEvents` (event names left out of the export), and `createTime`. A member Google does not
send is left out, so an absent switch is not `false`.

## google.analytics.bigquery-links.list

`property` (`properties/ID`) is required. `page_size` (1 to 200, Google returns at most 50 without it) and
`cursor` work as in the provider page; the cursor fits only the same connection and property. A link in the
answer that does not carry a valid name of the asked property is left out. An empty answer is an empty list.

## google.analytics.bigquery-links.get

`name` (`properties/ID/bigQueryLinks/ID`) is required; the link ID is not numeric. An answer for another link
than the one asked for is refused. Google answers a link that is missing like one the credential cannot see;
both are `not-found`.

## Targets and proof

The property in the argument or name follows the same rule as for the
[Google Ads links](google-ads-links.md#targets-and-proof).

## Provider content

The project, the location, the streams, and the event names are untrusted data. The project is shown only as
`projects/NUMBER`, and a stream only as a data stream of the same property; any other value is left out. The
location and the time are kept exactly as Google sends them, and a value that is not printable ASCII or too long
is left out. Event names are cut at 300 characters. An answer shows at most 200 streams and 1,000 excluded
events.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.
- Google documents no limit for the lists, so the counts above are Qatlas bounds.

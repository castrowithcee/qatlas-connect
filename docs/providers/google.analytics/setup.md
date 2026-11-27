---
description: >
  Guides a user through setting up the Google Analytics provider: the Cloud project and the enabled Admin and Data APIs, the three ways to get a credential (consent of a person, a service account key, impersonation), the access the account needs in Analytics, the scope each tool needs, adding the credential and connection to Qatlas, the connection test, and fixing errors by symptom.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-18
---

# Set up Google Analytics

This page shows how to connect Qatlas to Google Analytics 4. Targets, paging, and errors are described in the
[Google Analytics provider](README.md); the tools are described in [accounts](accounts.md),
[properties](properties.md), [data streams](data-streams.md), [custom dimensions](custom-dimensions.md),
[custom metrics](custom-metrics.md), [event create rules](event-create-rules.md),
[calculated metrics](calculated-metrics.md),
[annotations](annotations.md), [audiences](audiences.md), [channel groups](channel-groups.md), [expanded data sets](expanded-data-sets.md), [Display & Video 360 links](display-video360-links.md),
[AdSense links](adsense-links.md), [Search Ads 360 links](search-ads360-links.md), [reports](reports.md),
[audience exports](audience-exports.md), [report tasks](report-tasks.md),
[measurement protocol secrets](measurement-protocol-secrets.md), [access reports](access-reports.md),
[rollup links](rollup-links.md), [access bindings](access-bindings.md),
[data retention](data-retention.md), [Google Signals](google-signals.md), [Google Ads links](google-ads-links.md),
[Firebase links](firebase-links.md), [BigQuery links](bigquery-links.md), [attribution settings](attribution-settings.md),
and [change history](change-history.md).

## What you need

- A Google Cloud project, which Google uses to count the requests. It does not need a billing account for the
  reading tools.
- The **Google Analytics Admin API** enabled in that project, and for the report tools also the **Google
  Analytics Data API**.
- A way to prove who you are to Google. You choose one of three, see [Credential](#credential).
- Access to the Analytics account or property in Analytics itself. A credential only sees what the Google
  account or the service account behind it may see.
- The service is always `https://analyticsadmin.googleapis.com`, and the report tools use
  `https://analyticsdata.googleapis.com`; there is nothing to change for either.

## Enable the APIs

In the Google Cloud console, choose or create the project. Under APIs and services, open the library and enable
**Google Analytics Admin API**, and for the report tools **Google Analytics Data API**. Google changes its
pages from time to time; look for these names.

## Credential

Qatlas does not store an access token. It stores what is needed to get one when a tool runs, and renews it by
itself. Tokens, keys, and codes are never shown to an agent and never appear in a message, a result, or the log.
The credential is only for Google Analytics; it cannot also serve another Google provider.

### Consent of a person

Use this when you work with your own Google account.

1. In the Cloud console, under APIs and services, set up the consent screen of the project, then create an
   **OAuth client ID** of the type that suits a program on your computer (a desktop app). Note the client ID
   and, if Google shows one, the client secret.
2. Store them as the credential (see [Add it to Qatlas](#add-it-to-qatlas)): `client_id`, and `client_secret`
   if there is one.
3. Run `qconnect credential consent <credential>` in a terminal. It prints a link; open it in a browser and
   allow access. When the browser runs on another device, copy the address of the page that fails to load
   and paste it at the prompt of the command.

The consent asks for every scope the connections of the credential need together. A new consent replaces the
stored refresh token and is also how a scope that a new tool needs is added. An agent can never give the
consent; it needs a person at a terminal.

### Service account key

Use this for an unattended setup. In the Cloud console, create a service account in the project and a key for
it, and store the whole key file as the secret `service_account_key`. Then add the service account's address to
Analytics, see [Access in Analytics](#access-in-analytics). Do not use it together with a `client_id` in the
same credential.

### Impersonation

A person's consent can act as a service account instead of as themselves. Store `client_id` as for the
consent, name the service account in the `service_account` field of the connection, and give your Google
account the permission to create tokens for that service account (the role *Service Account Token Creator*).
Add the service account to Analytics as above.

### Access in Analytics

In Analytics, open Admin, then **Account access management** or **Property access management**, and add the
Google account or the service account address with at least the **Viewer** role. A property is visible with a
role on the property or on its account. The Analytics roles are not changed by the scopes below. The tools that
change something also need the **Editor** role: on the account to change or trash an account or to create a
property, and on the property or its account to change or trash a property or to change its calculated metrics, audiences, or expanded data sets.

## Rights and scopes

The reading tools need the scope `https://www.googleapis.com/auth/analytics.readonly`, the smallest scope that
reads Analytics configuration and report data. The access binding tools need the read scope
`https://www.googleapis.com/auth/analytics.manage.users.readonly` instead, because they list the users of an account
or property; a consent given before they were used needs to be repeated. Changing the roles of a binding and
removing bindings need `https://www.googleapis.com/auth/analytics.manage.users`, which also lets the token add
users, although no tool does; see [access bindings](access-bindings.md#scopes-and-profiles). The tools that change an account, a property, a data stream, an event create rule, an annotation, an audience, a channel group, or an expanded data set need
`https://www.googleapis.com/auth/analytics.edit`. Qatlas asks for the scope of the tools of the connections that
use the credential and no more. Each connection of a credential has to be for Google Analytics, so the
credential never carries the scope of another product.

One reading tool needs the editing scope: Google accepts only `analytics.edit` for the change history search, so a
credential that is used with `change-history.search` carries a token that could also change what its account may.
The tool is outside the profile `read` for that reason, see [change history](change-history.md#scope-and-profiles).

| Tool group | Tools | Scope |
| --- | --- | --- |
| [Accounts](accounts.md) | `accounts.list-summaries`, `accounts.list`, `accounts.get`, `accounts.get-data-sharing-settings` | `analytics.readonly` |
| [Accounts](accounts.md) | `accounts.update`, `accounts.delete` | `analytics.edit` |
| [Properties](properties.md) | `properties.list`, `properties.get` | `analytics.readonly` |
| [Reports](reports.md) | `reports.run`, `reports.get-metadata`, `reports.check-compatibility`, `reports.run-pivot`, `reports.batch-run`, `reports.batch-run-pivot`, `reports.run-realtime`, `reports.run-funnel` | `analytics.readonly` |
| [Audience exports](audience-exports.md) | `audience-exports.create`, `audience-exports.get`, `audience-exports.list`, `audience-exports.query` | `analytics.readonly` |
| [Report tasks](report-tasks.md) | `report-tasks.create`, `report-tasks.get`, `report-tasks.list`, `report-tasks.query` | `analytics.readonly` |
| [Access reports](access-reports.md) | `accounts.run-access-report`, `properties.run-access-report` | `analytics.readonly` |
| [Rollup links](rollup-links.md) | `rollup-links.list`, `rollup-links.get` | `analytics.readonly` |
| [Rollup links](rollup-links.md#google-analyticsrollup-linksdelete) | `rollup-links.delete` | `analytics.edit` |
| [Access bindings](access-bindings.md) | `access-bindings.list`, `access-bindings.get`, `access-bindings.batch-get` | `analytics.manage.users.readonly` |
| [Access bindings](access-bindings.md#googleanalyticsaccess-bindingsupdate) | `access-bindings.update`, `access-bindings.delete`, `access-bindings.batch-delete` | `analytics.manage.users` |
| [Properties](properties.md#changing-properties) | `properties.create`, `properties.update`, `properties.delete` | `analytics.edit` |
| [Data streams](data-streams.md) | `data-streams.list`, `data-streams.get` | `analytics.readonly` |
| [Data streams](data-streams.md#changing-data-streams) | `data-streams.create`, `data-streams.update`, `data-streams.delete` | `analytics.edit` |
| [Measurement protocol secrets](measurement-protocol-secrets.md) | `measurement-protocol-secrets.list`, `measurement-protocol-secrets.get` | `analytics.readonly` |
| [Measurement protocol secrets](measurement-protocol-secrets.md#googleanalyticsmeasurement-protocol-secretsdelete) | `measurement-protocol-secrets.delete` | `analytics.edit` |
| [Stream settings](data-streams.md#stream-settings-alpha) | `data-streams.get-enhanced-measurement`, `data-streams.get-data-redaction`, `data-streams.get-site-tag` | `analytics.readonly` |
| [Stream settings](data-streams.md#stream-settings-alpha) | `data-streams.update-enhanced-measurement`, `data-streams.update-data-redaction` | `analytics.edit` |
| [Custom dimensions](custom-dimensions.md) | `custom-dimensions.list`, `custom-dimensions.get` | `analytics.readonly` |
| [Custom dimensions](custom-dimensions.md#changing-custom-dimensions) | `custom-dimensions.create`, `custom-dimensions.update`, `custom-dimensions.archive` | `analytics.edit` |
| [Custom metrics](custom-metrics.md) | `custom-metrics.list`, `custom-metrics.get` | `analytics.readonly` |
| [Custom metrics](custom-metrics.md#changing-custom-metrics) | `custom-metrics.create`, `custom-metrics.update`, `custom-metrics.archive` | `analytics.edit` |
| [Key events](key-events.md) | `key-events.list`, `key-events.get` | `analytics.readonly` |
| [Key events](key-events.md#changing-key-events) | `key-events.create`, `key-events.update`, `key-events.delete` | `analytics.edit` |
| [Event create rules](event-create-rules.md) | `event-create-rules.list`, `event-create-rules.get` | `analytics.readonly` |
| [Event create rules](event-create-rules.md) | `event-create-rules.create`, `event-create-rules.update`, `event-create-rules.delete` | `analytics.edit` |
| [Calculated metrics](calculated-metrics.md) | `calculated-metrics.list`, `calculated-metrics.get` | `analytics.readonly` |
| [Calculated metrics](calculated-metrics.md#changing-calculated-metrics) | `calculated-metrics.create`, `calculated-metrics.update`, `calculated-metrics.delete` | `analytics.edit` |
| [Annotations](annotations.md) | `annotations.list`, `annotations.get` | `analytics.readonly` |
| [Annotations](annotations.md#changing-annotations) | `annotations.create`, `annotations.update`, `annotations.delete` | `analytics.edit` |
| [Audiences](audiences.md) | `audiences.list`, `audiences.get` | `analytics.readonly` |
| [Audiences](audiences.md#changing-audiences) | `audiences.create`, `audiences.update`, `audiences.archive` | `analytics.edit` |
| [Channel groups](channel-groups.md) | `channel-groups.list`, `channel-groups.get` | `analytics.readonly` |
| [Channel groups](channel-groups.md#changing-channel-groups) | `channel-groups.create`, `channel-groups.update`, `channel-groups.delete` | `analytics.edit` |
| [Expanded data sets](expanded-data-sets.md) | `expanded-data-sets.list`, `expanded-data-sets.get` | `analytics.readonly` |
| [Expanded data sets](expanded-data-sets.md#changing-expanded-data-sets) | `expanded-data-sets.create`, `expanded-data-sets.update`, `expanded-data-sets.delete` | `analytics.edit` |
| [Display & Video 360 links](display-video360-links.md) | `display-video360-links.list`, `display-video360-links.get`, `display-video360-link-proposals.list`, `display-video360-link-proposals.get` | `analytics.readonly` |
| [Display & Video 360 links](display-video360-links.md#deleting-and-cancelling) | `display-video360-links.delete`, `display-video360-link-proposals.cancel`, `display-video360-link-proposals.delete` | `analytics.edit` |
| [AdSense links](adsense-links.md) | `adsense-links.list`, `adsense-links.get` | `analytics.readonly` |
| [AdSense links](adsense-links.md#deleting-a-link) | `adsense-links.delete` | `analytics.edit` |
| [Search Ads 360 links](search-ads360-links.md) | `search-ads360-links.list`, `search-ads360-links.get` | `analytics.readonly` |
| [Search Ads 360 links](search-ads360-links.md#google-analyticssearch-ads360-linksdelete) | `search-ads360-links.delete` | `analytics.edit` |
| [Data retention](data-retention.md) | `data-retention.get` | `analytics.readonly` |
| [Data retention](data-retention.md#googleanalyticsdata-retentionupdate) | `data-retention.update` | `analytics.edit` |
| [Google Signals](google-signals.md) | `google-signals.get` | `analytics.readonly` |
| [Google Signals](google-signals.md#googleanalyticsgoogle-signalsupdate) | `google-signals.update` | `analytics.edit` |
| [Google Ads links](google-ads-links.md) | `google-ads-links.list` | `analytics.readonly` |
| [Google Ads links](google-ads-links.md#deleting-a-link) | `google-ads-links.delete` | `analytics.edit` |
| [Firebase links](firebase-links.md) | `firebase-links.list` | `analytics.readonly` |
| [Firebase links](firebase-links.md#deleting-a-link) | `firebase-links.delete` | `analytics.edit` |
| [BigQuery links](bigquery-links.md) | `bigquery-links.list`, `bigquery-links.get` | `analytics.readonly` |
| [BigQuery links](bigquery-links.md#deleting-a-link) | `bigquery-links.delete` | `analytics.edit` |
| [Attribution settings](attribution-settings.md) | `attribution-settings.get` | `analytics.readonly` |
| [Attribution settings](attribution-settings.md#googleanalyticsattribution-settingsupdate) | `attribution-settings.update` | `analytics.edit` |
| [Change history](change-history.md#scope-and-profiles) | `change-history.search` (reads only, but Google accepts `analytics.edit` alone) | `analytics.edit` |

## Add it to Qatlas

The guided setup does most of this for you:

1. Run `qconnect tui`.
2. In the Connections area, press `g` and follow the steps: provider `google.analytics`, the service, a
   credential, and the access. Choose targets if the connection should reach only some accounts or properties.
3. When the setup asks for the credential, choose where to keep it and give it the roles of your way above.

Without the guided setup, store the values on the command line. A value is read from standard input, so it does
not appear in the shell history:

```sh
printf %s "$GA_CLIENT_ID" | qconnect credential set analytics-login client_id
```

`analytics-login` is the name of the credential and `client_id` the role. For a service account key use the
role `service_account_key` and the content of the key file. Run `qconnect config validate` to check the
configuration file. It contacts no provider.

```yaml
services:
  analytics:
    provider: google.analytics

credentials:
  analytics-login:
    provider: google.analytics
    type: keyring

connections:
  analytics-read:
    service: analytics
    credential: analytics-login
    targets:
      - accounts/123456
```

Leave `targets` out to reach everything the credential sees.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. `[ok]` means Google accepted the
token, the Admin API is enabled in the project, and at least one account is visible. The test reads no report
data and uses no quota for reports. It does not test the Analytics Data API, which the report tools need.

## Fixing errors

| Symptom | Likely cause and fix |
| --- | --- |
| `consent-required` | The credential has a client ID but no consent yet. Run `qconnect credential consent <credential>` |
| `consent-expired` | Google no longer accepts the stored consent, for example after a revocation or a long pause. Give the consent again |
| `scope-missing` | The consent lacks the scope a tool needs. Give the consent again and allow all requested access |
| `credential-unusable` | The credential holds both or neither of a client ID and a service account key. Keep exactly one way |
| `auth` | Google rejected the token, also after a renewal. Check that the consent or key is still valid and belongs to the project you mean |
| `permission` with a hint that the Admin API is not enabled | Enable **Google Analytics Admin API** in the Cloud project of the credential and wait a few minutes |
| `permission` with a hint that the Data API is not enabled | Enable **Google Analytics Data API** in the same project |
| `permission` otherwise | The Google account or service account has no access in Analytics. Add it under access management, see [Access in Analytics](#access-in-analytics) |
| `not-found` in the connection test | The credential sees no account. Add it to an account in Analytics |
| `not-found` for a property | The property does not exist, or the credential cannot see it; Google gives the same answer |
| `impersonation-denied` | Your Google account may not create tokens for the service account. Grant *Service Account Token Creator* |
| `service-account-unknown` | The service account named in the connection does not exist. Check its address |
| `rate-limited` | The project or user used up its quota, or a report tool kept its reserve or parallel limit, see [reports](reports.md#quota). Wait; Qatlas does not repeat the request |
| A refusal that the resource is not available to this connection | The account or property is outside the targets of the connection. Change the targets or use another connection |
| A tool refuses without `filter` | `properties.list` needs a filter unless the connection binds exactly one account, see [properties](properties.md) |

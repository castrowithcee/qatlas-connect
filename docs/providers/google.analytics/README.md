---
description: >
  Describes the Google Analytics provider: the fixed Admin API origin, the token credential, the optional account and property targets and what each tool does with them, the profiles, paging, bounds on provider content, rate spacing, quotas, the connection test, and the limits of what is verified; the account and property tools have their own pages.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Analytics

Google Analytics is a provider for Google Analytics 4 on the Analytics Admin API. It lists the accounts and
properties a credential can see, with their configuration, reads one property, and creates, changes, and trashes
properties. The reading tools change nothing and read no report data, so they use no reporting quota.

[Set up Google Analytics](setup.md) leads through the credential. The tools are described in
[accounts](accounts.md) and [properties](properties.md).

## Configuration

A service is always the official origin `https://analyticsadmin.googleapis.com`, which applies when `base_url`
is left out. Qatlas refuses any other URL before a secret is read, because the access token belongs to that one
service.

The credential is a token credential: it does not hold a static key but issues short-lived access tokens, from
a person's consent, a service account key, or the impersonation of a service account. Its secret roles, the
consent, the storage of a renewed refresh token, and the rule that no token ever appears in a message, a result,
or the invocation log are those of every token provider; [setup](setup.md#credential) shows the steps for
Google. A credential belongs to one provider, so a Google Analytics credential is not shared with another Google
product. The tools send the access token only in the `Authorization` header.

The provider takes no further configuration and no exclusions.

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
| `properties.list` | Lists the properties of a bound account; see [properties](properties.md#googleanalyticspropertieslist) |
| `properties.get` | Reads a bound property, or a property of a bound account after one proof read of its parent |
| `properties.create` | Creates a property only in a bound account; a connection bound to properties only is refused |
| `properties.update`, `properties.delete` | Change or trash a bound property, or a property of a bound account after one proof read of its parent |

A property of another account is refused, also when it is named correctly.

## Profiles

The profile `read` is the recommended one and holds `google.analytics.accounts.list-summaries`,
`google.analytics.accounts.list`, `google.analytics.properties.list`, and `google.analytics.properties.get`.
The profile `write` reads like `read` and changes accounts and properties, each change with its own
confirmation: it adds `google.analytics.properties.create` and `google.analytics.properties.update`. Trashing a
property, `google.analytics.properties.delete`, is in no profile: a connection offers it only when its `tools`
list names it, and it needs the confirmation of a person. Which scope each tool needs is listed in [setup](setup.md#rights-and-scopes).

## Paging

A list returns `next_cursor` and `has_more` when Google reports a further page. The cursor wraps Google's page
token and fits only the tool, the connection, and the arguments that Google requires to stay the same, such as
the `filter`; a changed or invented cursor is refused before any request. `page_size` may change between
pages. Google may return fewer entries than asked for and a page can be empty while `has_more` is true.

## Provider content

Everything Google returns is untrusted third-party content, including display names. Names are cut at 300
characters and a cut text ends with `…`. Enumeration values and timestamps are kept exactly as Google sends
them, and a value that is not printable ASCII or too long is left out. A resource name that is not
`accounts/ID`, `properties/ID`, or `accountSummaries/ID` is left out. Lists are cut at 200 entries per page and
2,000 properties per account summary. Qatlas never fetches an address from a result.

## Rate spacing and quotas

Requests of one credential are spaced by at least 100 ms within a process. This is a cautious start, not a
Google limit. Google's Admin API quotas are set per Cloud project and per user, and the Cloud console shows
them. On a `429` the next request of the credential waits one second, or the seconds of `Retry-After` up to one
minute. The failed request is never repeated, and a missing `Retry-After` causes no retry loop. The reading
tools use no Data API quota; the Analytics Data API is not used by this provider yet.

## Connection test

`qconnect connection test` obtains an access token and lists one account summary. This reads no report data, so
it uses no reporting quota. Success proves the token, an enabled Analytics Admin API in the Cloud project of
the credential, and at least one visible account. A connection bound to properties is also tested by reading
each of them. No visible account is `not-found`. The test does not prove the Analytics Data API, which needs its
own activation.

## Errors

Qatlas' message never carries Google's text, because `message` and `details` may echo request values. Google's
answer goes along as the untrusted, redacted, and bounded original in the `provider` field of the error detail.
The class follows the HTTP status: 401 is `auth`, 403 `permission`, 404 `not-found`, 429 `rate-limited`, and
other failures `provider-error` or a transport class. A 401 renews the access token once and the request is
sent once more; a second 401 is an error, never a loop. A 403 with the reason `SERVICE_DISABLED` says that the
Admin API is not enabled in the Cloud project. Google answers a property or account the credential cannot see
like one that does not exist, so `not-found` and `permission` can both mean missing access. Symptoms and their
fixes are in [setup](setup.md#fixing-errors).

## Boundary

The provider reaches only the endpoints of the tool pages, each with a fixed path. It offers no passthrough of
method, path, header, query, or body.

Not verified:

- The quota figures of the Admin API are not checked by Qatlas and may change.
- The error form for a property the credential cannot see is not documented in the sources used; the HTTP
  status decides.
- The Analytics Data API is not covered by the connection test.

Left out on purpose: the deprecated members of the Admin API, any way to create or delete an account, and the
methods named in [properties](properties.md#left-out-on-purpose). A change is confirmed only by the answer
of Google, and between the proof read of a property and its change the property may be moved; Qatlas does not
close that window.

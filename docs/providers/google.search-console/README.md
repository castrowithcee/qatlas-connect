---
description: >
  Describes the Google Search Console provider: the token credential and its procedures, the scope of each tool, the fixed API origins, the optional property target and how it is proven, the profiles and the listed-only Indexing API notifications, quotas and rate spacing including the search-analytics query, the URL inspection, and the Indexing API, result bounds, the connection test, error classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console

Google Search Console is a provider for the Search Console API: the properties an account can see and the
sitemaps of a property, its search traffic, the index state of a URL, and the adding and removing of
properties and sitemaps. It also covers the Indexing API, which reads the notification status of URLs and
notifies Google of updated or removed URLs. The four tools that add or remove something change data, and the
two Indexing API notifications act at Google; every other tool is a read. [Set up Google Search Console](setup.md)
leads through the Google side and the consent. The tools are described per group in [sites](sites.md),
[sitemaps](sitemaps.md), [search analytics](search-analytics.md), [URL inspection](url-inspection.md), and
[indexing](indexing.md).

## Configuration

The API origin is fixed: `https://searchconsole.googleapis.com`. Leave `base_url` out or set it to that value;
Qatlas refuses any other value before a secret is read. The Indexing API tools use their own fixed origin,
`https://indexing.googleapis.com`. There is no API key: both APIs answer only to an OAuth access token.

The credential issues access tokens. It names `provider: google.search-console` and holds the roles of one
procedure:

| Procedure | Roles | Access of |
| --- | --- | --- |
| Consent | `client_id`, `client_secret` if the client has one, `refresh_token` from `qconnect credential consent` | the Google account that gave the consent |
| Service account | `service_account_key`, the complete key file | the service account |
| Impersonation | as consent, plus `service_account` on the connection | the named service account, through the consenting account |

A credential with both a client ID and a key, or neither, is unusable. Access tokens stay in memory, travel
only in the `Authorization` header, and are masked in every output; no tool returns a token.

The reading tools of the Search Console API need the scope `https://www.googleapis.com/auth/webmasters.readonly`.
The four changing tools need `https://www.googleapis.com/auth/webmasters`, because the read scope does not
suffice for them. All four Indexing API tools need `https://www.googleapis.com/auth/indexing`, the only scope of
that API, which allows notifications as well. A connection asks only for the scopes of the tools it offers, and
a consent asks for those of every connection of the credential. A connection by the effect `read` offers the
Indexing API reads too, so it asks for the Indexing scope as well, which Google cannot separate from
notifications. The profile `read` does not hold them, and a connection that should not hold the scope names
its tools in a `tools` list. A token without the scope ends the tool as `scope-missing` before Google is contacted; a new consent adds it. A
credential serves this provider only, so its token never carries scopes of another Google product.

The profile `read` is the recommended one and holds every reading tool of the Search Console API. The profile
`edit` holds those, `sites.add`, `sitemaps.submit`, `sitemaps.delete`, and the two Indexing API reads.
`sites.delete` is listed-only: it removes a property, and adding it again needs a new verification, so no
profile holds it and a connection offers it only when its `tools` list names it, whatever its effects say. The
Indexing API notifications are listed-only too: no profile and no effect offers them, only a `tools` list that
names them, see [indexing](indexing.md#listed-only-notifications). A connection offers the other changing tools
only when its effects include `create` or `delete`, and every call of one needs its own confirmation.

## Target

A connection may be bound to one property, spelled exactly as Search Console names it:

- `sc-domain:example.com` for a domain property, a DNS name of at least two labels (punycode for other
  scripts), or
- a URL prefix such as `https://www.example.com/`, with `http` or `https`, ending in a slash, without user
  information, query, or fragment.

Without a target the connection reaches every property the account can see. With one, `site_url` may be left
out and means the bound property; any other value, also the same property spelled differently, is refused
before a secret is read or a request is sent. `sites.list` then shows only the bound property. The Indexing
API tools take URLs instead of a property; a bound connection refuses every URL outside the property, see
[indexing](indexing.md#binding-to-a-property).

The connection test proves a bound property with `sites.get`: the account must hold it as owner, full user,
or restricted user. An unverified user has no access to the property's data, so Qatlas treats that level,
an unspecified one, and any unknown value as no access. Qatlas accepts the levels in both spellings Google
uses, `siteOwner` and `SITE_OWNER`.

## Quotas and rate spacing

The search-analytics query is counted apart: 1,200 queries per minute per site and per user, plus a load quota
that grows with the date range and with grouping or filtering by page or query. Qatlas spaces its queries by
50 ms per credential and property.

The URL inspection is counted apart: 2,000 per day and 600 per minute per property. Qatlas spaces its
inspections by 100 ms per credential and property and counts them per day (Pacific Time), see
[URL inspection](url-inspection.md#quota-and-spacing).

Google allows the site and sitemap methods 20 queries per second and 200 per minute per user, and
100,000,000 per day per Cloud project. The calls cost nothing.

Requests of one credential, and for an impersonation of one service account, are spaced by at least 300 ms
within a process, which is the per-user limit. The search-analytics query has its own limits and spacing, see
[search analytics](search-analytics.md#quota-and-spacing), and so has the Indexing API, whose notifications Qatlas
also counts per day, 200 per credential, see [indexing](indexing.md#quota-and-spacing). On a `429` the next
request of the credential to that API waits one second, or the seconds of `Retry-After` up to one minute. The
failed request is not repeated, and a missing `Retry-After` never leads to a retry.

## Results

Everything Google returns is untrusted third-party content. Search queries are personal data as well, see
[search analytics](search-analytics.md#search-queries-are-personal-data). Property addresses and sitemap paths are text and
are never fetched; they are cut at 2048 characters, every other text at 100, and a cut text ends with `…`. A
member that is not documented is dropped, and so is a flag that is not a boolean. A list of properties is cut
at 1000 entries and a list of sitemaps at 500; a cut list sets `truncated`. The answer read from Google is
capped at 4 MiB. Counts keep Google's form, a decimal string.

## Connection test

The connection test (`t` on a connection in `qconnect tui`) obtains an access token the way a call does, then lists the properties with
`sites.list`, and for a bound connection reads the bound property with `sites.get`. A pass proves the
credential, the scopes of the tools the connection offers, the Search Console API enabled in the Cloud project
of the OAuth client or service account, and at least one property with access. It proves nothing about the
Indexing API: neither its activation nor the owner role it needs.

| Result | Meaning |
| --- | --- |
| `consent-required` | the credential holds a client ID but no refresh token yet |
| `consent-expired` | Google no longer accepts the consent: it expired or was revoked |
| `scope-missing` | the token lacks a scope of the tools the connection offers |
| `credential-unusable` | the roles fit no procedure |
| `auth` | Google refused the client, the key, or the token |
| `permission` | the API is not enabled, or the account has no access to the bound property |
| `not-found` | the account sees no property it has access to |
| `impersonation-denied`, `service-account-unknown` | the impersonation was refused, see [setup](setup.md#troubleshooting) |

## Errors

Qatlas' message never carries Google's text. Google's own answer goes along as the original in the `provider`
field of the error detail (untrusted, redacted, and bounded): the `status`, the `message`, and the type and
reason of each detail. The answers of the token endpoints keep only their status and error code.

| Answer | Class |
| --- | --- |
| 400 | `invalid-provider-response`: Qatlas checked the request, so the API may have changed |
| 401 | `auth`, after the token was renewed once and the read was sent once more; a changing tool is never sent twice, so it ends at the first 401 and the next call renews the token |
| 403 | `permission`; for the Indexing API the message names the owner role and the API activation |
| 404 | `not-found` |
| 429 | `rate-limited` |
| 503 | `unreachable` |
| 504, or no answer in 20 seconds | `timeout` |
| Any other status | `provider-error` |

A redirect is never followed. An unreadable, oversized, or non-JSON answer is `invalid-provider-response`.
The changing tools get an empty answer from Google, with any 2xx status and with or without a content type;
that is success, and so is an empty JSON object. Any other body is `invalid-provider-response`.
Beyond the status, only `CREDENTIALS_MISSING` or `UNAUTHENTICATED` with 401, `PERMISSION_DENIED` with 403, and
`RATE_LIMIT_EXCEEDED` or `RESOURCE_EXHAUSTED` with 429 are read; no other reason chooses a class.

## Boundary

The provider reaches only the fixed origins and the fixed method of each tool. It offers no passthrough of
method, path, header, query, or body, and none of Google's global request parameters such as `fields`. A
batch of the Indexing API is built from fixed parts as well, see [indexing](indexing.md#batches).

Not verified:

- Whether Google answers a repeated delete with success or `404`. A `404` of a delete is mapped to
  `not-found`; that mapping was not observed live.
- Which permission level the changing methods need: Google's reference names only the scope. A refusal
  answers `403`, mapped to `permission`; that was not observed live.

- No live call was made. Methods, parameters, and answers are taken from the discovery document of the
  Search Console API v1, revision 20261115.
- Whether a service account added as a user of a property can read it through the API is known from
  community sources only.
- The spelling of permission levels in live answers: the discovery document names `SITE_OWNER` and the like,
  while answers are known to carry `siteOwner`. Qatlas accepts both.
- A disabled API answers 403 with the reason `SERVICE_DISABLED` across Google APIs; Qatlas does not read that
  reason and reports `permission`.

Left out on purpose:

- The deprecated mobile usability result of the URL inspection.
- The deprecated `indexed` count of a sitemap's contents.

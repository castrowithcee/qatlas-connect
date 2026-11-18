---
description: >
  Describes the Google Business Profile provider: the consent-only token credential, the one scope business.manage, the fixed API origins, the approval of the Cloud project and its error class, the performance tools, the optional account and location targets and how they are proven, the profile, quota and request spacing, result bounds, the connection test, error classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Business Profile

Google Business Profile is a provider for the Business Profile APIs v1: the accounts a person owns or manages
and the business locations in them, and the performance of a location. Most tools read; the tools of the
profile `write` change the role of existing admins and rename an account. [Set up Google Business
Profile](setup.md) leads through the Google side, the approval, and the consent. The tools are described per
group in [accounts](accounts.md), [locations](locations.md), [account admins](account-admins.md), [location
admins](location-admins.md), [performance](performance.md), [categories](categories.md), [chains](chains.md),
and [Google locations](google-locations.md).

## Credential

The credential names `provider: google.business-profile` and works only with the consent of a person:

| Roles | Access of |
| --- | --- |
| `client_id`, `client_secret` if the client has one, `refresh_token` from `qconnect credential consent` | the Google account that gave the consent |

The role `service_account_key` exists for every Google credential, but this provider does not use it: Google
documents no service account as a manager of a business profile, and an API key is refused by these APIs. A
credential with a service account key, or a connection that names a `service_account` to impersonate, ends as
`credential-unusable` before anything is sent.

Access tokens stay in memory, travel only in the `Authorization` header, and are masked in every output; no
tool returns a token. Until a consent is given, the connection is valid but answers `consent-required` before
any request.

## Scope and profile

Every tool needs `https://www.googleapis.com/auth/business.manage`. Google names no other scope for these APIs,
not even a read-only one, so a token of this provider may change whatever its account may change in Business
Profile, even through the profile `read`, whose tools change nothing. Use a Google account whose role on the profiles is
no wider than the task needs.

| Profile | Tools |
| --- | --- |
| `read` (recommended) | every read tool: `accounts.list`, `accounts.get`, `locations.list`, `locations.get`, `account-admins.list`, `location-admins.list` |
| `write` | `account-admins.update`, `location-admins.update`, `accounts.update`; each change needs confirmation |

The profile `read` changes nothing, but Google's only scope also allows changes, so the token could. The two
admin lists name other people (first and last name, and the email address of a pending invitation), which is
personal data; a connection that must not show them names its tools in a `tools` list instead of the profile.
The profile `write` is not recommended: it holds only the changing tools and needs a person to confirm each
change. Every change is spaced at 10 per minute and profile, see [account admins](account-admins.md).

## Configuration

The origins are fixed. Leave `base_url` out or set it to `https://mybusinessaccountmanagement.googleapis.com`;
Qatlas refuses any other value before a secret is read. The tools use three APIs, which are enabled in the Cloud
project one by one:

| API | Origin | Tools |
| --- | --- | --- |
| My Business Account Management API | `https://mybusinessaccountmanagement.googleapis.com` | [accounts](accounts.md) |
| My Business Business Information API | `https://mybusinessbusinessinformation.googleapis.com` | [locations](locations.md), [categories](categories.md), [chains](chains.md), [Google locations](google-locations.md) |
| Business Profile Performance API | `https://businessprofileperformance.googleapis.com` | [performance](performance.md) |

## Approval of the Cloud project

Google serves the Business Profile APIs only to a Cloud project it approved (Basic API Access). Until then the
project's quota is zero requests per minute; after the approval it is 300 requests per minute. The request for
access is described in [setup](setup.md#request-access-to-the-apis).

A project without approval answers with a rate limit. Qatlas reports it as `access-not-approved` when the rate
limit names a quota of zero, and in the connection test also when the first request is rate-limited. A rate
limit after a successful request is `rate-limited`.

## Target

A connection may be bound to one target:

- `accounts/ACCOUNT`, one account with a decimal ID, also a location group, and its locations, or
- `locations/LOCATION`, one location with a decimal ID.

Without a target the connection reaches every account and location the consenting account manages. Every
account and location argument is checked against its form and against the target before a secret is read or a
request is sent; a refusal never names the other object.

| Tool | Account target | Location target |
| --- | --- | --- |
| `accounts.list` | answers with the bound account only, read directly; `parent_account` is refused | not offered |
| `accounts.get` | the bound account only | not offered |
| `locations.list` | the locations of the bound account; another account and `accounts/-` are refused | answers with the bound location only, read directly; `account` and `filter` are refused |
| `locations.get` | first proves that the location is in the bound account's location list | the bound location only |
| `account-admins.list`, `account-admins.update`, `accounts.update` | the bound account only | not offered |
| `location-admins.list`, `location-admins.update` | prove the location first, like `locations.get` | the bound location only |
| `performance.*` | first prove, on every call, that the location is in the bound account's location list | the bound location only |

The proof of `locations.get` and of the performance tools reads the location list of the bound account, names
only, 100 at a time, until it finds the location. It reads at most 2000 locations; a location not found by then is refused like one outside
the target. The time between the proof and the read is a limit of the proof.

The tools of [categories](categories.md), [chains](chains.md), and [Google locations](google-locations.md) read
Google's reference data and the locations that exist on Google. They name no account or location of the
connection, so every connection offers them, whatever its target, and the target does not narrow their
results. They return no data of the user, but a Google location search can name businesses of other people.

A location names no account in Google's answer, so a location target offers no account tool and cannot tell
which account the location belongs to.

## Quotas and request spacing

Requests of one OAuth client are spaced by at least 200 ms within a process, which is the 300 requests per
minute of an approved project. Google counts the quota per project, shared by every user of the client. On a
`429` the next request of the client waits one second, or the seconds of `Retry-After` up to one minute. The
failed request is not repeated, and a missing `Retry-After` never leads to a retry. The calls cost nothing. The reference tools count against the same quota.

## Results

Everything Google returns is untrusted third-party content: names, addresses, phone numbers, websites,
descriptions, and labels. Websites and Google links are text and are never fetched. Texts are cut: identifiers
and codes at 100 characters, names, address lines, and phone numbers at 500, websites and links at 2048, and
descriptions at 4000; a cut text ends with `…`. Every list inside an account or a location is cut at 250
entries. A member that is not documented is dropped, and so is a value of the wrong type. The answer read from
Google is capped at 8 MiB. An empty list is left out of the result.

## Connection test

The connection test (`t` on a connection in `qconnect tui`) obtains an access token the way a call does, then
lists one account. With an account target it also reads that account. With a location target it reads only the
location with the read mask `name` and lists no account. A pass proves the consent, the scope, the enabled API,
and the approval of the project, not a role on a particular location.

| Result | Meaning |
| --- | --- |
| `consent-required` | the credential holds a client ID but no refresh token yet |
| `consent-expired` | Google no longer accepts the consent: it expired or was revoked |
| `scope-missing` | the token lacks `business.manage` |
| `credential-unusable` | the credential holds a service account key, or the connection impersonates a service account |
| `access-not-approved` | the project is not approved for the Business Profile APIs yet |
| `auth` | Google refused the client or the token |
| `permission` | the API is not enabled in the project of the OAuth client, or the account has no role |
| `not-found` | the bound account or location is not managed by the consenting account |
| `rate-limited` | a later request of the test was rate-limited |

## Errors

Qatlas' message never carries Google's text. Google's own answer goes along as the original in the `provider`
field of the error detail (untrusted, redacted, and bounded): the `status`, the `message`, and the type and
reason of each detail. The answers of the token endpoint keep only their status and error code.

| Answer | Class |
| --- | --- |
| 400 | `invalid-provider-response`: Qatlas checked the request, so the API may have changed |
| 401 | `auth`, after the token was renewed once and the read was sent once more |
| 403 | `permission` |
| 404 | `not-found` |
| 429 with a quota of zero | `access-not-approved` |
| 429 | `rate-limited` |
| 503 | `unreachable` |
| 504, or no answer in 20 seconds | `timeout` |
| Any other status | `provider-error` |

A redirect is never followed. An unreadable, oversized, or non-JSON answer is `invalid-provider-response`.
Beyond the status, only `CREDENTIALS_MISSING` or `UNAUTHENTICATED` with 401, `PERMISSION_DENIED` with 403,
`RATE_LIMIT_EXCEEDED` or `RESOURCE_EXHAUSTED` with 429, and a quota limit of `0` in the metadata of a 429 are
read; no other value chooses a class.

## Boundary

The provider reaches only the fixed origins and the fixed method of each tool. It offers no passthrough of
method, path, header, or body. The v4 accounts, admins, invitations, and transfer methods are deprecated and
not used. No tool invites anyone or creates an admin, and no tool creates an account or transfers a location.

Not verified:

- No live call was made. Methods, parameters, and answers are taken from the discovery documents of the My
  Business Account Management API v1, the My Business Business Information API v1, and the Business Profile
  Performance API v1, each of revision 20261116.
- How Google answers a project without approval. Qatlas assumes a `429` with the quota limit `0`; a pending
  approval may answer differently.
- Whether each Business Profile API needs to be enabled in the project on its own.
- Whether a read mask accepts nested field paths. Qatlas accepts only the top-level fields of a location.
- The exact form Google expects for several sort fields; Qatlas joins them with a comma.
- The exact form of chain IDs and category IDs. Qatlas accepts `chains/` with letters, digits, hyphens, and
  underscores, and `categories/gcid:` with lower-case letters, digits, and underscores, and refuses anything
  else before a request.
- How many names `categories.batch-get` accepts; Qatlas allows 50.
- Which classification Google gives `business.manage` when it verifies an OAuth app.
- Whether a change with `validate_only` counts against the 10 changes per minute; Qatlas counts it.
- The format of an admin ID; Qatlas accepts up to 64 letters, digits, `_`, and `-`.
- Whether Google allows `SITE_MANAGER` as the new role of an account admin; its documentation names only the
  creation of such an admin as refused.
- The date ranges the Performance API serves. Its discovery document, revision 20261116, names no limit, so
  Qatlas checks only that dates are real and ordered; see [performance](performance.md#date-ranges).

Left out on purpose:

- The deprecated metadata flags `canOperateLocalPost` and `canHaveBusinessCalls` of a location.
- `primaryOwner` of an account, which is input only.
- Service accounts and their impersonation, see [Credential](#credential).
- `admins.create` of an account and of a location: it invites a person by email address, which would open
  access for someone outside the profile. No tool takes an address.
- `accounts.create`: it would create an account with a new owner relation.
- The role `PRIMARY_OWNER` as a target role of an admin, and changing an admin who holds it: both transfer the
  ownership of the profile. An admin with a pending invitation is not changed either.
- `admins.delete`, the invitation methods, and `locations.transfer`.
- The deprecated metrics `BUSINESS_CONVERSATIONS` and `BUSINESS_FOOD_ORDERS` of the performance tools.

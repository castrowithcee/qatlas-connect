---
description: >
  Describes the Google Merchant provider: the token credential and its procedures, the single scope, the fixed API origin, the advanced-account and account targets and how an account is proven to belong to one, the tools and their groups, quotas, result bounds, the connection test, error classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-20
---

# Google Merchant

Google Merchant is a provider for the Merchant API v1 of Google Merchant Center. The table lists its tools; a tool
group with its own page links to it. [Set up Google Merchant](setup.md) leads through the Google side, the
developer registration, and the consent.

| Tool | What it does |
| --- | --- |
| `google.merchant.accounts.list` | the accounts the credential can access, with an optional Google filter |
| `google.merchant.accounts.get` | one account |
| `google.merchant.developer-registrations.get` | the Google Cloud projects registered as developers with an account |
| `google.merchant.business-infos.get` | the business address, customer service contacts, and phone number of an account, see [business profile](business-profile.md) |
| `google.merchant.business-infos.update` | change the address, customer service contacts, or Korean registration number; confirmed and offered only through the `tools` list of a connection, needs the Admin role, see [business profile](business-profile.md) |
| `google.merchant.business-identities.get` | the business identity declarations and the promotions consent of an account, see [business profile](business-profile.md) |
| `google.merchant.business-identities.update` | change the identity declarations or the promotions consent; confirmed and offered only through the `tools` list of a connection, needs the Admin role, see [business profile](business-profile.md) |
| `google.merchant.reports.search` | one report of an account, from a structured query, see [reports](reports.md) |
| `google.merchant.quotas.list` | the API quota groups of an account, see [reports](reports.md#quotas-and-limits) |
| `google.merchant.limits.list` | the limits of an account, see [reports](reports.md#quotas-and-limits) |
| `google.merchant.limits.get` | one limit of an account, see [reports](reports.md#quotas-and-limits) |
| `google.merchant.terms-of-service.get` | one version of the terms of service |
| `google.merchant.terms-of-service.retrieve-latest` | the latest terms of service for a kind and a region |
| `google.merchant.terms-of-service-agreement-states.get` | the agreement state of an account for one kind and country |
| `google.merchant.terms-of-service-agreement-states.retrieve-for-application` | the agreement state of an account for the application terms |

The terms tools are described in [terms of service](terms-of-service.md).

Every read tool needs no confirmation; the update tools of the business profile need one for
every call. The account resource has the members `name`, `accountId`,
`accountName`, `testAccount`, `adultContent`, `languageCode`, `homePageUri`, and `timeZone` (`id`, `version`);
all are output. It has no member for the account type, so Qatlas shows none: an advanced account is
recognised only by its sub-accounts, which a target proves.

## Configuration

The API origin is fixed: `https://merchantapi.googleapis.com`. Leave `base_url` out or set it to that value;
Qatlas refuses any other value before a secret is read. The API answers only to an OAuth access token; an API
key ends in `401`.

The credential issues access tokens. It names `provider: google.merchant` and holds the roles of one procedure:

| Procedure | Roles | Access of |
| --- | --- | --- |
| Consent | `client_id`, `client_secret` if the client has one, `refresh_token` from `qconnect credential consent` | the Google account that gave the consent |
| Service account | `service_account_key`, the complete key file | the service account |
| Impersonation | as consent, plus `service_account` on the connection | the named service account, through the consenting account |

A credential with both a client ID and a key, or neither, is unusable. Access tokens stay in memory, travel
only in the `Authorization` header, and are masked in every output; no tool returns a token.

All tools need the one scope `https://www.googleapis.com/auth/content`. Google offers no read-only scope for the
Merchant API: the scope allows reading and writing, and what a call may do follows from the role of the user or
service account in Merchant Center. A token without the scope ends a tool as `scope-missing` before Google is
contacted; a new consent adds it. A credential serves this provider only, so its token never carries scopes of
another Google product.

The profile `read` is the recommended one and holds the read tools that need no `tools` list. A
connection without a `tools` list offers tools by their effects, and `read` is the default effect. A tool marked
as listed-only, such as `business-infos.update`, is in no profile and reaches a connection only through its `tools` list.

## Targets

A connection may be bound to any number of targets, at most 100, of two kinds:

| Kind | Form | Reaches |
| --- | --- | --- |
| `advanced-account` | `advanced-accounts/ACCOUNT` | the advanced account itself, its direct sub-accounts, and the tools that act on the advanced account as a whole |
| `account` | `accounts/ACCOUNT` | that one account, a sub-account or a standalone account |

`ACCOUNT` is the decimal account ID without a leading zero, up to 19 digits. Both kinds name an account by the
same ID, so an account has one value whichever way it is chosen. A target that is not spelled like this makes the
connection unusable before any secret is read, and so does an exclusion: this provider takes none. Without a
target the connection reaches every account the credential can access.

The `account` argument of the tools is the plain ID, digits only. With exactly one target it may be left out and
means that account. Qatlas checks it against the form and the targets before any secret is read or a request
is sent. The path of every request is built from the checked ID, never from free input.

- An account named by a target, as an advanced account or as an account, is read without further requests.
- Any other account is read only after one proof read: Qatlas lists the direct sub-accounts of each bound
  advanced account (`accounts.listSubaccounts`, 500 per page, at most 20 pages) and reads the account only if
  it is among them. An account outside every target is refused without naming it. If a bound advanced account
  has more sub-accounts than the 20 pages show, the call ends instead of guessing; bind the accounts themselves.
- Only direct sub-accounts are proven. An account below a sub-account needs a target of its own.
- `accounts.list` checks every row it receives against the targets and drops the others, and a row whose account
  ID cannot be read, or whose `name` and `accountId` disagree. The filter only narrows what Google sends. A
  batch can therefore be short or empty while `next_cursor` says there is more. For rows that no target
  names, the sub-accounts of the bound advanced accounts are read once per call.
- `developer-registrations.get` acts on an advanced account as a whole. On a connection bound to targets it
  needs `advanced-accounts/ACCOUNT` for that account: a sub-account, an account target, or an advanced account
  that is not bound is not enough. A connection bound only to account targets is not offered the tool. Without
  targets any account ID is accepted.
- `terms-of-service.get` and `terms-of-service.retrieve-latest` read global documents and act on no account, so
  every connection is offered them whatever its targets; see [terms of service](terms-of-service.md).
- The time between the proof read and the call that follows is not covered: an account that leaves its advanced
  account in between is still read once.

## Quotas

Google counts each method group against a daily limit, which resets at 12:00 UTC, and a rolling per-minute
limit. The quota of `accounts.list` is counted per user, not per account. Calls for sub-accounts count against the
pool of the advanced account. Every call counts, also a failed one; the proof reads count as well. Qatlas does
not repeat a call after `429`, and the next request of the credential in the same process waits for the seconds
of `Retry-After`, at least 10 seconds and at most one minute.

## Results

Everything Google returns is untrusted third-party content. Account names and home page addresses come from
whoever runs the account. Addresses are text and are never fetched. A name is cut at 300 characters, an address
at 2048, every other text at 100, and a cut text ends with `…`. A member that is not documented is dropped, and
so is a value of the wrong type. A list of accounts holds at most 1000 entries, and a list of project IDs at most
100. The answer read from Google is capped at 4 MiB. Cursors are tied to the tool, the filter, the page size, and
the targets, and another combination refuses them.

`accounts.list` takes `page_size` from 1 to 500; without it Google sends up to 250. `filter` is at most 500
characters without control characters and uses the filter syntax of the accounts API, for example
`accountName = "*store*"`, `access = DIRECT`, `access = INDIRECT`, or `relationship(providerId = 123)`. Qatlas
does not parse it. The targets are applied to every row whatever Google does with the filter.

## Connection test

The connection test (`t` on a connection in `qconnect tui`) obtains an access token the way a call does and then
reads one account, which costs nothing:

- With an advanced account target, it reads the first one and then its developer registration.
- With only account targets, it reads the first one. It reads no registration, because that belongs to the advanced
  account.
- Without a target, it lists one account and reads the developer registration of that account.

A pass proves the credential, the scope, the Merchant API enabled in the Cloud project, access to one account, and,
where it was read, a registration with at least one Cloud project. It does not prove the role for any method, and
it does not show that the Cloud project of the credential is the registered one: Google returns the registered
projects, not the caller's.

| Result | Meaning |
| --- | --- |
| `consent-required` | the credential holds a client ID but no refresh token yet |
| `consent-expired` | Google no longer accepts the consent: it expired or was revoked |
| `scope-missing` | the token lacks the scope |
| `credential-unusable` | the roles fit no procedure |
| `auth` | Google refused the client, the key, or the token |
| `permission` | the API is not enabled, the account has no access to the read account, the registration read was refused, or the account has no registered Cloud project |
| `not-found` | the credential sees no account |
| `rate-limited` | a quota of the account or user is used up |
| `impersonation-denied`, `service-account-unknown` | the impersonation was refused, see [setup](setup.md#troubleshooting) |

`permission` after a successful account read means that Google refused the registration read or returned a
registration without a project. A credential without a target that reaches only sub-accounts can show this
too, because the registration belongs to the account the user was added to; bind the advanced account to test it.

## Errors

Qatlas' message never carries Google's text. Google's own answer goes along as the original in the `provider`
field of the error detail (untrusted, redacted, and bounded): the `status`, the `message`, and the type and
reason of each detail. The answers of the token endpoints keep only their status and error code.

| Answer | Class |
| --- | --- |
| 401 | `auth`, after the token was renewed once and the read was sent once more; a change is not sent again, the next call renews the token |
| 403 | `permission`: no access to the account, a missing role, the API not enabled, or an unregistered Cloud project; a `403` renews nothing |
| 404 | `not-found` |
| 429 | `rate-limited`; the reason `QUOTA_REQUEST_RATE_TOO_HIGH` names the per-minute limit and `QUOTA_TOO_MANY_REQUESTS` the daily one |
| 503 | `unreachable` |
| 504, or no answer in 30 seconds | `timeout` |
| Any other status | `provider-error` |

A redirect is never followed. An unreadable, oversized, or non-JSON answer is `invalid-provider-response`.
The reason of a `429` is the only detail of a Google error that chooses wording; no other reason chooses a class.
A change that may have reached Google, with a transport failure, a timeout, a `5xx`, or an unreadable answer, adds
that its outcome is unknown. Changes are never repeated.

## Boundary

The provider reaches only the fixed origin and the fixed method of each tool. It offers no passthrough of method,
path, header, query, or body. It uses no Google client library and does not use the Content API for Shopping.

Checked against the discovery document of the accounts API v1 (revision 20261118): `accounts.list` returns 250
accounts by default and at most 500, `accounts.listSubaccounts` has the same bounds, and `accounts.get` and
`developerRegistration.get` have no parameters besides the account.

Not verified:

- No live call was made. Methods, parameters, and answers are taken from the discovery document and the guides of
  the Merchant API.
- What Google answers for a Cloud project that is not registered as a developer, and for a registration read on
  a sub-account. Both end as `403` or another error and are reported by class, not by reason.
- Which Merchant Center role each method needs, and whether a service account with the role Standard or Read only
  is enough for these tools; Google names only the role Admin for some methods.
- Whether Merchant Center accepts an impersonated service account as a user.
- Whether the scope `content` needs an app verification for an OAuth client of your own project.

No tool of this provider invites a user, registers or unregisters a Cloud project, links an account to another
one, sets a notification address, or accepts terms for an account (`terms-of-service.accept` is deliberately
missing). Merchant API methods for local feed partners and for triggering actions are not offered.

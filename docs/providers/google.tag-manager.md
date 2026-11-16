---
description: >
  Describes the Google Tag Manager provider: the token credential and its procedures, the fixed origin, the account and container targets, scopes per tool group, the shared quota and request spacing, the fingerprint convention, the connection test, error forms and their classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Tag Manager

Google Tag Manager is a provider for the Tag Manager API v2. Its credential issues access tokens instead of
holding a static key. The tools read accounts, containers, and workspaces; the descriptors list their
arguments and result fields.

## Credential

The credential names its provider `google.tag-manager` and holds the token roles `client_id`,
`client_secret`, `refresh_token`, and `service_account_key`. Which of them hold a value decides the
procedure:

- **Consent:** the client ID (and the client secret, if the client has one) of an OAuth client of type
  desktop application in your own Google Cloud project. A person gives the consent with
  `qconnect credential consent <credential>`, which stores the refresh token. Until then the connection is
  valid but answers `consent-required` before any request.
- **Service account:** the complete key file of a service account. The key file must name
  `https://oauth2.googleapis.com/token` as its token address.
- **Impersonation:** the client ID with a consent, and a service account named by the connection. The
  consent asks only for `https://www.googleapis.com/auth/cloud-platform`, which the IAM Credentials API needs
  to create a token of the service account; the Tag Manager scopes go to that request.

Qatlas asks Google for lasting access (`access_type=offline`) and always shows the consent screen
(`prompt=consent`), so a renewed consent issues a new refresh token. A renewal asks for no narrower set of
scopes than the consent granted.

The Tag Manager API must be enabled in the Cloud project of the OAuth client or the service account, and
the Google account of the consent or the service account needs access to the accounts and containers in
Tag Manager itself.

## Configuration

The API has the fixed origin `https://tagmanager.googleapis.com`. Leave `base_url` out or set it to that
origin; any other value is refused before a secret is read. The access token travels only in the
`Authorization` header, never in a URL, and no request follows a redirect.

The profile `read` is the recommended one and holds every tool of this provider.

## Targets

A connection takes at most one target:

| Target | Reach |
| --- | --- |
| none | everything the credential reaches |
| `accounts/ACCOUNT` | one account and everything in it |
| `accounts/ACCOUNT/containers/CONTAINER` | one container; its account can be read, nothing else of it |

IDs are decimal without a leading zero. Every path argument is checked against its form and against the
target before a secret is read or a request is sent; a path outside the target is `invalid-request`. With a
target, `accounts.list` answers with the bound account only, and with a container target `containers.list`
answers with the bound container only; both read that one entry directly and never the list of the token.

## Scopes

Each tool needs the smallest scope Google documents for it. A connection asks for the union of the scopes of
the tools it offers; a new tool group with another scope needs a new consent.

| Tool group | Scope |
| --- | --- |
| accounts, containers, workspaces (read) | `https://www.googleapis.com/auth/tagmanager.readonly` |

Google also offers `tagmanager.edit.containers`, `tagmanager.edit.containerversions`, `tagmanager.publish`,
`tagmanager.delete.containers`, `tagmanager.manage.accounts`, and `tagmanager.manage.users`. A connection
that needs one of them is best given a credential of its own.

## Quota and spacing

Tag Manager allows 10,000 requests per day and 0.25 requests per second per Cloud project. The quota belongs
to the project of the OAuth client and is shared by every user of that client. Qatlas therefore spaces the
requests of one client ID by at least 4 seconds within a process, without a burst; a service account is
spaced by its key. A call that would wait beyond its deadline ends at once.

On `429` the next request of the client waits at least 4 seconds, or the seconds of `Retry-After` up to one
minute. The failed request is not repeated, and a missing `Retry-After` never leads to a retry.

## Lists

A list tool reads one batch per call. `next_cursor` continues it and is bound to the tool, its arguments,
and the target of the connection; a cursor of another call, or an altered one, is `invalid-request`. The list
ends when Google sends no continuation token. An account, container, or workspace without entries gives an
empty list.

## Fingerprint

Every result that carries a `fingerprint` returns it. Tools that change an object follow one convention: they
take the fingerprint last read as a required argument, so a change made in the meantime is not overwritten
silently.

## Results

Everything Google returns is untrusted content of whoever edits the account. IDs and paths are cut at 100
characters, names at 300, notes and descriptions at 2000, and URLs at 2048; a cut text ends with `…`. Lists
inside an object keep at most 100 entries, and an answer with more than 1000 entries or above 4 MiB is
`invalid-provider-response`. URLs are text and are never fetched.

## Connection test

`qconnect connection test` first obtains an access token, which tells a missing consent, an expired or
revoked consent, and a missing scope apart. It then reads the first batch of accounts, or the bound container
for a container target. A passing test proves the token and the enabled API, not a role on a particular
container. Tag Manager offers no page size, so the first batch is Google's default.

## Errors

Qatlas' message never carries Google's text. Google's answer goes along as the original in the `provider`
field of the error detail (untrusted, redacted, and bounded): the `status`, the `message`, and the type and
reason of each detail.

| Answer | Class |
| --- | --- |
| 401 | `auth`, after one renewal of the token and one repeat of the read |
| 403 | `permission`: no access in Tag Manager, or the API is not enabled |
| 429 | `rate-limited` |
| any other status | `invalid-provider-response` |
| no answer in 30 seconds | `timeout` |

Only 401 (`CREDENTIALS_MISSING`), 403 (`PERMISSION_DENIED`), and 429 (`RATE_LIMIT_EXCEEDED`) are observed
answers of this API, so no other status is given a class of its own. Failures of the token endpoint keep only
their status and error code.

## Boundary

The provider reaches only the fixed methods of its tools and offers no passthrough of method, path, header,
query, or body.

Not verified:

- Behaviour against the live API. Methods, parameters, and result members follow the discovery document
  `tagmanager` v2, revision 20261111.
- Whether a service account can be a user of a Tag Manager container.
- Whether a person with access to a container alone may read its account, which the container target does
  for `accounts.list` and `accounts.get`.
- The form of a quota failure (`QuotaFailure`, `Retry-After`) and of the answer to a stale fingerprint.
- Whether Google accepts fewer scopes when a refresh token is redeemed.

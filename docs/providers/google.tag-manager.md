---
description: >
  Describes the Google Tag Manager provider: the token credential and its procedures, the fixed origin, the account and container targets, profiles and scopes per tool group, the shared quota and request spacing, the fingerprint convention, the tag tools and their parameter structure, the connection test, error forms and their classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Tag Manager

Google Tag Manager is a provider for the Tag Manager API v2. Its credential issues access tokens instead of
holding a static key. The tools read accounts, containers, and workspaces, and read, create, update, delete,
and revert the tags of a workspace; the descriptors list their arguments and result fields.

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

Two profiles are offered:

- `read` (recommended) holds the tools that read: accounts, containers, workspaces, and tags.
- `write` holds the tools of `read` and `tags.create`, `tags.update`, and `tags.delete`. A deleted tag stays
  in the workspace changes until a version is published and can be brought back with `tags.revert`.

`tags.revert` is in no profile. It overwrites the workspace state of a tag, so a connection offers it only
when its `tools` list names it. Every tool that changes
something needs its own confirmation, which shows the name, the type, and the workspace or tag path.

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

Each tool needs the smallest scope Google lists for its method. A connection asks for the union of the scopes
of the tools it offers; offering a tool with another scope needs a new consent.

| Tools | Scope |
| --- | --- |
| every tool that reads | `https://www.googleapis.com/auth/tagmanager.readonly` |
| `tags.create`, `tags.update`, `tags.delete`, `tags.revert` | `https://www.googleapis.com/auth/tagmanager.edit.containers` |

A consent given for the `read` profile lacks the edit scope, so a changing tool answers `scope-missing`
before any request. Google also offers `tagmanager.edit.containerversions`, `tagmanager.publish`,
`tagmanager.delete.containers`, `tagmanager.manage.accounts`, and `tagmanager.manage.users`; no tool uses them.

## Quota and spacing

Tag Manager allows 10,000 requests per day and 0.25 requests per second per Cloud project. The quota belongs
to the project of the OAuth client and is shared by every user of that client. Qatlas therefore spaces the
requests of one client ID by at least 4 seconds within a process, without a burst; a service account is
spaced by its key. A call that would wait beyond its deadline ends at once.

On `429` the next request of the client waits at least 4 seconds, or the seconds of `Retry-After` up to one
minute. The failed request is not repeated, and a missing `Retry-After` never leads to a retry. A change is one
request and waits for the spacing once; it is never sent again, not even after a 401.

## Lists

A list tool reads one batch per call. `next_cursor` continues it and is bound to the tool, its arguments,
and the target of the connection; a cursor of another call, or an altered one, is `invalid-request`. The list
ends when Google sends no continuation token. An account, container, or workspace without entries gives an
empty list.

## Fingerprint

Every result that carries a `fingerprint` returns it. Tools that change an existing object follow one
convention: they take the fingerprint last read as a required argument, so a change made in the meantime is
not overwritten silently. `tags.update` and `tags.revert` refuse a call without it before any request. What
Google answers to a stale fingerprint is not verified, so it is not given a class of its own: it is
`invalid-provider-response` with Google's answer as the original.

## Tags

A tag lives in a workspace. The tools change the workspace only; a tag goes live with a published container
version, and no tool of this provider publishes one. A tag of type `html` (Custom HTML) or `img` (Custom
Image) runs code or calls the network on the pages of the container once a version that holds it is
published, which is why every change is confirmed.

| Tool | Does |
| --- | --- |
| `tags.list` | reads the tags of a workspace, a batch per call with `cursor` |
| `tags.get` | reads one tag by its path, the fingerprint included |
| `tags.create` | creates a tag in a workspace; a repeated call creates a second tag |
| `tags.update` | replaces a tag as a whole; needs `fingerprint` |
| `tags.delete` | deletes a tag from the workspace |
| `tags.revert` | discards the workspace changes of a tag; needs `fingerprint`; only by `tools` list |

`workspace` and `tag` are paths such as `accounts/1/containers/2/workspaces/3/tags/4`; both are checked against
the target of the connection. `tags.update` is a PUT of the whole tag: a member that is left out is removed,
so send every member as `tags.get` returned it, with the changes made.

The arguments of `create` and `update` are the members of the tag: `name` and `type` (needed), `parameter`,
`firingTriggerId`, `blockingTriggerId`, `setupTag`, `teardownTag`, `paused`, `liveOnly`, `priority`, `notes`,
`scheduleStartMs`, `scheduleEndMs`, `parentFolderId`, `tagFiringOption`, `monitoringMetadata`,
`monitoringMetadataTagNameKey`, and `consentSettings`. Members that Google assigns (`path`, `accountId`,
`containerId`, `workspaceId`, `tagId`, `fingerprint`, `tagManagerUrl`) are results only and are never sent.

A parameter is an object with `type` (needed: `template`, `integer`, `boolean`, `list`, `map`,
`triggerReference`, `tagReference`), `key` (needed on top level and inside a map), `value` as text,
`isWeakReference`, and for a `list` or `map` the nested parameters in `list` or `map`. Nesting goes five
levels deep; `priority` is one parameter, `monitoringMetadata` and `consentSettings.consentType` two levels.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "name": "Example tag", "type": "html",
 "paused": true,
 "parameter": [{"type": "template", "key": "html", "value": "<!-- example -->"},
               {"type": "list", "key": "rules", "list": [
                 {"type": "map", "map": [{"type": "template", "key": "k", "value": "v"}]}]}]}
```

`tags.revert` answers with the tag as it is in the latest container version; without `tag` in the answer the
version holds none, and the revert removed the tag from the workspace. `tags.delete` answers with the path
and `deleted: true`.

Parameter values (the `html` of a Custom HTML tag, constants, API keys) are data. They are returned and sent
unchanged and are never run or fetched, and they may hold secrets. The texts and lists of a tag are never
cut, unlike the texts of the other tools: a cut value that is sent back by `tags.update` would destroy
data. An answer that exceeds 4 MiB, or holds a text where a list belongs, is `invalid-provider-response`
instead of a shorter value. A request is bounded by 4 MiB and 5000 parameter entries.

## Results

Everything Google returns is untrusted content of whoever edits the account. In the results of accounts,
containers, and workspaces, IDs and paths are cut at 100 characters, names at 300, notes and descriptions at
2000, and URLs at 2048; a cut text ends with `…`. Lists inside an object keep at most 100 entries, and an
answer with more than 1000 entries or above 4 MiB is `invalid-provider-response`. Tags are not cut (see
Tags). URLs are text and are never fetched.

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
| 401 | `auth`, after one renewal of the token and one repeat of a read; a change is not repeated, and the next call renews the token |
| 403 | `permission`: no access in Tag Manager, or the API is not enabled |
| 429 | `rate-limited` |
| any other status | `invalid-provider-response` |
| no answer in 30 seconds | `timeout` |

After a timeout, a transport failure, a 5xx, or an unreadable answer to a change, the message says that the
outcome is unknown. The change is not repeated; read the tag before changing it again.

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
- The status Google answers to a successful `tags.delete`; 200 and 204 are accepted.
- How deep Google lets parameters nest, and which parameters a tag type needs.
- Whether Google accepts fewer scopes when a refresh token is redeemed.

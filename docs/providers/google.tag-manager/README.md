---
description: >
  Describes the Google Tag Manager provider: the token credential and its procedures, the fixed origin, the account and container targets, the profiles, the scope per tool group, the shared quota and request spacing, lists and cursors, the fingerprint convention, the shared content rules of tags, triggers, and variables, the built-in variables, the folders, the clients and transformations of server containers, the zones and their child containers, the Google tag configurations and their destination IDs, the result bounds, the connection test, error forms and their classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-17
---

# Google Tag Manager

Google Tag Manager is a provider for the Tag Manager API v2. Its credential issues access tokens instead of
holding a static key. [Set up Google Tag Manager](setup.md) leads through the Google side and the consent. The
tools are described per group in [accounts, containers, and workspaces](structure.md),
[workspace management](workspaces.md), [tags](tags.md), [triggers](triggers.md), [variables](variables.md),
[built-in variables](built-in-variables.md), [folders](folders.md), [clients](clients.md),
[transformations](transformations.md), [zones](zones.md), and [Google tag configurations](gtag-configs.md); the
descriptors list their arguments and result fields.

## Credential

The credential names its provider `google.tag-manager` and holds the token roles `client_id`,
`client_secret`, `refresh_token`, and `service_account_key`. Which of them hold a value decides the
procedure:

| Procedure | Roles | Access of |
| --- | --- | --- |
| Consent | `client_id`, `client_secret` if the client has one, `refresh_token` from `qconnect consent` | the Google account that gave the consent |
| Service account | `service_account_key`, the complete key file, naming `https://oauth2.googleapis.com/token` as its token address | the service account |
| Impersonation | as consent, plus a service account named by the connection | the named service account, through the consenting account |

Until a consent is given, the connection is valid but answers `consent-required` before any request. An
impersonation consent asks only for `https://www.googleapis.com/auth/cloud-platform`, which the IAM Credentials
API needs to create a token of the service account; the Tag Manager scopes go to that request.

Qatlas asks Google for lasting access (`access_type=offline`) and always shows the consent screen
(`prompt=consent`), so a renewed consent issues a new refresh token. A renewal asks for no narrower set of
scopes than the consent granted.

## Configuration

The API has the fixed origin `https://tagmanager.googleapis.com`. Leave `base_url` out or set it to that
origin; any other value is refused before a secret is read. The access token travels only in the
`Authorization` header, never in a URL, and no request follows a redirect.

Two profiles are offered:

- `read` (recommended) holds the tools that read: accounts, containers, workspaces and their change status,
  tags, triggers, variables, built-in variables, folders, clients, transformations, zones, and Google tag
  configurations.
- `write` holds the tools of `read` and the tools that create and update workspaces, that create, update, and
  delete tags, triggers, variables, clients, transformations, zones, and Google tag configurations, that enable
  and disable built-in variables, and that create and update folders. A deleted tag, trigger, variable, client,
  transformation, or zone and a disabled built-in variable stay in the workspace changes until a version is
  published and can be brought back with their `revert` tool. A deleted Google tag configuration has no
  `revert` tool.

`workspaces.delete`, `tags.revert`, `triggers.revert`, `variables.revert`, `clients.revert`,
`transformations.revert`, `zones.revert`, and `built-in-variables.revert` are in no profile. Deleting a
workspace loses its unpublished changes and a revert overwrites the workspace state of an object, so a
connection offers them only when its `tools` list names them.
Every tool that changes something needs its own confirmation, which shows the name, the type or types, and the
workspace or object path. The confirmation of a Google tag configuration shows the type, the path, and only
its destination IDs, see [Google tag configurations](gtag-configs.md).

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
| every tool that creates, updates, deletes, or reverts a tag, a trigger, a variable, a client, a transformation, a zone, a Google tag configuration, or a built-in variable, every tool that creates or updates a folder, and `workspaces.create` and `workspaces.update` | `https://www.googleapis.com/auth/tagmanager.edit.containers` |
| `workspaces.delete` | `https://www.googleapis.com/auth/tagmanager.delete.containers` |

A consent given for the `read` profile lacks the edit scope, so a changing tool answers `scope-missing`
before any request. Google also offers `tagmanager.edit.containerversions`, `tagmanager.publish`,
`tagmanager.manage.accounts`, and `tagmanager.manage.users`; no tool uses them.

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
ends when Google sends no continuation token. A container, workspace, or object group without entries gives
an empty list.

## Fingerprint

Every result that carries a `fingerprint` returns it. Tools that change an existing tag, trigger, variable,
client, transformation, zone, Google tag configuration, or folder follow one convention: they take the
fingerprint last read as a required argument, so a change made in the meantime is not overwritten silently.
[Built-in variables](built-in-variables.md) have none, because Google's methods for them carry no fingerprint.
The update and revert tools of tags, triggers, variables, clients, transformations, and zones and the update
tools of folders and Google tag configurations refuse a call without it before any request. What Google answers to a stale fingerprint is not verified, so it is not given a class of
its own: it is `invalid-provider-response` with Google's answer as the original.

## Tags, triggers, and variables

Tags, triggers, and variables live in a workspace. Their tools change the workspace only. An object goes live with a
published container version, and no tool of this provider publishes one.

Update replaces the whole object: a member that is left out is removed, so send every member as the get tool
returned it, with the changes made. Revert discards the workspace changes of one object and answers with the
object as it is in the latest container version; without the object in the answer the version holds none, and
the revert removed it from the workspace. Delete answers with the path and `deleted: true`.

A parameter is an object with `type` (needed: `template`, `integer`, `boolean`, `list`, `map`,
`triggerReference`, `tagReference`), `key` (needed on top level and inside a map), `value` as text,
`isWeakReference`, and for a `list` or `map` the nested parameters in `list` or `map`. Parameters nest five
levels deep where an object takes a list of them; members that hold one parameter are shallower and named
with their tool group.

Parameter and filter values (the `html` of a Custom HTML tag, the code of a Custom JavaScript variable,
constants, API keys, regular expressions, CSS selectors) are data. They are returned and sent unchanged and are never run or fetched, and they may hold
secrets. The texts and lists of a tag, trigger, or variable, the name and notes of a folder, and the name and description of a workspace are never cut, unlike the texts of the other tools: a cut
value that is sent back by an update would destroy data. An answer that exceeds 4 MiB, holds more than 5000
parameter entries in one object, or holds a text where a list belongs is `invalid-provider-response` instead of
a shorter value. A request is bounded by 4 MiB and 5000 parameter entries.

[Clients](clients.md) and [transformations](transformations.md) follow the same update, revert, parameter, and
never-cut rules, [zones](zones.md) the same update, revert, and never-cut rules.
[Google tag configurations](gtag-configs.md) follow the update, parameter, and never-cut rules, but have no
`revert` tool. Their update also needs `previous_parameter`, so that the confirmation can show the destination
ID before and after the change.

## Results

Everything Google returns is untrusted content of whoever edits the account. The bounds of the results of
accounts, containers, and workspaces are in [structure](structure.md); tags, triggers, and variables are not cut. An answer
with more than 1000 entries or above 4 MiB is `invalid-provider-response`. URLs are text and are never fetched.

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
outcome is unknown. The change is not repeated; read the object before changing it again.

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
- The status Google answers to a successful delete; 200 and 204 are accepted.
- How deep Google lets parameters nest, and which parameters a tag, trigger, variable, client, transformation,
  or Google tag configuration type needs.
- Whether Google accepts fewer scopes when a refresh token is redeemed.

---
description: >
  Describes the Google Business Profile provider: the consent-only token credential, the one scope business.manage, the fixed API origins, the approval of the Cloud project and its error class, the location, admin, performance, verification, notification, review, food menu, and service list tools, the My Business API v4 origin, the optional account and location targets and how they are proven, the profile, quota and request spacing, result bounds, the connection test, error classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Business Profile

Google Business Profile is a provider for the Business Profile APIs v1: the accounts a person owns or manages
and the business locations in them, their attributes, the performance of a location, the verification state of
a location, and the notification setting of an account, and for the My Business API v4, which Google keeps for
reviews, media, local posts, food menus, followers, service lists, and the batch read of locations. Most tools read; the tools of the profile `write` change the role of existing
admins, decline invitations, rename an account, set or remove attributes, create and change
locations, create, change, and delete place action links, reply to reviews, add photos and videos
by address and recategorize them, create and change local posts, replace the food menus of a location, and replace its service list. The deletions of a location, an admin,
a reply to a review, a photo or video, and a local post are listed-only. [Set up Google Business Profile](setup.md)
leads through the Google side, the approval, and the consent. The tools are described per group in
[accounts](accounts.md), [locations](locations.md), [account admins](account-admins.md), [location
admins](location-admins.md), [invitations](invitations.md), [attributes](attributes.md),
[performance](performance.md), [categories](categories.md), [chains](chains.md), [Google
locations](google-locations.md), [lodging](lodging.md), [verifications](verifications.md),
[notifications](notifications.md), [place action links](place-action-links.md), [place action
types](place-action-types.md), [reviews](reviews.md), [media](media.md), [local posts](local-posts.md), [food menus](food-menus.md), [followers](followers.md), and [service lists](service-lists.md).

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
| `read` (recommended) | every read tool: `accounts.list`, `accounts.get`, `locations.list`, `locations.get`, `locations.get-google-updated`, `locations.batch-get`, `account-admins.list`, `location-admins.list`, `invitations.list`, `lodging.get`, `lodging.get-google-updated`, `attributes.list`, `attributes.get`, `performance.*`, `verifications.list`, `verifications.fetch-options`, `verifications.get-merchant-state`, `notifications.get`, `place-action-links.list`, `place-action-links.get`, `place-action-types.list`, `reviews.list`, `reviews.get`, `reviews.batch-get`, `media.list`, `media.get`, `local-posts.list`, `local-posts.get`, `food-menus.get`, `followers.get`, `service-lists.get` |
| `write` | `account-admins.update`, `location-admins.update`, `accounts.update`, `locations.create`, `locations.update`, `invitations.decline`, `lodging.update`, `attributes.update`, `place-action-links.create`, `place-action-links.update`, `place-action-links.delete`, `reviews.update-reply`, `media.create`, `media.update`, `local-posts.create`, `local-posts.update`, `food-menus.update`, `service-lists.update`; each change needs confirmation |
| none | `locations.delete`, `account-admins.delete`, `location-admins.delete`, `reviews.delete-reply`, `media.delete`, and `local-posts.delete` are listed-only: only a `tools` list offers them |

The profile `read` changes nothing, but Google's only scope also allows changes, so the token could. The two
admin lists name other people (first and last name, and the email address of a pending invitation), the
invitation list names the accounts and locations of those who invited, with the address of a location,
`verifications.fetch-options` shows masked postal addresses, phone numbers, and email addresses a PIN could go to,
and the review tools name reviewers (name and photo link) and the media tools the customers who uploaded a photo (`attribution`), which is personal data; a connection that must not
show them names its tools in a `tools` list instead of the profile.
The profile `write` is not recommended: it holds only the changing tools that do not remove content, admins, or a location, and needs a person to confirm each
change. A connection that needs one of the listed-only deletions names it in a `tools` list; without such a list, a connection with the effect `delete` does not offer it. Every change is spaced at 10 per minute and profile, see [account admins](account-admins.md).

## Configuration

The origins are fixed. Leave `base_url` out or set it to `https://mybusinessaccountmanagement.googleapis.com`;
Qatlas refuses any other value before a secret is read. The tools use eight APIs, which are enabled in the Cloud
project one by one:

| API | Origin | Tools |
| --- | --- | --- |
| My Business Account Management API | `https://mybusinessaccountmanagement.googleapis.com` | [accounts](accounts.md) |
| My Business Business Information API | `https://mybusinessbusinessinformation.googleapis.com` | [locations](locations.md), [categories](categories.md), [chains](chains.md), [Google locations](google-locations.md), [attributes](attributes.md) |
| Business Profile Performance API | `https://businessprofileperformance.googleapis.com` | [performance](performance.md) |
| My Business Lodging API | `https://mybusinesslodging.googleapis.com` | [lodging](lodging.md) |
| My Business Verifications API | `https://mybusinessverifications.googleapis.com` | [verifications](verifications.md) |
| My Business Notifications API | `https://mybusinessnotifications.googleapis.com` | [notifications](notifications.md) |
| My Business Place Actions API | `https://mybusinessplaceactions.googleapis.com` | [place action links](place-action-links.md), [place action types](place-action-types.md) |
| My Business API (v4) | `https://mybusiness.googleapis.com/v4` | [reviews](reviews.md), [media](media.md), [local posts](local-posts.md), [food menus](food-menus.md), [followers](followers.md), [service lists](service-lists.md), [locations](locations.md#google-business-profilelocationsbatch-get) |

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
| `locations.get`, `locations.get-google-updated`, `locations.update`, `locations.delete` | first prove that the location is in the bound account's location list | the bound location only |
| `locations.batch-get` | the account must be the bound one and every location is proved like `locations.get`, in one pass over the location list; one location outside the binding fails the whole call | only the location ID of each name is compared, the account part is an argument; see [locations](locations.md#google-business-profilelocationsbatch-get) |
| `locations.create` | the bound account only | not offered |
| `account-admins.list`, `account-admins.update`, `account-admins.delete`, `accounts.update` | the bound account only | not offered |
| `invitations.list`, `invitations.decline` | the invitations of the bound account only | not offered |
| `location-admins.list`, `location-admins.update`, `location-admins.delete` | prove the location first, like `locations.get` | the bound location only |
| `lodging.*` | first prove the location like `locations.get` | the bound location only |
| `attributes.list` | with `location`, like `locations.get`; without it not tied to a location | with `location`, only the bound location |
| `attributes.get`, `attributes.update` | prove the location first, like `locations.get` | the bound location only |
| `performance.*` | first prove, on every call, that the location is in the bound account's location list | the bound location only |
| `verifications.*` | first prove, on every call, like `performance.*` | the bound location only |
| `notifications.get` | the bound account only | not offered |
| `place-action-links.*` | first prove the location of the link, on every call | the bound location only |
| `place-action-types.list` | proves the location of a `location=` filter; other filters need no proof | a `location=` filter accepts only the bound location |
| `media.*` | like `reviews.*`: the account must be the bound one and the location is proved like `locations.get` | only the location ID is compared, the account part is an argument; see [media](media.md) |
| `reviews.*` | accounts and locations as `accounts/ACCOUNT/locations/LOCATION`; the account must be the bound one and the location is proved like `locations.get` | only the location ID is compared, the account part is an argument; see [reviews](reviews.md#the-v4-client) |
| `followers.get` | the same v4 path and rules as `reviews.*`, on every call | as `reviews.*`; see [followers](followers.md#reading) |
| `local-posts.*` | the same v4 paths and rules as `reviews.*`, on every call | as `reviews.*`; see [local posts](local-posts.md#the-v4-paths) |
| `service-lists.*` | the same v4 paths and rules as `reviews.*`, on every call, including the live read before a change | as `reviews.*`; see [service lists](service-lists.md#the-v4-paths) |
| `food-menus.*` | the same v4 paths and rules as `reviews.*`, on every call, including the live read before a change | as `reviews.*`; see [food menus](food-menus.md#the-v4-paths) |

The proof of the location tools and of the performance and verification tools reads the location list of the bound account, names
only, 100 at a time, until it finds the location. It reads at most 2000 locations; a location not found by then is refused like one outside
the target. The time between the proof and the read is a limit of the proof.

The tools of [categories](categories.md), [chains](chains.md), and [Google locations](google-locations.md) read
Google's reference data and the locations that exist on Google. They name no account or location of the
connection, so every connection offers them, whatever its target, and the target does not narrow their
results. They return no data of the user, but a Google location search can name businesses of other people.

A location names no account in Google's answer, so a location target offers no account tool and cannot tell
which account the location belongs to. The account tools are therefore also `account-admins.delete`,
`invitations.list`, and `invitations.decline`: each needs an account, and a refusal beats guessing one.

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
not used. v4 is used only for reviews, media, local posts, food menus, followers, service lists, and `locations.batch-get`; it has no discovery document, see [reviews](reviews.md#the-v4-client). No tool invites anyone or creates an admin, and no tool creates an account or transfers a location. The
location writes are described in [locations](locations.md#writing-a-location).

Not verified:

- No live call was made. Methods, parameters, and answers are taken from the discovery documents of the My
  Business Account Management API v1, the My Business Business Information API v1, the Business Profile
  Performance API v1, the My Business Lodging API v1, the My Business Verifications API v1, the My Business
  Notifications API v1, and the My Business Place Actions API v1, each of revision 20261116.
- Whether Google accepts the field names of the update mask of a place action link in the snake_case form its
  discovery document names, and which link ID forms it issues; Qatlas accepts letters, digits, `_`, and `-` up to
  128 characters.
- How Google answers a project without approval. Qatlas assumes a `429` with the quota limit `0`; a pending
  approval may answer differently.
- Whether each Business Profile API needs to be enabled in the project on its own.
- Whether Google's attribute metadata names an attribute with or without the prefix `attributes/` in `parent`; Qatlas accepts both and always sends `attributes/ATTRIBUTE`.
- The largest page of attribute metadata: Google names only the default of 200, so Qatlas asks for at most 200.
- Whether Google accepts a mask that names an attribute without a value in `attributes` as a removal, as its documentation says: `attributes.update` relies on it.
- Whether a read mask accepts nested field paths. Qatlas accepts only the top-level fields of a location.
- Whether the read and update masks of the lodging accept nested paths and group names as field masks usually
  do, and whether Google answers an unset boolean without the field. Qatlas sends the paths as given and compares
  `previous_values` with exactly what Google answers.
- The exact form Google expects for several sort fields; Qatlas joins them with a comma.
- The exact form of chain IDs and category IDs. Qatlas accepts `chains/` with letters, digits, hyphens, and
  underscores, and `categories/gcid:` with lower-case letters, digits, and underscores, and refuses anything
  else before a request.
- How many names `categories.batch-get` accepts; Qatlas allows 50.
- Which classification Google gives `business.manage` when it verifies an OAuth app.
- How Google reacts to a change of the title, the address, or a category: a new verification, a review, or a
  suspended profile.
- Whether a deleted location can be restored.
- Whether an update mask accepts nested field paths, and whether Google applies the limits of its documentation
  for texts; Qatlas accepts only top-level fields and caps a `profile` description at 4000 characters.
- Whether a change with `validate_only` counts against the 10 changes per minute; Qatlas counts it.
- The format of an admin ID; Qatlas accepts up to 64 letters, digits, `_`, and `-`.
- Whether the entry of the consenting account itself is among the admins that `admins.delete` removes; the answer
  names no identity, so the tools cannot warn before a self-lockout.
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
- Deprecated attributes: `attributes.update` does not change them, since Google does not save such a change.
- `accounts.invitations.accept`: it makes the user of the connection an admin of an account or location that
  someone else controls, which widens the reach of the credential through an entry that only the other side
  controls. A person accepts an invitation in Google's own interface; declining is offered because it only
  reduces access, see [invitations](invitations.md#accepting-is-left-out).
- `locations.transfer`.
- The deprecated metrics `BUSINESS_CONVERSATIONS` and `BUSINESS_FOOD_ORDERS` of the performance tools.
- Deleting or reporting a review: `reviews.delete-reply` removes only the reply of the business.
- `media.startUpload` and the upload of local files, see [media](media.md#adding-by-address).
- `locations.verify`, `locations.verifications.complete`, and `verificationTokens.generate`: they start or confirm
  a verification or prepare one, see [verifications](verifications.md#left-out).
- `accounts.updateNotificationSetting`: it sets the destination of the account's notifications, see
  [notifications](notifications.md#left-out).

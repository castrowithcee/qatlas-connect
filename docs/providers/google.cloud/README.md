---
description: >
  Describes the Google Cloud provider: the token credential and its fixed origins, the targets (organization, folder, project, billing account) and what they reach, the read profile, the search tools with their structured filters, cursors, and per-row target check, the IAM role and permission reads, the Google error form and its classes, long-running operations, rate spacing, the connection test, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Cloud

Google Cloud is a provider for Google Cloud APIs with one credential that issues access tokens. This
package covers Cloud Resource Manager: it searches organizations, folders, and projects, reads one project, and
reads the state of a long-running operation. It also reads IAM roles and the roles and permissions of a
resource. Every tool is a read. The package also lays down what the later
Google Cloud packages build on: the service table, the token handling, the error mapping, the operation
helper, and the targets.

[Set up Google Cloud](setup.md) leads through the credential and the rights it needs. The fields of a tool
are in its descriptor; this page holds what the descriptor does not say.

## Configuration

The service is always the official origin `https://cloudresourcemanager.googleapis.com`, which applies when
`base_url` is left out. Qatlas refuses any other URL before a secret is read. Later packages reach other fixed
origins of Google (IAM, Service Usage, Compute, Storage, DNS, Billing); none of them can be configured, a
redirect is never followed, and a tool never takes an origin, a path, a header, or a body.

The credential takes the roles of any token credential and picks its procedure from the roles that hold a value:
a person's consent (`client_id`, optionally `client_secret`, and the `refresh_token` that
`qconnect credential consent` stores), a service account (`service_account_key`), or the impersonation of a
service account that the connection names with `service_account` (the consent of a person who may create
tokens for it). The connection holds no scope. Qatlas derives them from the tools it offers and asks for the
smallest scope the discovery document of each method lists: `cloud-platform.read-only` for Resource Manager,
and `cloud-platform` for the IAM tools, because their discovery documents list no narrower scope (Google
offers no read-only variant for these methods, see [IAM](iam.md)). No tool of this provider changes anything, whatever the scope
allows. The access token travels in the
`Authorization` header only, is never an argument or part of a result, and is masked in every output. A 401
renews the token once, and nothing else repeats a request.

## Targets

A connection may be bound to organizations (`organizations/ID`), folders (`folders/ID`), projects
(`projects/ID`, by project ID or number), and billing accounts (`billingAccounts/ID`). The first three nest as
Google nests them: an organization takes in its folders and projects, a folder its subfolders and projects.
Without targets a connection reaches whatever its credential reaches, and the credential stays the upper bound
either way.

A billing account is a flat target of its own. Billing accounts belong to no organization, folder, or
project, so a connection reaches one only if it names it: a connection bound to organizations, folders, or
projects reaches no billing account, and an unbound connection reaches none either. The tools of this package
use no billing account, and a connection bound to billing accounts alone offers none of them. The billing
tools of a later package read the target.

Three rules decide which tool a binding reaches. They are separate on purpose:

- **Resource tools** (`projects.get`, `projects.search`): the connection is unbound or names an organization,
  a folder, or a project. `folders.search`, `folders.get`, `folders.list`, and `projects.list` need an organization or a
  folder, because a project holds no folder and no child.
- **Organization tools** (`organizations.search`, `organizations.get`): the binding must name an organization expressly. A
  connection without targets does not run them, because its credential may see organizations the person never
  handed over. The rows are limited to the organizations the targets name.
- **IAM tools** (`iam-roles.*`, `iam-permissions.query-testable`): the connection is unbound or names an
  organization, a folder, or a project, so a connection bound to billing accounts alone neither runs nor is
  offered them, not even for predefined roles. Organization roles and an organization resource are reached
  only if a target names exactly that organization; a connection without targets is refused.
- **Tools without a resource** (`operations.get`): an operation belongs to no folder or project that Qatlas
  could prove, so the tool runs on a connection without targets or with an organization among them. It is not an
  organization tool: it reads no organization.

A tool the binding cannot serve is not offered; the handler refuses the same request on its own, before a
secret is read and before any request.

## Profiles

The profile `read` is the recommended one. It holds every tool that needs no more than the scope
`cloud-platform.read-only`: `google.cloud.organizations.search`, `google.cloud.folders.search`,
`google.cloud.projects.search`, `google.cloud.projects.get`, `google.cloud.operations.get`,
`google.cloud.organizations.get`, `google.cloud.folders.get`, `google.cloud.folders.list`, and
`google.cloud.projects.list`.

The profile `read-iam` adds the IAM tools (`google.cloud.iam-roles.list`, `google.cloud.iam-roles.get`,
`google.cloud.iam-roles.query-grantable`, `google.cloud.iam-permissions.query-testable`,
`google.cloud.iam-policies.get`, `google.cloud.iam-permissions.test`, `google.cloud.iam-deny-policies.list`,
`google.cloud.iam-deny-policies.get`). They accept only the
scope `cloud-platform`, so a connection with this profile asks for a token that may change whatever its account
may change, although no tool of this provider changes anything. It is not recommended: give it only to a
connection that needs IAM, preferably with a credential whose account has viewer roles only.

## Tools

- [IAM](iam.md): IAM policies, permission tests, and deny policies of projects, folders, organizations, and
  service accounts.

### Search

`organizations.search`, `folders.search`, and `projects.search` take the documented fields of the `query`
parameter as structured arguments and build the query themselves. There is no free `query` argument. Each value
is checked, bare when it holds letters, digits, `-`, `_`, or `.`, and quoted otherwise; a value with a quote, a
backslash, or a control character is refused, because Google documents no escape rule for them. A trailing `*`
is Google's suffix wildcard and is allowed only after a bare value.

| Tool | Arguments | Combined by Google as |
| --- | --- | --- |
| `organizations.search` | `directory_customer_id`, `domain` | not combinable: Qatlas refuses both together |
| `folders.search` | `parent`, `display_name`, `state` | `AND` |
| `projects.search` | `parent`, `parent_type`, `parent_id`, `project_id`, `display_name`, `state`, `label_text`, `labels` | any one of them (`OR`) |

Google documents for `projects.search` that a project matches if it fits any one clause, so each filter added
widens the result. The aliases Google accepts (`name` for `displayName`, `id` for `projectId`,
`lifecycleState` for `state`) are not offered separately. `labels` is an object of key to value, at most ten
entries, where the value `*` means any value of that label; `label_text` is Google's `labels` field, a match on a
label name or value.

All three take `page_size` (1 to 100) and `cursor`. Google's `nextPageToken` is wrapped in the Qatlas cursor
`next_cursor`, which is bound to the tool, the targets of the connection, and the filters; the page size stays
out so a next page may ask for another size. A cursor that is altered, cut short, or issued for another search
is refused before a secret is read. The end is a missing `nextPageToken`: `has_more` is then false. A page can
be empty while `has_more` is true, because rows outside the targets are dropped.

**Target check of the rows.** Google may ignore a filter it does not understand, so the server filter is only
an optimization. Qatlas checks every row against the targets and drops what lies outside, without naming it.
Organization rows are decided by the targets alone. A project or folder row is admitted if it is a target, or
if its `parent` names a target organization or folder. Only where the parent does not decide, Qatlas reads the
folders above it with `folders.get` and stops at the first target, at the organization, or at the end of the
chain. Folders it has placed once are remembered during the call, so rows below the same folder cost one walk.
A call spends at most 30 such reads; a search that needs more is refused and asks for a narrower filter, for
example a `parent`. A row without a usable name is an invalid response.

### Organizations, folders, and projects

Reading one organization or folder and listing the folders and projects below a parent are described on their
own pages: [organizations](organizations.md) (`organizations.get`), [folders](folders.md) (`folders.get`,
`folders.list`), and [projects](projects.md) (`projects.list`).

### Single reads

`projects.get` takes a project ID or number and applies the same check: it reads the project and, where its
parent does not decide, the folders above it. A project outside the targets is refused without a name.
`operations.get` takes the `service` (here only `resource-manager`; later packages extend the list) and the
operation `name`.

### IAM

[IAM roles and permissions](iam-roles.md) covers `iam-roles.list`, `iam-roles.get`,
`iam-roles.query-grantable`, and `iam-permissions.query-testable`.

## Long-running operations

Google answers a change with an `Operation`: `name`, `done`, `metadata`, and once done either an `error` or a
`response`. A mutation of a later package returns it as it is and never waits. `operations.get` reads the
state: `done=false` with its metadata while it runs, `done=true` with the response when it succeeded. A done
operation that carries an `error` is a provider error, mapped from the `google.rpc.Code`: 5 `not-found`, 7
`permission`, 8 `rate-limited`, 16 `auth`, any other `provider-error`. The message of that error and its
details are Google's text and go along only as the original. `metadata` and `response` are open objects and
are cut: text at 1024 characters, objects and lists at 100 entries, nesting at six levels, and the whole value
at 2000 values.

## Errors

Qatlas' message never carries Google's text, because `message` and `details` may echo request values. Google's
own answer goes along as the original in the `provider` field of the error detail (untrusted, redacted, and
bounded). Google answers with `{"error":{"code","message","status","details":[...]}}`; only values it documents
decide beyond the HTTP status.

| Answer | Class |
| --- | --- |
| 401 with `CREDENTIALS_MISSING`, `ACCESS_TOKEN_EXPIRED`, or status `UNAUTHENTICATED` | `auth` |
| 403 with reason `SERVICE_DISABLED` | `permission`, the message names Cloud Resource Manager and IAM as the APIs to enable |
| 403 with reason `ACCESS_TOKEN_SCOPE_INSUFFICIENT` | `scope-missing` |
| 403 with status `PERMISSION_DENIED` | `permission` |
| 404 with status `NOT_FOUND` | `not-found` |
| 400 | `invalid-provider-response` (Qatlas checked the arguments before sending) |
| 429 | `rate-limited` |
| any other 401, 403, 404, or 4xx | `invalid-provider-response` |
| 5xx | by HTTP status |

A failed token request ends as `auth`, `consent-required`, or `consent-expired`, as the core decides. An
unreadable, oversized (over 2 MiB), or non-JSON answer is `invalid-provider-response`. The timeout is 30
seconds, below the 60 seconds an invoke may take.

## Rate spacing

Requests of one credential are spaced by at least 250 ms within a process. This is a cautious start of
Qatlas, not a Google limit: quotas are set in the Cloud console and only partly readable through an API. On a
429 the next request of the credential waits at least one second, or the seconds of `Retry-After` up to one
minute; a missing `Retry-After` never leads to a loop, and the failed request is not repeated.

## Connection test

`qconnect connection test` obtains an access token and calls `projects.search` with `pageSize` 1, which costs
nothing. It then reads every organization, folder, and project target to prove it exists. A billing account
target is not read here. If the Cloud Resource Manager API is not enabled in the Cloud project of the
credential, the test ends as `permission` with a message that says so.

## Boundary

Not verified:

- Google's `SERVICE_DISABLED` reason is the documented one for a disabled API; the exact answer of Cloud
  Resource Manager for it was not seen. Quota errors (`QuotaFailure`, `Retry-After`) are not mapped by reason;
  the HTTP status decides.
- The discovery document names no maximum page size. The limit of 100 is a Qatlas bound, and a page of more
  than 200 rows is refused as an invalid response.
- Google documents no combination of `directory_customer_id` and `domain`, and no wildcard inside quotes or
  escape rule for a quote; Qatlas offers none.
- The maximum length of a filter value (64 characters) and the characters of a label key and value (lowercase
  ASCII letters, digits, `-`, `_`) are narrower than what Google accepts.
- The per-method rights are as the discovery document states them: `resourcemanager.organizations.get`,
  `resourcemanager.folders.get`, `resourcemanager.projects.get`, and, for the lists,
  `resourcemanager.folders.list` and `resourcemanager.projects.list` on the parent; the right behind `operations.get` is not
  stated.

Left out on purpose:

- The `tags` of a project or folder appear only if Google sends them; Google documents them as input only.
- Every change of a resource (create, update, move, delete, undelete, and the policies of the hierarchy), and
  the reads of IAM policies and tag bindings of this service.
- A free `query` string, a `$fields` parameter, and any base URL other than the official origin.

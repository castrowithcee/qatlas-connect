---
description: >
  Describes the Google Cloud provider: the token credential and its fixed origins, the targets (organization, folder, project, billing account) and what they reach, the read and IAM profiles, the search tools with their structured filters, cursors, and per-row target check, the Cloud Storage reads of buckets, notifications, managed folders, and HMAC key metadata, the IAM role and permission reads, the service account and key metadata reads, the Google error form and its classes, long-running operations, rate spacing, the connection test, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-17
---

# Google Cloud

Google Cloud is a provider for Google Cloud APIs with one credential that issues access tokens. This
package covers Cloud Resource Manager: it searches organizations, folders, and projects, reads one project, and
reads the state of a long-running operation. It also reads IAM roles and the roles and permissions of a
resource. The only change is to a single IAM binding, through the tools of [IAM bindings](iam-bindings.md); every
other tool is a read. The package also lays down what the later
Google Cloud packages build on: the service table, the token handling, the error mapping, the operation
helper, and the targets.

[Set up Google Cloud](setup.md) leads through the credential and the rights it needs. The fields of a tool
are in its descriptor; this page holds what the descriptor does not say.

## Configuration

The service is always the official origin `https://cloudresourcemanager.googleapis.com`, which applies when
`base_url` is left out. Qatlas refuses any other URL before a secret is read. Later packages reach other fixed
origins of Google (IAM, Service Usage, API Keys, Compute, Storage, DNS, Billing, Cloud Quotas); none of them can be configured, a
redirect is never followed, and a tool never takes an origin, a path, a header, or a body.

The credential takes the roles of any token credential and picks its procedure from the roles that hold a value:
a person's consent (`client_id`, optionally `client_secret`, and the `refresh_token` that
`qconnect credential consent` stores), a service account (`service_account_key`), or the impersonation of a
service account that the connection names with `service_account` (the consent of a person who may create
tokens for it). The connection holds no scope. Qatlas derives them from the tools it offers and asks for the
smallest scope the discovery document of each method lists: `cloud-platform.read-only` for Resource Manager,
and `cloud-platform` for the IAM and quota tools, because their discovery documents list no narrower scope
(Google offers no read-only variant for these methods, see [IAM](iam.md) and [Quotas](quotas.md)). Only the tools of the profiles
`service-accounts`, `service-accounts-revoke`, `api-keys-revoke`, `iam-bindings`, `compute-network-delete`, `compute-lifecycle`, `compute-instances-create`, and `services` and the tools
`service-accounts.delete`, `services.disable`, and `instances.delete`, which a `tools` list must name, change anything, each with a confirmation, whatever the scope allows. The access token travels in the
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
- **IAM tools** (`iam-roles.*`, `iam-permissions.query-testable`, `service-accounts.*`, `service-account-keys.*`): the connection is unbound or names an
  organization, a folder, or a project, so a connection bound to billing accounts alone neither runs nor is
  offered them, not even for predefined roles. Organization roles and an organization resource are reached
  only if a target names exactly that organization; a connection without targets is refused.
- **Storage tools** (`buckets.*`, `bucket-access-controls.list`, `notifications.list`, `managed-folders.list`, `hmac-keys.*`): the connection is unbound or names an
  organization, a folder, or a project. A bucket is bound to the targets by the project number of one read of the
  bucket, see [Buckets](buckets.md).
- **Tools without a resource** (`operations.get`): an operation belongs to no folder or project that Qatlas
  could prove, so the tool runs on a connection without targets or with an organization among them. It is not an
  organization tool: it reads no organization.

A tool the binding cannot serve is not offered; the handler refuses the same request on its own, before a
secret is read and before any request.

## Profiles

The profile `read` is the recommended one. It holds every tool that needs no more than a read scope,
`cloud-platform.read-only` or `compute.readonly`: `google.cloud.organizations.search`, `google.cloud.folders.search`,
`google.cloud.projects.search`, `google.cloud.projects.get`, `google.cloud.operations.get`,
`google.cloud.organizations.get`, `google.cloud.folders.get`, `google.cloud.folders.list`,
`google.cloud.projects.list`, `google.cloud.services.list`, `google.cloud.services.get`,
`google.cloud.services.batch-get`, `google.cloud.api-keys.list`, `google.cloud.api-keys.get`, and the Compute tools `google.cloud.instances.list`,
`google.cloud.instances.aggregated-list`, `google.cloud.instances.get`, `google.cloud.machine-types.list`,
`google.cloud.zones.list`, `google.cloud.regions.list`, `google.cloud.compute-operations.get`,
`google.cloud.compute-operations.list`, `google.cloud.networks.list`, `google.cloud.networks.get`,
`google.cloud.subnetworks.list`, `google.cloud.subnetworks.get`, `google.cloud.firewalls.list`,
`google.cloud.firewalls.get`, `google.cloud.addresses.list`, `google.cloud.addresses.get`,
`google.cloud.vpn-gateways.list`, `google.cloud.vpn-gateways.get`, `google.cloud.vpn-tunnels.list`,
`google.cloud.vpn-tunnels.get`, `google.cloud.disks.list`, `google.cloud.disks.aggregated-list`,
`google.cloud.disks.get`, `google.cloud.snapshots.list`, `google.cloud.snapshots.get`,
`google.cloud.images.list`, and `google.cloud.images.get`, which ask for `compute.readonly`, and the Storage tools
`google.cloud.buckets.list`, `google.cloud.buckets.get`, `google.cloud.notifications.list`,
`google.cloud.managed-folders.list`, `google.cloud.hmac-keys.list`, and `google.cloud.hmac-keys.get`, which ask for
`devstorage.read_only`. The two
operation tools return the email address of whoever started an operation (personal data) and carry their own
sensitivity. A connection bound to an organization or a folder needs `projects.get` next to the
Compute tools to prove the project of a call; `read` holds both.

The profile `read-iam` adds the IAM tools (`google.cloud.iam-roles.list`, `google.cloud.iam-roles.get`,
`google.cloud.iam-roles.query-grantable`, `google.cloud.iam-permissions.query-testable`,
`google.cloud.iam-policies.get`, `google.cloud.iam-permissions.test`, `google.cloud.iam-deny-policies.list`,
`google.cloud.iam-deny-policies.get`) and the service account tools (`google.cloud.service-accounts.list`,
`google.cloud.service-accounts.get`, `google.cloud.service-account-keys.list`,
`google.cloud.service-account-keys.get`). They accept only the
scope `cloud-platform`, so a connection with this profile asks for a token that may change whatever its account
may change, although no tool of this profile changes anything. It is not recommended: give it only to a
connection that needs IAM, preferably with a credential whose account has viewer roles only.

The profile `read-storage-access` adds the IAM policy and the access control list of a Cloud Storage bucket
(`google.cloud.buckets.get-iam-policy`, `google.cloud.bucket-access-controls.list`) to the tools of `read`. Google
lists only `devstorage.full_control` and `cloud-platform` for these two methods, so the token may change what its
account may in Cloud Storage, although no tool does (see [Buckets](buckets.md)). It is not recommended.

The profile `read-quotas` adds the quota tools (`google.cloud.quota-infos.list`, `google.cloud.quota-infos.get`,
`google.cloud.quota-preferences.list`, `google.cloud.quota-preferences.get`) to the tools of `read`. They accept
only the scope `cloud-platform`, and preferences may hold personal data (see [Quotas](quotas.md)). It is not
recommended.

The profile `service-accounts` holds only the changing tools `google.cloud.service-accounts.create`,
`google.cloud.service-accounts.patch`, `google.cloud.service-accounts.disable`, and
`google.cloud.service-accounts.enable`. It asks for `cloud-platform`, needs the effects `create` and `update`,
and is not recommended.

The profile `service-accounts-revoke` holds only `google.cloud.service-account-keys.disable`,
`google.cloud.service-account-keys.enable`, and `google.cloud.service-account-keys.delete`. It asks for
`cloud-platform`, needs the effects `update` and `delete`, and is not recommended. It does not hold
`google.cloud.service-accounts.delete`: that tool is listed-only and only a `tools` list that names it exactly
releases it.

The profile `api-keys-revoke` holds only `google.cloud.api-keys.delete` and `google.cloud.api-keys.undelete`. It
asks for the scope `apikeys`, needs the effects `update` and `delete`, and is not recommended. Google uses the
same scope to create keys and read their values, so the token may do that although no tool does.

The profile `compute-network-delete` holds `google.cloud.firewalls.delete`, `google.cloud.addresses.delete`,
`google.cloud.vpn-tunnels.delete`, `google.cloud.vpn-gateways.delete`, and `google.cloud.projects.get`, which proves
a project against targets of an organization or a folder. The four deletions ask for the scope `compute`, which
Google also uses to change Compute resources, so the token may do that although no tool does. The profile needs the
effect `delete` for the deletions and `read` for `projects.get`, is not recommended, and holds no listed-only tool;
every call needs the confirmation of a person. See [Firewall rules](firewalls.md), [IP addresses](addresses.md),
[HA VPN gateways](vpn-gateways.md), and [VPN tunnels](vpn-tunnels.md).

The profile `compute-lifecycle` holds `google.cloud.instances.start`, `google.cloud.instances.stop`,
`google.cloud.instances.reset`, `google.cloud.instances.set-machine-type`, and `google.cloud.projects.get`. The four
tools ask for the scope `compute`, which Google also uses to change Compute resources, so the token may do that
although no tool does. The profile needs the effect `update` for the instance tools and `read` for `projects.get`, is
not recommended, and every call needs the confirmation of a person. See [Compute instances](instances.md#lifecycle).

The profile `compute-instances-create` holds `google.cloud.instances.insert` and `google.cloud.projects.get`, which
proves a project against targets of an organization or a folder and names the default service account by its
project number. `instances.insert` asks for the scope `compute`, which Google also uses to change Compute resources,
so the token may do that although no tool does. The profile needs the effect `create` for the tool and `read` for
`projects.get` and is not recommended. It does not hold `google.cloud.instances.delete`: that tool is listed-only and
only a `tools` list that names it exactly releases it. An instance is created without an external address, without
`ssh-keys`, `sshKeys`, or `windows-keys` metadata, and without a service account of another project, and costs money
while it exists. See [Compute instances](instances.md#creating-an-instance).

The profile `services` holds only `google.cloud.services.enable` and `google.cloud.services.batch-enable`. It asks
for `cloud-platform`, needs the effect `update`, and is not recommended. It does not hold
`google.cloud.services.disable`: that tool is listed-only and only a `tools` list that names it exactly releases
it.

The profile `iam-bindings` holds the tools of `read`, `google.cloud.iam-policies.get`, and the three binding
tools `google.cloud.iam-bindings.add-member`, `google.cloud.iam-bindings.remove-member`, and
`google.cloud.iam-bindings.change-role`. They have the effect `update`, so the connection needs that effect, and
every call needs the confirmation of a person. It is not recommended; the rule which members may be granted is
on [IAM bindings](iam-bindings.md).

## Tools

- [Services](services.md): the enabled and available APIs of a project from Service Usage, and enabling and
  disabling them.
- [IAM](iam.md): IAM policies, permission tests, and deny policies of projects, folders, organizations, and
  service accounts.
- [API keys](api-keys.md): the metadata of the API keys of a project, and deleting and restoring a key.
- [IAM bindings](iam-bindings.md): adding a member to, removing a member from, and moving a member between the
  bindings of the IAM policy of a project, folder, or organization.
- [Compute instances](instances.md): the virtual machine instances of a project from Compute Engine, one, per
  zone, and across all zones, without metadata values, and creating an instance and deleting one (listed-only).
- [Machine types](machine-types.md): the machine types of a zone from Compute Engine.
- [Zones](zones.md): the zones of a project from Compute Engine.
- [Regions](regions.md): the regions of a project from Compute Engine, with their quotas.
- [Compute operations](compute-operations.md): the state of Compute Engine operations in a zone, a region, or
  globally, with the email address of whoever started each.
- [VPC networks](networks.md): the VPC networks of a project from Compute Engine, one and all.
- [Subnetworks](subnetworks.md): the subnetworks of a region from Compute Engine, one and all.
- [Firewall rules](firewalls.md): the VPC firewall rules of a project from Compute Engine, one and all, and the
  confirmed deletion of a rule.
- [IP addresses](addresses.md): the reserved internal and external IP addresses of a region, one and all, and the
  confirmed deletion of an address.
- [HA VPN gateways](vpn-gateways.md): the HA VPN gateways of a region from Compute Engine, one and all, and the
  confirmed deletion of a gateway.
- [VPN tunnels](vpn-tunnels.md): the VPN tunnels of a region from Compute Engine, never with their shared secret, and the
  confirmed deletion of a tunnel.
- [Disks](disks.md): the persistent disks of a project from Compute Engine, one, per zone, and across all zones,
  never with encryption key material.
- [Disk snapshots](snapshots.md): the global disk snapshots of a project from Compute Engine, never with customer-supplied
  key material.
- [Images](images.md): the images of a project from Compute Engine, never with customer-supplied key material.
- [Buckets](buckets.md): the Cloud Storage buckets of a project, one and all, and the IAM policy of a bucket, bound to
  the targets by the project number of the bucket.
- [Bucket access controls](bucket-access-controls.md): the access control list of a Cloud Storage bucket.
- [Notifications](notifications.md): the Pub/Sub notification configurations of a Cloud Storage bucket.
- [Managed folders](managed-folders.md): the managed folders of a Cloud Storage bucket.
- [HMAC keys](hmac-keys.md): the metadata of the HMAC keys of a project, never a key value.
- [Quotas](quotas.md): the quotas of a service and the quota preferences of a project from Cloud Quotas.

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
`operations.get` takes the `service` (`resource-manager`, `service-usage` for the operation of a change of
services, or `api-keys` for the operation of `api-keys.delete` or `api-keys.undelete`) and the operation `name`.

### Service accounts

[Service accounts](service-accounts.md) covers `service-accounts.list`, `service-accounts.get`, and the changes
`service-accounts.create`, `service-accounts.patch`, `service-accounts.disable`, and `service-accounts.enable`, and
the listed-only `service-accounts.delete`;
[service account keys](service-account-keys.md) covers `service-account-keys.list` and `service-account-keys.get`,
which return metadata only and never key material, and the revocations `service-account-keys.disable`,
`service-account-keys.enable`, and `service-account-keys.delete`.

### IAM

[IAM roles and permissions](iam-roles.md) covers `iam-roles.list`, `iam-roles.get`,
`iam-roles.query-grantable`, and `iam-permissions.query-testable`.

## Long-running operations

Google answers a change with an `Operation`: `name`, `done`, `metadata`, and once done either an `error` or a
`response`. A mutation returns it as it is and never waits. `operations.get` reads the
state: `done=false` with its metadata while it runs, `done=true` with the response when it succeeded. A member
named `keyString` is removed from the metadata and the response of every operation, whatever the service. A done
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
| 409 with status `ABORTED` (a stale `etag` of `setIamPolicy`) | `provider-error`, the message starts with `conflict` |
| 400 with status `FAILED_PRECONDITION` (a stale `etag`, a restore after the retention period) | `provider-error` |
| other 400 | `invalid-provider-response` (Qatlas checked the arguments before sending) |
| 429 | `rate-limited` |
| any other 401, 403, 404, or 4xx | `invalid-provider-response` |
| 5xx | by HTTP status |

Compute Engine may answer in the older error form, without a status string and with a list of `errors` that carry a
`reason`. Qatlas reads the reasons `notFound`, `forbidden`, and `authError` of it like `NOT_FOUND`,
`PERMISSION_DENIED`, and `UNAUTHENTICATED`; the 403 reason `SERVICE_DISABLED` names Compute Engine among the APIs.

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
- Every change of a resource (create, update, move, delete, undelete, and the policies of the hierarchy apart
  from a single IAM binding), and the reads of tag bindings of this service.
- A free `query` string, a `$fields` parameter, and any base URL other than the official origin.

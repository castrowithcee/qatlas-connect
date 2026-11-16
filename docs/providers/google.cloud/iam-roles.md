---
description: >
  Describes the Google Cloud IAM role and permission reads: iam-roles.list, iam-roles.get, iam-roles.query-grantable, and iam-permissions.query-testable, with the target binding of projects and organizations, the structured resource, the POST reads, paging and its bounds, and the resource types left out.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# IAM roles and permissions

Four read tools of the IAM API (`https://iam.googleapis.com`, discovery `iam` v1, revision 20261106): the roles
of IAM and of a project or organization, one role, the roles grantable on a resource, and the permissions
testable on a resource. They create and change no role. This page covers the group `iam-permissions` as well,
which holds one tool today. [The provider page](README.md) holds targets, errors, and rate spacing.

## Target binding

- **Predefined roles** (`parent_type` `predefined`, or a `roles/NAME` role) belong to no target. A connection
  with or without targets reads them, with one exception: a connection bound to billing accounts alone
  reaches no resource of the hierarchy, so it is not offered any IAM tool. The narrower reading is
  deliberate: a binding that names only billing accounts hands over nothing of IAM.
- **Project roles and project resources** are bound to the targets before any IAM request. If the targets
  decide by the name alone, the call ends before a secret is read. Otherwise Qatlas reads the project and the
  folders above it, as `projects.get` does, and refuses a project outside the targets without naming it.
  Google documents the IAM paths and full resource names with the project ID, so Qatlas uses the ID the
  project read reports, also when the argument was a project number.
- **Folders** (a resource of the two query tools only) need an organization or folder target or no targets,
  and are proved the same way.
- **Organization roles and the organization resource** run only if a target names exactly that organization.
  A connection without targets, with another organization, or with folders or projects alone is refused
  before a secret is read, because organization roles and the permissions of an organization reach beyond
  what such a binding hands over. An organization needs no proof read.

## Resource and role names

Nothing is taken as free text. The two query tools take `resource_type` (`project`, `folder`, `organization`)
and `resource_id`, and Qatlas builds `fullResourceName` as
`//cloudresourcemanager.googleapis.com/projects/PROJECT_ID`, `.../folders/ID`, or `.../organizations/ID`.
`iam-roles.get` takes a full role name that must match one of the three documented forms (`roles/NAME`,
`projects/PROJECT/roles/ID`, `organizations/ID/roles/ID`) exactly, with no wildcard, and `iam-roles.list` takes
`parent_type` and `parent_id`. The answer must name what was asked: a role other than the requested one, or a
role row of another project or of a project on a folder query, is an invalid response.

**Resource types Google documents that this group cannot offer.** The page on full resource names lists the
resources of many services, among them BigQuery datasets, Cloud Billing accounts, Cloud Run services, Spanner
instances, Cloud SQL instances, Cloud Storage buckets, Compute Engine instances, networks and subnetworks,
GKE clusters, IAM service accounts and their keys, Identity-Aware Proxy services, and Pub/Sub topics. The
targets of a connection are organizations, folders, projects, and billing accounts, and Qatlas can bind
only organizations, folders, and projects to them. The resources of other services have no type here, and a
billing account has no type either, because a binding to billing accounts alone reaches no IAM tool (see
above). Google's pages for the two methods list no supported resource types, so the list of unsupported ones
follows from the full resource names page, not from the method.

## POST reads

`iam-roles.query-grantable` and `iam-permissions.query-testable` are POST methods that only read. They are
safe reads (`effect` read, `idempotency` safe, no confirmation). The body holds only `fullResourceName`,
`pageSize`, and where given `view` and `pageToken`, all built from checked arguments. A 401 renews the token
once and sends the same body again.

## Roles in a query answer

A grantable query on a project or folder also returns the custom roles of its organization. If a target names
that organization, the role is returned in full. Otherwise only its name goes out, with no title, description,
or permissions, because it lies outside the binding. A project role of another project is an invalid response.

## Paging and bounds

`page_size` is limited by Qatlas to 500, and to 10 with `view` `FULL`, because a role can list thousands of
permissions and an answer larger than 2 MiB is an invalid response. Without `page_size` Qatlas sends 100 (10 for
FULL), so no answer is unbounded. Google allows larger pages (1000 and 3000 for roles, 1000 for permissions).
`includedPermissions` is cut at 1000 per role, and `includedPermissionsTotal` then holds the number Google sent.
`iam-roles.get` always returns the permissions. Google leaves `stage` out for ALPHA roles, and `deleted` and
`apiDisabled` appear only when true.

The `cursor` wraps Google's `nextPageToken`. It is bound to the tool, the targets, and the arguments that
decide the sequence (parent or resource, `view`, `show_deleted`); the page size is not part of it. A cursor that
is altered, cut short, or from another request is refused before a secret is read.

## Boundary

- Scope: the discovery document lists `cloud-platform` alone for all four methods, so that is the scope
  requested, and the tools belong to the profile `read-iam`, not to the recommended `read`. Google's reference page also names `https://www.googleapis.com/auth/iam`, which the discovery
  document does not list.
- The deprecated `onlyInPredefinedRoles` field of a permission is left out. Role titles and descriptions are
  untrusted text and are cut.
- Rights per method are not stated by the discovery document for the two queries; see the setup page.

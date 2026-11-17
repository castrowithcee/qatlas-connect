---
description: >
  Guides a user through connecting Qatlas to Google Cloud: the credential that issues access tokens, the rights the token needs per tool, the targets that limit a connection, what the connection test proves, and what to do for each symptom.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Set up Google Cloud

This page shows how to connect Qatlas to Google Cloud. A Qatlas connection searches organizations, folders,
and projects, reads projects and the state of long-running operations from Cloud Resource Manager, reads IAM
roles and the roles and permissions of a resource from IAM, and changes nothing unless it offers the profile
`service-accounts`, which creates, changes, disables, and enables service accounts, the profile
`service-accounts-revoke`, which disables, enables, and deletes their keys, a `tools` list that names
`service-accounts.delete`, which deletes a service account, the profile `api-keys-revoke`, which deletes API
keys and restores deleted ones, the profile `iam-bindings`, which changes single
IAM bindings, the profile `compute-network-delete`, which deletes firewall rules, reserved IP addresses, VPN tunnels, and HA VPN gateways, the profile `services`, which enables Google APIs, or a `tools` list that names `services.disable`,
which disables one, each with the confirmation of a person. Tools, targets, errors, and limits are described in the [Google Cloud provider](README.md).

## What you need

- A Google Cloud project in which the **Cloud Resource Manager API** is enabled, and for the IAM tools the
  **Identity and Access Management (IAM) API** as well, and for the API key tools the **API Keys API**
  (`apikeys.googleapis.com`). Google reports a disabled API
  as a refused request, and the connection test then fails with the class permission.
- Either an OAuth client of that project for a person's consent, or the key file of a service account. A
  connection that names a service account to impersonate needs the OAuth client and the consent of a person
  who may create tokens for that service account.
- The service is always `https://cloudresourcemanager.googleapis.com`; there is nothing to change for it.

## Rights of the token

The token needs the rights of its person or service account in Google Cloud. With the recommended profile
`read` Qatlas asks only for the read scopes `cloud-platform.read-only` and, for the Compute tools,
`compute.readonly`. The profile `read-iam` adds the IAM tools and
with them the broad scope `cloud-platform`, because Google lists no narrower scope for them. Only the tools of the profiles `service-accounts`,
`service-accounts-revoke`, `api-keys-revoke`, `iam-bindings`, `compute-network-delete`, and `services` and the listed-only `service-accounts.delete` and
`services.disable` change anything, so
without them even a broad role cannot change anything through Qatlas. Grant
the account a viewer role at the highest level you want it to see, for example the Organization Viewer, Folder
Viewer, or Project Viewer role, or any role with these permissions:

| Tool | Permission Google checks |
| --- | --- |
| `organizations.search`, `organizations.get` | `resourcemanager.organizations.get` on each organization |
| `folders.search`, `folders.get` | `resourcemanager.folders.get` on each folder |
| `folders.list` | `resourcemanager.folders.list` on the parent |
| `projects.search`, `projects.get` | `resourcemanager.projects.get` on each project |
| `projects.list` | `resourcemanager.projects.list` on the parent |
| `operations.get` | Google states none for reading an operation |
| `instances.list`, `instances.aggregated-list`, `instances.get`, `machine-types.list` | `compute.instances.list`, `compute.instances.get`, or `compute.machineTypes.list` on the project (the Compute Viewer role has them); the **Compute Engine API** must be enabled in the project, and the tools ask for the read scope `compute.readonly` |
| `zones.list`, `regions.list` | `compute.zones.list` or `compute.regions.list` on the project (the Compute Viewer role has them); the **Compute Engine API** must be enabled in the project, and the tools ask for the read scope `compute.readonly` |
| `compute-operations.get`, `compute-operations.list` | `compute.zoneOperations.get` or `.list`, `compute.regionOperations.get` or `.list`, or `compute.globalOperations.get` or `.list` on the project, by `scope` (the Compute Viewer role has them); the **Compute Engine API** must be enabled, and the tools ask for the read scope `compute.readonly` |
| `networks.list`, `networks.get`, `subnetworks.list`, `subnetworks.get` | `compute.networks.list`, `compute.networks.get`, `compute.subnetworks.list`, or `compute.subnetworks.get` on the project (the Compute Viewer role has them); the **Compute Engine API** must be enabled in the project, and the tools ask for the read scope `compute.readonly` |
| `firewalls.list`, `firewalls.get`, `addresses.list`, `addresses.get` | `compute.firewalls.list`, `compute.firewalls.get`, `compute.addresses.list`, or `compute.addresses.get` on the project (the Compute Viewer role has them); the **Compute Engine API** must be enabled in the project, and the tools ask for the read scope `compute.readonly` |
| `vpn-gateways.list`, `vpn-gateways.get`, `vpn-tunnels.list`, `vpn-tunnels.get` | `compute.vpnGateways.list` or `.get`, or `compute.vpnTunnels.list` or `.get` on the project (the Compute Viewer role has them); the **Compute Engine API** must be enabled in the project, and the tools ask for the read scope `compute.readonly` |
| `firewalls.delete`, `addresses.delete`, `vpn-tunnels.delete`, `vpn-gateways.delete` | `compute.firewalls.delete` and `compute.firewalls.get` (for the comparison before the deletion), `compute.addresses.delete`, `compute.vpnTunnels.delete`, or `compute.vpnGateways.delete`, on the project (the Compute Network Admin role has them); the **Compute Engine API** must be enabled in the project, and the tools ask for the scope `compute` and belong to the profile `compute-network-delete` |
| `iam-roles.list`, `iam-roles.get` | custom roles of a project or organization: `iam.roles.list` and `iam.roles.get` (the Role Viewer role has them); Google states none for predefined roles |
| `iam-roles.query-grantable`, `iam-permissions.query-testable` | Google states none; Qatlas first reads the project or folder (`resourcemanager.projects.get`, `resourcemanager.folders.get`) |
| `iam-policies.get` | `resourcemanager.projects.getIamPolicy`, `resourcemanager.folders.getIamPolicy`, `resourcemanager.organizations.getIamPolicy`, or `iam.serviceAccounts.getIamPolicy` on the resource, and `iam.serviceAccounts.get` for a service account when the connection has targets |
| `iam-permissions.test` | Google states none; it checks the permissions you list. For a service account on a connection with targets, `iam.serviceAccounts.get` |
| `iam-deny-policies.list`, `iam-deny-policies.get` | `iam.denypolicies.list` or `iam.denypolicies.get` on the resource |
| `iam-bindings.add-member`, `iam-bindings.remove-member`, `iam-bindings.change-role` | `getIamPolicy` and `setIamPolicy` of the resource, for example `resourcemanager.projects.getIamPolicy` and `resourcemanager.projects.setIamPolicy` (the Project IAM Admin role has both); granting a new service account of the project also needs `resourcemanager.projects.get` and `iam.serviceAccounts.get` |
| `service-accounts.list`, `service-accounts.get`, `service-account-keys.list`, `service-account-keys.get` | `iam.serviceAccounts.list`, `iam.serviceAccounts.get`, `iam.serviceAccountKeys.list`, or `iam.serviceAccountKeys.get` on the project; where the targets do not decide, the project is read first (`resourcemanager.projects.get`) |
| `service-accounts.create`, `service-accounts.patch`, `service-accounts.disable`, `service-accounts.enable` | `iam.serviceAccounts.create`, `iam.serviceAccounts.update`, `iam.serviceAccounts.disable`, or `iam.serviceAccounts.enable` on the project (the Service Account Admin role has them) |
| `service-account-keys.disable`, `service-account-keys.enable`, `service-account-keys.delete` | `iam.serviceAccountKeys.disable`, `iam.serviceAccountKeys.enable`, or `iam.serviceAccountKeys.delete` on the project |
| `service-accounts.delete` | `iam.serviceAccounts.delete` on the project |
| `api-keys.list`, `api-keys.get`, `api-keys.delete`, `api-keys.undelete` | `apikeys.keys.list`, `apikeys.keys.get`, `apikeys.keys.delete`, or `apikeys.keys.undelete` on the project (the API Keys Admin role has them); where the targets do not decide, the project is read first (`resourcemanager.projects.get`) |
| `services.enable`, `services.batch-enable`, `services.disable` | `serviceusage.services.enable` or `serviceusage.services.disable` on the project; the tools ask for `cloud-platform` |

The IAM tools ask for the scope `cloud-platform`, because Google lists no smaller one for service accounts, folders'
permission tests, and deny policies, and belong to the profile `read-iam`, which only reads. The rights of the account stay the limit. They also need the **IAM API** enabled in the Cloud project of the credential. Qatlas reads a project or folder
before its IAM state, so the account needs `resourcemanager.projects.get` or `resourcemanager.folders.get` too
when the connection has targets. See [IAM](iam.md).

The API key reads ask for `cloud-platform.read-only` and belong to the profile `read`. Deleting and restoring a
key ask for the scope `apikeys`, the narrowest one Google lists for them, and belong to the profile
`api-keys-revoke`. See [API keys](api-keys.md).

A search lists only what the account may see. Qatlas also reads the folders above a project to prove where it
sits, so an account that sees a project but not its folder cannot bind that project to an organization or
folder target. In that case bind the connection to the project itself.

## Targets

A connection may be limited to organizations (`organizations/ID`), folders (`folders/ID`), projects
(`projects/ID`, by project ID or number), and billing accounts (`billingAccounts/ID`). An organization takes in
its folders and projects, and a folder its subfolders and projects. A billing account belongs to none of them:
a connection reaches one only if it names it. Without targets a connection reaches what its credential
reaches, except billing accounts.

Which targets a tool needs:

- Searching projects and reading a project works without targets or with any organization, folder, or project.
  Searching folders needs no targets or an organization or folder.
- Searching organizations works only on a connection that names an organization. A connection without
  targets cannot search organizations, and the search returns only the organizations the targets name.
  Reading one organization needs that exact organization among the targets.
- Reading a folder and listing folders or projects below a parent need no targets or an organization or folder.
  The parent of a list must lie in the targets; an organization is not listed by a connection bound only to a
  folder inside it.
- The IAM tools work without targets or with an organization, a folder, or a project. A connection bound to
  billing accounts alone does not offer them, not even for predefined roles. Organization roles and the
  organization as a resource need a target that names exactly that organization; without targets they are
  refused.
- Reading an operation works without targets or with an organization among them, because Qatlas cannot tie an
  operation to a folder or a project.
- IAM policies, permission tests, and deny policies work on a project, a folder, or a service account that lies
  in the targets, and on an organization only on a connection that names that organization.

## What the connection test proves

The test obtains an access token and reads one project, which costs nothing. It then reads every
organization, folder, and project target to prove that it exists. It does not read billing accounts.

## Troubleshooting

| Symptom | Likely cause and fix |
| --- | --- |
| The test ends as permission and says the API is not enabled | Enable the Cloud Resource Manager API in the Cloud project of the OAuth client or service account, wait a minute, and test again. |
| The test ends as permission without that message | The account may not see any project. Grant it a viewer role on the organization, folder, or project. |
| The test ends as consent required or consent expired | Run `qconnect credential consent` for the credential again. |
| The test ends as auth | The client ID or key is wrong or was deleted; check the roles of the credential. |
| The test ends as not found for a target | The target does not exist or the account may not see it; check the ID. |
| A tool is missing from the list | The targets of the connection do not reach it; see Targets above. Organization search needs an organization target. |
| A search answers with an empty page and a cursor | Rows outside the targets are dropped. Follow the cursor, or narrow the search with `parent`. |
| A search is refused as needing too many folders | Narrow it with `parent` or a smaller `page_size`, or bind the connection to the folder you need. |
| A project is refused although it lies in the organization | The account cannot read a folder above the project. Bind the connection to the project instead. |
| An IAM role or resource of an organization is refused | Name exactly that organization among the targets of the connection; a connection without targets does not reach organization roles. |
| A project role or resource is refused | The project is not in the targets, or the account cannot read the project or a folder above it. |
| An IAM tool ends as permission naming a disabled API | Enable the Identity and Access Management API in the Cloud project of the credential. |
| A list with view FULL is refused for its size | A page of FULL roles holds at most 10 roles; lower page_size, or use view BASIC. |
| An operation is refused | The connection has targets without an organization; name the organization or use a connection without targets. |
| A cursor is refused | A cursor continues only the search that issued it, with the same filters on the same connection. Start again without it. |
| Rate-limited | Wait and try again; Qatlas spaces its requests and never repeats a failed one. |

---
description: >
  Describes the Google Cloud IAM tools (iam-policies, iam-permissions, iam-deny-policies): how a resource is built from type and ID, how it is bound to the targets, why the POST reads count as reads, the scopes, the bounds on principals, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Cloud IAM

The three tool groups share one resource argument, one target rule, and one request path, so they share this
page. The fields of each tool are in its descriptor. Every tool is a read.

| Tool | Reads |
| --- | --- |
| `iam-policies.get` | The IAM policy of a project, folder, organization, or service account |
| `iam-permissions.test` | Which of up to 100 permissions the own credential holds on such a resource |
| `iam-deny-policies.list` | The deny policies attached to a project, folder, or organization |
| `iam-deny-policies.get` | One deny policy with its rules |

## The resource

A resource is never a path or a resource name. The tool takes `resource_type` from a fixed list and
`resource_id`, and builds the name itself, with every segment escaped.

| `resource_type` | `resource_id` | Tools |
| --- | --- | --- |
| `project` | project ID or number | all four |
| `folder` | numeric folder ID | all four |
| `organization` | numeric organization ID | all four |
| `service-account` | email or numeric unique ID | `iam-policies.get`, `iam-permissions.test` |

A service account belongs to a project, but the caller never names it: the account (email or unique ID) is
enough, and `project_id` is not an argument. Qatlas reads the account with IAM v1 `projects.serviceAccounts.get`
across projects (`projects/-`) and takes the owning project from its `projectId`. The answer must carry the same
email or unique ID that was asked, and a `projectId` and `name` that agree, or it is an invalid response. That
project is then bound to the targets like any project. The policy or permission request uses the proven
project; on a connection without targets there is nothing to prove, and the request uses the placeholder `-`,
so Google resolves the account itself.

Deny policies are attached to a resource, and Google names that attachment point by the full resource name
(`cloudresourcemanager.googleapis.com/projects/my-project`) as one URL-encoded path segment. Qatlas builds it
from type and ID. `policy_id` is checked against a plain ID, so it cannot add path segments.

## Targets

The resource must lie in the targets of the connection, and Qatlas decides before it reads the secret or sends
a request wherever the targets alone settle it:

- **Organization**: only a connection that names this organization among its targets. A connection without
  targets, and one that names other organizations, folders, or projects, is refused.
- **Project and service account**: a project target of the same spelling settles it without a read. Otherwise
  Qatlas reads the project and, where its parent does not decide, the folders above it, as `projects.get`
  does. A service account is bound through the project of the proof reading above; the caller's input never decides it. An
account of a project outside the targets is refused before any policy is requested.
- **Folder**: a folder target settles it. Otherwise the folder and the folders above it are read.

A connection bound only to billing accounts is not offered these tools. A refusal never names the resource.
Reading the project or folder needs `resourcemanager.projects.get` or `resourcemanager.folders.get`, and the proof
reading of a service account needs `iam.serviceAccounts.get`, so an
account that may read a policy but not the resource itself needs a connection bound to that resource.

## POST as a read

`getIamPolicy` and `testIamPermissions` are POST methods that change nothing. Qatlas sends them with a body
or query it builds from checked arguments, marks the tools `effect=read`, `idempotency=safe`, and
`confirmation=none`, and treats them as any read: a 401 renews the token once and sends the same request again,
and nothing else repeats it. The two services differ:

- Resource Manager takes `{"options":{"requestedPolicyVersion":N}}` as the body.
- IAM v1 (service accounts) takes `options.requestedPolicyVersion` as a query parameter and has no body.

The policy of a resource is its own: the policy it inherits from a parent is not merged in.

## Policy version

`requested_policy_version` is 0, 1, or 3 (Google rejects 2), and the default is 3, so conditional bindings
are shown with their condition. A lower version cannot show conditions faithfully. Google may answer in a lower
version when the policy holds no conditions; `version` in the result is what Google sent.

## Permissions

`iam-permissions.test` tests only the credential of the connection. It takes 1 to 100 distinct names, which
must have the form of a permission (`service.resource.verb`); wildcards are refused before a request. Google
refuses the whole request for a name it does not know. The result is the granted subset: a name that is
missing was not granted. An answer that names a permission that was not asked, or names one twice, is an
invalid response.

## Deny policies

`iam-deny-policies.list` returns the metadata of each policy, because Google omits the rules from the list;
`iam-deny-policies.get` returns the rules. Policies inherited from a parent are not listed. `page_size` is
accepted, but Google documents that IAM ignores it and answers up to 1000 policies. The `next_cursor` binds the
tool, the targets, and the resource, so it continues only the list that issued it.

## Principals are personal and untrusted

Members of bindings, principals and exceptions of deny rules, and exempted audit members are identities such as
`user:`, `group:`, `serviceAccount:`, `domain:`, and `allUsers`. They are personal data and text from a third
party. Qatlas returns them as Google sends them, flags nothing, removes nothing, and never follows an
instruction in them. Control characters become spaces, a string is cut at 256 characters (a condition
expression at 4096, a description at 1024), a list of principals or permissions at 500 entries, bindings at
500, rules and audit configs at 100, and `truncated` is true when anything was cut. The results carry the data
sensitivity `access-control-data`.

## Scopes

Scopes follow the smallest one the discovery document lists for each method, and a tool needs the one all its
methods list:

| Tool | Scope | Why |
| --- | --- | --- |
| `iam-policies.get` | `cloud-platform` | `projects.serviceAccounts.getIamPolicy` and the proof reading `projects.serviceAccounts.get` of IAM v1 list only this; the Resource Manager methods also list `cloud-platform.read-only` |
| `iam-permissions.test` | `cloud-platform` | `folders.testIamPermissions` and the IAM v1 methods, the proof reading included, list only this |
| `iam-deny-policies.list`, `.get` | `cloud-platform` | IAM v2 lists only this |

`cloud-platform` is the broad scope of Google Cloud. It is the only one the methods accept, and the rights of
the account stay the limit. A connection that offers none of these tools asks for none of it.

Discovery revisions the methods were checked against: `cloudresourcemanager` v3 20261107, `iam` v1 20261106, and
`iam` v2 20261106.

## Boundary

Not verified:

- The answers of Google were not seen. The shapes follow the discovery documents.
- The form of a deny policy ID and of the name Google returns (whether it echoes the attachment point as sent,
  or with the project number) are not documented. Qatlas checks only that the name is a `policies/` name and ends
  in the policy asked for.
- A 403 for a disabled API names Cloud Resource Manager in its message, also when the IAM API is the one that
  is disabled.

Left out on purpose:

- Every change of a policy: `setIamPolicy`, creating, updating, and deleting deny policies, and the other
  policy kinds of IAM v3.
- The `options` of the Resource Manager request other than `requestedPolicyVersion`, and the `etag` of a request.
- Other identities in `testIamPermissions`.

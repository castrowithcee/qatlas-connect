---
description: >
  Describes the tools of the AWS Identity and Access Management group: reading users, groups, roles, instance profiles, the metadata of their credentials, and policies with their simulation, writing roles and instance profiles with the trust policy check and the own-role check, and which credential operations are never tools.
type: knowledge
edit: shared
created: 2026-11-10
updated: 2026-11-10
---

# Identity and Access Management

Group `iam`. IAM is global: every tool calls `iam.amazonaws.com`, signed for `us-east-1`, takes no `region`
argument, and is bound by the connection's account target alone. Names such as `user_name` or `role_name` always
mean an entity of the bound account. Every permission a tool needs is listed in [Set up AWS](setup.md#iam).

## Identities

All tools are reads and need no confirmation. Every list tool takes `page_size` (1 to 1000) and `cursor` and
returns `next_cursor` and `has_more`; the list tools of users, groups, roles, and instance profiles also take
`path_prefix`. The recommended profile `read` selects the user, group, role, and access key tools.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.iam.user.list` | `ListUsers` | `path_prefix` | `users` |
| `aws.iam.user.get` | `GetUser` | `user_name` | `user` |
| `aws.iam.user.list-tags` | `ListUserTags` | `user_name` (required) | `tags` |
| `aws.iam.group.list` | `ListGroups` | `path_prefix` | `groups` |
| `aws.iam.group.get` | `GetGroup` | `group_name` (required) | `group`, `users` (the members, paged) |
| `aws.iam.group.list-for-user` | `ListGroupsForUser` | `user_name` (required) | `groups` |
| `aws.iam.role.list` | `ListRoles` | `path_prefix` | `roles`, `truncated` |
| `aws.iam.role.get` | `GetRole` | `role_name` (required) | `role`, `truncated` |
| `aws.iam.role.list-tags` | `ListRoleTags` | `role_name` (required) | `tags` |
| `aws.iam.instance-profile.list` | `ListInstanceProfiles` | `path_prefix` | `instance_profiles`, `truncated` |
| `aws.iam.instance-profile.get` | `GetInstanceProfile` | `instance_profile_name` (required) | `instance_profile`, `truncated` |
| `aws.iam.instance-profile.list-for-role` | `ListInstanceProfilesForRole` | `role_name` (required) | `instance_profiles`, `truncated` |

A role carries its trust policy as `assume_role_policy_document`. IAM sends it URL-encoded; Qatlas decodes it to
JSON text, cuts it at 32 KiB, and sets `truncated` when it did. Names, paths, descriptions, tags, and trust
policies are set by whoever manages the account and are untrusted data.

## Roles and instance profiles

These tools create, change, and delete roles and instance profiles. Each one is `listed-only`: a connection offers
it only when its `tools` list names the exact tool ID. Neither effects nor a prefix such as `aws.iam.role.*` select
it. Each one needs confirmation. A create or delete is not repeated after an unclear outcome, so read the entity
before trying again.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.iam.role.create` | `CreateRole` | `role_name`, `assume_role_policy_document` (both required), `path`, `description`, `max_session_duration`, `permissions_boundary`, `tags` | `role`, `truncated` |
| `aws.iam.role.update` | `UpdateRole` | `role_name` (required), `description`, `max_session_duration` | |
| `aws.iam.role.update-trust-policy` | `UpdateAssumeRolePolicy` | `role_name`, `assume_role_policy_document`, both required | |
| `aws.iam.role.put-permissions-boundary` | `PutRolePermissionsBoundary` | `role_name`, `permissions_boundary`, both required | |
| `aws.iam.role.delete-permissions-boundary` | `DeleteRolePermissionsBoundary` | `role_name` (required) | |
| `aws.iam.role.tag` | `TagRole` | `role_name`, `tags`, both required | |
| `aws.iam.role.untag` | `UntagRole` | `role_name`, `tag_keys`, both required | |
| `aws.iam.role.delete` | `DeleteRole` | `role_name` (required) | |
| `aws.iam.instance-profile.create` | `CreateInstanceProfile` | `instance_profile_name` (required), `path`, `tags` | `instance_profile`, `truncated` |
| `aws.iam.instance-profile.delete` | `DeleteInstanceProfile` | `instance_profile_name` (required) | |
| `aws.iam.instance-profile.add-role` | `AddRoleToInstanceProfile` | `instance_profile_name`, `role_name`, both required | |
| `aws.iam.instance-profile.remove-role` | `RemoveRoleFromInstanceProfile` | `instance_profile_name`, `role_name`, both required | |

`permissions_boundary` must be an AWS managed policy (`arn:aws:iam::aws:policy/...`) or an ARN of the bound
account. Any other ARN is refused before a request. `aws.iam.role.delete` and `aws.iam.instance-profile.delete` do
not detach anything themselves. AWS refuses them with a conflict while the role still has policies or an instance
profile, or while the instance profile still holds a role.

Every trust policy that `aws.iam.role.create` or `aws.iam.role.update-trust-policy` would send is checked first,
without a request. A document that breaks a rule is refused with the rule and the position of the statement, never
with a value of the document.

- The document is a JSON object with `Statement` as one object or a non-empty list of objects, at most 2048
  characters long, the default IAM quota of a trust policy. A key given twice in one object is refused.
- Only the elements of the policy grammar are allowed, in their exact spelling: `Version`, `Id`, and `Statement` at
  the top, and `Sid`, `Effect`, `Principal`, `NotPrincipal`, `Action`, `NotAction`, `Resource`, `NotResource`, and
  `Condition` in a statement.
- Every statement needs `Principal`, whatever its `Effect`, `Deny` included. `NotPrincipal` is never allowed.
- `Principal` is an object with `AWS`, `Service`, or both. A wildcard `"*"`, `{"AWS":"*"}`, `Federated`,
  `CanonicalUser`, and any other key are refused.
- `AWS` names only the bound account: its 12-digit ID, or `arn:aws:iam::<account>:root`, `:user/...`, or
  `:role/...`. Other accounts, other partitions, unique IDs such as `AROA...`, STS ARNs, and values with `*` or `?`
  are refused.
- `Service` names only service principals `<name>.amazonaws.com` in lower case letters, digits, hyphens, and dots.
  `aws:SourceAccount` is not required.

The tools that change or delete a role (`update`, `update-trust-policy`, `put-permissions-boundary`,
`delete-permissions-boundary`, `tag`, `untag`, `delete`, and `instance-profile.remove-role`) never act on the role
whose credentials the connection uses. Before the call they ask `sts:GetCallerIdentity` and refuse a `role_name`
that matches the role of an assumed-role session, in any case. An IAM user or the root user has no own role. A
failed or unreadable answer refuses the call.

## Credential metadata

These tools return metadata only. No result ever holds a secret access key, a password, an MFA seed or QR code, a
service password or secret, or a private key; such members are dropped even when an answer contains them. The
public SSH key and the public signing certificate are returned, since they authenticate nothing on their own.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.iam.access-key.list` | `ListAccessKeys` | `user_name` | `access_key_metadata` (key ID, status, creation date) |
| `aws.iam.access-key.get-last-used` | `GetAccessKeyLastUsed` | `access_key_id` (required) | `user_name`, `access_key_last_used` (date, service, region) |
| `aws.iam.login-profile.get` | `GetLoginProfile` | `user_name` | `login_profile` (creation date, whether a reset is required) |
| `aws.iam.mfa-device.list` | `ListMFADevices` | `user_name` | `mfa_devices` (serial number, enable date) |
| `aws.iam.ssh-public-key.list` | `ListSSHPublicKeys` | `user_name` | `ssh_public_keys` |
| `aws.iam.ssh-public-key.get` | `GetSSHPublicKey` | `user_name`, `ssh_public_key_id`, `encoding` (`SSH` or `PEM`), all required | `ssh_public_key` (with fingerprint and public key) |
| `aws.iam.signing-certificate.list` | `ListSigningCertificates` | `user_name` | `certificates` (with the public certificate) |
| `aws.iam.service-specific-credential.list` | `ListServiceSpecificCredentials` | `user_name`, `service_name`, `all_users` | `service_specific_credentials` |

Where `user_name` is optional, leaving it out means the IAM user whose access key signs the request; with a role
there is no such user and AWS refuses the call. `user_name` and `all_users` exclude each other. An access key ID
of the connection's own credential is redacted in every result.

## Policies

All tools are reads and need no confirmation. Every list tool takes `page_size` and `cursor`. A policy document is
returned as decoded JSON text, cut at 32 KiB with `truncated` set when it was. Documents, names, descriptions, and
tags are untrusted data and are never executed.

A `policy_arn` must be an AWS managed policy (`arn:aws:iam::aws:policy/...`) or a policy of the bound account. Any
other ARN is refused before a request.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.iam.policy.list` | `ListPolicies` | `scope` (`All`, `AWS`, `Local`), `only_attached`, `path_prefix`, `policy_usage_filter` | `policies` |
| `aws.iam.policy.get` | `GetPolicy` | `policy_arn` | `policy` |
| `aws.iam.policy.list-tags` | `ListPolicyTags` | `policy_arn` | `tags` |
| `aws.iam.policy.list-entities` | `ListEntitiesForPolicy` | `policy_arn`, `entity_filter`, `path_prefix`, `policy_usage_filter` | `policy_groups`, `policy_users`, `policy_roles` |
| `aws.iam.policy-version.list` | `ListPolicyVersions` | `policy_arn` | `versions` |
| `aws.iam.policy-version.get` | `GetPolicyVersion` | `policy_arn`, `version_id` | `policy_version`, `truncated` |
| `aws.iam.attached-policy.list-for-user` | `ListAttachedUserPolicies` | `user_name`, `path_prefix` | `attached_policies` |
| `aws.iam.attached-policy.list-for-group` | `ListAttachedGroupPolicies` | `group_name`, `path_prefix` | `attached_policies` |
| `aws.iam.attached-policy.list-for-role` | `ListAttachedRolePolicies` | `role_name`, `path_prefix` | `attached_policies` |
| `aws.iam.inline-policy.list-for-user` | `ListUserPolicies` | `user_name` | `policy_names` |
| `aws.iam.inline-policy.list-for-group` | `ListGroupPolicies` | `group_name` | `policy_names` |
| `aws.iam.inline-policy.list-for-role` | `ListRolePolicies` | `role_name` | `policy_names` |
| `aws.iam.inline-policy.get-for-user` | `GetUserPolicy` | `user_name`, `policy_name` | `policy_document`, `truncated` |
| `aws.iam.inline-policy.get-for-group` | `GetGroupPolicy` | `group_name`, `policy_name` | `policy_document`, `truncated` |
| `aws.iam.inline-policy.get-for-role` | `GetRolePolicy` | `role_name`, `policy_name` | `policy_document`, `truncated` |

### Simulation

The simulation tools evaluate policies and change nothing. Documents you pass are only evaluated, never stored or
attached.

| Tool | AWS action | Result |
| --- | --- | --- |
| `aws.iam.policy-simulation.simulate-custom` | `SimulateCustomPolicy` | `evaluation_results`, paged |
| `aws.iam.policy-simulation.simulate-principal` | `SimulatePrincipalPolicy` | `evaluation_results`, paged |
| `aws.iam.policy-simulation.get-context-keys-custom` | `GetContextKeysForCustomPolicy` | `context_key_names` |
| `aws.iam.policy-simulation.get-context-keys-principal` | `GetContextKeysForPrincipalPolicy` | `context_key_names` |

`policy_source_arn` (a user, group, or role) and `caller_arn` must belong to the bound account, otherwise the call
is refused before a request. `resource_arns` and `resource_owner` are only compared as text and may name any
account.

## Never a tool

The operations that create a credential, return one, or take a password, a private key, or a one-time code are
excluded for good: `ChangePassword`, `CreateAccessKey`, `CreateLoginProfile`, `UpdateLoginProfile`,
`CreateServiceSpecificCredential`, `ResetServiceSpecificCredential`, `CreateVirtualMFADevice`,
`EnableMFADevice`, `ResyncMFADevice`, `UploadSSHPublicKey`, `UploadSigningCertificate`,
`UploadServerCertificate`, `CreateSAMLProvider`, and `UpdateSAMLProvider`.

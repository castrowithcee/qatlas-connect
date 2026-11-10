---
description: >
  Describes the tools of the AWS Identity and Access Management group: reading users, groups, roles, instance profiles, the metadata of their credentials, policies with their simulation, the account state, identity providers, server certificates, and further lookups, writing roles and instance profiles with the trust policy check and the own-role check, generating and reading the credential report and the service last accessed reports, and which credential operations are never tools.
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

## Account state, identity providers, and further reads

All tools are reads and need no confirmation. Every list tool that pages takes `page_size` and `cursor` and returns
`next_cursor` and `has_more`. Names, descriptions, tags, URLs, certificate text, SAML metadata, and policy documents
are set by whoever manages the account and are untrusted data: they are cut at their limits, and a URL is never
opened. Policy documents are decoded JSON text cut at 32 KiB, with `truncated` set when one was.

An ARN argument must be an ARN of the `aws` partition in the bound account. Any other ARN is refused before a
request, without being repeated in the message.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.iam.account.get-summary` | `GetAccountSummary` | | `summary_map` (counter name to number) |
| `aws.iam.account.get-authorization-details` | `GetAccountAuthorizationDetails` | `filter` (list of `User`, `Role`, `Group`, `LocalManagedPolicy`, `AWSManagedPolicy`) | `user_detail_list`, `group_detail_list`, `role_detail_list`, `policies`, `truncated`, paged |
| `aws.iam.account.get-password-policy` | `GetAccountPasswordPolicy` | | `password_policy`; `not-found` when the account has none |
| `aws.iam.account.list-aliases` | `ListAccountAliases` | | `account_aliases`, paged |
| `aws.iam.account.get-properties` | `GetAccountProperties` | | `properties` |
| `aws.iam.oidc-provider.list` | `ListOpenIDConnectProviders` | | `open_id_connect_provider_list` (ARNs) |
| `aws.iam.oidc-provider.get` | `GetOpenIDConnectProvider` | `open_id_connect_provider_arn` (required) | `url`, `client_id_list`, `thumbprint_list`, `create_date`, `tags` |
| `aws.iam.oidc-provider.list-tags` | `ListOpenIDConnectProviderTags` | `open_id_connect_provider_arn` (required) | `tags`, paged |
| `aws.iam.saml-provider.list` | `ListSAMLProviders` | | `saml_provider_list` (ARN, creation and expiry date) |
| `aws.iam.saml-provider.get` | `GetSAMLProvider` | `saml_provider_arn` (required) | `saml_provider_uuid`, `saml_metadata_document`, dates, `tags`, `assertion_encryption_mode`, `key_list`, `truncated` |
| `aws.iam.saml-provider.list-tags` | `ListSAMLProviderTags` | `saml_provider_arn` (required) | `tags`, paged |
| `aws.iam.server-certificate.list` | `ListServerCertificates` | `path_prefix` | `server_certificate_metadata_list`, paged |
| `aws.iam.server-certificate.get` | `GetServerCertificate` | `server_certificate_name` (required) | `server_certificate` (metadata, public certificate and chain, tags), `truncated` |
| `aws.iam.server-certificate.list-tags` | `ListServerCertificateTags` | `server_certificate_name` (required) | `tags`, paged |
| `aws.iam.mfa-device.get` | `GetMFADevice` | `serial_number` (required), `user_name` | `user_name`, `serial_number`, `enable_date`, `certifications` |
| `aws.iam.mfa-device.list-tags` | `ListMFADeviceTags` | `serial_number` (required) | `tags`, paged |
| `aws.iam.virtual-mfa-device.list` | `ListVirtualMFADevices` | `assignment_status` (`Assigned`, `Unassigned`, `Any`) | `virtual_mfa_devices` (serial number, user, enable date, tags), paged |
| `aws.iam.instance-profile.list-tags` | `ListInstanceProfileTags` | `instance_profile_name` (required) | `tags`, paged |
| `aws.iam.service-linked-role.get-deletion-status` | `GetServiceLinkedRoleDeletionStatus` | `deletion_task_id` (required) | `status`, `reason` |
| `aws.iam.role-template.get-version` | `GetRoleTemplateVersion` | `template_arn` (required), `minor_version` | `role_template_version`, `truncated` |
| `aws.iam.human-readable-summary.get` | `GetHumanReadableSummary` | `entity_arn` (required), `locale` | `summary_content`, `locale`, `summary_state` |
| `aws.iam.outbound-federation.get-info` | `GetOutboundWebIdentityFederationInfo` | | `issuer_identifier`, `jwt_vending_enabled` |
| `aws.iam.organizations-features.list` | `ListOrganizationsFeatures` | | `organization_id`, `enabled_features` |
| `aws.iam.organizations-access-report.get` | `GetOrganizationsAccessReport` | `job_id` (required), `sort_key` | `job_status`, dates, counts, `access_details`, `error_details`, paged |
| `aws.iam.delegation-request.get` | `GetDelegationRequest` | `delegation_request_id` (required), `delegation_permission_check` | `delegation_request`, `permission_check_status`, `permission_check_result` |
| `aws.iam.delegation-request.list` | `ListDelegationRequests` | `owner_id` | `delegation_requests`, paged |

Secrets never reach a result. The seed and the QR code of a virtual MFA device are removed from
`aws.iam.virtual-mfa-device.list`, even when AWS sends them. `key_list` of a SAML provider holds only the ID and the
date of each key, never the key. A server certificate result holds the public certificate text and chain, which
authenticate nothing on their own; a private key is not part of the answer. A password policy holds the rules for
passwords, not a password.

## Reports

The credential report lists every IAM user and the root account with the state of their passwords, access keys,
MFA devices, and signing certificates. A service last accessed report shows which services an identity or policy
used last. A report is made in two steps: a generate tool starts it, and a get tool reads it once it is ready.

The two generate tools are `listed-only` and need confirmation: a connection offers one only when its `tools` list
names the exact tool ID, as for the role tools. They are free, change no account data, and only produce a report.
They are not repeated after an unclear outcome. The read tools are open reads and need no confirmation.

| Tool | AWS action | Arguments | Result |
| --- | --- | --- | --- |
| `aws.iam.credential-report.generate` | `GenerateCredentialReport` | none | `state` (`STARTED`, `INPROGRESS`, `COMPLETE`), `description` |
| `aws.iam.credential-report.get` | `GetCredentialReport` | `page_size` (1 to 500, 100 when left out), `cursor` | `rows`, `total_rows`, `dropped_columns`, `generated_time`, `report_format` |
| `aws.iam.service-last-accessed.generate` | `GenerateServiceLastAccessedDetails` | `arn` (required), `granularity` (`SERVICE_LEVEL` or `ACTION_LEVEL`) | `job_id` |
| `aws.iam.service-last-accessed.get-details` | `GetServiceLastAccessedDetails` | `job_id` (required), `page_size` (1 to 1000), `cursor` | `job_status`, `job_type`, `job_creation_date`, `job_completion_date`, `services_last_accessed`, `error` |
| `aws.iam.service-last-accessed.get-entities` | `GetServiceLastAccessedDetailsWithEntities` | `job_id`, `service_namespace` (both required), `page_size` (1 to 1000), `cursor` | `job_status`, `job_creation_date`, `job_completion_date`, `entity_details_list`, `error` |
| `aws.iam.service-access.list-policies` | `ListPoliciesGrantingServiceAccess` | `arn`, `service_namespaces` (both required), `cursor` | `policies_granting_service_access` |

### Credential report rows

`aws.iam.credential-report.get` never returns the report as it comes. AWS sends it as a base64 CSV file; Qatlas
decodes and parses it and returns one object per row in `rows`, one for each IAM user and one for the root account
(`user` is `<root_account>`). The keys are the column names of the report, which are already snake_case:
`user`, `arn`, `user_creation_time`, `password_enabled`, `password_last_used`, `password_last_changed`,
`password_next_rotation`, `mfa_active`, `access_key_1_active`, `access_key_1_last_rotated`,
`access_key_1_last_used_date`, `access_key_1_last_used_region`, `access_key_1_last_used_service`, the same five
for `access_key_2`, `cert_1_active`, `cert_1_last_rotated`, `cert_2_active`, and `cert_2_last_rotated`.

- A field that is empty, `N/A`, or `not_supported` is left out of its row, so a missing key means no value.
  Other words of the report, such as `no_information`, stay as text.
- `password_enabled`, `mfa_active`, and the `*_active` columns are booleans. Any other value in them makes the
  answer invalid. All other columns are text, including the dates.
- Only the columns above are returned. Any other column is dropped; its name is listed in `dropped_columns` when it
  is a plain name, at most 32 names. `user` and `arn` must be in the report and filled in every row.
- The report is capped at 4 MiB (base64), 10000 rows, 64 columns, and 2048 bytes per field. A report that is not
  base64, not UTF-8, not valid CSV, has rows of a different width than the header, repeats a column, or breaks a cap
  is refused as an invalid provider response.
- `rows` pages by `page_size` and `cursor`. `next_cursor` is bound to the tool and carries the generation time of the
  report: it is refused when changed, and when the report was generated again meanwhile, so start the rows again
  without `cursor`. `total_rows` counts the whole report. `generated_time` and `report_format` are AWS's.

If no report exists, the call fails as `not-found`. While AWS is still generating one, or when the last one
expired, it fails as a `provider-error` whose message says so. Start a new report with
`aws.iam.credential-report.generate` and read it again when the state is `COMPLETE`; AWS makes a new report at most
every four hours.

### Service last accessed reports

`arn` of `aws.iam.service-last-accessed.generate` must be an ARN of the `aws` partition in the bound account: a
user, group, role, or customer managed policy. An AWS managed policy ARN is refused, as is any other account.
`arn` of `aws.iam.service-access.list-policies` must be a user, group, or role of the bound account. The check runs
before any request. Each generate call starts a new job; read the job with its `job_id` until `job_status` is
`COMPLETED`. Names, ARNs, and the `error` message of a job are set by whoever manages the account or by AWS and are
untrusted data. `aws.iam.service-access.list-policies` has no `page_size`, since AWS has no such parameter; the
others take it up to 1000.

## Never a tool

The operations that create a credential, return one, take a password, a private key, or a one-time code, or grant
trust to an identity outside the account are excluded for good: `ChangePassword`, `CreateAccessKey`,
`CreateLoginProfile`, `UpdateLoginProfile`, `CreateServiceSpecificCredential`, `ResetServiceSpecificCredential`,
`CreateUser`, `CreateVirtualMFADevice`, `EnableMFADevice`, `ResyncMFADevice`, `UploadSSHPublicKey`,
`UploadSigningCertificate`, `UploadServerCertificate`, `CreateSAMLProvider`, `UpdateSAMLProvider`,
`CreateOpenIDConnectProvider`, `UpdateOpenIDConnectProviderThumbprint`, `AddClientIDToOpenIDConnectProvider`,
`EnableOutboundWebIdentityFederation`, `CreateDelegationRequest`, `SendDelegationToken`, and
`AcceptDelegationRequest`.

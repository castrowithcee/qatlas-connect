---
description: >
  Guides a user through setting up the AWS provider: the secret roles of the credential, choosing least permissions, and what the connection test proves.
type: knowledge
edit: shared
created: 2026-11-06
updated: 2026-11-11
---

# Set up AWS

This page names what a connection of the `aws` provider needs. Role assumption, targets, errors, and limits are
described in the [AWS provider](README.md); the tools are described per group: the
[Security Token Service](sts.md), [Identity and Access Management](iam.md), and [Simple Email Service](ses.md).

## Configuration

A connection of this provider needs a credential with these secret roles and targets.

| Secret role | Required | Meaning |
| --- | --- | --- |
| `access_key_id` | yes | Access key ID of an IAM user, or of temporary credentials (`AKIA...`, `ASIA...`). |
| `secret_access_key` | yes | The secret access key that belongs to it. |
| `session_token` | no | Session token of temporary credentials. Leave it unset for a long-term IAM user key. |
| `role_arn` | no | ARN of an IAM role of the bound account to assume, `arn:aws:iam::<account>:role/<name>`. Not a secret; it is a role because the configuration model keeps no other per-credential setting. |
| `external_id` | no | External ID the role's trust policy requires. Treated as a secret. Only valid together with `role_arn`. |

Every value is registered with the redactor. No tool, argument, result, error text, or log line carries a
credential. The service has no base URL of your own: leave `base_url` out, since every endpoint comes from a fixed
table and any other value is refused.

Use an IAM identity with the least permissions the tools you enable need. The STS tools need no permission except
`sts:DecodeAuthorizationMessage` for the decode tool.

### IAM

Each [IAM tool](iam.md) needs the permission `iam:<action>` of its AWS action, for example `iam:ListUsers` for
`aws.iam.user.list` and `iam:GetAccessKeyLastUsed` for `aws.iam.access-key.get-last-used`. A policy that grants
`iam:List*` and `iam:Get*` covers every IAM read, but also reads that are not tools yet; grant the single actions to
stay narrow. The recommended profile `read` needs `iam:ListUsers`, `iam:GetUser`, `iam:ListUserTags`,
`iam:ListGroups`, `iam:GetGroup`, `iam:ListGroupsForUser`, `iam:ListRoles`, `iam:GetRole`, `iam:ListRoleTags`,
`iam:ListAccessKeys`, and `iam:GetAccessKeyLastUsed`.

The policy tools need `iam:ListPolicies`, `iam:GetPolicy`, `iam:ListPolicyTags`, `iam:ListEntitiesForPolicy`,
`iam:ListPolicyVersions`, `iam:GetPolicyVersion`, `iam:ListAttached{User,Group,Role}Policies`,
`iam:List{User,Group,Role}Policies`, `iam:Get{User,Group,Role}Policy`, and for the simulation
`iam:SimulateCustomPolicy`, `iam:SimulatePrincipalPolicy`, `iam:GetContextKeysForCustomPolicy`, and
`iam:GetContextKeysForPrincipalPolicy`. They are not part of the profile `read`.

The [account and identity provider tools](iam.md#account-state-identity-providers-and-further-reads) need
`iam:GetAccountSummary`, `iam:GetAccountAuthorizationDetails`, `iam:GetAccountPasswordPolicy`,
`iam:ListAccountAliases`, `iam:GetAccountProperties`, `iam:ListOpenIDConnectProviders`,
`iam:GetOpenIDConnectProvider`, `iam:ListOpenIDConnectProviderTags`, `iam:ListSAMLProviders`, `iam:GetSAMLProvider`,
`iam:ListSAMLProviderTags`, `iam:ListServerCertificates`, `iam:GetServerCertificate`,
`iam:ListServerCertificateTags`, `iam:GetMFADevice`, `iam:ListMFADeviceTags`, `iam:ListVirtualMFADevices`,
`iam:ListInstanceProfileTags`, `iam:GetServiceLinkedRoleDeletionStatus`, `iam:GetRoleTemplateVersion`,
`iam:GetHumanReadableSummary`, `iam:GetOutboundWebIdentityFederationInfo`, `iam:ListOrganizationsFeatures`,
`iam:GetOrganizationsAccessReport`, `iam:GetDelegationRequest`, and `iam:ListDelegationRequests`, each for its own
tool. `iam:GetAccountAuthorizationDetails` returns every policy document of the account, so grant it with care. They
are not part of the profile `read`.

The [report tools](iam.md#reports) need `iam:GenerateCredentialReport`, `iam:GetCredentialReport`,
`iam:GenerateServiceLastAccessedDetails`, `iam:GetServiceLastAccessedDetails`,
`iam:GetServiceLastAccessedDetailsWithEntities`, and `iam:ListPoliciesGrantingServiceAccess`, each for its own tool.
The two generate tools are listed-only and need confirmation. They are not part of the profile `read`.

The [role tools](iam.md#roles-and-instance-profiles) need `iam:CreateRole`, `iam:UpdateRole`,
`iam:UpdateAssumeRolePolicy`, `iam:PutRolePermissionsBoundary`, `iam:DeleteRolePermissionsBoundary`, `iam:TagRole`,
`iam:UntagRole`, `iam:DeleteRole`, `iam:CreateInstanceProfile`, `iam:DeleteInstanceProfile`,
`iam:AddRoleToInstanceProfile`, and `iam:RemoveRoleFromInstanceProfile`, each for its own tool. Creating with tags
also needs `iam:TagRole` or `iam:TagInstanceProfile`, and adding a role to an instance profile also needs
`iam:PassRole` for that role. The own-role check calls `sts:GetCallerIdentity`, which needs no permission. Grant
only the actions of the tools the connection lists.

### SES

Each [SES tool](ses.md) needs the permission `ses:<Operation>` of its AWS operation: `ses:ListEmailIdentities`,
`ses:GetEmailIdentity`, `ses:ListTagsForResource`, `ses:GetEmailIdentityPolicies`, `ses:CreateEmailIdentity`,
`ses:DeleteEmailIdentity`, `ses:PutEmailIdentityDkimAttributes`, `ses:PutEmailIdentityDkimSigningAttributes`,
`ses:PutEmailIdentityMailFromAttributes`, `ses:PutEmailIdentityFeedbackAttributes`,
`ses:PutEmailIdentityConfigurationSetAttributes`, `ses:TagResource`, and `ses:UntagResource`, each for its own tool.
Creating with tags also needs `ses:TagResource`. Grant only the actions of the tools the connection offers. An account
without production access is in the SES sandbox and sends only to verified recipients.

## Connection test

The test assumes the role if one is configured, calls `sts:GetCallerIdentity` at the STS endpoint of the first
listed region, and checks that the account is the bound one. It is free and needs no permission. **It proves the
credential and the account only.** It proves neither the permissions for any other service nor that the other
listed regions are enabled for the account.

A credential of another account is reported as a test error that says so, since that refusal has no class of its
own. Every other failure is reported by its class.


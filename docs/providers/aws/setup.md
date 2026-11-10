---
description: >
  Guides a user through setting up the AWS provider: the secret roles of the credential, choosing least permissions, and what the connection test proves.
type: knowledge
edit: shared
created: 2026-11-06
updated: 2026-11-10
---

# Set up AWS

This page names what a connection of the `aws` provider needs. Role assumption, targets, errors, and limits are
described in the [AWS provider](README.md); the tools are described per group: the
[Security Token Service](sts.md) and [Identity and Access Management](iam.md).

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

## Connection test

The test assumes the role if one is configured, calls `sts:GetCallerIdentity` at the STS endpoint of the first
listed region, and checks that the account is the bound one. It is free and needs no permission. **It proves the
credential and the account only.** It proves neither the permissions for any other service nor that the other
listed regions are enabled for the account.

A credential of another account is reported as a test error that says so, since that refusal has no class of its
own. Every other failure is reported by its class.


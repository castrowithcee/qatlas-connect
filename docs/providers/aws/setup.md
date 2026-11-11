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
[Security Token Service](sts.md), [Identity and Access Management](iam.md), [Simple Email Service](ses.md), and
[Amazon S3](s3.md), and [Amazon Route 53](route53.md).

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
`ses:GetEmailIdentity`, `ses:ListTagsForResource`, `ses:GetEmailIdentityPolicies`, `ses:CreateEmailIdentityPolicy`,
`ses:UpdateEmailIdentityPolicy`, `ses:DeleteEmailIdentityPolicy`, `ses:CreateEmailIdentity`,
`ses:DeleteEmailIdentity`, `ses:PutEmailIdentityDkimAttributes`, `ses:PutEmailIdentityDkimSigningAttributes`,
`ses:PutEmailIdentityMailFromAttributes`, `ses:PutEmailIdentityFeedbackAttributes`,
`ses:PutEmailIdentityConfigurationSetAttributes`, `ses:TagResource`, and `ses:UntagResource`, each for its own tool.
Creating with tags also needs `ses:TagResource`. The identity policy write tools are offered only when the tools list
of the connection names them. Grant only the actions of the tools the connection offers. An account without production
access is in the SES sandbox and sends only to verified recipients.

### S3

Each [S3 tool](s3.md) needs the permission `s3:<Action>` for the bucket (`arn:aws:s3:::<bucket>`), except `bucket.list`,
which needs `s3:ListAllMyBuckets` on all resources. The reads need `s3:ListAllMyBuckets` (`bucket.list`),
`s3:ListBucket` (`bucket.head`), `s3:GetBucketLocation`, `s3:GetBucketVersioning`, `s3:GetEncryptionConfiguration`,
`s3:GetBucketTagging`, `s3:GetBucketPublicAccessBlock`, `s3:GetLifecycleConfiguration` (`lifecycle.get`),
`s3:GetBucketCORS` (`cors.get`), `s3:GetBucketAcl` (`acl.get`), `s3:GetBucketWebsite`, `s3:GetBucketLogging` (`logging.get`), `s3:GetBucketNotification` (`notification.get`),
`s3:GetReplicationConfiguration` (`replication.get`), `s3:GetBucketOwnershipControls`, `s3:GetAccelerateConfiguration`,
`s3:GetBucketRequestPayment`, `s3:GetBucketObjectLockConfiguration` (`object-lock.get`), `s3:GetObjectRetention` and
`s3:GetObjectLegalHold` (`object-retention.get` and `object-legal-hold.get`, for the object `arn:aws:s3:::<bucket>/<key>`), each for its own tool. The writes need:

| Tool | Permission |
| --- | --- |
| `aws.s3.bucket.create` | `s3:CreateBucket` |
| `aws.s3.bucket.delete` | `s3:DeleteBucket` |
| `aws.s3.versioning.put` | `s3:PutBucketVersioning` |
| `aws.s3.encryption.put`, `aws.s3.encryption.delete` | `s3:PutEncryptionConfiguration` |
| `aws.s3.tagging.put`, `aws.s3.tagging.delete` | `s3:PutBucketTagging` |
| `aws.s3.public-access-block.put` | `s3:PutBucketPublicAccessBlock` and `s3:GetBucketPublicAccessBlock` (the read before the change) |
| `aws.s3.ownership.put` | `s3:PutBucketOwnershipControls` |
| `aws.s3.lifecycle.delete` | `s3:PutLifecycleConfiguration` |
| `aws.s3.cors.delete` | `s3:PutBucketCORS` |

Creating with tags also needs `s3:PutBucketTagging`. Setting a KMS key also needs the permissions the key's policy
requires for the identity. Grant only the actions of the tools the connection offers; a policy such as `s3:*` also
grants every object action.

### Route 53

Each [Route 53 tool](route53.md) needs the permission `route53:<Action>` of its AWS operation. The permissions
`route53:ListHostedZones` (`hosted-zone.list`), `route53:ListHostedZonesByName`, `route53:GetHostedZone`,
`route53:GetHostedZoneCount`, `route53:GetChange`, `route53:ListTagsForResource`, `route53:ListResourceRecordSets`
(`record-set.list`), `route53:TestDNSAnswer` (`dns-answer.test`), `route53:ListHealthChecks`, `route53:GetHealthCheck`,
`route53:GetHealthCheckCount`, `route53:GetHealthCheckStatus`, `route53:GetHealthCheckLastFailureReason`, and `route53:GetDNSSEC` (`dnssec.get`) read. The writes need:

| Tool | Permission |
| --- | --- |
| `aws.route53.hosted-zone.create` | `route53:CreateHostedZone`; for a private zone also `ec2:DescribeVpcs`, `ec2:DescribeRegions`, and `route53:AssociateVPCWithHostedZone` |
| `aws.route53.hosted-zone.update-comment` | `route53:UpdateHostedZoneComment` |
| `aws.route53.hosted-zone.delete` | `route53:DeleteHostedZone` |
| `aws.route53.tag.change` | `route53:ChangeTagsForResource` |
| `aws.route53.record-set.change` | `route53:ChangeResourceRecordSets` |
| `aws.route53.health-check.create` | `route53:CreateHealthCheck` |
| `aws.route53.health-check.update` | `route53:UpdateHealthCheck` |
| `aws.route53.health-check.delete` | `route53:DeleteHealthCheck` |
| `aws.route53.dnssec.enable` | `route53:EnableHostedZoneDNSSEC` |
| `aws.route53.dnssec.disable` | `route53:DisableHostedZoneDNSSEC` |
| `aws.route53.key-signing-key.create` | `route53:CreateKeySigningKey`; also `kms:DescribeKey`, `kms:GetPublicKey`, `kms:Sign`, and `kms:CreateGrant` on the KMS key |
| `aws.route53.key-signing-key.activate` | `route53:ActivateKeySigningKey`; also the `kms` permissions above on the KMS key |
| `aws.route53.key-signing-key.deactivate` | `route53:DeactivateKeySigningKey` |
| `aws.route53.key-signing-key.delete` | `route53:DeleteKeySigningKey`; also the `kms` permissions above on the KMS key |

Route 53 permissions name hosted zones as `arn:aws:route53:::hostedzone/<id>`, health checks as
`arn:aws:route53:::healthcheck/<id>`, and changes as `arn:aws:route53:::change/<id>`; `route53:CreateHostedZone`,
`route53:ListHostedZones`, `route53:GetHostedZoneCount`, `route53:CreateHealthCheck`, `route53:ListHealthChecks`, and
`route53:GetHealthCheckCount` apply to all resources. The tag tools need `route53:ListTagsForResource` and
`route53:ChangeTagsForResource` for health checks as for zones. Grant only the actions of the tools the connection offers; a policy such as `route53:*` also
grants every change of the records of a zone. The `kms` permissions are the minimum the policy of the identity needs on the
one KMS key in `us-east-1`; the DNSSEC writes are listed-only and need the tools to be named in the connection's tools list.

**The KMS key policy.** Route 53 uses the key through the service principal, so the key policy of the KMS key, not
the identity policy, must allow `dnssec-route53.amazonaws.com` to `kms:DescribeKey`, `kms:GetPublicKey`, `kms:Sign`,
and `kms:CreateGrant` (the grant with the condition `kms:GrantIsForAWSResource` true), and the statement should limit the
service with the condition keys `aws:SourceAccount` (the bound account) and `aws:SourceArn` (`arn:aws:route53:::hostedzone/*`). The key must be an asymmetric `ECC_NIST_P256` key for signing and verifying in
`us-east-1`. Without that, `key-signing-key.create`, `key-signing-key.activate`, and `dnssec.enable` are refused by Route 53, as
`InvalidKMSArn` or `InvalidKeySigningKeyStatus` (the exact code is not measured): add the principal to the key policy, then repeat the call. Qatlas does not
create or change the KMS key.

## Connection test

The test assumes the role if one is configured, calls `sts:GetCallerIdentity` at the STS endpoint of the first
listed region, and checks that the account is the bound one. It is free and needs no permission. **It proves the
credential and the account only.** It proves neither the permissions for any other service nor that the other
listed regions are enabled for the account.

A credential of another account is reported as a test error that says so, since that refusal has no class of its
own. Every other failure is reported by its class.


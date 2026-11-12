---
description: >
  Describes the tools of the AWS SES v2 group: reading, creating, configuring, tagging, and deleting sender identities with DKIM and MAIL FROM, creating and configuring configuration sets, reading their event destinations, managing the suppression list, the identity, ARN, archive, and domain checks, the DNS help derived from checked values, and what is never a tool.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-12
---

# Simple Email Service

Group `ses`, the SES v2 API. SES is regional: every tool takes an optional `region`, which may be left out when the
connection lists exactly one region and otherwise must name an allowed one. Every permission a tool needs is listed in
[Set up AWS](setup.md#ses). The tools manage the sender identities (domains and email addresses), the configuration sets, and the suppression list of
the bound account.

## Tools

Reads need no confirmation. Every other tool needs confirmation. The three identity policy tools that write are
listed-only: a connection offers them only when its tools list names each one exactly, not by effect or prefix.

| Tool | AWS operation | Effect | Confirmation |
| --- | --- | --- | --- |
| `aws.ses.email-identity.list` | `ListEmailIdentities` | read | no |
| `aws.ses.email-identity.get` | `GetEmailIdentity` | read | no |
| `aws.ses.email-identity.list-tags` | `ListTagsForResource` | read | no |
| `aws.ses.email-identity-policy.list` | `GetEmailIdentityPolicies` | read | no |
| `aws.ses.email-identity-policy.create` | `CreateEmailIdentityPolicy` | create, not repeated | yes, listed-only |
| `aws.ses.email-identity-policy.update` | `UpdateEmailIdentityPolicy` | update | yes, listed-only |
| `aws.ses.email-identity-policy.delete` | `DeleteEmailIdentityPolicy` | delete, not repeated | yes, listed-only |
| `aws.ses.email-identity.create` | `CreateEmailIdentity` | create, not repeated | yes |
| `aws.ses.email-identity.delete` | `DeleteEmailIdentity` | delete, not repeated | yes |
| `aws.ses.email-identity.put-dkim-attributes` | `PutEmailIdentityDkimAttributes` | update | yes |
| `aws.ses.email-identity.put-dkim-signing-attributes` | `PutEmailIdentityDkimSigningAttributes` | update | yes |
| `aws.ses.email-identity.put-mail-from-attributes` | `PutEmailIdentityMailFromAttributes` | update | yes |
| `aws.ses.email-identity.put-feedback-attributes` | `PutEmailIdentityFeedbackAttributes` | update | yes |
| `aws.ses.email-identity.put-configuration-set-attributes` | `PutEmailIdentityConfigurationSetAttributes` | update | yes |
| `aws.ses.email-identity.tag` | `TagResource` | update | yes |
| `aws.ses.email-identity.untag` | `UntagResource` | update | yes |

### Configuration sets and suppression

| Tool | AWS operation | Effect | Confirmation |
| --- | --- | --- | --- |
| `aws.ses.configuration-set.list` | `ListConfigurationSets` | read | no |
| `aws.ses.configuration-set.get` | `GetConfigurationSet` | read | no |
| `aws.ses.configuration-set.create` | `CreateConfigurationSet` | create, not repeated | yes |
| `aws.ses.configuration-set.delete` | `DeleteConfigurationSet` | delete, not repeated | yes |
| `aws.ses.configuration-set.put-delivery-options` | `PutConfigurationSetDeliveryOptions` | update | yes |
| `aws.ses.configuration-set.put-reputation-options` | `PutConfigurationSetReputationOptions` | update | yes |
| `aws.ses.configuration-set.put-sending-options` | `PutConfigurationSetSendingOptions` | update | yes |
| `aws.ses.configuration-set.put-suppression-options` | `PutConfigurationSetSuppressionOptions` | update | yes |
| `aws.ses.configuration-set.put-tracking-options` | `PutConfigurationSetTrackingOptions` | update | yes |
| `aws.ses.configuration-set.put-vdm-options` | `PutConfigurationSetVdmOptions` | update | yes |
| `aws.ses.configuration-set.put-archiving-options` | `PutConfigurationSetArchivingOptions` | update | yes |
| `aws.ses.configuration-set-event-destination.get` | `GetConfigurationSetEventDestinations` | read | no |
| `aws.ses.suppressed-destination.list` | `ListSuppressedDestinations` | read | no |
| `aws.ses.suppressed-destination.get` | `GetSuppressedDestination` | read | no |
| `aws.ses.suppressed-destination.put` | `PutSuppressedDestination` | create | yes |
| `aws.ses.suppressed-destination.delete` | `DeleteSuppressedDestination` | delete, not repeated | yes |

`aws.ses.email-identity.create` with tags also needs `ses:TagResource`. A create or delete whose outcome is unclear
is not repeated: read the identity, its policies, or the configuration set first.

## Checks before the call

- **Identity form.** `email_identity` must be a domain name or an email address of a domain: letters, digits, dots, and
  hyphens, and for an address also `+` and `_` in the local part. Anything else is refused before any request.
- **ARN.** The tools that take `resource_arn` (`aws.ses.email-identity.tag`, `untag`, `list-tags`) accept the ARN of an
  SES identity (`arn:aws:ses:<region>:<account>:identity/<name>`) or of a configuration set
  (`arn:aws:ses:<region>:<account>:configuration-set/<name>`) in the bound account and in an allowed region of the
  connection. The same three tools tag configuration sets; their IDs name the identity for historical reasons. Another
  account, another region, or another resource type is refused.
- **Configuration set name.** `configuration_set_name` must be 1 to 64 letters, digits, underscores, or hyphens.
- **Archive ARN.** `archive_arn` of `put-archiving-options`, and `archiving_options.archive_arn` of
  `configuration-set.create`, must be the ARN of a mail archive
  (`arn:aws:ses:<region>:<account>:mailmanager-archive/a-...`) in the bound account and an allowed region. Another
  account, another region, or another resource type is refused.
- **Tracking domain.** `custom_redirect_domain` of `put-tracking-options`, and `tracking_options.custom_redirect_domain`
  of `configuration-set.create`, is checked only for the form of a domain name. SES checks that you own it.

## Identity policies

`create` and `update` check the `policy` document before any call; a document that breaks a rule is refused and
nothing is sent. The rules hold for every statement, whatever its `Effect`:

- A principal is only the bound account (its 12-digit ID or `arn:aws:iam::<account>:root`), a user or a role of the bound
  account, or an AWS service principal `<name>.amazonaws.com`.
- A service principal needs, in the same statement, `aws:SourceAccount` with exactly the bound account under
  `StringEquals`, or `aws:SourceArn` with only ARNs of the bound account (the account field spelled out, no `*` or `?`)
  under `ArnEquals`, `ArnLike`, or `StringEquals`. Values are a string or a list. Condition keys are compared without
  case, and two spellings of one key in an operator are refused. `ForAnyValue:`, `ForAllValues:`, and `...IfExists`
  operators on these keys are refused.
- Refused: `*` in any form, `NotPrincipal`, `Federated`, `CanonicalUser` and every principal type except `AWS` and
  `Service`, another account, a service without the condition, a wildcard in the account field, a missing `Principal`,
  invalid JSON, a key twice in one object, an element the policy grammar does not define, and a document above 131072
  characters.

A refusal names the rule and the statement by position, never a value of the document. `delete` takes the identity and
the policy name and needs no document. `ses:CreateEmailIdentityPolicy`, `ses:UpdateEmailIdentityPolicy`, and
`ses:DeleteEmailIdentityPolicy` are the permissions.

## DNS help

`get` and `create` return `dns_records`, the records to create at the DNS provider. They are derived by the tool, not
copied from the answer:

- Names come from the identity the caller gave, in lowercase.
- A DKIM CNAME is `<token>._domainkey.<identity>`, pointing to `<token>.<signing_hosted_zone>`. SES uses different
  DKIM zones per region and identity (`dkim.amazonses.com`, `dkim.eu-central-2.amazonses.com`), so the zone is the
  `signing_hosted_zone` of the answer. It is taken only in the form `dkim.amazonses.com` or
  `dkim.<label>.amazonses.com` in lowercase; with another value or none, there are no DKIM records. A token must be
  20 to 64 lowercase letters and digits, and at most eight records are listed.
- A configured MAIL FROM domain, which must be a subdomain of the identity, gives an MX record
  `10 feedback-smtp.<region>.amazonses.com` and a TXT record `v=spf1 include:amazonses.com ~all`.
- An email address needs no records of its own and gives none.

## Configuration sets

A configuration set groups sending settings that mail uses when a sender names the set or an identity has it as its
default. `create` takes every option as a structured argument: `tracking_options`, `delivery_options`,
`reputation_options`, `sending_options`, `tags`, `suppression_options`, `vdm_options`, `archiving_options`, and
`message_security_options`. Each option also has its own `put-...` tool. `get` returns the options and the tags. `delete`
removes the set with its event destinations; mail that names it afterwards is rejected. `put-sending-options` with
`sending_enabled` false pauses sending with the set. `UpdateConfigurationSet` of the SES API is not offered yet.

Event destinations are read only. `aws.ses.configuration-set-event-destination.get` returns the destinations of a set,
including their targets; the answer is untrusted data. These actions are not tools, because an event destination sends
the events of a configuration set to SNS, Firehose, EventBridge, or Pinpoint and so carries data out of the account:

- `CreateConfigurationSetEventDestination`
- `UpdateConfigurationSetEventDestination`
- `DeleteConfigurationSetEventDestination`

## Suppression list

The account-level suppression list holds addresses SES no longer sends to after a bounce or a complaint. `list` filters
by `reasons` (`BOUNCE`, `COMPLAINT`), `start_date`, and `end_date` (RFC 3339 timestamps) and pages like the other lists,
with a page size of up to 1000. `get` returns one entry. `put` adds an address with a reason, after which SES no longer
sends mail to it. `delete` removes an address: it receives mail again after the call, also when it bounced or
complained before.

Email addresses are personal data. The suppression tools carry the data class `aws-ses-recipient-data`, and entries of
the list, including their attributes, are untrusted data. The tenant of the SES API is not offered; the tools act on
the list of the account.

## Not offered

- No key material of your own (BYODKIM): the selector and the private key are not arguments, and the origin `EXTERNAL`
  is not offered. Easy DKIM keys are generated by SES, with `RSA_1024_BIT` or `RSA_2048_BIT` for the next key.
- Creating, changing, and deleting event destinations of a configuration set, as listed above.
- `SendCustomVerificationEmail` is never a tool: it would send mail to any address given as an argument.

## Limits

- An account without production access is in the SES sandbox and sends only to verified recipients and from verified
  identities. The tools neither lift nor check this.
- DKIM and domain verification take time after the records exist. How long, and the sandbox behaviour, have not been
  proven against a live account.
- How the options of a configuration set affect deliverability has not been proven against a live account.
- Everything SES returns, including the identity policies, the event destinations, and the suppression list, is
  untrusted data.

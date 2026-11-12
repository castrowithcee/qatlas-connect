---
description: >
  Describes the tools of the AWS SES v2 group: reading, creating, configuring, tagging, and deleting sender identities with DKIM and MAIL FROM, the account status, the sandbox, the production access request and the account attributes, the identity and ARN checks, the DNS help derived from checked values, and what is never a tool.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-12
---

# Simple Email Service

Group `ses`, the SES v2 API. SES is regional: every tool takes an optional `region`, which may be left out when the
connection lists exactly one region and otherwise must name an allowed one. Every permission a tool needs is listed in
[Set up AWS](setup.md#ses). The tools manage the sender identities (domains and email addresses) of the bound account and read and change the
account status of one region.

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
| `aws.ses.account.get` | `GetAccount` | read | no |
| `aws.ses.account.put-details` | `PutAccountDetails` | update | yes |
| `aws.ses.account.put-sending-attributes` | `PutAccountSendingAttributes` | update | yes |
| `aws.ses.account.put-suppression-attributes` | `PutAccountSuppressionAttributes` | update | yes |
| `aws.ses.account.put-dedicated-ip-warmup-attributes` | `PutAccountDedicatedIpWarmupAttributes` | update | yes |
| `aws.ses.account.put-vdm-attributes` | `PutAccountVdmAttributes` | update | yes |

`aws.ses.email-identity.create` with tags also needs `ses:TagResource`. A create or delete whose outcome is unclear
is not repeated: read the identity or its policies first.

## Account status and the sandbox

SES accounts are separate per region. `aws.ses.account.get` returns for one region:

- `production_access_enabled`: false means the region is in the SES sandbox, where SES sends only to verified
  addresses and domains and the quota is small. True means production.
- `send_quota`: `max24_hour_send`, `max_send_rate` per second, and `sent_last24_hours`.
- `sending_enabled` and `enforcement_status` (the reputation state of the account).
- `details`: the production access request (`mail_type`, `website_url`, `contact_language`,
  `additional_contact_email_addresses`) and `review_details` with the review `status` and `case_id`. It is data of the
  account owner and untrusted.
- `suppression_attributes`, `vdm_attributes`, `dedicated_ip_auto_warmup_enabled`, and `pricing_attributes`.

## Requesting production access

`aws.ses.account.put-details` submits the request for one region and needs confirmation. It takes `mail_type`
(`MARKETING` or `TRANSACTIONAL`) and `website_url`, both required, and optionally `contact_language` (`EN` or `JA`),
`additional_contact_email_addresses` (up to four), and `production_access_enabled`. The use case text is deprecated by
AWS and not offered. What goes to AWS is the region, the mail type, the website, the contact language, and the contact
addresses; the contact addresses go only to AWS and are not recipients of any mail. The tool sends no mail and does not
answer AWS on your behalf.

AWS reviews the request, and the decision can take a while; how long, and why a request is refused, are not proven
against a live account. Call `aws.ses.account.get` again and read `details.review_details`. The request is idempotent:
the same call can be repeated.

## Account attributes

Each tool needs confirmation and acts on the account of the region it names:

- `put-sending-attributes`: `sending_enabled`. False stops all sending of the account in that region.
- `put-suppression-attributes`: `suppressed_reasons` (`BOUNCE`, `COMPLAINT`) and `validation_attributes`.
- `put-dedicated-ip-warmup-attributes`: `auto_warmup_enabled`.
- `put-vdm-attributes`: `vdm_attributes` with `vdm_enabled` and optionally the dashboard and guardian switches. Virtual
  Deliverability Manager costs extra on top of the sending price; the price is not retrieved by the tool.

`PutAccountPricingAttributes` is not offered.

## Checks before the call

- **Identity form.** `email_identity` must be a domain name or an email address of a domain: letters, digits, dots, and
  hyphens, and for an address also `+` and `_` in the local part. Anything else is refused before any request.
- **ARN.** The tools that take `resource_arn` (`tag`, `untag`, `list-tags`) accept only the ARN of an SES identity in
  the bound account and in an allowed region of the connection. Another account, another region, or another resource
  type is refused.

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

## Not offered

- No key material of your own (BYODKIM): the selector and the private key are not arguments, and the origin `EXTERNAL`
  is not offered. Easy DKIM keys are generated by SES, with `RSA_1024_BIT` or `RSA_2048_BIT` for the next key.
- `SendCustomVerificationEmail` is never a tool: it would send mail to any address given as an argument.

## Limits

- An account without production access is in the SES sandbox and sends only to verified recipients and from verified
  identities. `aws.ses.account.get` shows it; only AWS lifts it, after it reviews the request.
- DKIM and domain verification take time after the records exist. How long, and the sandbox behaviour, have not been
  proven against a live account.
- Everything SES returns, including the identity policies, is untrusted data.

---
description: >
  Describes the tools of the AWS SES v2 group: reading, creating, configuring, tagging, and deleting sender identities with DKIM and MAIL FROM, sending mail with confirmation, costs, sandbox, and no automatic repetition, the identity, address, and ARN checks, the DNS help derived from checked values, and what is never a tool.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-12
---

# Simple Email Service

Group `ses`, the SES v2 API. SES is regional: every tool takes an optional `region`, which may be left out when the
connection lists exactly one region and otherwise must name an allowed one. Every permission a tool needs is listed in
[Set up AWS](setup.md#ses). The tools manage the sender identities (domains and email addresses) of the bound account
and send mail from them.

## Tools

Reads need no confirmation. Every other tool needs confirmation. The three identity policy tools that write are
listed-only: a connection offers them only when its tools list names each one exactly, not by effect or prefix. The
send tools have the effect `execute`, which no connection offers by default.

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
| `aws.ses.email.send` | `SendEmail` | execute, not repeated | yes |
| `aws.ses.email.send-bulk` | `SendBulkEmail` | execute, not repeated | yes |

`aws.ses.email-identity.create` with tags also needs `ses:TagResource`. A create or delete whose outcome is unclear
is not repeated: read the identity or its policies first.

## Checks before the call

- **Identity form.** `email_identity` must be a domain name or an email address of a domain: letters, digits, dots, and
  hyphens, and for an address also `+` and `_` in the local part. Anything else is refused before any request.
- **ARN.** The tools that take `resource_arn` (`tag`, `untag`, `list-tags`) accept only the ARN of an SES identity in
  the bound account and in an allowed region of the connection. Another account, another region, or another resource
  type is refused. The same holds for the identity ARNs of the send tools (`from_email_address_identity_arn`,
  `feedback_forwarding_email_address_identity_arn`) and for a `template_arn`, which must name a template. Sending with
  an identity of another account (sending authorization) is therefore not possible.
- **Recipients.** A call sends to at most 50 recipients, To, Cc, and Bcc together and across every destination of a
  bulk call, the per-message limit of the
  [SES quotas](https://docs.aws.amazon.com/ses/latest/dg/quotas.html). More recipients take more calls, each
  confirmed on its own.
- **Addresses.** Every sender, reply-to, and recipient address must be `local@domain` or `Display Name
  <local@domain>`, 7-bit ASCII as SES requires: an RFC 5322 dot-atom local part of at most 64 characters, a domain
  name (Punycode for a Unicode domain), at most 254 characters. A display name is printable ASCII without `"(),:;<>@[\]`
  or is quoted; other characters need MIME encoded-word. The feedback address takes no display name.
- **Size.** A message above 40 MB, the SES v2 limit, is refused.

## Sending mail

`send` sends one mail with simple content (subject, text, HTML, headers, attachments) or a template: a stored one by
name or ARN, or one given inline, with template data. `send-bulk` sends one template to several destinations, each its
own mail with its own recipients, template data, headers, and tags, and returns status and message ID per destination;
one destination can fail while others succeed.

- **What leaves the account.** Region, sender, recipients (To, Cc, Bcc), subject or template, configuration set, and
  content go to AWS and from there to the recipients and their mail providers. A sent mail cannot be taken back. The
  tool fetches no URL from the content and runs nothing in it.
- **Confirmation.** Each call needs confirmation of exactly its request. The tool description is what the confirmation
  covers: what is sent, the costs, the sandbox, and that nothing is repeated.
- **Costs.** According to the [AWS price list](https://aws.amazon.com/ses/pricing/), 0.10 USD per 1,000 mails, each
  recipient counted, plus 0.12 USD per GB of attachments.
- **Sandbox.** An account without production access sends only to verified recipients, at most 200 mails a day and one
  per second.
- **No automatic repetition.** A send is never repeated, not after a rate limit, a timeout, or a server error. A
  timeout, a server-side failure, or an answer that cannot be read says that the mail may have gone out: check, for
  example with the recipient, before sending again. The SES refusals (message rejected, MAIL FROM domain not
  verified, sending paused, account suspended) mean the mail was not sent.
- **Pace.** Sends of one connection and region start at least one second apart, the sandbox rate, so no account
  exceeds its rate through the tool. A faster account that still hits its limit gets a rate limit that is not repeated.

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
- No raw MIME content (`Content.Raw` of `SendEmail`): it carries headers and parts of its own that no check of the tool
  sees. Simple and template content are the structured way to send, attachments included.
- No multi-region endpoint (`EndpointId`): it has a host of its own outside the endpoint table and needs another
  signature. A send goes to the regional endpoint of the call.

## Limits

- An account without production access is in the SES sandbox and sends only to verified recipients and from verified
  identities. The tools neither lift nor check this.
- Delivery, bounces, and the sandbox behaviour of the send tools have not been proven against a live account.
- DKIM and domain verification take time after the records exist. How long, and the sandbox behaviour, have not been
  proven against a live account.
- Everything SES returns, including the identity policies, is untrusted data.

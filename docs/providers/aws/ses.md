---
description: >
  Describes the tools of the AWS SES v2 group: sender identities with DKIM and MAIL FROM, the identity and ARN checks, the DNS help derived from checked values, email templates, contact lists with topics, contacts and their privacy, and what is never a tool.
type: knowledge
edit: shared
created: 2026-11-11
updated: 2026-11-12
---

# Simple Email Service

Group `ses`, the SES v2 API. SES is regional: every tool takes an optional `region`, which may be left out when the
connection lists exactly one region and otherwise must name an allowed one. Every permission a tool needs is listed in
[Set up AWS](setup.md#ses). The tools manage the sender identities (domains and email addresses), the email templates, the contact lists with their
topics, and the contacts of the bound account.

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
| `aws.ses.email-template.list` | `ListEmailTemplates` | read | no |
| `aws.ses.email-template.get` | `GetEmailTemplate` | read | no |
| `aws.ses.email-template.test-render` | `TestRenderEmailTemplate` | read, sends nothing | no |
| `aws.ses.email-template.create` | `CreateEmailTemplate` | create, not repeated | yes |
| `aws.ses.email-template.update` | `UpdateEmailTemplate` | update | yes |
| `aws.ses.email-template.delete` | `DeleteEmailTemplate` | delete, not repeated | yes |
| `aws.ses.contact-list.list` | `ListContactLists` | read | no |
| `aws.ses.contact-list.get` | `GetContactList` | read | no |
| `aws.ses.contact-list.create` | `CreateContactList` | create, not repeated | yes |
| `aws.ses.contact-list.update` | `UpdateContactList` | update | yes |
| `aws.ses.contact-list.delete` | `DeleteContactList` | delete, not repeated | yes |
| `aws.ses.contact.list` | `ListContacts` | read | no |
| `aws.ses.contact.get` | `GetContact` | read | no |
| `aws.ses.contact.create` | `CreateContact` | create, not repeated | yes |
| `aws.ses.contact.update` | `UpdateContact` | update | yes |
| `aws.ses.contact.delete` | `DeleteContact` | delete, not repeated | yes |

`create` of an identity, a template, or a contact list with tags also needs `ses:TagResource`. A create or delete whose
outcome is unclear is not repeated: read the identity, policies, template, list, or contact first. The list tools of
templates, contact lists, and contacts page with `page_size` (at most 100) and `cursor`.

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

## Email templates

A template has a `subject`, a `text`, and an `html` part with `{{placeholders}}`. `create` and `update` check the content
for size only, never for meaning: markup, scripts, and links pass, and no URL in a template is ever fetched. Each of
`html` and `text` is limited to 512000 characters (the 500 KB template limit of SES, which the API model does not
state); more is refused before any request.
`update` replaces the whole content, so give every part to keep. `get` and `test-render` return template content as
untrusted data, and a part longer than 131072 characters is cut, which `truncated` reports. `test-render` takes
`template_data` as JSON text, renders the template, and sends no mail.

## Contact lists and topics

A contact list holds contacts and topics. A topic has a `topic_name`, a `display_name`, a `description`, and a
`default_subscription_status` (`OPT_IN` or `OPT_OUT`) that applies to contacts who state no preference for it.
`contact-list.update` replaces the topics and the description: the topics given replace all existing ones. Deleting a
list with `contact-list.delete` deletes all its contacts too.

## Contacts and privacy

A contact is an email address in one list, with `topic_preferences` (`OPT_IN` or `OPT_OUT` per topic), `unsubscribe_all`,
and `attributes_data`, JSON text. The email address is limited to 254 characters. `contact.update` replaces preferences, state, and
attributes, so what is left out is reset. `contact.list` filters by `filtered_status` and `topic_filter`. Contacts are
added one by one; importing from a file is not offered.

Email addresses and attributes are personal data. They are classified as recipient data (`aws-ses-recipient-data`) in the
tool risk, are returned to the caller by `list` and `get`, and reach the agent's context. Read contacts
only when the task needs them, and keep the list and attribute names free of personal data. Contact attributes, names,
and descriptions are untrusted data.

## Not offered

- No key material of your own (BYODKIM): the selector and the private key are not arguments, and the origin `EXTERNAL`
  is not offered. Easy DKIM keys are generated by SES, with `RSA_1024_BIT` or `RSA_2048_BIT` for the next key.
- `SendCustomVerificationEmail` is never a tool: it would send mail to any address given as an argument. The custom
  verification email templates (`Create`, `Update`, `Delete`, `Get`, and `ListCustomVerificationEmailTemplates`) are not
  tools either, since they only serve that mail.
- No import of contacts from a file (`CreateImportJob`).

## Limits

- An account without production access is in the SES sandbox and sends only to verified recipients and from verified
  identities. The tools neither lift nor check this.
- DKIM and domain verification take time after the records exist. How long, and the sandbox behaviour, have not been
  proven against a live account.
- Everything SES returns, including the identity policies, is untrusted data.

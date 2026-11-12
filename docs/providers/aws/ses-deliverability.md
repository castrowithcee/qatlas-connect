---
description: >
  Describes the SES v2 tools for deliverability: metrics of the Virtual Deliverability Manager with their limits, message and email address insights, blacklist reports, recommendations, the Deliverability Dashboard and its statistics and campaigns, inbox placement tests with the test message they send, reading import and export jobs, identity certificates with the ACM certificate ARN check, third-party data in insights, and the costs.
type: knowledge
edit: shared
created: 2026-11-12
updated: 2026-11-12
---

# SES deliverability, metrics, and jobs

Part of group `ses`; the identity, account, configuration set, template, contact, and send tools are in
[Simple Email Service](ses.md). These tools are regional like the rest: every tool takes an optional `region`, and the
permissions are listed in [Set up AWS](setup.md#ses). Reads need no confirmation; every other tool needs confirmation.

## Tools

| Tool | AWS operation | Effect | Confirmation |
| --- | --- | --- | --- |
| `aws.ses.metrics.batch-get` | `BatchGetMetricData` | read | no |
| `aws.ses.message-insights.get` | `GetMessageInsights` | read | no |
| `aws.ses.email-address-insights.get` | `GetEmailAddressInsights` | read | no |
| `aws.ses.blacklist-report.get` | `GetBlacklistReports` | read | no |
| `aws.ses.domain-statistics-report.get` | `GetDomainStatisticsReport` | read | no |
| `aws.ses.deliverability-campaign.get` | `GetDomainDeliverabilityCampaign` | read | no |
| `aws.ses.deliverability-campaign.list` | `ListDomainDeliverabilityCampaigns` | read | no |
| `aws.ses.recommendation.list` | `ListRecommendations` | read | no |
| `aws.ses.deliverability-dashboard.get` | `GetDeliverabilityDashboardOptions` | read | no |
| `aws.ses.deliverability-dashboard.put-option` | `PutDeliverabilityDashboardOption` | update, paid | yes |
| `aws.ses.deliverability-test-report.get` | `GetDeliverabilityTestReport` | read | no |
| `aws.ses.deliverability-test-report.list` | `ListDeliverabilityTestReports` | read | no |
| `aws.ses.deliverability-test-report.create` | `CreateDeliverabilityTestReport` | create, paid, sends a test message, not repeated | yes |
| `aws.ses.import-job.get` | `GetImportJob` | read | no |
| `aws.ses.import-job.list` | `ListImportJobs` | read | no |
| `aws.ses.export-job.get` | `GetExportJob` | read | no |
| `aws.ses.export-job.list` | `ListExportJobs` | read | no |
| `aws.ses.email-identity-certificate.list` | `ListEmailIdentityCertificates` | read | no |
| `aws.ses.email-identity-certificate.associate` | `AssociateEmailIdentityCertificate` | update | yes |
| `aws.ses.email-identity-certificate.disassociate` | `DisassociateEmailIdentityCertificate` | update | yes |

The list tools page by cursor. The tools shorten the strings and lists of provider answers to a fixed length.

## Metrics

`aws.ses.metrics.batch-get` reads Virtual Deliverability Manager (VDM) metric data of one region. One call carries 1 to
10 queries; each has an `id` (1 to 255 characters), the `namespace` `VDM`, a `metric` (`SEND`, `COMPLAINT`,
`PERMANENT_BOUNCE`, `TRANSIENT_BOUNCE`, `OPEN`, `CLICK`, `DELIVERY`, `DELIVERY_OPEN`, `DELIVERY_CLICK`, or
`DELIVERY_COMPLAINT`), `start_date` and `end_date`, and optionally up to 3 `dimensions` from `EMAIL_IDENTITY`,
`CONFIGURATION_SET`, `ISP`, and `TENANT_NAME` (AWS checks the number and names of the dimensions). The tool refuses a
call before any request when the number of queries is outside 1 to 10, an `id` is outside 1 to 255 characters, a
`metric` or the `namespace` is not one of the listed values, or a date is missing. The API reference names no limit on
the length of the period, so the tool checks none and leaves it to AWS. AWS allows at most 16 calls and 160 queries per
second. The answer has `results` (timestamps and values per query `id`) and `errors` for queries that failed. VDM must
be on (`aws.ses.account.put-vdm-attributes`).

## Insights

`aws.ses.message-insights.get` (by message ID) and `aws.ses.email-address-insights.get` need VDM. The message insights
hold the sender address, the subject, tags, and per recipient the ISP and the delivery events. The address insights send
the address to AWS only, send no mail, and return validation verdicts. Both can carry addresses and subject lines of
third parties; they are untrusted provider content, shortened to a fixed length, and never run or followed. Their data
sensitivity is `aws-ses-third-party-content`, also for the campaign and test report tools below.

## Reputation and the Deliverability Dashboard

- `aws.ses.blacklist-report.get` returns the blacklist entries of IP addresses or domains you list.
- `aws.ses.recommendation.list` pages through the VDM recommendations of a region and takes a `filter` of one or two
  entries from `TYPE`, `IMPACT`, `STATUS`, and `RESOURCE_ARN`.
- `aws.ses.deliverability-dashboard.get` shows whether the dashboard is on, the subscription expiry, and the subscribed
  domains.
- `aws.ses.domain-statistics-report.get` returns the inbox placement statistics of a subscribed domain for a period.
- `aws.ses.deliverability-campaign.list` and `.get` return the campaigns the dashboard found for a subscribed domain,
  with subject and sender address, which are content of mail and can be of third parties.
- `aws.ses.deliverability-test-report.list` and `.get` return the inbox placement test reports.

`aws.ses.deliverability-dashboard.put-option` turns the dashboard on or off and sets the tracked domains. `.create` on
`aws.ses.deliverability-test-report` starts an inbox placement test: **SES sends one test message** with the subject and
body you give, from a sender identity of the account, to mailboxes at AWS test providers, and reports where it lands.
The message cannot be recalled, so the tool needs confirmation and is not repeated after an unclear outcome: list the
reports first. Only simple content (subject and body) is offered; raw content, templates, attachments, and custom
headers are not. The sender address must pass the identity form check of [Simple Email Service](ses.md).

**Costs.** AWS charges for the Deliverability Dashboard subscription and for inbox placement tests, and VDM, with its
metric and insight queries, costs extra on top of the sending price. The tools show the hint in their description and
need confirmation where they start a charge; they do not retrieve any price.

## Import and export jobs

`aws.ses.import-job.list` and `.get`, and `aws.ses.export-job.list` and `.get`, read the jobs of a region: status,
timestamps, counts, destination or source, and failure information, including the S3 locations of the job. Starting or
cancelling a job is not offered. Filter values of an export job (addresses and subjects) are shown as the job holds them
and are untrusted.

## Identity certificates

`aws.ses.email-identity-certificate.list` returns the S/MIME certificates associated with an identity.
`associate` links a certificate of AWS Certificate Manager (ACM) to an identity and an address of it, `disassociate`
removes the link; both need confirmation. For a domain identity `from_address` is required and must belong to the
domain. `certificate_arn` must be the ARN of an ACM certificate in the bound account and in an allowed region of the
connection (`arn:aws:acm:<region>:<account>:certificate/<id>`); another account, region, service, resource type, or
partition is refused before any request. The tools change no certificate in ACM.

## Limits

- Metrics, insights, the dashboard, and the tests have not been proven against a live account, nor have their costs.
- Everything these tools return is untrusted data.

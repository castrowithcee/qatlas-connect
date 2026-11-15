---
description: >
  Describes the Account tools: listing regions with their opt-in status, enabling and disabling opt-in regions, reading and changing the account name, the primary contact, and the alternate contacts, reading the root user email address, the account argument, the handling of personal data, and the excluded email change.
type: knowledge
edit: shared
created: 2026-11-14
updated: 2026-11-14
---

# Account

Group `account`. The tools read and change which regions the bound account has opted in to, and its name and contacts. Account is a global
service: the calls go to `account.us-east-1.amazonaws.com` and ignore the region list of the connection, so the tools
take no `region` argument and a connection needs only its account target. The permissions are listed in
[Set up AWS](setup.md#account). Reads need no confirmation; every change needs confirmation.

## Region tools

| Tool | AWS operation | Effect | Confirmation |
| --- | --- | --- | --- |
| `aws.account.region.list` | `ListRegions` | read | no |
| `aws.account.region.get-opt-status` | `GetRegionOptStatus` | read | no |
| `aws.account.region.enable` | `EnableRegion` | update | yes |
| `aws.account.region.disable` | `DisableRegion` | update | yes |

`region_opt_status` is one of `ENABLED_BY_DEFAULT`, `ENABLED`, `ENABLING`, `DISABLING`, or `DISABLED`. `list` takes
the optional `region_opt_status_contains` (up to five of these values) and pages by cursor with `page_size` from 1 to
50. `get-opt-status`, `enable`, and `disable` take `region_name`, a region of the `aws` partition by form; other
partitions and malformed names are refused before any request.

## The account argument

Every tool accepts an optional `account_id`, except `primary-email.get`, which requires it. It must be the account the connection is bound to; any other value is
refused before any request. AWS accepts the argument only for management and member accounts of an organization, and
without it the call acts on the bound account.

## Opt-in and the region list

Enabling or disabling a region changes the account, not the connection. The tools never change the region list, so
a regional tool uses a newly enabled region only after you add `region/<name>` to the targets of the connection. The
result of `enable` and `disable` carries `allowlist_note`, which says this, and for `disable` whether the region is
still in the list.

The opt-in takes a while: the status goes through `ENABLING` to `ENABLED`, or through `DISABLING` to `DISABLED`.
Read it with `get-opt-status`. Both calls are repeatable; AWS answers a change that is already under way with a
conflict error. Disabling a region removes access to the region and its resources; the tool removes nothing itself.

Region names in answers come from AWS, are untrusted, and are shortened to a fixed length.

## Contacts and account information

| Tool | AWS operation | Effect | Confirmation |
| --- | --- | --- | --- |
| `aws.account.information.get` | `GetAccountInformation` | read | no |
| `aws.account.name.put` | `PutAccountName` | update | yes |
| `aws.account.contact-information.get` | `GetContactInformation` | read | no |
| `aws.account.contact-information.put` | `PutContactInformation` | update | yes |
| `aws.account.alternate-contact.get` | `GetAlternateContact` | read | no |
| `aws.account.alternate-contact.put` | `PutAlternateContact` | update | yes |
| `aws.account.alternate-contact.delete` | `DeleteAlternateContact` | delete | yes |
| `aws.account.primary-email.get` | `GetPrimaryEmail` | read | no |

`information.get` returns `account_id`, `account_name`, `account_created_date`, and `account_state`. `name.put` sets
`account_name`.

`contact-information` is the primary contact for billing and contact: `full_name`, `address_line1` to
`address_line3`, `city`, `state_or_region`, `district_or_county`, `postal_code`, `country_code`, `phone_number`,
`company_name`, and `website_url`. `get` also returns `verification_status` of the phone number. `put` replaces the
contact in full: a member that is left out is removed.

`alternate-contact` takes `alternate_contact_type`: `BILLING`, `OPERATIONS`, or `SECURITY`. `put` sets `name`, `title`,
`email_address`, and `phone_number` for that type. AWS sends the notices of the type to `email_address`; the tool only
stores the contact and sends no mail. Setting the `SECURITY` contact redirects the security notices of AWS to the new
address, so check it first. `get` and `delete` of a type without a contact fail with a not-found error. `put` is
repeatable; `delete` is not.

`primary-email.get` reads the email address of the root user and requires `account_id`, which must be the bound
account. The change of that address (`StartPrimaryEmailUpdate`, `AcceptPrimaryEmailUpdate`) is not a tool, and
neither is the query of its status (`GetPrimaryEmailUpdateStatus`). The verification of phone numbers and the GovCloud
account information are not tools either.

### Personal data

Names, addresses, phone numbers, and email addresses are personal data. The tools return them in the result only;
the invoke log holds no results. A result that you pass on to a chat or a file takes the data with it. Values come
from AWS, are untrusted, and are shortened to a fixed length.

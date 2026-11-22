---
description: >
  Describes the billing tools of the Google Ads provider: billing-setups.list, account-budgets.list, account-budget-proposals.list and payments-accounts.list, which only read, with the manager target rule, the fixed field sets and what each answer holds, the Explorer access level that Google blocks for the billing services, which tool reaches Google through a query and which through PaymentsAccountService, the filters and paging, the binding of resource names to the account, what no tool does (create, change or remove billing, budget proposals or payments accounts), and what was checked against Google's references and what is not verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: billing tools

These tools read how a Google Ads account is paid for: its billing setups, its account budgets, the proposals
for them, and the payments accounts that can pay for it. They add to the [Google Ads provider](README.md), whose
configuration, targets, quotas, and errors apply here too.

| Tool | Effect | Reaches Google through | What it lists |
| --- | --- | --- | --- |
| `google.ads.billing-setups.list` | read | query `FROM billing_setup` | billing setups with their payments account |
| `google.ads.account-budgets.list` | read | query `FROM account_budget` | account budgets with their pending proposal |
| `google.ads.account-budget-proposals.list` | read | query `FROM account_budget_proposal` | proposals to create, change, end or remove an account budget |
| `google.ads.payments-accounts.list` | read | `PaymentsAccountService.ListPaymentsAccounts` | the payments accounts that can pay for the account |

All four belong to the profile `read`.

## What no tool does

The group only reads. No tool and no argument creates, changes, or removes a billing setup, an account budget
proposal, or a payments account: `BillingSetupService` and `AccountBudgetProposalService` are not used, and
there is no tool that proposes a budget, ends one, or sets a spending limit. The tools take no operation, no
`validate_only`, no query, no field list, no resource name, and no header; the field sets and the queries are
fixed in Qatlas, and the only values that reach a query are IDs and values of closed lists that Qatlas
checks first. A test fails when a tool of this group has another effect than reading or takes one of these
arguments. Invoices, incentives, and reservations are not part of this group.

## Manager accounts

Billing belongs to the paying account, and a manager account has its own. On a connection with targets, a manager
account therefore needs a manager target that names it (`managers/ID`) for every tool of this group. A customer
target that names a manager, and a manager that is only reached below a manager target, are refused as
`invalid-request`. Qatlas reads `customer.manager` of the account once to tell, after the membership read when
one is needed, and before the call itself. A connection without targets reaches what its credential reaches and
makes no such read. The order is the one of the [settings tools](settings.md): form of the arguments, targets,
secret, membership, manager check.

## Access level of the Cloud project

Google's page about access levels lists `PaymentsAccountService`, `BillingSetupService`,
`AccountBudgetProposalService`, and `InvoiceService` as restricted services and says that these features need
the Basic or Standard access level; at Explorer they are blocked.

- `payments-accounts.list` calls `PaymentsAccountService`, so it needs at least Basic.
- The other three tools read through `GoogleAdsService.Search`, not through the two billing services that
  Google names, and `AccountBudget` has no service of its own. The page does not say whether Google also blocks
  the query on `billing_setup`, `account_budget`, and `account_budget_proposal` at Explorer. That is not
  verified. Plan for at least Basic here too; the answer of a blocked call is mapped as described under
  [Errors](README.md#errors).

## Filters and paging

The three lists run a search that selects every field of the resource named below, sorted by `id`, and page like
`customer-clients.list`: `page_size` (1 to 1000, default 100) and an opaque `cursor` that is bound to the
arguments, the targets, and `login_customer_id`. Filters are joined with `AND`.

| Tool | Filters |
| --- | --- |
| `billing-setups.list` | `ids` (up to 100), `statuses` (`PENDING`, `APPROVED_HELD`, `APPROVED`, `CANCELLED`) |
| `account-budgets.list` | `ids` (up to 100), `statuses` (`PENDING`, `APPROVED`, `CANCELLED`) |
| `account-budget-proposals.list` | `ids` (up to 100), `statuses` (`PENDING`, `APPROVED_HELD`, `APPROVED`, `CANCELLED`, `REJECTED`), `proposal_types` (`CREATE`, `UPDATE`, `END`, `REMOVE`) |

The values are the values of the enums in v26 without `UNSPECIFIED` and `UNKNOWN`. An answer may still carry a
value that a later version adds; the output schema does not list the values.

`payments-accounts.list` takes `customer_id` only. Google's request has no other member, the answer has no
pages, and Qatlas returns at most 500 payments accounts and sets `truncated` when it cut more. Google returns the
payments accounts of all managers between the `login-customer-id` of the connection and the account, inclusive.

## Fields

Every selectable field of the resource in the fields reference of v26 is returned; none of them is marked
deprecated. Amounts are in micros of the account currency, and `int64` values are strings. A field that Google
does not send is absent.

- **`billing-setups.list`**: `resource_name`, `id`, `status`, `payments_account`, `payments_account_info`
  (`payments_account_id`, `payments_account_name`, `payments_profile_id`, `payments_profile_name`,
  `secondary_payments_profile_id`), `start_date_time`, `end_date_time`, `end_time_type`.
- **`account-budgets.list`**: `resource_name`, `id`, `billing_setup`, `status`, `name`, `proposed_start_date_time`,
  `approved_start_date_time`, `proposed_end_date_time` or `proposed_end_time_type`, `approved_end_date_time` or
  `approved_end_time_type`, `proposed_spending_limit_micros` or `_type`, `approved_spending_limit_micros` or
  `_type`, `adjusted_spending_limit_micros` or `_type`, `total_adjustments_micros`, `amount_served_micros`,
  `purchase_order_number`, `notes`, and `pending_proposal` (`account_budget_proposal`, `proposal_type`, `name`,
  `start_date_time`, `end_date_time` or `end_time_type`, `spending_limit_micros` or `spending_limit_type`,
  `purchase_order_number`, `notes`, `creation_date_time`).
- **`account-budget-proposals.list`**: `resource_name`, `id`, `billing_setup`, `account_budget`, `proposal_type`,
  `status`, `proposed_name`, `proposed_start_date_time`, `approved_start_date_time`, `proposed_end_date_time` or
  `proposed_end_time_type`, `approved_end_date_time` or `approved_end_time_type`,
  `proposed_spending_limit_micros` or `_type`, `approved_spending_limit_micros` or `_type`,
  `proposed_purchase_order_number`, `proposed_notes`, `creation_date_time`, `approval_date_time`.
- **`payments-accounts.list`**: `resource_name`, `payments_account_id`, `name`, `currency_code`,
  `payments_profile_id`, `secondary_payments_profile_id`, `paying_manager_customer`.

**Left out because they cannot be selected.** `BillingSetup.start_time_type` and
`AccountBudgetProposal.proposed_start_time_type` are in Google's message definitions as inputs for creating
the object (a start of "now"), but the fields reference of v26 does not list them, so the query language cannot
return them. They are not returned. Nothing else of the four resources is left out, and no field is left out for
reasons of security or privacy: the payments profile IDs are returned.

**Business data.** The answers hold the names of payments accounts and profiles, purchase order numbers, and
notes that people typed. They hold no card or bank data and no data of other people; the tool descriptions say so.

## What the tools check

- Every resource name in an answer is bound to the account of the call: the resource name of a row must be
  the one built from the account and the row's own ID, and the names of the billing setup, the payments account,
  the account budget, and the pending proposal inside a row must belong to the same account. The ID of a payments
  account is part of its resource name, so both must agree. `paying_manager_customer` names another account by
  design and is only checked for the form `customers/ID`. Anything else is `invalid-provider-response`.
- Rows come in the order asked for, otherwise the answer is `invalid-provider-response`.
- Everything Google returns is untrusted text: control characters become spaces, names and notes are cut at
  1024 and 4096 bytes, codes and IDs at 128 bytes, and the list of payments accounts at 500 entries.
- Qatlas does not check the digit counts that Google documents for a payments account ID (16 digits) and a
  payments profile ID (12 digits); it checks only that they are plain text.

Quota: a list costs one operation of the daily quota per page, `payments-accounts.list` one operation, plus one per
membership read and one for the check that the account is no manager account.

## Boundary

Checked against Google's references of v26 (the field pages of `billing_setup`, `account_budget`, and
`account_budget_proposal`, the message definitions of the three resources and of `PaymentsAccount`, the
definition of `PaymentsAccountService`, and the access level page):

- The three field pages list 12, 32, and 21 fields, all selectable, none deprecated, and all of them are returned;
  this is the only source for the selectable set, which the generated field catalog of the provider repeats.
- `ListPaymentsAccounts` is `GET /v26/customers/{customer_id}/paymentsAccounts` with `customer_id` as its only
  request member and a repeated `payments_accounts` in the answer.

Not verified:

- Whether the Explorer restriction also applies to the query on `billing_setup`, `account_budget`, and
  `account_budget_proposal`, and which error a blocked call gives. No live call has been made.
- Which role an account needs to read billing data. Google does not document it per call.
- Whether the answer of `ListPaymentsAccounts` has a size limit or pages. The reference names none.
- The form of the payments account ID in a resource name; Qatlas accepts letters, digits, `-`, and `_` up to 64
  characters.

Left out on purpose:

- Creating or removing a billing setup, making or withdrawing an account budget proposal, and everything else
  that changes billing or spending.
- Invoices, incentives, and reservations.
- `start_time_type` and `proposed_start_time_type`, which cannot be selected.
- Any free query, field list, path, or header.

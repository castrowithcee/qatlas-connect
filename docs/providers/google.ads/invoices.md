---
description: >
  Describes the Google Ads tools invoices.list, incentives.fetch and third-party-app-analytics-links.list: what they read, the arguments Google requires for invoices, the rule that invoices and app analytics links need a manager target on manager accounts, why incentives.fetch is not bound to an account, the Explorer access level lock of InvoiceService, the withheld PDF address and shareable link ID, the fields left out, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: invoices, incentives and app analytics links

These tools read the invoices Google issued for an account, the advertising credit that Google offers the user, and
the third-party app analytics links of an account. All three only read. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.invoices.list` | read | lists the invoices of one billing setup for one month |
| `google.ads.incentives.fetch` | read | fetches the incentive offer Google makes to the user of the credential |
| `google.ads.third-party-app-analytics-links.list` | read | lists the third-party app analytics links of an account |

All three belong to the profile `read`. No tool changes anything here.

## What is left out on purpose

- **No incentive is applied.** `IncentiveService.ApplyIncentive` books credit to an account and is not offered.
  `incentives.fetch` returns the `incentive_id` of each option, which only that call would take.
- **The shareable link ID is never read.** `third_party_app_analytics_link.shareable_link_id` is the value a
  third party is given to attach its app analytics to the account, so it grants access. The fixed query of
  `third-party-app-analytics-links.list` selects `resource_name` alone, the answer is decoded into a structure
  without a member for the value, no argument can name a field or a filter, and an answer that carries it anyway
  is not copied. It is also withheld from `reports.search` ([report tools](reports.md)).
  `ThirdPartyAppAnalyticsLinkService.RegenerateShareableLinkId` is not offered either.
- **The PDF address of an invoice is not returned.** `Invoice.pdf_url` links to a PDF copy of the invoice; Google
  says it is fetched with an OAuth token of the same Google account. Nothing read for this package shows that the
  address is free of access values, so it is treated as one: it is read only to set `has_pdf`, and never copied
  to a result, an error, or a log. The invoice figures are all in the answer.
- **No free resource name.** `billing_setup_id` is a number. Qatlas builds
  `customers/ID/billingSetups/N` from the checked `customer_id` and this number, and refuses an answer whose
  invoices name another account or another billing setup.
- **No free query.** The queries and the parameters are fixed; no argument takes a field, a condition, a path, or
  a header.

## Access level and rights

- **`InvoiceService` is blocked at the Explorer access level.** Google's page about access levels lists it with
  the billing services; the Cloud project needs at least Basic access. That page names neither `IncentiveService`
  nor `ThirdPartyAppAnalyticsLinkService`, so no Explorer lock is stated for them; secondary summaries that count
  `IncentiveService` among the blocked billing services are not borne out by the page.
- **Invoices exist only for accounts with monthly invoicing.** Google answers `NOT_INVOICED_CUSTOMER` otherwise,
  and `YEAR_MONTH_TOO_OLD` for a month before January 2019. Google documents that when `login-customer-id` is set
  it must be the manager account that pays for the account. Set the service option `login_customer_id` to it.
- **Which role** each call needs is not documented; the invoice guide names `ACTION_NOT_PERMITTED` for a user
  without the right to view the invoices of the billing setup.

## Manager accounts

Invoices are billing data, and the app analytics links belong to the connections of an account. On a connection with
targets, a manager account therefore needs a manager target that names it (`managers/ID`) for `invoices.list` and
`third-party-app-analytics-links.list`. A customer target, and a manager account that is only reached below a
manager target, are refused as `invalid-request` before the tool reads anything of the account. To tell, Qatlas
reads `customer.manager` of the account once. A connection without targets reaches what its credential reaches and
makes no such read. This is the narrower reading of the rule for tools that act on a manager account.

`incentives.fetch` is different: the offer belongs to the user of the credential, not to an account. The call takes
no `customer_id`, sends no `login-customer-id`, and so no target can be checked against it. Like
`customers.list-accessible` it is therefore not bound to the targets of a connection.

## invoices.list

Calls `InvoiceService.ListInvoices`, `GET customers/ID/invoices`. Google requires all of these, and so does the
tool:

| Argument | Google parameter | Rule |
| --- | --- | --- |
| `customer_id` | path | ten digits; a target, or below a manager target |
| `billing_setup_id` | `billing_setup` | positive whole number, the end of `customers/ID/billingSetups/N` |
| `issue_year` | `issue_year` | whole number 2019 to 9999, sent as four digits |
| `issue_month` | `issue_month` | `JANUARY` to `DECEMBER` (`UNSPECIFIED` and `UNKNOWN` are no months) |

`include_granular_level_invoice_details` is optional and false by default. When true, Google adds the
per-campaign costs (`campaign_summaries`), the itemized regulatory costs (`regulatory_cost_summaries`) and the
adjustments (`adjustment_summaries`).

The answer has no page token. It holds every documented field of `Invoice` except the PDF address: `resource_name`,
`id`, `type`, `billing_setup`, `payments_account_id`, `payments_profile_id`, `issue_date`, `due_date`,
`service_date_range`, `currency_code`, the amounts in micros (`adjustments_*`, `regulatory_costs_*`,
`export_charge_*`, `subtotal`, `tax`, `total`), `corrected_invoice`, `replaced_invoices`, `account_budget_summaries`
and `account_summaries` with all their members, and `has_pdf`. Amounts are micros, millionths of the currency unit,
returned as text because they are 64-bit numbers. Every list is cut at 500 entries, and `truncated` says when one
was. Text is cut and control characters are replaced.

Every invoice must carry the resource name `customers/ID/invoices/...` of the account asked for and, when it names
a billing setup, the one asked for; references to accounts, budgets and invoices must have the form of their kind.
Anything else makes the answer `invalid-provider-response`. With consolidated billing Google lists invoices and
entries for every account under the billing setup, so `customer` and `customer_descriptive_name` in the summaries
can name accounts outside the targets. They are third-party data and are returned as they are.

## incentives.fetch

Calls `IncentiveService.FetchIncentive`, `GET incentives:fetchIncentive`. No parameter is required. Optional:
`language_code` (a language tag such as `de-DE`; Google defaults to English), `country_code` (two letters; Google
defaults to US), `incentive_type` (`ACQUISITION`, the only value), and `email`. Google documents `email` for channel
partners that do not use OAuth; this provider always does, so there is no reason to set it. It is personal data and is
checked like the address filters of the [user access tools](user-access.md).

The answer holds `incentive_offer` with `consolidated_terms_and_conditions_url`, `offer_type` (`NO_INCENTIVE` or
`CYO_INCENTIVE`) and, for a choose-your-own offer, `cyo_incentives` with `low_offer`, `medium_offer` and
`high_offer`. Each has `incentive_id`, `spend` (`award_amount` and `required_amount` as `currency_code`, `units`
and `nanos`), `incentive_terms_and_conditions_url` and `incentive_type`. A terms address is returned only when it
is a plain `https` address without user information; anything else is dropped.

## third-party-app-analytics-links.list

Runs the fixed search `SELECT third_party_app_analytics_link.resource_name FROM third_party_app_analytics_link`.
The resource has two fields, `resource_name` and `shareable_link_id`; the second is withheld, so the answer holds
for each link its `resource_name` (`customers/ID/thirdPartyAppAnalyticsLinks/LINK_ID`) and `id`. Rows are sorted by
ID in the tool, since Google offers no order on the name, and cut at 500 with `truncated`. The provider, the app and
the vendor of the connection are fields of the account link of type third-party app analytics, which this tool does
not read. Each row must carry a name of the account asked for.

## Boundary

Checked against the proto files of v26 (`invoice_service`, `invoice`, `incentive_service`,
`third_party_app_analytics_link`, `month_of_year`, `incentive_type`, `incentive_offer_type`), the invoice guide, and
the page about access levels:

- `ListInvoices` requires `customer_id`, `billing_setup`, `issue_year` and `issue_month`; the year is a four-digit
  string and invoices before 2019 do not exist.
- `FetchIncentive` is not a method of an account: its path has no customer, and none of its parameters is required.
- The method pages of the REST reference could not be read; the proto files and guides above were the source.
- The Explorer lock names `InvoiceService` only, see above.

Not verified: that Google accepts the single-field search on `third_party_app_analytics_link` and returns rows for
accounts that have a link, which role each call needs, whether the `pdf_url` of an invoice holds an access value,
and the behavior of the offers for accounts that are not eligible. No live call has been made.

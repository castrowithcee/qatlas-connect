---
description: >
  Describes the link read tools of the Google Ads provider: account-links.list, product-links.list, product-link-invitations.list and data-links.list, with the fixed field set of each query, the structured filters, the manager target rule, paging, the identifiers of linked accounts that can lie outside the targets, the fields that are not offered, what was checked against Google's field reference, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: link read tools

These tools list what a Google Ads account is linked to: app analytics providers, products such as Merchant
Center, invitations to link a product, and data such as YouTube videos. They only read. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | Lists |
| --- | --- | --- |
| `google.ads.account-links.list` | read | links to third-party app analytics providers |
| `google.ads.product-links.list` | read | links to products: Merchant Center, data partners, Google Ads accounts, advertising partners, Hotel Center |
| `google.ads.product-link-invitations.list` | read | invitations to link a product |
| `google.ads.data-links.list` | read | links to data, today YouTube videos |

All four belong to the `read` profile. No tool creates, accepts, changes, or removes a link or an invitation.

## Fixed field sets

Each tool runs one GAQL search on one resource (`account_link`, `product_link`, `product_link_invitation`,
`data_link`) and selects a set of fields that is defined in the code. No argument adds a field, names a resource,
or passes a condition, an order, a limit, or a query: the only inputs are `customer_id`, the structured filters
below, `page_size`, and `cursor`, and the input schemas accept nothing else. Qatlas builds the query from them.
A test holds each set against the field reference of the provider, so a field that v26 lets a query select is
either in the set or the test fails.

Every filter is a condition `AND`-joined with the others. Enum filters take a list of values of the enum, each at
most once; ID filters take up to 100 positive whole numbers; text filters take a value of the form that Google
documents for the field, without quotes, backslashes, spaces, or control characters, and match it exactly.
Qatlas does not escape GAQL strings, so a value outside that form is refused as `invalid-request` before a
secret is read.

| Tool | Filters |
| --- | --- |
| `account-links.list` | `statuses`, `types`, `account_link_ids`, `app_vendors`, `app_id`, `app_analytics_provider_ids` |
| `product-links.list` | `types`, `product_link_ids`, `data_partner_ids`, `merchant_center_ids`, `google_ads_customer_id`, `advertising_partner_customer_id`, `allowed_domain` |
| `product-link-invitations.list` | `statuses`, `types`, `product_link_invitation_ids`, `merchant_center_ids`, `hotel_center_ids`, `advertising_partner_customer_id`, `allowed_domain` |
| `data-links.list` | `statuses`, `types`, `data_link_ids`, `product_link_ids`, `channel_id`, `video_id` |

The values and the fields of each answer are in the descriptor of the tool. A product link has no status field
in v26; a link that exists is active.

## Paging

Rows come sorted by their ID, and `page_size` (1 to 1000, default 100) with an opaque `cursor` pages through
them like `customer-clients.list`: the cursor is bound to the tool, the arguments, the targets, and
`login_customer_id`. A data link is named by its product link and its own ID, so `data-links.list` sorts by both.
Because GAQL has no `OR`, a page after the first reads the rest of the product link of the cursor and, when
that does not fill the page, the product links that follow, which costs a second operation.

## Manager accounts

On a connection with targets, a manager account needs a manager target that names it (`managers/ID`) for all four
tools, as for the user access and invitation tools. A customer target that names a manager, and a manager that is
only reached below a manager target, are refused as `invalid-request` before the tool reads anything of the
account; Qatlas reads `customer.manager` of the account once to tell. A connection without targets makes no such
read. This is the narrower reading of the rule for tools that act on a manager account: Google documents no
difference between reading and changing here.

## What is bound to the account, and what is not

A row must name the queried account and its own ID(s) in its resource name; otherwise the answer is
`invalid-provider-response`, as is a row out of the requested order or an ID that is not a positive whole number.
The identifier of the account on the other side of a link (`google_ads.customer`, `advertising_partner.customer`)
is returned as `customers/ID` and checked for its form only: it names another account and can lie outside the
targets, like `google_ads_conversion_customer` of `customers.get`. Text is untrusted and cut as in the README.

## Not offered

- `ThirdPartyAppAnalyticsLink.shareable_link_id` is a value that gives access to a third party. It belongs to
  another resource and is not selected here; no tool of this group reads it.
- `data_link.youtube_link_metadata` and `brand_channel_id` of the YouTube identifier are no selectable fields of
  the field reference of v26 and are not selected.
- Linked accounts, products, and data are only identifiers: no tool calls Google Merchant Center, YouTube, or
  another service to resolve them, and none fetches an address.

## Boundary

Checked against the field reference and the message pages of Google for v26 (`AccountLink`, `ProductLink`,
`ProductLinkInvitation`, `DataLink`, and their field pages):

- The status values match the ones the field reference gives: `account_link.status` (`ENABLED`, `PENDING_APPROVAL`,
  `REJECTED`, `REMOVED`, `REQUESTED`, `REVOKED`), `product_link_invitation.status` (`ACCEPTED`, `EXPIRED`,
  `PENDING_APPROVAL`, `REJECTED`, `REQUESTED`, `REVOKED`), and `data_link.status` (`DISABLED`, `ENABLED`,
  `PENDING_APPROVAL`, `REJECTED`, `REQUESTED`, `REVOKED`). A test compares the lists of the tools with the catalog.
- The product link types include `HOTEL_CENTER`, which a summary of the data partner, Google Ads, and Merchant
  Center links does not name, and invitations carry a Hotel Center ID. Both are listed.
- The YouTube identifier has `channel_id` and `video_id` as selectable fields; `brand_channel_id`, which a
  summary names, is not among them. The field reference wins.
- No field of the four resources is marked deprecated on the pages read.
- The field pages state no condition that a query on these resources must carry.

Not verified: that Google accepts the queries as built and returns the messages as the field pages describe (no
live call has been made), which role an account needs to read links, whether the services are available at the
Explorer access level, how many rows a real account holds, and whether Google compares the text filters
case-sensitively.

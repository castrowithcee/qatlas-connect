---
description: >
  Describes the link tools of the Google Ads provider: the read tools account-links.list, product-links.list, product-link-invitations.list and data-links.list with the fixed field set of each query, the structured filters, the manager target rule, paging, the identifiers of linked accounts that can lie outside the targets, and the tools that end or step back from a link or invitation, account-links.remove, product-links.remove, data-links.remove, product-link-invitations.update and data-links.update, with the methods behind them, the statuses they set, where validate_only exists, their effects, the fields and actions that are not offered, what was checked against Google's references, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: link tools

These tools list what a Google Ads account is linked to, and end or step back from such a link: app analytics
providers, products such as Merchant Center, invitations to link a product, and data such as YouTube videos.
They add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here
too. The four list tools only read; the five tools in [Ending links](#ending-links) change an account.

| Tool | Effect | Lists |
| --- | --- | --- |
| `google.ads.account-links.list` | read | links to third-party app analytics providers |
| `google.ads.product-links.list` | read | links to products: Merchant Center, data partners, Google Ads accounts, advertising partners, Hotel Center |
| `google.ads.product-link-invitations.list` | read | invitations to link a product |
| `google.ads.data-links.list` | read | links to data, today YouTube videos |

All four belong to the `read` profile.

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
list tools and the five tools below, as for the user access and invitation tools. A customer target that names a manager, and a manager that is
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

## Ending links

| Tool | Effect | Method | What it does |
| --- | --- | --- | --- |
| `google.ads.account-links.remove` | delete | `AccountLinkService.MutateAccountLink` | removes a link to an app analytics provider |
| `google.ads.product-links.remove` | delete | `ProductLinkService.RemoveProductLink` | removes a link to a product or account |
| `google.ads.data-links.remove` | delete | `DataLinkService.RemoveDataLink` | removes a link to data such as a YouTube video |
| `google.ads.product-link-invitations.update` | update | `ProductLinkInvitationService.UpdateProductLinkInvitation` | sets the status `REJECTED` or `REVOKED` of an invitation |
| `google.ads.data-links.update` | update | `DataLinkService.UpdateDataLink` | sets the status `DISABLED`, `REVOKED`, or `REJECTED` of a data link |

The three `remove` tools are offered only through the `tools` list of a connection. The two `update` tools are
offered by the effect `update` or the `tools` list. All five need the confirmation of every call and belong to
no profile. Each names one object by its IDs, as the list tools return them (`account_link_id`,
`product_link_id`, `product_link_invitation_id`, and for a data link `product_link_id` with `data_link_id`), and
Qatlas builds the resource name from them and the checked `customer_id`. A resource name, a status other than the
ones above, an operation list, or a header is no argument. On a connection with targets, `customer_id` must be a
target or lie below a manager target, and a manager account needs a manager target that names it; both are
checked before a secret is read.

Each call sends one request that changes exactly one object, so it is atomic, without `partial_failure`. It is
sent once and never repeated: after a timeout, a cut connection, a server error, or an answer that does not name
the object asked for, the error says that the outcome is unknown, and the object is to be read with the list
before it is changed again. The answer names the resource name Google returned, which must be that of the object
asked for; an update adds the status that was set.

`validate_only` exists where the method has it: `account-links.remove` and `product-links.remove`. With it,
Google validates and answers errors only, and the answer has no `resource_name`. `product-link-invitations.update`,
`data-links.remove`, and `data-links.update` have no `validate_only` in v26, so their tools have none, and the
call changes the object. `AccountLinkService.MutateAccountLink` takes a single `operation` rather than a list.

The statuses are only steps back. `REJECTED` and `REVOKED` end an invitation (the invitee declines, the sender
withdraws), and `DISABLED`, `REVOKED`, and `REJECTED` end or decline a data link. The method pages of Google do
not list which transitions a method accepts, so Google decides, and a transition it refuses changes nothing.
Removing a link or ending one can stop what it provides, such as product data for ads, and no tool brings a
removed link or invitation back.

### Not offered

- No tool creates a link or an invitation, accepts one, or sets `ACCEPTED`, `ENABLED`, `PENDING_APPROVAL`, or
  `REQUESTED`: `CreateAccountLink`, `CreateProductLink`, `CreateProductLinkInvitation`, `CreateDataLink`, and the
  update of an account link are not offered, and neither are `RemoveProductLinkInvitation` and `MoveManagerLink`.
  Creating a data link shares the account name and ID with a third party, which Google documents.
- `ThirdPartyAppAnalyticsLinkService.RegenerateShareableLinkId` is not offered, and `shareable_link_id` is not
  read.

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

For the tools that end links, checked against the service definitions of v26 (`account_link_service.proto`,
`product_link_service.proto`, `product_link_invitation_service.proto`, `data_link_service.proto`) and the
status enums:

- `MutateAccountLink`, `RemoveProductLink`, `UpdateProductLinkInvitation`, `UpdateDataLink`, and `RemoveDataLink`
  are single methods without an `operations` list. A request holds one object, so the fixed bound is one.
- Only `RemoveProductLink` and `MutateAccountLink` have `validate_only`.
- `MutateAccountLink` has `partial_failure`, which no tool sets. Its description says that creating through it is
  not supported.
- The required fields are the customer ID and the resource name, plus the status for the two updates; the update
  methods have no field mask.

Not verified: that Google accepts the queries as built and returns the messages as the field pages describe (no
live call has been made), which role an account needs to read links, whether the services are available at the
Explorer access level, how many rows a real account holds, and whether Google compares the text filters
case-sensitively. For the tools that end links: which status transitions and which account roles Google accepts
(the method pages name neither), whether the REST answers hold the shapes of the service definitions, and the
effect of a removal on a live account (no live call has been made).

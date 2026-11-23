---
description: >
  Describes the Google Ads tools for manager and client links: customer-manager-links.list and update, customer-client-links.list and update, with what they read and change, the statuses an update may set per side (INACTIVE and REFUSED on the account, CANCELED and hidden on the manager), that no tool creates, accepts or moves a link, the rule that all four need a manager target on manager accounts, the filtering of other ends under a customer target, validate_only, the mutation path, confirmation, tools list requirement, the unknown outcome, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: manager and client links

A link ties a Google Ads account to a manager account. Google shows one link from both ends: as a
`customer_manager_link` on the managed account and as a `customer_client_link` on the manager. These tools read
both and let the two ends turn a link down. They add to the read tools of the [Google Ads provider](README.md),
whose configuration, targets, quotas, and errors apply here too. What every changing tool does (tools list,
confirmation, one atomic request, no repetition, unknown outcome) is described for the
[settings tools](settings.md); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customer-manager-links.list` | read | lists the manager links of an account |
| `google.ads.customer-manager-links.update` | update | declines (`REFUSED`) or ends (`INACTIVE`) one manager link |
| `google.ads.customer-client-links.list` | read | lists the client links of a manager account |
| `google.ads.customer-client-links.update` | update | withdraws a request (`CANCELED`) and hides or shows a client link (`hidden`) |

The two list tools belong to the profile `read`. The two update tools belong to no profile: a connection offers
them only when its `tools` list names each one.

## What is left out on purpose

- **No link is made or accepted.** There is no tool and no argument that creates a `CustomerClientLink`
  (a manager's request) and none that sets `ACTIVE` (accepting a request) or `PENDING`. `MoveManagerLink`, which
  gives a manager access to an account, is not offered either. A link gives a manager account access to an
  account, which Qatlas Connect never grants.
- **Only the statuses that turn a link down are reachable.** The input schema of each update tool lists the
  permitted values only, and the handler checks them again before a secret is read or a request is made; any
  other value is `invalid-request` and makes no request.

  | Tool | Reachable | Not reachable |
  | --- | --- | --- |
  | `customer-manager-links.update` | `status` `INACTIVE`, `REFUSED` | `ACTIVE`, `PENDING`, `CANCELED`, every other value, `hidden` |
  | `customer-client-links.update` | `status` `CANCELED`, `hidden` (true or false), both together | `ACTIVE`, `PENDING`, `INACTIVE`, `REFUSED`, every other value |

- **No resource name, operation, or field mask is an argument.** The name is built from the checked
  `customer_id`, the other account, and `manager_link_id`, and the field mask from the arguments that are given.
- **No `partial_failure`, no `response_content_type`.** Neither service has them in v26. Each request is atomic
  with exactly one operation.

## The two sides

Google's enum gives each status its side: `REFUSED` is the answer of the account that was asked, `INACTIVE` ends
a link in effect from the account's side, and `CANCELED` is a manager's withdrawal of a request that has not been
answered. Google's guide on linking manager accounts documents `REFUSED` and `INACTIVE` for
`CustomerManagerLinkService` (called as the managed account) and the request with `PENDING` for
`CustomerClientLinkService` (called as the manager). It does not document `CANCELED` or `INACTIVE` as values of an
update through `CustomerClientLinkService`, and not `CANCELED` through `CustomerManagerLinkService`. Qatlas takes
the narrower reading:

- On the managed account (`customer-manager-links.update`, `customer_id` is the managed account) it offers
  `REFUSED` and `INACTIVE`. `INACTIVE` removes the manager's access, which can include the access of the
  credential in use.
- On the manager (`customer-client-links.update`, `customer_id` is the manager account) it offers `CANCELED`
  and `hidden`. Ending an active link from the manager's side with `INACTIVE` is not offered, since Google does
  not document it for this service. A side that Google would refuse anyway is not tried: a status that is not
  allowed for the link is refused by Google (`ManagerLinkError`, for example `INVALID_STATUS_CHANGE`) and
  reported as `provider-error` with the note that nothing was changed.

`hidden` only decides whether the link shows in the Google Ads interface; it does not change the access.

## Manager accounts

Links are access control. On a connection with targets, a manager account therefore needs a manager target that
names it (`managers/ID`) for all four tools, the read tools included. A customer target, and a manager account
that is only reached below a manager target, are refused as `invalid-request` before the tool reads or changes
anything of the account. To tell, Qatlas reads `customer.manager` of the account once; a connection without
targets reaches what its credential reaches and makes no such read. This is the same rule as for the
[invitation and approval tools](access.md).

A list shows the account at the other end of each link. On a connection whose only reach to the account is a
customer target, a row appears only when a target names the other account. Below a manager target and without
targets every row is shown. The filter runs after the page is read, so a page can hold fewer rows than
`page_size`; `has_more` and `next_cursor` still describe the unfiltered rows.

## customer-manager-links.list

Runs a GAQL search `FROM customer_manager_link` with all its fields: `resource_name`, `manager_link_id`,
`manager_customer`, and `status` (`PENDING`, `ACTIVE`, `INACTIVE`, `REFUSED`, `CANCELED`). Each row is returned
as `resource_name` (`customers/ID/customerManagerLinks/MANAGER_ID~LINK_ID`), `manager_link_id`,
`manager_customer` (`customers/MANAGER_ID`), `manager_customer_id`, and `status`. Rows come sorted by
`manager_link_id`. Filters, joined with `AND`: `statuses`, `manager_link_ids` (at most 100),
`manager_customer_ids` (at most 100 ten-digit IDs). `page_size` (1 to 1000, default 100) and `cursor` page through
the rows as for `customer-clients.list`: the cursor holds the last link ID and is bound to the arguments, the
targets, and `login_customer_id`. A row out of order, with another account's name, with a counterpart that is not
an account name, or without a positive link ID is `invalid-provider-response`.

## customer-client-links.list

Runs a GAQL search `FROM customer_client_link` with all its fields: `resource_name`, `manager_link_id`,
`client_customer`, `status`, and `hidden`. Each row is returned as `resource_name`
(`customers/ID/customerClientLinks/CLIENT_ID~LINK_ID`), `manager_link_id`, `client_customer`
(`customers/CLIENT_ID`), `client_customer_id`, `status`, and `hidden`. Filters: `statuses`, `manager_link_ids`,
`client_customer_ids` (at most 100 each), and `hidden`. Sorting, paging, and row checks are as above.

## customer-manager-links.update

Calls `CustomerManagerLinkService.MutateCustomerManagerLink` with one operation in `operations[]`: an `update` of
`status` with the field mask `status`. The arguments are `customer_id` (the managed account), `manager_customer_id`
(the manager at the other end), `manager_link_id`, `status`, and `validate_only`. With `validate_only` Google
validates and answers without a result; the tool then answers without `resource_name`. The answer must hold
exactly one result with the name of the link of the call (or, with `validate_only`, none or an empty one);
anything else is `invalid-provider-response` with the note that the outcome is unknown. A manager account cannot be
linked to itself, so `customer_id` and the other account must differ.

## customer-client-links.update

Calls `CustomerClientLinkService.MutateCustomerClientLink` with one `operation` (this service takes a single
operation, not a list): an `update` with the field mask of the given fields, `status`, `hidden`, or `status,hidden`.
At least one of the two is required. The arguments are `customer_id` (the manager account), `client_customer_id`,
`manager_link_id`, `status`, `hidden`, and `validate_only`. The answer holds one `result` with the link name,
which must be that of the call.

## Boundary

Checked against the reference of Google for v26 (the proto files `customer_manager_link_service.proto` and
`customer_client_link_service.proto`, the resources `CustomerManagerLink` and `CustomerClientLink`, the enum
`ManagerLinkStatus`, the error enum `ManagerLinkError`, and the guide on linking manager accounts):

- `MutateCustomerManagerLinkRequest` holds `customer_id`, `operations[]` (required) and `validate_only`;
  `MutateCustomerClientLinkRequest` holds `customer_id`, one `operation` (required) and `validate_only`. Neither
  has `partial_failure` or `response_content_type`. Google's reference states no upper bound for the operations
  of the manager link request; the tool sends exactly one.
- `CustomerManagerLinkOperation` and `CustomerClientLinkOperation` carry `update_mask` and an `update` whose
  resource name must be valid; only the client link operation has `create`, which is not offered.
- The resource names are `customers/ID/customerManagerLinks/MANAGER_ID~LINK_ID` and
  `customers/ID/customerClientLinks/CLIENT_ID~LINK_ID`; `manager_link_id`, `manager_customer`, and the
  resource name of both resources are output only or immutable, so they are read and never set.
- `CREATING_ENABLED_LINK_NOT_ALLOWED` shows that Google itself refuses to create a link as `ACTIVE`; `INVALID_STATUS_CHANGE`
  is the refusal of a status change that the link's state does not allow.
- Google's guide documents ending and declining on the managed account and the request on the manager. The
  sides in the table above for `CANCELED`, `hidden`, and `INACTIVE` on the manager rest on the status enum text
  and the resource fields, not on a statement of the guide for the update calls. That is why the tools offer
  only the statuses of each side that the enum assigns to it.

Google's access level page names `CustomerManagerLinkService` and `CustomerClientLinkService` among the services
that projects at the Explorer access level cannot use, so the two update tools need at least Basic.

Not verified: whether that block also stops the GAQL reads of the two lists, that Google accepts the cut-down GAQL of the two lists as described (no live call has been made),
which role the calls need (Google documents none for these services), whether `CANCELED` or `INACTIVE` is accepted
for a client link update in practice, and what `validate_only` returns in its result for these two services.

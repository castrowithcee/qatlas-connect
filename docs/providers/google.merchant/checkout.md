---
description: >
  Describes the Google Merchant tools for the checkout settings of an account: reading them, enrolling an account, changing the destinations and the checkout or cart template, deleting the settings, who may use them, the arguments and the update mask, the https check of the templates, the review states, the delayed processing, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant checkout settings

The checkout settings of a Merchant Center account enroll it in the Checkout program: buyers who find a product on
free listings or in Shopping ads can go straight to the checkout or the cart page of the shop. The settings name
where the links show and, for the whole account, the one address template that builds them. Four tools cover
them. Targets, quotas, results, and errors are as described in the [provider overview](README.md).

| Tool | Method | Effect |
| --- | --- | --- |
| `google.merchant.checkout-settings.get` | `accounts.programs.checkoutSettings.get` | reads the settings |
| `google.merchant.checkout-settings.create` | `accounts.programs.checkoutSettings.create` | enrolls the account |
| `google.merchant.checkout-settings.update` | `accounts.programs.checkoutSettings.patch` | changes members of the settings |
| `google.merchant.checkout-settings.delete` | `accounts.programs.checkoutSettings.delete` | deletes the settings and unenrolls the account |

## Access

`get` is a read and belongs to the profile `read`. `create`, `update`, and `delete` are offered only to a connection
whose `tools` list names them, no effect profile selects them, and they need a confirmation on every call: the
templates steer customers to a shop, and `delete` removes the checkout links from free listings and Shopping ads.
Google documents no Merchant Center role for these methods; a missing role is answered with `403`, which Qatlas
reports as `permission`. Google documents that the account must take part in Shopping ads, free listings, or
both, and that the feature applies to products sold in the United States.

The `account` argument is the account ID. The three changes require it, `get` takes it from a connection with
exactly one target. It is checked against the targets before any secret is read or a request is sent. An account
that no target names is read or changed only after the proof read of the sub-accounts of a bound advanced
account, as for `accounts.get`; a refused account is never changed. An account target is enough: these tools act on
one account, not on the advanced account as a whole.

The program is always `checkout`, part of every path and no argument. The path of every request is built from the
checked account ID.

## Arguments

Every writable member of the resource is a structured argument:

| Argument | Meaning |
| --- | --- |
| `eligible_destinations` | `SHOPPING_ADS`, `FREE_LISTINGS`, or both, each once; required on create |
| `uri_settings` | exactly one of `checkout_uri_template` (to the checkout page with the item) and `cart_uri_template` (to the cart page with the item) |

`enrollmentState`, `reviewState`, `effectiveEnrollmentState`, `effectiveReviewState`, `effectiveUriSettings`, and the
resource name are output only and are no arguments. `uri_settings` may be left out on create to select the
destinations only, for products that carry their own checkout links.

`update` sends only the members it is given and names exactly those in the update mask (`eligibleDestinations`,
`uriSettings`), so every other member stays as it is. At least one member is required. A member that is passed
replaces the whole member: `eligible_destinations` replaces the list, and `uri_settings` replaces the template, so
passing the other kind of template switches it. There is no free mask. The settings cannot be cleared by `update`;
`delete` removes them.

`delete` takes no argument besides the account. Google answers with an empty object; the tool answers with the
account ID and `deleted: true`, which only tells that Google accepted the deletion.

## Templates

A template (`uri_settings.checkout_uri_template` or `uri_settings.cart_uri_template`) must be an absolute `https`
address with a host, without user information, white space, control characters, or backslashes, at most 2048
characters. Qatlas refuses anything else, an empty value, or both kinds at once before a secret is read or a request
is sent. Placeholders pass: Google replaces `{id}` with the offer ID of the product, as in
`https://shop.example.invalid/checkout?item_id={id}`. Google's guide asks for this placeholder and spells it `{ID}`
once, while every example uses `{id}`. Qatlas does not require a placeholder, because one of Google's code samples
has none, and does not check the spelling; Google refuses a template that does not fit its rules. Google also
requires that the domain of the template matches the domain of the products, that the page works with `GET`, and
that it needs no sign-in; Qatlas checks none of this. Google documents no length for a template; 2048 is the bound
of Qatlas and of a shown result. Templates are text handed to Google and never fetched.

An account-level template and the per-product `checkout_link_template` attribute of the feed exclude each other;
use one of them.

## Results

The settings show `name`, `eligibleDestinations`, `uriSettings` and `effectiveUriSettings` (`checkoutUriTemplate` or
`cartUriTemplate`), `enrollmentState`, `reviewState`, `effectiveEnrollmentState`, and `effectiveReviewState`.
The effective members are those inherited from the parent account of a marketplace seller. Everything is untrusted
content of whoever runs the account: texts are cut, members that are not documented are dropped, and templates are
text that is never fetched. The states are shown as Google names them: `INACTIVE`, `ENROLLED`, `OPTED_OUT` for
the enrollment, and `IN_REVIEW`, `APPROVED`, `DISAPPROVED` for the review. A review can begin only for an enrolled
account.

## Delayed processing and no repetition

The answer of `create` and `update` only tells that Google accepted the change. Google reviews the template, and the
review state is `IN_REVIEW` after every change of an address; Google documents no duration. Each call counts against
the quota of the Merchant API, a failed one too. Qatlas sends a change once and never repeats it, also not after a
renewed token. A failure that may have reached Google, such as a server error, a lost connection, or an unreadable
answer, says that the outcome is unknown: read the settings before changing them again. A `4xx` answer rejected the
request before any change.

## Boundary

Checked against the reference pages of the methods and the guide to the checkout settings in the accounts API v1:
the collection path is `accounts/{account}/programs/{program}/checkoutSettings`, the only program is `checkout`,
`create` is a `POST` whose body is the settings and whose parent is the program, `patch` is a `PATCH` with a
required update mask whose supported members are `eligibleDestinations` and `uriSettings`, in snake case and in
lower camel case, `get` and `delete` have no parameters besides the name and no body, `delete` answers an empty
object, and every method uses the scope `content`. `eligibleDestinations` is required on create. `uriSettings`
holds exactly one of its two members.

Where the pages differ, Qatlas follows the more cautious reading:

- The overview of the resource lists the two destinations as the valid values, and the reference of the type is
  not published; Qatlas offers only `SHOPPING_ADS` and `FREE_LISTINGS`.
- The guide spells the required placeholder `{ID}` once and `{id}` everywhere else, and one of Google's
  code samples has none; Qatlas passes both spellings and requires none.

Not verified:

- No live call was made.
- Whether Google refuses a second `create` for an enrolled account, or an update of `uriSettings` that leaves
  the template of the other kind in place.
- The quota group of these methods and their per-minute and daily limits.
- Which Merchant Center role the methods need.
- Which programs and conditions admit an account to Checkout beyond the prerequisites Google names.

No tool of this provider enables or disables a program, and none sets the per-product checkout links of the feed.

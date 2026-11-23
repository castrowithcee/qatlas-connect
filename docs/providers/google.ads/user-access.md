---
description: >
  Describes the user access tools of the Google Ads provider: customer-user-accesses.list for the users of an account with their roles and e-mail addresses, and customer-user-accesses.update and remove that change the role of an existing user or remove a user, with the manager target rule, the data that is returned and the field that is left out, the single operation without validate_only, the multi-party authorization review, what the tools never do, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: user access tools

These tools read and change who has access to a Google Ads account. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too. The change
tools follow the rules of the [settings tools](settings.md) (tools list, confirmation per call, one request sent
once, unknown outcome, resource names bound to the account); this page names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customer-user-accesses.list` | read | lists the users of an account with role and e-mail addresses |
| `google.ads.customer-user-accesses.update` | update | changes the role of an existing user |
| `google.ads.customer-user-accesses.remove` | delete | removes an existing user from the account |

`list` belongs to the `read` profile. `update` and `remove` belong to no profile: a connection offers them only
when its `tools` list names each one.

## Manager accounts

The tools act on the account whose users they read or change, and a manager account has its own users, whose
roles apply to every account below it. On a connection with targets, a manager account therefore needs a manager
target that names it (`managers/ID`). A customer target that names a manager, and a manager that is only reached
below a manager target, are refused as `invalid-request`. Qatlas reads `customer.manager` of the account once to
tell, after the membership read when one is needed, and before the call itself. A connection without targets
reaches what its credential reaches and makes no such read. The order is the one of the settings tools: form of
the arguments, targets, secret, membership, manager check.

## customer-user-accesses.list

Runs a GAQL search `FROM customer_user_access` on the account, sorted by `user_id`, and pages like
`customer-clients.list` with `page_size` (1 to 1000, default 100) and an opaque `cursor`. Filters are joined with
`AND`: `user_ids` (up to 100), `access_roles`, and `email_address_contains`. The text filter is refused as
`invalid-request` before a secret is read when it holds a quote, a double quote, a backslash, or a control
character; `[`, `]`, `%`, and `_` match literally.

Each user has `resource_name`, `user_id`, `email_address`, `access_role` (`ADMIN`, `STANDARD`, `READ_ONLY`,
`EMAIL_ONLY`), `access_creation_date_time`, `inviter_user_email_address`, and
`pending_multi_party_auth_review`, the name of a review that is still open.

**Personal data.** The e-mail addresses of users and of the people who invited them are personal data of third
parties. They are returned as Google has them, and the tool description and its data sensitivity say so.

**Left out on purpose.** `passkey_enabled`, whether a user has a passkey, is security information about another
person. It is neither selected nor mapped, so it is not in any answer even if Google sends it.

Rows must name the queried account and their own user ID, a review must belong to the account, and the order
must be the one asked for; anything else is `invalid-provider-response`. Text is untrusted and cut as in the
README.

## customer-user-accesses.update and remove

Both call `CustomerUserAccessService.MutateCustomerUserAccess` with the single `operation` that this service
takes, not a list of operations. The name `customers/ID/customerUserAccesses/USER_ID` is built from the checked
`customer_id` and `user_id`; no argument takes a resource name.

- `update` sends an update of `access_role` only, with the mask `accessRole`. The new role is `ADMIN`,
  `STANDARD`, `READ_ONLY`, or `EMAIL_ONLY`. Giving a user `ADMIN` lets that user control the access of others,
  and the confirmation says so.
- `remove` removes the access. The user loses all access to the account.

Both act on an access that exists. Neither creates an access, sends an invitation, or accepts one: those are not
offered by any tool of the provider. The `user_id` is the one `list` returns.

**No validate_only.** The Mutate service of the user access has no `validate_only` and no
`response_content_type`, so the tools offer neither argument; Google would refuse the unknown members. The
answer holds `customer_id`, `user_id`, `resource_name`, and, when Google opened one, `multi_party_auth_review`.
Read the users with `list` before and after the change.

**Multi-party authorization.** Google may answer a change by opening a multi-party authorization review that
has to be resolved before the change takes effect. The answer then names the review. Resolving a review is not
part of these tools. The name of the review must belong to the account of the call, like the resource name of
the result; an answer that names another account is `invalid-provider-response` with the note that the outcome
is unknown, since the change may have been made.

**Quota.** Each change counts as one operation, a refused one too, plus the membership read and the manager
check when they apply. `list` costs one operation per page, plus the same.

## Boundary

Not verified:

- Which role an account needs to read or change users, and which role changes open a review. Google does not
  document it per call.
- Whether the service is blocked for projects at the Explorer access level, as Google's access level page
  states, and which error that gives. No live call has been made.
- Whether Google accepts the mask in camelCase, and whether `access_role` is the only updatable field: the
  reference gives no mutability for it.
- The form of the name of a review. Qatlas accepts `customers/ID/multiPartyAuthReviews/` and an identifier of
  letters, digits, `-`, and `_`.

Left out on purpose:

- Invitations, including their creation, and the resolution of reviews, which belong to other tools.
- `passkey_enabled`.
- `validate_only`, `response_content_type`, `partial_failure`, and a list of operations, which this service
  does not take or the tools do not offer.

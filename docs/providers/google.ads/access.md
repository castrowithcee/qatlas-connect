---
description: >
  Describes the Google Ads tools for invitations and multi-party approvals: customer-user-access-invitations.list and remove, multi-party-auth-reviews.list and resolve, with what they read and change, that no tool sends an invitation or approves a request, the rule that all four need a manager target on manager accounts, the absence of validate_only, the mutation path, confirmation, tools list requirement, the unknown outcome, the personal data in the answers, the fields left out, and what was checked against Google's reference and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: invitations and approval requests

These tools read the invitations that gave people access to a Google Ads account, read the requests for a second
administrator's approval, revoke an invitation, and turn a request down. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too. What every
changing tool does (tools list, confirmation, one atomic request, no repetition, unknown outcome) is described
for the [settings tools](settings.md); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customer-user-access-invitations.list` | read | lists the invitations of an account |
| `google.ads.customer-user-access-invitations.remove` | delete | revokes one invitation |
| `google.ads.multi-party-auth-reviews.list` | read | lists the multi-party approval requests of an account |
| `google.ads.multi-party-auth-reviews.resolve` | update | rejects or revokes one request |

The two list tools belong to the profile `read`. `remove` and `resolve` belong to no profile: a connection
offers them only when its `tools` list names each one.

## What is left out on purpose

- **No invitation is sent.** There is no tool and no argument that creates a `CustomerUserAccessInvitation`:
  no e-mail address and no role can be passed to a changing tool, and the Mutate request of `remove` carries a
  `remove` operation only. An invitation gives a person access to an account, which Qatlas Connect never grants.
- **No request is approved.** `resolve` accepts the statuses `REJECTED` and `REVOKED` only. `APPROVED` is
  refused by the input schema and again in the handler before a secret is read, and no other status is
  accepted. Approving a request would send an invitation or change who may use the account. An answer that
  nevertheless names a created invitation or access ends as `invalid-provider-response` with the note that the
  outcome is unknown.
- **No `validate_only`, no `response_content_type`.** Their Mutate requests in v26 have neither
  (`MutateCustomerUserAccessInvitationRequest` holds `customer_id` and `operation`,
  `ResolveMultiPartyAuthReviewRequest` holds `customer_id` and `operations`), so the tools offer no way to
  validate a call without making it. The confirmation is the only guard: it names the account and the object.
- **The passkey setting of a person is never returned.** A request that changes an existing access carries the
  new `CustomerUserAccess` of that person, in which Google may include `passkey_enabled`. Qatlas maps the fields
  it names and no others, so the setting is never decoded, and no query selects it on its own. Nothing else in
  the two resources is a credential, a token, or a link that grants access.

## Manager accounts

Invitations and approval requests belong to the control of who may act in an account, like the access of users.
On a connection with targets, a manager account therefore needs a manager target that names it (`managers/ID`)
for all four tools, the read tools included. A customer target, and a manager account that is only reached
below a manager target, are refused as `invalid-request` before the tool reads or changes anything of the account.
To tell, Qatlas reads `customer.manager` of the account once. A connection without targets reaches what its
credential reaches and makes no such read. This is the narrower reading of the rule for tools that act on a
manager account; Google does not document a difference between reading and changing here.

## Personal data of other people

The answers of the list tools hold e-mail addresses of invited and requesting people (`email_address`,
`request_user_email`, `inviter_user_email_address`), user IDs, and the free text `justification` of a request.
They are personal data of third parties and are returned as Google gives them, with the text cut and control
characters replaced. The filters `email_address` and `request_user_email` take an address without space, quote,
or backslash and match it exactly; Qatlas does not escape GAQL strings and refuses such characters before a
secret is read.

## customer-user-access-invitations.list

Runs a GAQL search `FROM customer_user_access_invitation` with all its fields: `resource_name`, `invitation_id`,
`access_role` (`ADMIN`, `STANDARD`, `READ_ONLY`, `EMAIL_ONLY`), `email_address`, `creation_date_time`
(`YYYY-MM-DD HH:MM:SS`), and `invitation_status` (`PENDING`, `DECLINED`, `EXPIRED`). Rows come sorted by
`invitation_id`. Filters: `invitation_statuses`, `access_roles`, `invitation_ids` (at most 100),
`email_address`; they are joined with `AND`. `page_size` (1 to 1000, default 100) and `cursor` page through the
rows as for `customer-clients.list`: the cursor holds the last ID, is bound to the arguments, the targets, and
`login_customer_id`, and an answer out of order is `invalid-provider-response`. An invitation that waits for a
multi-party approval is not listed by Google; the request shows it. Each row must carry the ID and the resource
name of the account asked for, otherwise the answer is invalid.

Not offered as filters: the creation time, since Google documents no comparison on this string field.

## customer-user-access-invitations.remove

Calls `CustomerUserAccessInvitationService.MutateCustomerUserAccessInvitation` with one `remove` operation.
The name is built as `customers/ID/customerUserAccessInvitations/INVITATION_ID` from the checked `customer_id`
and the positive whole number `invitation_id`; no resource name is accepted as an argument. The answer holds
`resource_name`, or `multi_party_auth_review` when Google holds the revocation for approval by a second
administrator. Both must belong to the account and the invitation of the call; otherwise the answer is
`invalid-provider-response` and says the outcome is unknown. Google requires an `ADMIN` user for the call.

## multi-party-auth-reviews.list

Runs a GAQL search `FROM multi_party_auth_review` with all its fields: `resource_name`,
`multi_party_auth_review_id`, `creation_date_time`, `review_status` (`PENDING`, `APPROVED`, `REJECTED`,
`REVOKED`, `EXPIRED`), `approval_date_time`, `justification`, `request_user_email`, `operation_type` (`CREATE`,
`UPDATE`, `REMOVE`), `target_resource` (`CUSTOMER_USER_ACCESS`, `CUSTOMER_USER_ACCESS_INVITATION`), and the
details under review: `customer_user_access_review` with `old_customer_user_access` and
`new_customer_user_access` (`resource_name`, `user_id`, `email_address`, `access_role`,
`access_creation_date_time`, `inviter_user_email_address`, `pending_multi_party_auth_review`; only the fields
that need review are set), or `customer_user_access_invitation_review` with `new_customer_user_access_invitation`.
Rows come sorted by ID. Filters: `review_statuses`, `operation_types`, `target_resources`, `review_ids` (at most
100), `request_user_email`; paging as above. Every resource name in a row (the review, the accesses, the pending
review, the invitation) must belong to the account of the call, otherwise the answer is invalid.

## multi-party-auth-reviews.resolve

Calls `MultiPartyAuthReviewService.ResolveMultiPartyAuthReview` at
`customers/ID/multiPartyAuthReview:resolve` (singular, as in Google's HTTP mapping) with exactly one operation,
the only number Google supports. The review name is built from `customer_id` and the positive whole number
`multi_party_auth_review_id`.

| `new_status` | Meaning | Who may |
| --- | --- | --- |
| `REJECTED` | denies a request that another administrator made | a user other than the requester |
| `REVOKED` | withdraws a request the credential's own user made | the requester only |

Google's guide states the two conditions; Qatlas does not check them and passes Google's refusal on as the error.
The request is atomic. Google answers with `result_or_error`: a `partial_failure_error` in the single entry is a
failed call, mapped like any `GoogleAdsFailure` (the first documented code decides; otherwise
`provider-error` with the note that nothing was changed), and the call is not repeated. An answer without exactly
one entry, without the review of the call, or with the name of an invitation or an access is
`invalid-provider-response` with the note that the outcome is unknown.

## Boundary

Checked against the reference and guides of Google for v26 (the proto files of the API, the guides
Manage user access invitations and Multi-party approvals, the page about access levels):

- Both Mutate requests lack `validate_only`, which the other changing tools of this provider offer. The tools here offer none.
- The review service answers at `multiPartyAuthReview:resolve` and takes `operations[]` with one operation,
  not the form of the other Mutate services. Its answer carries a partial failure per entry.
- Invitations and requests expire after 20 days. Confirmed by the proto and the guide.
- `MultiPartyAuthReviewService`, like the invitation service, is not available at the Explorer access level;
  a summary of that page may name only the invitation service. Both need at least Basic.
- A limit of 70 open invitations per account, which secondary summaries give, is on no Google page read for this
  package and does not matter for the tools here, which send no invitation. It is not stated as a limit.
- Google's guide says that managing invitations needs an `ADMIN` user; which role the list calls need is not
  documented.

Not verified: that Google accepts the cut-down GAQL of the two lists and returns the review messages as
described (no live call has been made), which role the list tools need, whether a revocation of an invitation
triggers an approval request in practice, and the case-sensitivity of the address filters.

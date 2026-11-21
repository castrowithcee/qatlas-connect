---
description: >
  Describes the service and relationship tools of the Google Merchant provider: listing and reading the services of an advanced account, rejecting a service, listing and reading the relationships of an account, setting the alias of a relationship, the service and relationship ID forms, the advanced-account target the service tools need, how provider display names are masked outside the targets, the fixed reject and alias requests, the roles needed, the access effect, confirmation and the tools list, result bounds, the tools that are left out on purpose, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant: services and relationships

The service and relationship tools of the [Google Merchant provider](README.md). The account argument, targets,
quotas, cursors, and errors are described there.

A service gives a provider account a defined kind of access to a Merchant Center account, for example managing its
products or campaigns, or aggregating it as a sub-account. A service starts as a proposal and exists once both
sides agree. A relationship is the plain link between the account and a provider account; its only member the
account can change is an alias. The tools read both, can reject a service, and can set the alias of a relationship.

| Tool | Does |
| --- | --- |
| `account-services.list` | reads the services of an advanced account, a batch per call with `cursor` |
| `account-services.get` | reads one service |
| `account-services.reject` | rejects a proposed or approved service |
| `account-relationships.list` | reads the relationships of an account, a batch per call with `cursor` |
| `account-relationships.get` | reads one relationship |
| `account-relationships.update` | sets the alias of a relationship |

The four reads are in the `read` profile and need no confirmation. `account-services.reject` is in no profile: a
connection offers it only when its `tools` list names it, and every call needs a confirmation that shows the service
and the account. `account-relationships.update` is in no profile either, because a change is never in the `read`
profile; a connection that selects the effect `update` offers it, and every call needs a confirmation that shows the
account, the relationship, and the new alias. It is not a listed-only tool: it gives no access and ends none.

## Left out on purpose

There is no tool to propose or approve a service. Both give another account access to your account, which this
provider never does: it can read such access and end it, not create it. A service that a provider proposed can be
approved only in Merchant Center. Nor is there a tool to create a relationship; a relationship comes from a service or
from linking in Merchant Center. Linking an account to a Google Business Profile account or to a Local Feeds
Partnership provider is left out for the same reason and is described in [linked accounts](links.md).

## Arguments

`account` is the numeric Merchant Center account ID, checked against the form and the targets before any secret is
read.

- The three `account-services` tools act on the advanced account itself. On a connection bound to targets the
  argument must be an advanced account the connection is bound to (`advanced-accounts/ACCOUNT`); a sub-account, an
  account target, or any other account is refused without naming it, and a connection bound only to account targets is
  not offered these tools. With exactly one advanced account bound, the argument may be left out. Without targets any
  account ID is accepted.
- The relationship tools work on any account the connection reaches. An account that no target names is read or
  changed only after one proof read of the sub-accounts of a bound advanced account, as described in the README; a
  refused account is never changed. `account-relationships.update` requires `account`, the reads take it from a
  connection with exactly one target.

`service` is the service ID, the last segment of `accounts/ACCOUNT/services/SERVICE`, and one path segment. The
discovery document gives the name pattern `accounts/{account}/services/{service}` with no form for the ID beyond
"no slash", and the guides show no service name. Qatlas accepts only 1 to 64 letters, digits, dashes, and underscores.
A slash, a dot, a colon, a space, an escape, or any other character is refused before any secret is read or request is
sent. Whether the ID names a service is Google's answer (`not-found`).

`relationship` is the relationship ID, the last segment of `accounts/ACCOUNT/relationships/RELATIONSHIP`, and one path
segment. The discovery document gives the pattern `accounts/{account}/relationships/{relationship}` and a numeric
example, but no rule for the ID. Qatlas applies the same narrow form as for a service, 1 to 64 letters, digits, dashes,
and underscores, and refuses anything else before any secret is read or request is sent.

The list tools also take `page_size` (1 to 1000) and `cursor`. Google sends 100 by default and at most 1000; Qatlas
always sends the page size, 100 when `page_size` is left out. A cursor is tied to the tool, the account, the page size,
and the targets. Google documents no filter for either list, so there is none.

## Result

A service shows `name`, `provider`, `providerDisplayName`, `externalAccountId`, `handshake` (`approvalState`: `PENDING`,
`WAITING`, `ESTABLISHED`, or `REJECTED`; `actor`: `ACCOUNT` or `OTHER_PARTY`), `mutability` (`MUTABLE` or `IMMUTABLE`),
and the kind of the service. The kinds are `accountManagement`, `productsManagement`, `campaignsManagement`,
`localListingManagement`, `accountAggregation`, and `comparisonShopping`; the payload of each is an empty message, so a
kind shows as `{}` and says only which kind the service is. A relationship shows `name`, `provider`,
`providerDisplayName`, and `accountIdAlias`. Every member is shown as documented; none is derived. This is the single
relationship shape of the provider: tools that read or change a relationship use it unchanged.

The provider is `providers/ID` for a provider account, or `providers/GOOGLE_ADS` or
`providers/GOOGLE_BUSINESS_PROFILE`.

### Display names of provider accounts

`providerDisplayName` is the display value of a linked account, not an identifier. A link to an object outside the
binding shows an identifier only, so Qatlas keeps the display name only where the provider is inside:

- on a connection without targets, which reaches what the credential reaches;
- for a provider `providers/ID` whose ID a target names, as an advanced account or an account;
- for `providers/GOOGLE_ADS` and `providers/GOOGLE_BUSINESS_PROFILE`, which are not accounts.

For every other provider the display name is dropped and `provider` stays. This includes a provider account that is a
sub-account of a bound advanced account: telling that would take a read for every row, so the narrower reading
applies. `externalAccountId` is an identifier in the system of the provider and is always shown.

Texts come from Google and from the provider account and are untrusted: names, aliases, and display names are cut at 300
characters, IDs, states, and the provider at 100, a cut text ends with `…`, a member that is not documented is
dropped, and so is a value of the wrong type. A batch holds at most 1000 entries.

## Rejecting a service

`account-services.reject` sends one `POST` to `accounts/ACCOUNT/services/SERVICE:reject` with the documented empty
request body; nothing of the request comes from free input. The result is `accepted: true` and the name of the
service. It says only that Google accepted the request.

- **Access effect.** Google rejects both proposed and approved services. Rejecting an approved service ends the access
  it gave the provider account: a provider that managed products, campaigns, or sub-accounts through it loses that access.
  This can break what the provider's platform does for the account. There is no dry run.
- **Delay.** Google processes the change afterwards and the access can take a while to end. Read the service again to
  see the state.
- **Role.** Merchant Center guides name the role Admin on the receiving account for rejecting. Google answers a missing
  role with `403`, class `permission`; Qatlas cannot check the role beforehand. A service with `mutability` `IMMUTABLE`
  cannot be changed through this API and Google refuses the call.
- **No repetition.** A change is sent once. A rate limit (`429`) holds the next request of the credential and is not
  repeated. After a transport failure, a timeout, a `5xx`, or an unreadable answer the message says that the outcome is
  unknown; read the service before changing it again.
- **Quota.** Each call counts against the quota of the method group, also when it fails.

## Setting the alias of a relationship

`account-relationships.update` sends one `PATCH` to `accounts/ACCOUNT/relationships/RELATIONSHIP`. The update mask is
the constant `accountIdAlias`, the only member the discovery document lists as writable, and the body holds that member
alone; `provider` and `name` cannot be set, and no argument names a mask. The result is the relationship as Google
answered, in the shape above, with the same masking of the display name.

- **Alias.** `account_id_alias` is 1 to 300 characters without control characters. Google asks for an alias that is
  unique among the relationships with the same provider and documents no length limit, so the bound is Qatlas' own. The
  alias cannot be cleared here: an empty value is refused, because what Google does with an empty alias is not
  documented.
- **Role.** The discovery document says the method requires admin access. Qatlas cannot check the role beforehand;
  Google answers a missing role with `403`, class `permission`.
- **Delay.** Google applies the change afterwards, so read the relationship again to see it.
- **No repetition.** The change is sent once, also after `401`, `429`, or `5xx`. After a transport failure, a timeout, a
  `5xx`, or an unreadable answer the message says that the outcome is unknown; read the relationship before changing it
  again. Each call counts against the quota of the method group, also when it fails.

## Boundary

Checked against the discovery document of the accounts API v1 (revision 20261118):

- `services.list` and `relationships.list` take `parent`, `pageSize` (default 100, maximum 1000), and `pageToken`.
  The description of `services.list` says it supports filtering, but the document lists no filter parameter, so none is
  offered. The research behind this provider named `provider` and `externalAccountId` as service filters; they are
  members of the service, not parameters.
- `relationships.get` takes only the name. `relationships.patch` takes the name and `updateMask`, whose documented
  values are `account_id_alias` and `accountIdAlias`; Qatlas sends the second. The text says the method requires admin
  access.
- `services.get` and `services.reject` take only the name. The reject request is the empty `RejectAccountServiceRequest`
  and the answer is `Empty`.
- `AccountService` has no member besides the ones above, and `AccountRelationship` has `name`, `provider`,
  `providerDisplayName`, and `accountIdAlias`. No member is marked deprecated, so none is left out.
- The path pattern of a service name is `accounts/{account}/services/{service}` with no restriction on the ID besides
  the missing slash; Qatlas uses the narrower form above.

Not verified:

- No live call was made. Answers are taken from the discovery document and the guides of the Merchant API.
- Which role each method needs: the guides name Admin for rejecting, the discovery document names none for the service
  methods and admin access for the alias change.
- The form of a real relationship ID, whether Google refuses an alias that another relationship of the same provider
  uses, and the length limit of an alias.
- The form of a real service ID. A real ID outside letters, digits, dash, and underscore, or longer than 64 characters,
  is refused by Qatlas.
- How long access takes to end after a rejection, and in which state a rejected service stays visible.
- Whether a repeated rejection is refused by Google or has no further effect.
- Whether a provider account can be a sub-account of a bound advanced account while its display name is masked: the
  masking does not prove membership.

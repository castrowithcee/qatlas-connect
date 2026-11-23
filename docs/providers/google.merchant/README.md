---
description: >
  Describes the Google Merchant provider: the token credential and its procedures, the single scope, the fixed API origin, the advanced-account and account targets and how an account is proven to belong to one, the tools and their groups, quotas, result bounds, the connection test, error classes, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-21
---

# Google Merchant

Google Merchant is a provider for the Merchant API v1 of Google Merchant Center. The table lists its tools; a tool
group with its own page links to it. [Set up Google Merchant](setup.md) leads through the Google side, the
developer registration, and the consent.

| Tool | What it does |
| --- | --- |
| `google.merchant.accounts.list` | the accounts the credential can access, with an optional Google filter |
| `google.merchant.accounts.get` | one account |
| `google.merchant.developer-registrations.get` | the Google Cloud projects registered as developers with an account |
| `google.merchant.data-sources.list`, `.get` | the data sources of an account, one batch or one source, see [data sources](data-sources.md) |
| `google.merchant.data-sources.create`, `.update` | create an API data source and change its name, countries, destinations, or default rule; confirmed, see [data sources](data-sources.md) |
| `google.merchant.data-sources.fetch` | asks Merchant Center to fetch a file data source now, from the address configured in Merchant Center; confirmed, see [data sources](data-sources.md) |
| `google.merchant.data-sources.delete` | deletes a data source of any kind; confirmed and only through the `tools` list, see [data sources](data-sources.md) |
| `google.merchant.file-uploads.get` | the latest file upload of a file data source: state, item counts, issues, see [data sources](data-sources.md) |
| `google.merchant.developer-registrations.get-account-for-gcp-registration` | the account the Cloud project of the credential is registered with; on a bound connection only if it lies within the targets, see [developer registration](developer-registrations.md) |
| `google.merchant.developer-registrations.unregister-gcp` | unregisters the Cloud project of the credential from an account, which ends its API access, Qatlas included; confirmed and only through the `tools` list, see [developer registration](developer-registrations.md) |
| `google.merchant.products.list` | the processed products of an account, with all attributes, see [products and issues](products.md) |
| `google.merchant.products.get` | one processed product, see [products and issues](products.md) |
| `google.merchant.aggregate-product-statuses.list` | product counts and issues per reporting context and country, see [products and issues](products.md) |
| `google.merchant.account-issues.list` | the account-level issues of an account, see [products and issues](products.md) |
| `google.merchant.local-inventories.list` | the local inventories of one product, one entry per store, see [local inventories](local-inventories.md) |
| `google.merchant.local-inventories.insert` | replaces the whole local inventory of one store code of a product; confirmed, only through the `tools` list, see [local inventories](local-inventories.md) |
| `google.merchant.local-inventories.delete` | deletes the local inventory of one store code of a product; confirmed, only through the `tools` list, see [local inventories](local-inventories.md) |
| `google.merchant.regional-inventories.list` | the regional inventories of one product, one entry per region, see [regional inventories](regional-inventories.md) |
| `google.merchant.regional-inventories.insert` | replaces the whole regional inventory of one region of a product; confirmed, only through the `tools` list, see [regional inventories](regional-inventories.md) |
| `google.merchant.regional-inventories.delete` | deletes the regional inventory of one region of a product; confirmed, only through the `tools` list, see [regional inventories](regional-inventories.md) |
| `google.merchant.accounts.list-subaccounts` | the sub-accounts of an advanced account, see [account tools](accounts.md) |
| `google.merchant.accounts.update` | changes the settings of an account, see [account tools](accounts.md) |
| `google.merchant.accounts.delete` | deletes an account, see [account tools](accounts.md) |
| `google.merchant.accounts.create-test-account` | creates a test account, see [account tools](accounts.md) |
| `google.merchant.programs.list`, `.get` | the programs of an account with state, active regions, and unmet requirements, see [programs](programs.md) |
| `google.merchant.programs.enable`, `.disable` | enable or disable participation in a program; confirmed and offered only through the `tools` list of a connection, see [programs](programs.md) |
| `google.merchant.business-infos.get` | the business address, customer service contacts, and phone number of an account, see [business profile](business-profile.md) |
| `google.merchant.business-infos.update` | change the address, customer service contacts, or Korean registration number; confirmed and offered only through the `tools` list of a connection, needs the Admin role, see [business profile](business-profile.md) |
| `google.merchant.business-identities.get` | the business identity declarations and the promotions consent of an account, see [business profile](business-profile.md) |
| `google.merchant.business-identities.update` | change the identity declarations or the promotions consent; confirmed and offered only through the `tools` list of a connection, needs the Admin role, see [business profile](business-profile.md) |
| `google.merchant.reports.search` | one report of an account, from a structured query, see [reports](reports.md) |
| `google.merchant.quotas.list` | the API quota groups of an account, see [reports](reports.md#quotas-and-limits) |
| `google.merchant.limits.list` | the limits of an account, see [reports](reports.md#quotas-and-limits) |
| `google.merchant.limits.get` | one limit of an account, see [reports](reports.md#quotas-and-limits) |
| `google.merchant.terms-of-service.get` | one version of the terms of service |
| `google.merchant.terms-of-service.retrieve-latest` | the latest terms of service for a kind and a region |
| `google.merchant.terms-of-service-agreement-states.get` | the agreement state of an account for one kind and country |
| `google.merchant.terms-of-service-agreement-states.retrieve-for-application` | the agreement state of an account for the application terms |
| `google.merchant.autofeed-settings.get` | whether the autofeed of an account is on and whether the account is eligible |
| `google.merchant.automatic-improvements.get` | the automatic item updates, image improvements, and shipping improvements of an account |
| `google.merchant.autofeed-settings.update` | turns the autofeed of an account on or off (update, listed tools only) |
| `google.merchant.automatic-improvements.update` | changes the automatic improvements of an account (update, listed tools only) |
| `google.merchant.users.list`, `google.merchant.users.get` | the users of an account, see [users](users.md) |
| `google.merchant.users.update`, `google.merchant.users.delete` | replace the roles of a user, remove a user; only through the `tools` list, see [users](users.md) |
| `google.merchant.homepages.get` | the home page address of an account and whether it is claimed, see [home page](homepages.md) |
| `google.merchant.homepages.update` | sets the home page address, see [home page](homepages.md) |
| `google.merchant.homepages.claim` | claims the home page, see [home page](homepages.md) |
| `google.merchant.homepages.unclaim` | removes the claim of the home page, see [home page](homepages.md) |
| `google.merchant.regions.list`, `.get` | the regions of an account, one batch or one region, see [regions](regions.md) |
| `google.merchant.regions.create`, `.update` | create a region and replace members of it; confirmed, only through the `tools` list of a connection, see [regions](regions.md) |
| `google.merchant.regions.delete` | deletes one region; confirmed, only through the `tools` list, see [regions](regions.md) |
| `google.merchant.regions.batch-create`, `.batch-update`, `.batch-delete` | create, replace members of, or delete 1 to 100 regions in one request that counts as one call; confirmed, only through the `tools` list, see [regions](regions.md) |
| `google.merchant.promotions.list`, `.get` | the processed promotions of an account, one batch or one promotion, see [promotions](promotions.md) |
| `google.merchant.promotions.insert` | creates a promotion or REPLACES the one with the same ID, country, and language; confirmed, only through the `tools` list, cannot be undone through Qatlas, see [promotions](promotions.md) |
| `google.merchant.omnichannel-settings.list` | the omnichannel settings of an account, see [omnichannel settings](omnichannel.md) |
| `google.merchant.omnichannel-settings.get` | the omnichannel setting of one country, see [omnichannel settings](omnichannel.md) |
| `google.merchant.omnichannel-settings.create` | creates the omnichannel setting of a country, see [omnichannel settings](omnichannel.md) |
| `google.merchant.omnichannel-settings.update` | changes the omnichannel setting of a country, see [omnichannel settings](omnichannel.md) |
| `google.merchant.omnichannel-settings.request-inventory-verification` | starts the inventory verification process at Google for a country; confirmed, only through the `tools` list, see [omnichannel settings](omnichannel.md) |
| `google.merchant.checkout-settings.get` | the checkout settings of an account, see [checkout settings](checkout.md) |
| `google.merchant.checkout-settings.create` | enrolls an account in the Checkout program, see [checkout settings](checkout.md) |
| `google.merchant.checkout-settings.update` | changes the checkout settings of an account, see [checkout settings](checkout.md) |
| `google.merchant.checkout-settings.delete` | deletes the checkout settings of an account, see [checkout settings](checkout.md) |
| `google.merchant.shipping-settings.get` | the whole shipping setting of an account, see [shipping settings](shipping-settings.md) |
| `google.merchant.shipping-settings.insert` | REPLACES the whole shipping setting (update, confirmation, listed tools only), see [shipping settings](shipping-settings.md) |
| `google.merchant.online-return-policies.list` | the online return policies of an account, see [return policies](return-policies.md) |
| `google.merchant.online-return-policies.get` | one online return policy, see [return policies](return-policies.md) |
| `google.merchant.online-return-policies.create` | creates an online return policy, see [return policies](return-policies.md) |
| `google.merchant.online-return-policies.delete` | deletes an online return policy, see [return policies](return-policies.md) |
| `google.merchant.product-reviews.list`, `.get` | the product reviews of an account, one batch or one review, with the identifiers of the reviewers (personal data of third parties), see [product reviews](product-reviews.md) |
| `google.merchant.product-reviews.delete` | deletes one product review; confirmed, only through the `tools` list, see [product reviews](product-reviews.md) |
| `google.merchant.notification-subscriptions.list` | the notification subscriptions of an account |
| `google.merchant.notification-subscriptions.get` | one notification subscription |
| `google.merchant.notification-subscriptions.delete` | deletes a notification subscription (delete, confirmation, listed tools only) |
| `google.merchant.account-services.list`, `.get` | the services of an advanced account with the state of each handshake, see [services](services.md) |
| `google.merchant.account-services.reject` | reject a service and end the access it gave a provider account; confirmed and offered only through the `tools` list of a connection, see [services](services.md) |
| `google.merchant.account-relationships.list`, `.get` | the relationships of an account with provider accounts, see [services](services.md) |
| `google.merchant.account-relationships.update` | sets the alias of a relationship; confirmed, see [services](services.md) |
| `google.merchant.gbp-accounts.list` | the Google Business Profile accounts linked to an account, see [linked accounts](links.md) |
| `google.merchant.lfp-providers.find` | the Local Feeds Partnership providers offered in a country, see [linked accounts](links.md) |
| `google.merchant.conversion-sources.list` | the conversion sources of an account, with archived ones on request, see [conversion sources](conversions.md) |
| `google.merchant.conversion-sources.get` | one conversion source, see [conversion sources](conversions.md) |
| `google.merchant.conversion-sources.create` | creates a Merchant Center destination as conversion source (create), see [conversion sources](conversions.md) |
| `google.merchant.conversion-sources.update` | changes a Merchant Center destination (update), see [conversion sources](conversions.md) |
| `google.merchant.conversion-sources.undelete` | restores an archived Merchant Center destination (update, tools list only), see [conversion sources](conversions.md) |
| `google.merchant.conversion-sources.delete` | archives a Merchant Center destination or removes a Google Analytics link (delete, tools list only), see [conversion sources](conversions.md) |
| `google.merchant.issue-resolutions.render-account-issues` | the issues of an account with resolution content and actions, as text, see [issue rendering](issue-resolutions.md) |
| `google.merchant.issue-resolutions.render-product-issues` | the issues of one product with resolution content and actions, as text, see [issue rendering](issue-resolutions.md) |
| `google.merchant.email-preferences.get` | the email preferences of the user of the credential, see [email preferences](email-preferences.md) |
| `google.merchant.email-preferences.update` | opts the user of the credential in or out of news and tips mails (update, confirmation required), see [email preferences](email-preferences.md) |
| `google.merchant.product-inputs.insert` | uploads a product input to an API data source and REPLACES an existing input of the same identity (create, confirmation, `tools` list only), see [product inputs](product-inputs.md) |
| `google.merchant.product-inputs.update` | sets and removes single attributes of a product input with an update mask that Qatlas builds (update, confirmation, `tools` list only), see [product inputs](product-inputs.md) |
| `google.merchant.product-inputs.delete` | deletes a product input from a data source (delete, confirmation, `tools` list only), see [product inputs](product-inputs.md) |

The terms tools are described in [terms of service](terms-of-service.md), the two updates in [Autofeed and
automatic improvements](autofeed.md), the account tools beyond the first three in [account tools](accounts.md), the product and issue tools in
[products and issues](products.md), the local inventory tools in [local inventories](local-inventories.md), the regional inventory tools in
[regional inventories](regional-inventories.md),
the user tools in [users](users.md), the home page tools in [home page](homepages.md), the region tools in
[regions](regions.md), the promotion tools in [promotions](promotions.md), the review tools in [product reviews](product-reviews.md), the subscription tools in [Notification subscriptions](notifications.md), the
omnichannel tools in [omnichannel settings](omnichannel.md), the service and relationship tools in [services](services.md),
the Business Profile and Local Feeds Partnership reads in [linked accounts](links.md), the conversion tools in [conversion sources](conversions.md), the checkout tools in [checkout
settings](checkout.md), the return policy tools in [return policies](return-policies.md), the shipping tools in [shipping settings](shipping-settings.md), the render tools in
[issue rendering](issue-resolutions.md), the email tools in [email preferences](email-preferences.md), and the product input tools in [product inputs](product-inputs.md); the
render tools are reads although Google takes them as `POST`.

Every read tool needs no confirmation, and every change needs one for every call. The account resource has the
members `name`, `accountId`, `accountName`, `testAccount`, `adultContent`, `languageCode`, `homePageUri`, and
`timeZone` (`id`, `version`); all are output. It has no member for the account type, so Qatlas shows none: an
advanced account is
recognised only by its sub-accounts, which a target proves.

## Configuration

The API origin is fixed: `https://merchantapi.googleapis.com`. Leave `base_url` out or set it to that value;
Qatlas refuses any other value before a secret is read. The API answers only to an OAuth access token; an API
key ends in `401`.

The credential issues access tokens. It names `provider: google.merchant` and holds the roles of one procedure:

| Procedure | Roles | Access of |
| --- | --- | --- |
| Consent | `client_id`, `client_secret` if the client has one, `refresh_token` from `qconnect credential consent` | the Google account that gave the consent |
| Service account | `service_account_key`, the complete key file | the service account |
| Impersonation | as consent, plus `service_account` on the connection | the named service account, through the consenting account |

A credential with both a client ID and a key, or neither, is unusable. Access tokens stay in memory, travel
only in the `Authorization` header, and are masked in every output; no tool returns a token.

All tools need the one scope `https://www.googleapis.com/auth/content`. Google offers no read-only scope for the
Merchant API: the scope allows reading and writing, and what a call may do follows from the role of the user or
service account in Merchant Center. A token without the scope ends a tool as `scope-missing` before Google is
contacted; a new consent adds it. A credential serves this provider only, so its token never carries scopes of
another Google product.

The profile `read` is the recommended one and holds the read tools that need no `tools` list. A
connection without a `tools` list offers tools by their effects, and `read` is the default effect. A tool marked
as listed-only, such as `programs.enable`, is in no profile and reaches a connection only through its `tools` list.

## Targets

A connection may be bound to any number of targets, at most 100, of two kinds:

| Kind | Form | Reaches |
| --- | --- | --- |
| `advanced-account` | `advanced-accounts/ACCOUNT` | the advanced account itself, its direct sub-accounts, and the tools that act on the advanced account as a whole |
| `account` | `accounts/ACCOUNT` | that one account, a sub-account or a standalone account |

`ACCOUNT` is the decimal account ID without a leading zero, up to 19 digits. Both kinds name an account by the
same ID, so an account has one value whichever way it is chosen. A target that is not spelled like this makes the
connection unusable before any secret is read, and so does an exclusion: this provider takes none. Without a
target the connection reaches every account the credential can access.

The `account` argument of the tools is the plain ID, digits only. With exactly one target it may be left out and
means that account. Qatlas checks it against the form and the targets before any secret is read or a request
is sent. The path of every request is built from the checked ID, never from free input.

- An account named by a target, as an advanced account or as an account, is read without further requests.
- Any other account is read only after one proof read: Qatlas lists the direct sub-accounts of each bound
  advanced account (`accounts.listSubaccounts`, 500 per page, at most 20 pages) and reads the account only if
  it is among them. An account outside every target is refused without naming it. If a bound advanced account
  has more sub-accounts than the 20 pages show, the call ends instead of guessing; bind the accounts themselves.
- Only direct sub-accounts are proven. An account below a sub-account needs a target of its own.
- `accounts.list` checks every row it receives against the targets and drops the others, and a row whose account
  ID cannot be read, or whose `name` and `accountId` disagree. The filter only narrows what Google sends. A
  batch can therefore be short or empty while `next_cursor` says there is more. For rows that no target
  names, the sub-accounts of the bound advanced accounts are read once per call.
- `developer-registrations.get`, `developer-registrations.unregister-gcp`, the user tools, and the `account-services` tools act on an advanced account as a
  whole. On a connection bound to targets they need `advanced-accounts/ACCOUNT` for that account: a sub-account, an account target, or an advanced account
  that is not bound is not enough. A connection bound only to account targets is not offered these tools. Without
  targets any account ID is accepted.
- `developer-registrations.get-account-for-gcp-registration` takes no account. On a bound connection the account
  Google names is checked against the targets, with the same proof read, and refused without naming it if it lies
  outside; see [developer registration](developer-registrations.md).
- `terms-of-service.get` and `terms-of-service.retrieve-latest` read global documents and act on no account, so
  every connection is offered them whatever its targets; see [terms of service](terms-of-service.md).
- The time between the proof read and the call that follows is not covered: an account that leaves its advanced
  account in between is still read once.

## Quotas

Google counts each method group against a daily limit, which resets at 12:00 UTC, and a rolling per-minute
limit. The quota of `accounts.list` is counted per user, not per account. Calls for sub-accounts count against the
pool of the advanced account. Every call counts, also a failed one; the proof reads count as well. Qatlas does
not repeat a call after `429`, and the next request of the credential in the same process waits for the seconds
of `Retry-After`, at least 10 seconds and at most one minute.

## Results

Everything Google returns is untrusted third-party content. Account names and home page addresses come from
whoever runs the account. Addresses are text and are never fetched. A name is cut at 300 characters, an address
at 2048, every other text at 100, and a cut text ends with `…`. A member that is not documented is dropped, and
so is a value of the wrong type. A list of accounts holds at most 1000 entries, and a list of project IDs at most
100. The answer read from Google is capped at 4 MiB. Cursors are tied to the tool, the filter, the page size, and
the targets, and another combination refuses them.

`accounts.list` takes `page_size` from 1 to 500; without it Google sends up to 250. `filter` is at most 500
characters without control characters and uses the filter syntax of the accounts API, for example
`accountName = "*store*"`, `access = DIRECT`, `access = INDIRECT`, or `relationship(providerId = 123)`. Qatlas
does not parse it. The targets are applied to every row whatever Google does with the filter.

## Connection test

The connection test (`t` on a connection in `qconnect tui`) obtains an access token the way a call does and then
reads one account, which costs nothing:

- With an advanced account target, it reads the first one and then its developer registration.
- With only account targets, it reads the first one. It reads no registration, because that belongs to the advanced
  account.
- Without a target, it lists one account and reads the developer registration of that account.

A pass proves the credential, the scope, the Merchant API enabled in the Cloud project, access to one account, and,
where it was read, a registration with at least one Cloud project. It does not prove the role for any method, and
it does not show that the Cloud project of the credential is the registered one: Google returns the registered
projects, not the caller's.

| Result | Meaning |
| --- | --- |
| `consent-required` | the credential holds a client ID but no refresh token yet |
| `consent-expired` | Google no longer accepts the consent: it expired or was revoked |
| `scope-missing` | the token lacks the scope |
| `credential-unusable` | the roles fit no procedure |
| `auth` | Google refused the client, the key, or the token |
| `permission` | the API is not enabled, the account has no access to the read account, the registration read was refused, or the account has no registered Cloud project |
| `not-found` | the credential sees no account |
| `rate-limited` | a quota of the account or user is used up |
| `impersonation-denied`, `service-account-unknown` | the impersonation was refused, see [setup](setup.md#troubleshooting) |

`permission` after a successful account read means that Google refused the registration read or returned a
registration without a project. A credential without a target that reaches only sub-accounts can show this
too, because the registration belongs to the account the user was added to; bind the advanced account to test it.

## Errors

Qatlas' message never carries Google's text. Google's own answer goes along as the original in the `provider`
field of the error detail (untrusted, redacted, and bounded): the `status`, the `message`, and the type and
reason of each detail. The answers of the token endpoints keep only their status and error code.

| Answer | Class |
| --- | --- |
| 401 | `auth`, after the token was renewed once and the read was sent once more; a change is not sent again, the next call renews the token |
| 403 | `permission`: no access to the account, a missing role, the API not enabled, or an unregistered Cloud project; a `403` renews nothing |
| 404 | `not-found` |
| 429 | `rate-limited`; the reason `QUOTA_REQUEST_RATE_TOO_HIGH` names the per-minute limit and `QUOTA_TOO_MANY_REQUESTS` the daily one |
| 503 | `unreachable` |
| 504, or no answer in 30 seconds | `timeout` |
| Any other status | `provider-error` |

A redirect is never followed. An unreadable, oversized, or non-JSON answer is `invalid-provider-response`.
The reason of a `429` is the only detail of a Google error that chooses wording; no other reason chooses a class.
A change that may have reached Google, with a transport failure, a timeout, a `5xx`, or an unreadable answer, adds
that its outcome is unknown. Changes are never repeated.

## Boundary

The provider reaches only the fixed origin and the fixed method of each tool. It offers no passthrough of method,
path, header, query, or body. It uses no Google client library and does not use the Content API for Shopping.

Checked against the discovery document of the accounts API v1 (revision 20261118): `accounts.list` returns 250
accounts by default and at most 500, `accounts.listSubaccounts` has the same bounds, and `accounts.get` and
`developerRegistration.get` have no parameters besides the account.

Not verified:

- No live call was made. Methods, parameters, and answers are taken from the discovery document and the guides of
  the Merchant API.
- What Google answers for a Cloud project that is not registered as a developer, and for a registration read on
  a sub-account. Both end as `403` or another error and are reported by class, not by reason.
- Which Merchant Center role each method needs, and whether a service account with the role Standard or Read only
  is enough for these tools; Google names only the role Admin for some methods.
- Whether Merchant Center accepts an impersonated service account as a user.
- Whether the scope `content` needs an app verification for an OAuth client of your own project.

Changes are sent once: an update with a JSON body built from the structured arguments, a deletion without a
body. They are not repeated after `401`, `429`, or `5xx`, because Google may already have applied them, and every
call counts against the quota.

No tool of this provider proposes or approves a service, invites a user or accepts an invitation, registers a Cloud project (`registerGcp`), links an account to another
one (a Business Profile account or a Local Feeds Partnership provider included), sets a notification address, or accepts terms for an account (`terms-of-service.accept` is deliberately
missing). The Merchant API methods of the Local Feeds Partnership service (stores, inventories, sales) and for triggering actions are not offered. Submitting product reviews (`productReviews.insert`) is not offered either.

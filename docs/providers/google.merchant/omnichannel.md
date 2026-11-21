---
description: >
  Describes the Google Merchant tools for the omnichannel settings of an account: listing and reading them, creating and updating the setting of a country, who may use them, the arguments and the update mask, the https check of every address, the review and verification states, the delayed processing, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant omnichannel settings

The omnichannel settings of a Merchant Center account say how a country takes part in local programs such as
free local listings and local inventory ads: the type of the local storefront, the product pages Google reviews
for in-stock and pickup serving, the policy and about pages, and the contact of the inventory verification. Each
country has one setting, named by its region code. Five tools cover them. Targets, quotas, results, and errors
are as described in the [provider overview](README.md).

| Tool | Method | Effect |
| --- | --- | --- |
| `google.merchant.omnichannel-settings.list` | `accounts.omnichannelSettings.list` | reads the settings of all countries |
| `google.merchant.omnichannel-settings.get` | `accounts.omnichannelSettings.get` | reads the setting of one country |
| `google.merchant.omnichannel-settings.create` | `accounts.omnichannelSettings.create` | creates the setting of a country |
| `google.merchant.omnichannel-settings.update` | `accounts.omnichannelSettings.patch` | changes members of the setting of a country |
| `google.merchant.omnichannel-settings.request-inventory-verification` | `accounts.omnichannelSettings.requestInventoryVerification` | starts the inventory verification process at Google for a country |

## Access

`list` and `get` are reads and belong to the profile `read`. `create` and `update` are offered only to a connection
whose `tools` list names them, no effect profile selects them, and they need a confirmation on every call: the
addresses they set steer customers to a shop, and Google starts a review of each. Google documents no Merchant
Center role for these methods; a missing role is answered with `403`, which Qatlas reports as `permission`.

The `account` argument is the account ID. `create` and `update` require it, the reads take it from a connection with
exactly one target. It is checked against the targets before any secret is read or a request is sent. An account
that no target names is read or changed only after the proof read of the sub-accounts of a bound advanced
account, as for `accounts.get`; a refused account is never changed. An account target is enough: these tools act on
one account, not on the advanced account as a whole.

## Inventory verification request

`request-inventory-verification` takes `account` (required, as for `create`) and `region_code` (required). It sends a
`POST` to `accounts/{account}/omnichannelSettings/{region}:requestInventoryVerification` with an empty request
body and shows the `OmnichannelSetting` of the answer in the form of `get`. It sets nothing: the contact comes from
the setting, so `inventory_verification` must have been stored by `create` or `update`. The request starts a
verification process at Google for that country, Google mails the contact, and the state of the verification
changes as the process runs. It is offered only to a connection whose `tools` list names it, needs a confirmation
on every call, is never repeated, and counts against the quota of the Merchant API, a failed call too. The
answer only tells that Google accepted the request, it can still show the earlier state, and the process takes
about 48 hours. The account and region code are checked before any secret is read or a request is sent.

## Arguments

`get` and `update` name the country by `region_code`, two uppercase letters such as `CH`, and `create` sets it.
Google takes it as a CLDR region code and cannot change it afterwards; the two-letter form is the bound of Qatlas.
The path of every request is built from the checked account ID and region code.

Every writable member of the resource is a structured argument:

| Argument | Meaning |
| --- | --- |
| `lsf_type` | `GHLSF` (storefront hosted by Google), `MHLSF_BASIC`, or `MHLSF_FULL` (hosted by the merchant); required on create |
| `in_stock` | `uri` of the product page Google reviews for in-stock serving; an empty object for `GHLSF` |
| `pickup` | `uri` of the pickup product page, reviewed for pickup serving |
| `odo` | `uri` of the on display to order policy |
| `about` | `uri` of the about page (imprint) |
| `inventory_verification` | `contact` (name) and `contact_email` of the person Google asks to verify the inventory |

The members `state` of every address, `lfpLink`, and the states of the inventory verification are output only and
are no arguments. The link to a provider of local feeds cannot be made here; it is not offered.

`update` sends only the members it is given and names exactly those in the update mask, so every other member stays
as it is. At least one member is required. A member that is passed replaces the whole member: Google clears what
the new value leaves out. There is no free mask and no wildcard. Google does not allow a change to a feature that
is running or approved, and switching from a merchant-hosted type to `GHLSF` needs `in_stock` in the same call, as
an empty object. Qatlas does not repeat these rules; it refuses only what is plainly wrong and leaves the rest to
Google.

## Addresses

Every address (`in_stock.uri`, `pickup.uri`, `odo.uri`, `about.uri`) must be an absolute `https` address with a
host, without user information, white space, control characters, or backslashes, at most 2048 characters. An
empty `uri` is refused, except that `in_stock` may be left without one. Qatlas refuses anything else before a
secret is read or a request is sent, and `GHLSF` together with an `in_stock` address is refused too, because
Google requires that address to be empty for it. Google documents that the domain of these addresses should match
that of the home page of the account; Qatlas does not check this. Addresses are text handed to Google and never
fetched. Google documents no length for them; 2048 is the bound of Qatlas and of a shown result.

`contact` is 1 to 300 characters without control characters. `contact_email` is one mail address of at most 254
characters; Google sends the contact a mail, and the contact verifies by a button in it.

## Results

A setting shows `name`, `regionCode`, `lsfType`, `inStock`, `pickup`, `odo`, and `about` (each `uri` and `state`),
`lfpLink` (`lfpProvider`, `externalAccountId`, `state`), and `inventoryVerification` (`state`, `contact`,
`contactEmail`, `contactState`). Everything is untrusted content of whoever runs the account: texts are cut,
members that are not documented are dropped, and addresses are text that is never fetched. The states are shown as
Google names them: `ACTIVE`, `FAILED`, `RUNNING`, `ACTION_REQUIRED` for the review of an address, and
`INACTIVE`, `RUNNING`, `SUCCEEDED`, `SUSPENDED`, `ACTION_REQUIRED` for the inventory verification.

`list` takes `page_size` from 1 to 1000 and a `cursor`; Google sends 50 settings when `page_size` is left out. A
cursor is tied to the tool, the account, the page size, and the targets.

## Delayed processing and no repetition

The answer of `create` and `update` only tells that Google accepted the change. Google then reviews each address:
manual reviews usually take from a couple of hours to a couple of days, and the inventory verification about 48
hours, so the states of a fresh setting start as under review. Each call counts against the quota of the Merchant
API, a failed one too. Qatlas sends a change once and never repeats it, also not after a renewed token. A failure
that may have reached Google, such as a server error, a lost connection, or an unreadable answer, says that the
outcome is unknown: read the setting before changing it again. A `4xx` answer rejected the request before any
change.

## Boundary

Checked against the reference pages of the methods and the guide to the omnichannel settings in the accounts API
v1: the collection path is `accounts/{account}/omnichannelSettings`, the setting is named by its region code in the
guide's requests, `create` is a `POST` whose body is the setting with `regionCode` and `lsfType`, `patch` is a
`PATCH` with a required update mask whose supported members are `lsfType`, `inStock`, `pickup`, `odo`, `about`,
and `inventoryVerification`, `list` takes `pageSize` (50 by default, at most 1000) and `pageToken`, and every
method uses the scope `content`. `regionCode` is immutable, `lfpLink` and every `state` are output only.

Where the pages differ, Qatlas follows the more cautious reading:

- The reference spells the members of the update mask in camel case but says they are supported in snake case
  only, and the guide's requests use snake case. Qatlas sends snake case, for example `lsf_type` and
  `inventory_verification`.
- The reference marks `inStock.uri` as required for `MHLSF_BASIC` and `MHLSF_FULL`, while the guide asks for an
  address in `in_stock` or in `pickup`. Qatlas requires neither and leaves the answer to Google.
- The reference lists `pickup.uri`, `odo.uri`, and `about.uri` as required members of their objects, so Qatlas
  requires them whenever the member is passed.

Not verified:

- No live call was made.
- Which spelling of the update mask Google accepts, and whether it answers an empty `in_stock` object on `update`
  as the guide describes.
- Whether Google refuses a second `create` for a country that has a setting, or a country code that no program
  supports.
- The quota group of these methods and their per-minute and daily limits.
- Which Merchant Center role the methods need.

Inventory verification request:

- Checked against the reference page of the method and the API description: `POST`, path with the setting name,
  empty body, answer holding `omnichannelSetting`, scope `content`. No difference was found. The reference page
  does not list the states of the verification; the states shown are those of the API description: `ACTION_REQUIRED`,
  `INACTIVE`, `RUNNING`, `SUCCEEDED`, `SUSPENDED`.
- Not verified: the states from which Google accepts the request, what it answers in other states, and whether a
  stored contact is required. Qatlas does not check them and leaves the answer to Google. No live call was made.

The link to a provider of local feeds and the search for such a provider are not offered by these tools.

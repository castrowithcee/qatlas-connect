---
description: >
  Describes the business profile tools of the Google Merchant provider: reading and changing the business info (address, customer service contacts, Korean registration number) and the business identity (owner declarations, promotions consent) of an account, the account check, the update mask allowlist and the structured arguments, the https check of the customer service address, the Admin role and how a missing role ends, confirmation and the tools list, delayed public effect, result bounds, fields left out, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant: business profile

The business profile tools of the [Google Merchant provider](README.md). The account argument, targets, quotas, and
errors are described there.

The business info holds the business address, the customer service contacts, the phone number, and, for Korea, the
business registration number. The business identity holds the declarations that a business is black-owned,
latino-owned, a small business, veteran-owned, or women-owned, and the consent to use them in promotions.

| Tool | Does |
| --- | --- |
| `business-infos.get` | reads the business info of an account |
| `business-infos.update` | changes the address, the customer service, or the Korean registration number |
| `business-identities.get` | reads the business identity of an account |
| `business-identities.update` | changes owner declarations or the promotions consent |

The two `get` tools are in the `read` profile and need no confirmation. The two `update` tools are in no profile: a
connection offers them only when its `tools` list names them, and every call needs a confirmation that shows the
account and the fields to change.

## Arguments

`account` is the numeric Merchant Center account ID. It is checked against the targets of the connection before any
secret is read; a sub-account of a bound advanced account is proven by one read of the sub-accounts first, and no
read or change follows an account that is not proven. A refusal does not name the account.

Both update tools take `update_mask`, a required list of the fields to change, and one structured argument per
field. The mask is not free text. Each entry must be one of the paths below, each path may appear once, each listed
path needs its value, and a value without its path is refused. The request carries the mask in the order below and
a body built only from the structured arguments. Nothing is passed through.

| Tool | Allowed `update_mask` entries | Source |
| --- | --- | --- |
| `business-infos.update` | `address`, `customerService`, `koreanBusinessRegistrationNumber` | `updateMask` of `updateBusinessInfo` |
| `business-identities.update` | `blackOwned`, `latinoOwned`, `promotionsConsent`, `smallBusiness`, `veteranOwned`, `womenOwned` | `updateMask` of `updateBusinessIdentity` |

The source is the description of the `updateMask` parameter in the discovery document of the accounts API v1
(revision 20261118), which lists these fields as supported in `snake_case` and `lowerCamelCase`; Qatlas sends
`lowerCamelCase`.

A listed object is a field mask entry for the whole object, so Google takes members you leave out as unset: list
`address` with only a postal code and the other address fields are probably cleared (not verified). Give every member
the object should keep.

### Business info

- `address`: `regionCode` (required, two capital letters, a CLDR region code), `addressLines` (up to 5 lines of up to
  200 characters), `postalCode` (up to 20 characters), `administrativeArea` and `locality` (up to 100 characters
  each). These are the five fields Google reads; it ignores every other address field, so Qatlas offers no others.
- `customer_service`: at least one of `uri`, `email`, and `phone`.
  - `uri` directs customers to a page. It must be an absolute `https` address with a host, without user information,
    without whitespace, control characters, or backslashes, and at most 2048 characters. It is checked by the same
    check as every address of this provider that sends customers somewhere; placeholders such as `{id}` pass. The
    address is text handed to Google and is never fetched.
  - `email` must be one plain address without a display name, at most 254 characters.
  - `phone` takes either `e164Number` (`+` and 7 to 15 digits, no spaces) with an optional `extension` (1 to 40
    digits), or a `shortCode` with `regionCode` (two capital letters) and `number` (1 to 8 digits), not both.
- `korean_business_registration_number`: ten digits as `XXX-XX-XXXXX`.

Texts must not contain control characters. The bounds of the texts and lists are Qatlas's own: the discovery document
names none.

### Business identity

`promotions_consent` is `PROMOTIONS_CONSENT_GIVEN` or `PROMOTIONS_CONSENT_DENIED`. `black_owned`, `latino_owned`,
`small_business`, `veteran_owned`, and `women_owned` are `SELF_IDENTIFIES_AS` or `DOES_NOT_SELF_IDENTIFY_AS`. The
`UNSPECIFIED` values of both enumerations mean that no choice was made and cannot be sent. Google offers the owner
declarations only to businesses with the business country US, and not to marketplaces (the small business
declaration not to marketplaces, the others also not to marketplace sellers); Google refuses what does not apply.
Google marks `promotionsConsent` as required in the resource; as an update with a mask, it is sent only when listed.

## Fields that cannot be written

`phone` and `phoneVerificationState` of the business info are output only in the discovery document and are not in
the update mask, so no tool writes them. The earlier research listed `phone` among the parameters; the discovery
document says output only, and the document holds. The read shows both. The `name` of each resource is the identifier
and is built by Qatlas from the account. No field of either resource is deprecated in the discovery document
(revision 20261118), so none was left out for that reason.

## Result

`business-infos.get` shows `name`, `phone` (`e164Number`, `extension`, `shortCode`), `phoneVerificationState`
(`PHONE_VERIFICATION_STATE_VERIFIED` or `_UNVERIFIED`), `customerService` (`uri`, `email`, `phone`), `address`
(`regionCode`, `recipients`, `languageCode`, `locality`, `addressLines`, `sublocality`, `organization`, `postalCode`,
`sortingCode`, `administrativeArea`, `revision`), and `koreanBusinessRegistrationNumber`. `business-identities.get`
shows `name`, `promotionsConsent`, and `identityDeclaration` of each of the five attributes. The update tools return
`accepted: true` and the object as Google returned it.

The contact data (e-mail address, phone numbers, postal address) is personal data of the merchant. The read tools
show it unmasked, as the provider contracts require for the breadth of the API, and the data goes to the agent that
reads it.

Everything Google returns is untrusted: texts are cut at 300 characters, addresses at 2048, codes at 100, address
line lists hold at most 100 entries, a member of an unexpected type is dropped or ends the call as an invalid answer,
and a member the discovery document does not list is never held. Addresses are text that is never fetched.

## Changing the profile

- **Admin role.** Both methods require admin access in Merchant Center. A user or service account without the Admin
  role gets `403` with the message "User is not an administrator of account ..." from Google. Qatlas cannot check
  the role beforehand: the change is sent once and the error ends as class `permission` with a message that names the
  Admin role and says to ask an administrator of the account. It is not repeated, and a `403` renews no token. The
  read tools do not need the Admin role. The Google message itself is not read to choose the class or the wording.
- **Delayed and public effect.** The result says only that Google accepted the request. Google processes the change
  afterwards, so read the object again to see the state. The address and customer service contacts can be shown to
  customers, and the promotions consent decides whether Google may use the declarations in promotions. There is no
  dry run. A test account is the safe way to try it.
- **No repetition.** A change is sent once. A rate limit (`429`) holds the next request of the credential and is not
  repeated. After a transport failure, a timeout, a `5xx`, or an unreadable answer the message says that the outcome
  is unknown; read the object before changing it again.
- **Idempotency.** Sending the same values again has the same effect as one call, but each call counts against the
  quota of the method group, also when it fails.

## Boundary

Checked against the discovery document of the accounts API v1 (revision 20261118): `getBusinessInfo`,
`getBusinessIdentity`, `updateBusinessInfo`, and `updateBusinessIdentity` take only the resource name and, for the
updates, `updateMask`; the updates state "Executing this method requires admin access" and the document names no
limits on lengths or counts. The homepage, e-mail preferences, and the other settings of the accounts API are separate
tools and not part of this page.

Not verified:

- No live call was made. Answers are taken from the discovery document and the guides of the Merchant API.
- Whether Google accepts an empty mask (it is optional in the document; Qatlas always sends one).
- What Google does with an empty customer service member, and whether it validates e-mail addresses, phone numbers,
  or the address against more than the documented rules. Qatlas checks only the forms above; Google may refuse more.
- Whether a sub-account may be changed through the advanced account or only by an admin of the account itself.
- How long Google needs until a change shows.

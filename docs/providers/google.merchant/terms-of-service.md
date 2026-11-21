---
description: >
  Describes the Google Merchant tools that read terms of service: the latest or a given version of the terms, and the agreement state of an account with the terms it has accepted and must still accept. Covers how the global terms tools relate to targets, argument forms, result members, limits checked against the discovery document, and the deliberately missing acceptance.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant terms of service

Four read tools show which terms of service exist and which an account has accepted or still has to accept. They
change nothing and need no confirmation. [Google Merchant](README.md) describes the credential, targets, quotas,
and errors that apply to them as well.

| Tool | What it reads |
| --- | --- |
| `google.merchant.terms-of-service.get` | one version of the terms of service, by version |
| `google.merchant.terms-of-service.retrieve-latest` | the latest terms of service for a kind and a region |
| `google.merchant.terms-of-service-agreement-states.get` | the agreement state of an account for one kind and country |
| `google.merchant.terms-of-service-agreement-states.retrieve-for-application` | the agreement state of an account for the application terms |

All four are in the profile `read`.

## Terms and targets

`terms-of-service.get` and `terms-of-service.retrieve-latest` read global documents. They act on no account and
take no `account` argument, so the targets of a connection cannot narrow them: they are offered to every
connection, with any targets or none, as long as the effect or profile allows reads. The documents are the same
for every account, and the credential is still needed to call the API.

The two agreement state tools belong to an account. Their `account` argument is checked like that of
`accounts.get`: against the ID form and the targets before any secret is read, with the proof read for an account
that only a bound advanced account contains, and optional on a connection with exactly one target. They do not
need the advanced account target.

## Arguments

| Tool | Argument | Form |
| --- | --- | --- |
| `terms-of-service.get` | `version` | the segment after `termsOfService/` in a name: 1 to 64 characters of letters, digits, `_`, `.`, `-`, starting with a letter or digit |
| `terms-of-service.retrieve-latest` | `kind` | `MERCHANT_CENTER`, the only kind Google offers |
| | `region_code` | a CLDR region code: two capital letters such as `US`, three digits, or `001` for terms that apply in any country |
| `terms-of-service-agreement-states.get` | `account` | the plain account ID |
| | `identifier` | `MERCHANT_CENTER-` and a region code, for example `MERCHANT_CENTER-US` or `MERCHANT_CENTER-EU` |
| `terms-of-service-agreement-states.retrieve-for-application` | `account` | the plain account ID |

Every value is checked against its form before a secret is read or a request is sent. A value goes into the path
or query only after that check, never as free input.

## Results

Terms of service: `name` (`termsOfService/VERSION`), `kind`, `regionCode`, `external`, and `fileUri`. `external`
says the terms can only be accepted through an external process.

Agreement state: `name` (`accounts/ACCOUNT/termsOfServiceAgreementStates/IDENTIFIER`), `regionCode`,
`termsOfServiceKind`, `required` (`termsOfService`, `tosFileUri`), and `accepted` (`termsOfService`,
`acceptedBy`, `validUntil` with `year`, `month`, `day`). `required` is present when the account must accept terms,
`accepted` when it has accepted some. Both can be present: the accepted terms then end on `validUntil`, and the
required ones must be accepted before. For a sub-account, `acceptedBy` can be the advanced account.

The text of the terms is not returned. `fileUri` and `tosFileUri` are addresses of untrusted content, shown as
text and never fetched. Texts are cut as described in the [results bounds](README.md#results), a member that is
not documented or has the wrong type is dropped, and a date part that is not an integer from 0 to 9999 reads as 0,
which Google uses for "not set".

An acceptance, for example in Merchant Center, can take some time to show in the agreement state.

## Boundary

Checked against the discovery document of the accounts API v1 (revision 20261118):

- `termsOfService.get`, `termsOfService.retrieveLatest`, and both agreement state methods are plain `GET`
  methods. `retrieveLatest` takes `kind` and `regionCode` as query parameters, both required by the description.
- The resource name of an agreement state is written `termsOfServiceAgreementState/…` in the description of the
  resource, but the path pattern of the methods is `accounts/{account}/termsOfServiceAgreementStates/{identifier}`.
  The tools use the pattern of the methods. The returned `name` is shown as Google sends it.
- Google documents only the kind `MERCHANT_CENTER`; `TERMS_OF_SERVICE_KIND_UNSPECIFIED` is unused and is not
  accepted.
- The documented lists of region codes and identifiers are open: Google names examples (`MERCHANT_CENTER-EU`,
  `MERCHANT_CENTER-US`) and no complete list, so the tools check only the form.

Not verified: no live call was made, and the role a method needs is not documented for the read methods.

`google.merchant.terms-of-service.accept` is not offered. Accepting terms is a legally binding declaration of a
person for an account, which Qatlas does not make on anyone's behalf. Terms are accepted in Merchant Center.

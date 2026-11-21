---
description: >
  Describes the developer registration tools of the Google Merchant provider: reading the registration of an account, finding the account a Cloud project is registered with and how that answer is bound to the targets of a connection, and unregistering the Cloud project, with the effect on Qatlas access, confirmation, tools list requirement, delayed processing, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant: developer registration

The developer registration ties a Google Cloud project to a Merchant Center account; Google refuses the API calls of
a project that is not registered. These tools read it and can end it. The account argument, targets, quotas, and
errors are described in the [Google Merchant provider](README.md), and [setup](setup.md) explains how to register a
project outside Qatlas.

| Tool | Does |
| --- | --- |
| `developer-registrations.get` | reads the Cloud projects registered with an account |
| `developer-registrations.get-account-for-gcp-registration` | reads the account the Cloud project of the credential is registered with |
| `developer-registrations.unregister-gcp` | unregisters the Cloud project of the credential from an account |

The two reads are in the `read` profile and need no confirmation. `unregister-gcp` is in no profile: a connection
offers it only when its `tools` list names it, and every call needs a confirmation.

## get-account-for-gcp-registration

The method takes no account: Google answers with the account the calling Cloud project is registered with, as
`name` (`accounts/ACCOUNT` or the bare ID). Qatlas therefore cannot check an account argument before the call, and adds none.
Instead it checks the answer before it returns anything:

- Without targets the call reaches what the credential reaches and returns the answer.
- With targets the account must be named by a target, or be a direct sub-account of a bound advanced account (one
  proof read, as for `accounts.get`). Any other account, a name that is neither `accounts/ACCOUNT` nor a plain decimal ID, or an
  answer without a name ends the call. The refusal does not name the account: it says only that the registered
  account lies outside the targets of the connection.
- A connection bound only to account targets is offered the tool; it works when the project is registered with one
  of those accounts.

## unregister-gcp

The `account` argument is required and must be an advanced account the connection is bound to as
`advanced-accounts/ACCOUNT`; without targets any account ID is accepted. A sub-account target, an account target, or
an advanced account that is not bound is refused before any secret is read or request is sent. Google requires the
role Admin.

The call unregisters the Cloud project of the credential. According to Google the project can still call the API for
at most one day after the call, and then loses its API access to the account. The same holds for every other
credential of that project, and for this connection. Qatlas cannot register a project: `registerGcp` is not offered,
because it creates a user and sets a contact address. A new registration has to be made outside Qatlas, as described
in [setup](setup.md). The description of the tool and the text of the confirmation name this effect.

Qatlas sends a fixed empty request once and never repeats it. The result says only that Google accepted the request
(`accepted`, `name`); read the registration again after a while to see the state.

## Boundary

Checked against the reference pages of the methods: `getAccountForGcpRegistration` is `GET
accounts/v1/accounts:getAccountForGcpRegistration` without parameters and body and answers with `name`;
`unregisterGcp` is `POST accounts/v1/accounts/ACCOUNT/developerRegistration:unregisterGcp` with an empty body and an
empty answer, with the scope `content`. The discovery document names the fields `name` and `gcpIds` for the resource.

Not verified: no live call was made. The form of `name` in the answer of `getAccountForGcpRegistration` is open in
the documentation ("the name of the merchant account id"): on a bound connection the tool accepts `accounts/ACCOUNT`
and a plain decimal ID, both checked against the targets, and refuses everything else. The effect on the other
credentials of the project follows from the documented effect on the project and was not observed.

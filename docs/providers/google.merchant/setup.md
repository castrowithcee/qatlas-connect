---
description: >
  Guides a user through connecting Qatlas to Google Merchant Center: a Cloud project with the Merchant API, the developer registration outside Qatlas, an own OAuth client with a consent, a service account, or the impersonation of one, the role needed in Merchant Center, adding it to Qatlas, the connection test, and troubleshooting by symptom.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-21
---

# Set up Google Merchant

This page shows how to connect Qatlas to Google Merchant Center. Configuration, targets, scope, quota, and errors
are described in the [Google Merchant provider](README.md).

## What you need

- A Merchant Center account. A Merchant Center account for testing is enough to try the connection.
- A Google Cloud project with the **Merchant API** enabled. Enable it in the project of the OAuth client or of the
  service account.
- The **developer registration** of that Cloud project with your Merchant Center account. Qatlas never registers a
  project: you do it once, outside Qatlas, see below.
- A Google account, or a service account, that is a user of the Merchant Center account.
- One of three ways to sign in, described below. Pick one per credential.
- Qatlas keeps the secrets in the vault or the system keyring. A credential from environment variables works
  for a service account key or an existing refresh token, but cannot take a consent.

## Access in Merchant Center

All tools use one Google scope, `content`, which allows reading and writing. What the account may do is decided
by its role in Merchant Center, not by the Cloud project. Give the lowest role that the tools of the
connection need. Google documents that changes to account settings and to the home page, and deleting an account,
need the role Admin, and names no role for the data source, omnichannel, checkout, and return policy methods. Whether the role
Read only is enough for the reads is not verified; the connection test shows it.

A service account must be added in Merchant Center under Settings, Access and services, People and access, with
its e-mail address, like a person. It cannot sign in to the Merchant Center interface. For an advanced account,
the user of the advanced account reaches its sub-accounts, so one role on the advanced account is enough.

## Register the Cloud project as a developer

Google requires this once per Cloud project and Merchant Center account, and Qatlas does not do it for you.

1. Sign in with credentials of the Cloud project that Qatlas will use, as a user with the role Admin of the
   Merchant Center account. Use the account the user was added to, not a sub-account through its advanced account.
2. Call `developerRegistration:registerGcp`, for example with the API Explorer of the Merchant API or any HTTP
   client, with your own e-mail address in place of `DEVELOPER_EMAIL`:

```
POST https://merchantapi.googleapis.com/accounts/v1/accounts/ACCOUNT_ID/developerRegistration:registerGcp
{ "developerEmail": "DEVELOPER_EMAIL" }
```

3. Google creates a user with the role API developer for that address, or sends an invitation to accept. The
   account must be a production account, not a test account, and needs a verified website. If the last user with
   the role API developer is removed, Google blocks the API for the project after a grace period of 30 days.

Google describes the steps in its guide [Quickstart: direct API calls](https://developers.google.com/merchant/api/guides/quickstart/direct-api-calls).
The tool `google.merchant.developer-registrations.get` and the connection test show whether the account lists a
registered project. The tool `unregister-gcp` can end the registration and with it the access of Qatlas; a new
registration is always made as above, see [developer registration](developer-registrations.md).

## Way 1: your Google account with a consent

1. In the Google Cloud console, open the project and configure the OAuth consent screen. Set its publishing
   status to **In production**: while it stays in **Testing**, Google ends a consent after seven days and
   Qatlas reports `consent-expired`. Whether the scope `content` needs a verification of the app for an own
   client is not verified.
2. Under Credentials, create an **OAuth client ID** of the type **Desktop app**. Qatlas receives the answer
   on `127.0.0.1` with a port the system picks, which a desktop client allows without registering an address.
3. Store the client ID, and the client secret Google shows with it, in a credential. Each value is read from
   standard input, so it does not appear in the shell history:

```sh
printf %s "$CLIENT_ID" | qconnect credential set merchant client_id
printf %s "$CLIENT_SECRET" | qconnect credential set merchant client_secret
```

4. Give the consent: run `qconnect credential consent merchant` in a terminal, open the printed link, and sign in
   with the account that is a user of the Merchant Center account. The command stores the refresh token.

## Way 2: a service account

1. In the Cloud project, create a service account and a key of the type JSON. The Cloud role of the service
   account does not change what it may do in Merchant Center.
2. Add the service account as a user in Merchant Center, as above.
3. Store the whole key file in the credential:

```sh
qconnect credential set merchant-robot service_account_key < key.json
```

Keys of service accounts are blocked by default in newer Google organizations; then use way 3.

## Way 3: impersonate a service account

Your account signs in as in way 1 and obtains tokens of a service account, without a key file.

1. Enable the **IAM Service Account Credentials API** in the Cloud project.
2. Grant your account the role **Service Account Token Creator** on the service account.
3. Add the service account as a user in Merchant Center, as above. Whether Merchant Center accepts it is not
   verified; the connection test shows it.
4. Set `service_account` on the connection to the service account's e-mail address, then give the consent as in way 1.

## Add it to Qatlas

The guided setup in `qconnect tui` (Connections, `n`) covers service, credential, and access. Written by hand:

```yaml
services:
  merchant:
    provider: google.merchant

credentials:
  merchant:
    provider: google.merchant
    type: keyring

connections:
  merchant:
    service: merchant
    credential: merchant
    targets:
      - advanced-accounts/1000001
```

Leave `targets` out to reach every account the credential can access. Use `advanced-accounts/ID` for an advanced
account with its sub-accounts, and `accounts/ID` for a single account. Run `qconnect config validate` to check the
file; it contacts no provider.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. `[ok]` means Google issued a token, the
Merchant API is enabled, and an account can be read. For an advanced account target, or without a target, it also
means that the account lists a registered Cloud project.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `consent-required` | Run `qconnect credential consent <credential>` |
| `consent-expired` | The consent ended or was revoked, or the consent screen is still in Testing; set it to In production and give the consent again |
| `scope-missing` | Give the consent again; it then asks for the scope your connections need |
| `credential-unusable` | The credential holds both a client ID and a key, or neither; keep one |
| `auth` | Google refused the client ID and secret, or the service account key; check that they belong to the project and were not deleted. An API key does not work |
| `permission` | Check in this order: the Merchant API is enabled in the project of the OAuth client or service account; the Google account or service account is a user of the Merchant Center account; the Cloud project is registered as a developer with the account (see above); the role is high enough. For a sub-account read, the user must be on the advanced account or on the sub-account |
| `permission` only at the advanced account | The registration is missing or the registered projects list is empty: register the project |
| `not-found` | The credential sees no account: add the user in Merchant Center, or check which Google account gave the consent |
| `invalid-request` for an account | The account is outside the targets of the connection, or it is not a direct sub-account of a bound advanced account: bind it as `accounts/ID` |
| `impersonation-denied` | Grant your account the role Service Account Token Creator on the service account |
| `service-account-unknown` | Correct `service_account` on the connection |
| `rate-limited` | Wait. Google limits each method group per minute and per day, and the daily limit resets at 12:00 UTC; `accounts.list` is counted per user |

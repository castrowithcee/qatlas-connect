---
description: >
  Guides a user through setting up the Google Business Profile provider: a Cloud project with the Business Profile APIs, the request for Basic API Access, an own OAuth client and the consent, the role the Google account needs on the profiles, adding it to Qatlas, the connection test, and troubleshooting by symptom.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Set up Google Business Profile

This page shows how to connect Qatlas to Google Business Profile. Credential, targets, bounds, errors, and the
tool groups are described in the [Google Business Profile provider](README.md).

## What you need

- A Google Cloud project that Google approved for the Business Profile APIs. The approval takes a request and
  some days, see below; without it every call fails with `access-not-approved`.
- A Google account that is an owner or manager of the business profiles you want to read.
- Qatlas keeps the secrets in the vault or the system keyring. A credential from environment variables works
  only with an existing refresh token, since it cannot take a consent.

A service account does not work with this provider, see [Credential](README.md#credential).

## Create the Cloud project

1. In the Google Cloud console, create a project or pick one. Its project number is what Google approves.
2. Under APIs and services, enable the **My Business Account Management API** and the **My Business Business
   Information API**. Each Business Profile API is enabled on its own.
   For the verification and notification tools also enable the **My Business Verifications API** and the **My
   Business Notifications API**.

## Request access to the APIs

Google shows the Business Profile APIs with a quota of zero requests per minute until it approves the project.

1. Make sure the business profile you manage is verified and has been active for at least 60 days, with a
   website. Google states these conditions for the request and may change them.
2. Fill in Google's access request form for the Business Profile APIs (Basic API Access), linked from Google's
   Business Profile API documentation, with the project number and the e-mail address of the Google account
   that manages the profile.
3. Wait for Google's answer. Once approved, the quota of the project shows 300 requests per minute under APIs
   and services, Quotas.

## Create the OAuth client and give the consent

1. In the Cloud console, configure the OAuth consent screen of the same project. Set its publishing status to
   **In production**: while it stays in **Testing**, Google ends a consent after seven days and Qatlas reports
   `consent-expired`. Which verification Google asks for before it grants `business.manage` to your own client
   is not established.
2. Under Credentials, create an **OAuth client ID** of the type **Desktop app**. Qatlas receives the answer on
   `127.0.0.1` with a port the system picks, which a desktop client allows without registering an address.
   The client must belong to the approved project.
3. Store the client ID, and the client secret Google shows with it, in a credential. Each value is read from
   standard input, so it does not appear in the shell history:

```sh
printf %s "$CLIENT_ID" | qconnect credential set business-profile client_id
printf %s "$CLIENT_SECRET" | qconnect credential set business-profile client_secret
```

4. Give the consent: run `qconnect credential consent business-profile` in a terminal, open the printed link,
   and sign in with the Google account that manages your profiles. The command stores the refresh token. Over
   SSH, copy the address the browser was sent to and paste it at the hidden prompt. The browser setup
   (`qconnect web`) offers the same on the page of the credential.

## Role on the profiles

The tools see what the consenting Google account sees in Business Profile. An owner or manager of a location
group or of a location can read it; Google's reference names no smaller role for reading. The token holds
`business.manage`, which also allows changes, so use an account whose role is no wider than the task needs.

## Add it to Qatlas

The guided setup in `qconnect tui` (Connections, `n`) covers service, credential, and access. Written by hand:

```yaml
services:
  business-profile:
    provider: google.business-profile

credentials:
  business-profile:
    provider: google.business-profile
    type: keyring

connections:
  business-profile:
    service: business-profile
    credential: business-profile
    target: accounts/1234567890
```

Leave `target` out to reach every account and location of the Google account, or set `locations/LOCATION` to
reach one location. The IDs show in the results of `accounts.list` and `locations.list`. Run
`qconnect config validate` to check the file; it contacts no provider.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. `[ok]` means Google issued a token
with `business.manage`, the API is enabled, and the project is approved, and that the bound account or location
is reachable if the connection has a target.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `access-not-approved` | The project is not approved yet, or the OAuth client belongs to another project. Request access as above and wait for Google's answer |
| `consent-required` | Run `qconnect credential consent <credential>` |
| `consent-expired` | The consent ended or was revoked, or the consent screen is still in Testing; set it to In production and give the consent again |
| `scope-missing` | Give the consent again and allow all requested access |
| `credential-unusable` | The credential holds a service account key, or the connection names a `service_account`; use a consent instead |
| `auth` | Google refused the client ID and secret; check that they belong to the project and were not deleted |
| `permission` | Enable the API in the project of the OAuth client, or give the Google account a role on the profile |
| `not-found` | The bound account or location is not managed by the consenting Google account; check the target |
| `rate-limited` | Wait a minute; an approved project may send 300 requests per minute |

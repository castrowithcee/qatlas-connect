---
description: >
  Guides a user through setting up the Google Tag Manager provider: a Cloud project with the Tag Manager API, an own OAuth client with a consent, a service account, or the impersonation of one, the Tag Manager access each tool group needs, adding it to Qatlas, the connection test, and troubleshooting by symptom.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Set up Google Tag Manager

This page shows how to connect Qatlas to Google Tag Manager. Configuration, targets, scopes, quota, and errors
are described in the [Google Tag Manager provider](README.md); the tools in
[accounts, containers, and workspaces](structure.md), [tags](tags.md), [triggers](triggers.md), and
[built-in variables](built-in-variables.md).

## What you need

- A Google Cloud project with the **Tag Manager API** enabled. Enable it in the project of the OAuth client or
  of the service account.
- A Google account, or a service account, that has access to the Tag Manager accounts and containers you want
  to reach. Access is granted in Tag Manager itself, not in Google Cloud.
- One of three ways to sign in, described below. Pick one per credential.
- Qatlas keeps the secrets in the vault or the system keyring. A credential from environment variables works
  for a service account key or an existing refresh token, but cannot take a consent.

## Access per tool group

| Tool group | Scope | Access in Tag Manager |
| --- | --- | --- |
| [accounts, containers, and workspaces](structure.md) | `tagmanager.readonly` | read access to the account or container |
| [tags](tags.md), [triggers](triggers.md), and [built-in variables](built-in-variables.md), reading | `tagmanager.readonly` | read access to the container |
| [tags](tags.md), [triggers](triggers.md), and [built-in variables](built-in-variables.md), changing | `tagmanager.edit.containers` | edit access to the container |

Qatlas asks for no broader scope than the tools of your connections need. No tool publishes a version, so no
publish right is needed.

## Way 1: your Google account with a consent

1. In the Google Cloud console, open the project and configure the OAuth consent screen. Set its publishing
   status to **In production**: while it stays in **Testing**, Google ends a consent after seven days and
   Qatlas reports `consent-expired`.
2. Under Credentials, create an **OAuth client ID** of the type **Desktop app**. Qatlas receives the answer
   on `127.0.0.1` with a port the system picks, which a desktop client allows without registering an address.
3. Store the client ID, and the client secret Google shows with it, in a credential. Each value is read from
   standard input, so it does not appear in the shell history:

```sh
printf %s "$CLIENT_ID" | qconnect credential set tag-manager client_id
printf %s "$CLIENT_SECRET" | qconnect credential set tag-manager client_secret
```

4. Give the consent: run `qconnect consent tag-manager` in a terminal, open the printed link, and
   sign in with the account that has access in Tag Manager. The command stores the refresh token.

## Way 2: a service account

1. In the Cloud project, create a service account and a key of the type JSON.
2. In Tag Manager, add the service account's e-mail address as a user of the account or container with the
   access of the table above. Whether Tag Manager accepts a service account as a user is not verified; the
   connection test shows it.
3. Store the whole key file in the credential:

```sh
qconnect credential set tag-manager-robot service_account_key < key.json
```

## Way 3: impersonate a service account

Your account signs in as in way 1 and obtains tokens of a service account, without a key file.

1. Enable the **IAM Service Account Credentials API** in the Cloud project.
2. Grant your account the role **Service Account Token Creator** on the service account.
3. Add the service account as a user in Tag Manager as in way 2.
4. Set `service_account` on the connection to the service account's e-mail address, then give the consent as
   in way 1.

## Add it to Qatlas

The guided setup in `qconnect tui` (Connections, `n`) covers service, credential, and access. Written by
hand:

```yaml
services:
  tag-manager:
    provider: google.tag-manager

credentials:
  tag-manager:
    provider: google.tag-manager
    type: keyring

connections:
  tag-manager:
    service: tag-manager
    credential: tag-manager
    target: accounts/1000001/containers/2000002
```

Leave `target` out to reach everything the credential reaches. Choose the profile `write` in the guided setup to
change tags, triggers, and built-in variables. Run `qconnect config validate` to check the file; it contacts no provider. Adding a tool later may
need a new consent, if the tool needs a scope the stored consent lacks.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. `[ok]` means Google issued a token,
the Tag Manager API is enabled, and the account can be read, or the bound container for a container target.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `consent-required` | Run `qconnect consent <credential>` |
| `consent-expired` | The consent ended or was revoked, or the consent screen is still in Testing; set it to In production and give the consent again |
| `scope-missing` | Give the consent again; it then asks for the scopes your connections need |
| `credential-unusable` | The credential holds both a client ID and a key, or neither; keep one |
| `auth` | Google refused the client ID and secret, or the service account key; check that they belong to the project and were not deleted |
| `permission` | Enable the Tag Manager API in the project of the OAuth client or service account, or give the account access in Tag Manager |
| `impersonation-denied` | Grant your account the role Service Account Token Creator on the service account |
| `service-account-unknown` | Correct `service_account` on the connection |
| `rate-limited` | Wait; Tag Manager allows 0.25 requests per second per Cloud project, and Qatlas spaces requests accordingly |

---
description: >
  Guides a user through setting up the Google Search Console provider: a Cloud project with the Search Console API and, for the indexing tools, the Indexing API, an own OAuth client with a consent, a service account, or the impersonation of one, the rights each tool group needs including the owner role of the Indexing API, adding it to Qatlas, the connection test, and troubleshooting by symptom.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Set up Google Search Console

This page shows how to connect Qatlas to Google Search Console. Configuration, target, bounds, and errors are
described in the [Google Search Console provider](README.md); the tools in [sites](sites.md),
[sitemaps](sitemaps.md), [search analytics](search-analytics.md), [URL inspection](url-inspection.md), and
[indexing](indexing.md).

## What you need

- A Google Cloud project with the **Google Search Console API** enabled, and for the indexing tools also the
  **Web Search Indexing API** (the Indexing API), see [Indexing API](#indexing-api).
- A Google account, or a service account, that is a user of the Search Console properties you want to read.
- One of three ways to sign in, described below. Pick one per credential.
- Qatlas keeps the secrets in the vault or the system keyring. A credential from environment variables works
  for a service account key or an existing refresh token, but cannot take a consent.

## Rights per tool group

| Tool group | Scope | Right in Search Console |
| --- | --- | --- |
| [sites](sites.md), reading | `webmasters.readonly` | none to list; owner, full, or restricted user to read a property's data |
| [sites](sites.md), `add` and `delete` | `webmasters` | Google's reference names no level; a refusal shows as `permission` |
| [sitemaps](sitemaps.md), reading | `webmasters.readonly` | owner, full, or restricted user of the property |
| [sitemaps](sitemaps.md), `submit` and `delete` | `webmasters` | Google's reference names no level; use owner or full user, since a restricted user may be refused |
| [search analytics](search-analytics.md) | `webmasters.readonly` | owner, full, or restricted user of the property |
| [URL inspection](url-inspection.md) | `webmasters.readonly` | owner, full, or restricted user of the property |
| [indexing](indexing.md), all four tools | `indexing` | owner of the property |

Qatlas asks for no broader scope than the tools of your connections need: the `webmasters` scope only joins
the consent when a connection offers one of the four changing tools. `sites.add`, `sitemaps.submit`, and
`sitemaps.delete` need the `edit` profile or the effects `create` and `delete`; `sites.delete` needs a `tools`
list that names it. The `indexing` scope joins when a connection offers an indexing tool, which the `edit` profile, the
effect `read`, or a `tools` list does; the `read` profile does not. Adding a property does not verify it, so a
property you add stays without access until you verify it in Search Console.

## Way 1: your Google account with a consent

1. In the Google Cloud console, open the project and configure the OAuth consent screen. Set its publishing
   status to **In production**: while it stays in **Testing**, Google ends a consent after seven days and
   Qatlas reports `consent-expired`.
2. Under Credentials, create an **OAuth client ID** of the type **Desktop app**. Qatlas receives the answer
   on `127.0.0.1` with a port the system picks, which a desktop client allows without registering an address.
3. Store the client ID, and the client secret Google shows with it, in a credential. Each value is read from
   standard input, so it does not appear in the shell history:

```sh
printf %s "$CLIENT_ID" | qconnect credential set search-console client_id
printf %s "$CLIENT_SECRET" | qconnect credential set search-console client_secret
```

4. Give the consent: run `qconnect credential consent search-console` in a terminal, open the printed link, and sign in
   with the account that is a user of your properties. The command stores the refresh token. Over SSH, copy
   the address the browser was sent to and paste it at the hidden prompt. The browser setup (`qconnect web`)
   offers the same on the page of the credential.

Whether Google asks for a verification of your own client before it grants the read scope is not
established.

## Way 2: a service account

1. In the Cloud project, create a service account and a key of the type JSON.
2. In Search Console, open the property, then Settings, Users and permissions, and add the service account's
   e-mail address as a user with **Full** or **Restricted** rights. Whether Google serves a property to a
   service account added this way is known from community reports only; the connection test shows it.
3. Store the whole key file in the credential:

```sh
qconnect credential set search-console-robot service_account_key < key.json
```

Qatlas checks that the key file names Google's token endpoint `https://oauth2.googleapis.com/token` and refuses
any other.

## Way 3: impersonate a service account

Your account signs in as in way 1 and obtains tokens of a service account, without a key file.

1. Enable the **IAM Service Account Credentials API** in the Cloud project.
2. Grant your account the role **Service Account Token Creator** on the service account.
3. Add the service account to the properties as in way 2.
4. Set `service_account` on the connection to the service account's e-mail address, then give the consent as
   in way 1. That consent asks only for the scope the impersonation needs; the read scope goes to the
   service account's token.

## Indexing API

The indexing tools use a second Google API with its own switch and its own rights.

1. In the Cloud project of the OAuth client or service account, enable the **Web Search Indexing API**. The
   connection test does not check it.
2. In Search Console, open the property, then Settings, Users and permissions, and add the account behind
   the credential as an **Owner**; for a service account use its e-mail address. Google accepts notifications
   only from an owner, and only for URLs of that property.
3. Use the Indexing API only for URLs whose content carries `JobPosting` or `BroadcastEvent` structured data.
   Google sets this limit, and Qatlas does not check it.
4. The notification tools are offered only by a connection whose `tools` list names them, see
   [indexing](indexing.md#listed-only-notifications). With way 1, give the consent again after adding an
   indexing tool, so it covers the `indexing` scope.

## Add it to Qatlas

The guided setup in `qconnect tui` (Connections, `n`) covers service, credential, and access. Written by
hand:

```yaml
services:
  console:
    provider: google.search-console

credentials:
  search-console:
    provider: google.search-console
    type: keyring

connections:
  search-console:
    service: console
    credential: search-console
    target: sc-domain:example.com
```

Leave `target` out to reach every property of the account. Run `qconnect config validate` to check the file;
it contacts no provider. Adding a tool later may need a new consent, if the tool needs a scope the stored
consent lacks.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. `[ok]` means Google issued a token with the read scope, the Search
Console API is enabled, and the account has access to at least one property, and to the bound one if the
connection has a target.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `consent-required` | Run `qconnect credential consent <credential>` |
| `consent-expired` | The consent ended or was revoked, or the consent screen is still in Testing; set it to In production and give the consent again |
| `scope-missing` | Give the consent again; it then asks for the scopes your connections need, also the `webmasters` scope of a changing tool |
| `permission` on `sites.delete` or a sitemap change | Give the account a higher level on the property in Search Console; Google's reference does not name the level a change needs |
| `credential-unusable` | The credential holds both a client ID and a key, or neither; keep one |
| `auth` | Google refused the client ID and secret, or the service account key; check that they belong to the project and were not deleted |
| `permission` in the test | Enable the Google Search Console API in the project of the OAuth client or service account, or give the account rights on the bound property |
| `not-found` in the test | The account sees no property with rights; add it as a user in Search Console |
| `impersonation-denied` | Grant your account the role Service Account Token Creator on the service account |
| `service-account-unknown` | Correct `service_account` on the connection |
| `rate-limited` | Wait a minute; Google allows 200 requests per minute per user for these tools |
| `permission` on an indexing tool | Make the account an owner of the property and enable the Web Search Indexing API in the Cloud project |
| `rate-limited` on an indexing notification | The daily quota of 200 notifications is used up, by Qatlas' count or Google's; it resets at midnight Pacific Time |
| `scope-missing` on an indexing tool, or in the test after adding one | Give the consent again; it then asks for the `indexing` scope |

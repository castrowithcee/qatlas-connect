---
description: >
  Guides a user through setting up the Google Ads provider: a Cloud project with the Google Ads API and its access level, an own OAuth client with a consent, a service account added as a user in Google Ads, or the impersonation of one, the manager account in login_customer_id, the targets, adding it to Qatlas, the connection test, and troubleshooting by symptom.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-23
---

# Set up Google Ads

This page shows how to connect Qatlas to the Google Ads API. A connection lists the accounts a credential
reaches, reads account settings, lists the hierarchy below a manager account, lists invitations and approval
requests of an account, and, when its `tools` list names them, changes account settings and turns down invitations and
approval requests. Configuration, targets,
errors, and limits are described in the [Google Ads provider](README.md).

## What you need

- A Google Cloud project with the **Google Ads API** enabled. Enabling it gives the project the access level
  **Test**, which reaches test accounts only.
- For real accounts, a higher access level of that project (below).
- A Google account, or a service account, that is a user of the Google Ads accounts you want to read.
- One of three ways to sign in, described below. Pick one per credential.
- Qatlas keeps the secrets in the vault or the system keyring. A credential from environment variables works
  for a service account key or an existing refresh token, but cannot take a consent.
- No developer token: Google ended them, and Qatlas sends none.

## Access level of the Cloud project

The access level belongs to the Cloud project that owns the OAuth client or the service account. Google states:

| Level | Production accounts | Test accounts | How |
| --- | --- | --- | --- |
| Test | none | 15,000 operations a day | given when you enable the API |
| Explorer | 2,880 operations a day | 15,000 | apply on the Google Ads API overview page in the Cloud console, under "Upgrade access level"; usually approved automatically |
| Basic | 15,000 operations a day | 15,000 | apply the same way after brand verification |
| Standard | unlimited | unlimited | "Start application" on the same page, from Basic; a manual review of about 10 business days |

Each call of a Qatlas tool and each membership read counts as one operation, also when it fails. A change with a
list of entries counts one operation per entry. With Test access, a call on a production account ends as
`permission` and names the test access. Check the current terms on Google's page about access levels; they change
from time to time.

## Rights in Google Ads

Google Ads has one scope, `adwords`, for reading and changing. Qatlas asks for it for every tool. The rights come
from the role of the account in Google Ads. The read tools only read, so the role **Read-only** is
enough for them, except the lists of invitations and approval requests, whose role Google does not document (see
[invitations and approval requests](access.md); the services need at least the Basic access level). The invoice list needs at least the Basic access level as well and an account with monthly invoicing (see
[invoices, incentives and app analytics links](invoices.md)). The creator insights are open to allowlisted customers only (see
[suggestion tools](suggestions.md)). The tools that change
settings (see [settings tools](settings.md)), campaigns (see [campaign tools](campaigns.md)), campaign budgets (see [campaign budget tools](campaign-budgets.md)), ad groups (see [ad group tools](ad-groups.md)), asset groups (see [asset group tools](asset-groups.md)), ad group criteria (see [ad group criterion tools](ad-group-criteria.md)), ads (see [ad tools](ads.md)), ad group bid modifiers (see [ad group bid modifier tools](ad-group-bid-modifiers.md)), the content of ads and ad parameters (see [ad content tools](ad-content.md); `ad-parameters.create` and `.update` are offered by their effect), bidding strategies (see [bidding strategy tools](bidding-strategies.md)), conversion actions (see [conversion actions](conversions.md)), goals and campaign goal configs (see [goal tools](goals.md)), conversion goal settings (see [conversion goal setting tools](conversion-goals.md)), conversion value rules (see [conversion value rule tools](conversion-value-rules.md)), conversion value rule sets (see [conversion value rule set tools](conversion-value-rule-sets.md)), custom conversion goals (see [custom conversion goal tools](custom-conversion-goals.md)), bidding data exclusions (see [bidding data exclusions](bidding-data-exclusions.md)), or assets (see [asset tools](assets.md)), `campaign-groups.remove` (see [campaign group tools](campaign-groups.md)), those that revoke an invitation,
turn down a request, change a manager or client link or remove a link, and `recommendations.apply` (see [recommendation tools](recommendations.md))
are offered only when the
`tools` list of a connection names them (the link changes need at least the Basic access level); Google does not document which call needs which role, so test them with
`validate_only` first. The link tools `product-link-invitations.update` and `data-links.update` are offered by the
effect `update` as well, and they and `data-links.remove` have no `validate_only` (see [link tools](account-links.md)).
Give the account no more than it needs, since the token itself would allow changes.

The [billing tools](billing.md) only read. Google blocks `PaymentsAccountService`, which `payments-accounts.list`
calls, at the Explorer access level, so that tool needs at least Basic; whether the same holds for the queries of
the other three billing tools is not verified. On a manager account every billing tool needs a manager target.

If the account reaches a client through a manager account, set `login_customer_id` on the service to the ID of
that manager. Without it Google evaluates the direct access of the account and refuses the call.

## Way 1: your Google account with a consent

1. In the Google Cloud console, open the project and configure the OAuth consent screen. Set its publishing
   status to **In production**: while it stays in **Testing**, Google ends a consent after seven days and
   Qatlas reports `consent-expired`.
2. Under Credentials, create an **OAuth client ID** of the type **Desktop app**.
3. Store the client ID, and the client secret Google shows with it, in a credential. Each value is read from
   standard input, so it does not appear in the shell history:

```sh
printf %s "$CLIENT_ID" | qconnect credential set google-ads client_id
printf %s "$CLIENT_SECRET" | qconnect credential set google-ads client_secret
```

4. Give the consent: run `qconnect credential consent google-ads` in a terminal, open the printed link, and sign
   in with the account that is a user of your Google Ads accounts. The command stores the refresh token. Over
   SSH, copy the address the browser was sent to and paste it at the hidden prompt. The browser setup
   (`qconnect web`) offers the same on the page of the credential.

## Way 2: a service account

1. In the Cloud project, create a service account and a key of the type JSON. Some organizations block service
   account keys; use way 3 then.
2. Sign in to Google Ads as an administrator, open **Admin**, then **Access and security**, and add the e-mail
   address of the service account as a user with the role **Read-only** or higher. Service accounts do not
   support **Email only**, and Google does not let you make one an administrator at first. One address can be
   a user of at most 20 accounts, manager accounts included; add it to a manager account to reach more, and set
   `login_customer_id` to that manager.
3. Store the whole key file in the credential:

```sh
qconnect credential set google-ads-robot service_account_key < key.json
```

Qatlas checks that the key file names Google's token endpoint `https://oauth2.googleapis.com/token` and refuses
any other.

## Way 3: impersonate a service account

Your account signs in as in way 1 and obtains tokens of a service account, without a key file.

1. Enable the **IAM Service Account Credentials API** in the Cloud project.
2. Grant your account the role **Service Account Token Creator** on the service account.
3. Add the service account in Google Ads as in way 2.
4. Set `service_account` on the connection to the e-mail address of the service account, then give the consent
   as in way 1.

Google does not document this way for Google Ads, nor which project then decides the access level. Try it with
the connection test and a call of `customers.get`.

## Add it to Qatlas

The guided setup in `qconnect tui` (Connections, `n`) covers service, credential, and access. Written by
hand, with the accounts reached through the manager account 1234567890:

```yaml
services:
  ads:
    provider: google.ads
    options:
      login_customer_id: "1234567890"

credentials:
  google-ads:
    provider: google.ads
    type: keyring

connections:
  google-ads:
    service: ads
    credential: google-ads
    targets:
      - managers/1234567890
```

Write the IDs without hyphens. Leave `options` out if the account reaches its accounts directly. Leave `targets`
out to reach every account of the credential, or name single accounts with `customers/CUSTOMER_ID`. For a second
manager hierarchy, add another service with its own `login_customer_id` and a connection on it. Run
`qconnect config validate` to check the file; it contacts no provider. The form of `login_customer_id` is checked
on the first call, before a secret is read.

## Check the connection

In `qconnect tui`, open Connections, select the connection, and press `t`. `[ok]` means Google issued a token
with the `adwords` scope, the Google Ads API is enabled in the project, and the account was accepted. The test
does not show the access level or the access to a target; call `customers.get` with the ID of a target for that.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| `permission` naming test access | The Cloud project has the access level Test. Apply for Explorer or higher, or use a test account |
| `permission` naming `login_customer_id` | The account reaches the client through a manager: set `login_customer_id` to that manager, or correct it if the manager does not reach the client |
| `permission` naming the API | Enable the Google Ads API in the project of the OAuth client or service account |
| `permission` naming the terms of service | Accept the terms of the Google Ads API on its page in the Cloud console |
| `auth` naming no Google Ads account | Add the Google account or the service account as a user in Google Ads |
| `auth` | Google refused the token; check that the client or key belongs to the project and was not deleted |
| `not-found` | No account has this ID; check `customer_id` |
| `invalid-request`, not available to this connection | The account is no target and not below a manager target; add a target or check the ID |
| An error naming the membership check | The manager target could not be read to prove the account below it; the class says why |
| `provider-error` naming `login_customer_id` | The value of the option is no account Google accepts, or the option is malformed |
| `consent-required` | Run `qconnect credential consent <credential>` |
| `consent-expired` | The consent ended or was revoked, or the consent screen is still in Testing; set it to In production and give the consent again |
| `scope-missing` | Give the consent again |
| `credential-unusable` | The credential holds both a client ID and a key, or neither; keep one |
| `impersonation-denied` | Grant your account the role Service Account Token Creator on the service account |
| `rate-limited` | Wait; the daily operations of the access level may be used up. Qatlas never repeats a failed call |

---
description: >
  Describes the Google Cloud service account key tools: the reads service-account-keys.list and service-account-keys.get (metadata only, the key types filter, the checks of project and key names, the disable reason) and the revocations disable and enable (profile service-accounts-revoke) and the listed-only delete, the scope, and why public and private key material is never returned.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud service account keys

Two read tools of the IAM API (`https://iam.googleapis.com`, discovery `iam` v1, revision 20261106): the metadata
of the keys of a service account and of one key. Three more tools revoke a key (see
[Revoking keys](#revoking-keys)). The account and project rules of
[service accounts](service-accounts.md) apply unchanged. [The provider page](README.md) holds targets, errors,
and rate spacing.

| Tool | Google method | Arguments |
| --- | --- | --- |
| `service-account-keys.list` | `projects.serviceAccounts.keys.list` | `project_id`, `account`, `key_types` |
| `service-account-keys.get` | `projects.serviceAccounts.keys.get` | `project_id`, `account`, `key_id` |

`key_types` is a list of `USER_MANAGED` and `SYSTEM_MANAGED`, each at most once; without it all keys are listed.
Qatlas sends it as repeated `keyTypes` query parameters. `KEY_TYPE_UNSPECIFIED` is refused, because Google ends
the request with an error for it. The list is not paged: Google's method has no page parameters.
`key_id` is the last segment of the key name and is checked against a plain ID pattern.

## No key material

No tool of this provider returns key material. The result types hold only the metadata fields, so
`privateKeyData` and `publicKeyData` are dropped when Google sends them, and so are other fields Qatlas does not
name. The argument `publicKeyType` of Google's get method is not offered and never sent, so Google returns no
public key. A public key counts as an access value and is only for a tool that a connection names expressly in
its `tools`; this package has none. No tool creates or uploads a key.

## Result

Each key carries `name`, `keyAlgorithm`, `keyOrigin`, `keyType`, `validAfterTime`, `validBeforeTime`, `disabled`
(present only when true), `disableReason` (for example
`SERVICE_ACCOUNT_KEY_DISABLE_REASON_EXPOSED`), and `extendedStatus` (a list of `{key, value}`, for example a
detected exposure). Text is untrusted and cut.

The key names must belong to the checked project (`projects/PROJECT_ID/serviceAccounts/ACCOUNT/keys/KEY_ID`),
and a key asked for by `key_id` must carry that ID; otherwise the answer is an invalid response and nothing is
returned. The account segment is not compared with `account`, because Google may name it by email or unique ID.

## Scope and profile

Discovery `iam` v1 lists only `cloud-platform` for both read methods, so the read tools belong to the profile
`read-iam`. The tools `disable` and `enable` belong to `service-accounts-revoke`, and `delete` is listed-only. The rights are listed in [setup](setup.md).

## Left out on purpose

Creating and uploading a key, and the data fields `publicKeyData`,
`privateKeyData`, and `privateKeyType`.

## Revoking keys

| Tool | Effect | Google method | Arguments |
| --- | --- | --- | --- |
| `service-account-keys.disable` | update | `projects.serviceAccounts.keys.disable` | `project_id`, `account`, `key_id` |
| `service-account-keys.enable` | update | `projects.serviceAccounts.keys.enable` | `project_id`, `account`, `key_id` |
| `service-account-keys.delete` | delete | `projects.serviceAccounts.keys.delete` | `project_id`, `account`, `key_id` |

`disable` and `enable` belong to the profile `service-accounts-revoke`, which holds only these two tools, is not
recommended, and is not part of `read-iam`; the connection needs the effect `update`. `delete` is listed-only: it
belongs to no profile, and a connection offers it only when its `tools` list names it. They ask for
`cloud-platform`, the only scope discovery `iam` v1 lists for these methods. The account needs the permissions
`iam.serviceAccountKeys.disable`, `enable`, or `delete` on the project, which Google decides. Discovery lists no
member in the disable and enable requests, so the tools take no reason and send an empty body.

Every call needs the confirmation of a person. It names the project, the account, and the key. Deleting a key is
not reversible, and the confirmation says so. The project check and the key ID check are those of the reads and
happen before any secret is read.

- `disable` and `enable` are idempotent. Google answers with an empty body, so the result is `projectId`,
  `account` and `keyId` as given, and `disabled` (true after `disable`). A disabled key can be enabled again.
- `delete` removes the key for good. The result is `projectId`, `account`, `keyId`, and `deleted`. Google refuses
  system-managed keys, which shows as a provider error. A key that does not exist ends as `not_found`. Deleting a
  key does not revoke short-lived credentials already issued from it.
- A change is sent once and never repeated. After a transport failure, a 5xx answer, or an unreadable answer the
  message says that the outcome is unknown: read the key before changing it again.
- No answer carries key material, and no tool creates or uploads a key.

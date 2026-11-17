---
description: >
  Describes the Google Cloud API key tools: reading the metadata of the API keys of a project with api-keys.list and api-keys.get, deleting a key with api-keys.delete and restoring it with api-keys.undelete (profile api-keys-revoke), the operations they return, the scopes, and why the value of a key is never returned.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud API keys

Four tools of the API Keys API (`https://apikeys.googleapis.com`, discovery `apikeys` v2, revision 20261110). The
keys of a project live at `projects/PROJECT/locations/global`, which is the only location the tools use. The
API Keys API (`apikeys.googleapis.com`) must be enabled in the Cloud project of the credential. [The provider
page](README.md) holds targets, errors, and rate spacing.

| Tool | Effect | Google method | Arguments |
| --- | --- | --- | --- |
| `api-keys.list` | read | `projects.locations.keys.list` | `project_id`, `show_deleted`, `page_size`, `cursor` |
| `api-keys.get` | read | `projects.locations.keys.get` | `project_id`, `key_id` |
| `api-keys.delete` | delete | `projects.locations.keys.delete` | `project_id`, `key_id`, `etag`, `check_existing_usage` |
| `api-keys.undelete` | update | `projects.locations.keys.undelete` | `project_id`, `key_id` |

`project_id` is a project ID or number, never a `projects/` name. It must lie in the targets of the connection:
with project targets alone another project is refused before a secret is read, and where an organization or a
folder is a target Qatlas reads the project and the folders above it first, as `projects.get` does. `key_id` is
the last segment of the key name (lowercase letters, digits, and hyphens, at most 63 characters) and is checked
before any request.

## No key value

No tool returns the value of a key (`keyString`). The tools never call `getKeyString`, the result types do not
hold the member, so it is dropped where Google sends it, and it is also removed, at any depth, from the metadata
and the response of every operation, because the operation of an undelete carries the restored key. No tool
creates a key, patches its restrictions, or looks a key up by its value.

## Reading

`api-keys.list` takes `show_deleted` (also the keys deleted in the last 30 days), `page_size` (1 to 100), and the
`next_cursor` of the previous page as `cursor`. The cursor is bound to the tool, the targets, the project, and
`show_deleted`. A page of more than 200 rows is an invalid response.

Each key carries `name`, `uid`, `displayName`, `createTime`, `updateTime`, `deleteTime` (only for a deleted
key), `annotations`, `restrictions` (`apiTargets` with service and methods, `androidKeyRestrictions`,
`browserKeyRestrictions`, `serverKeyRestrictions`, `iosKeyRestrictions`), `serviceAccountEmail`, and `etag`. A key
without `restrictions` has none. Text is untrusted and cut, and lists hold at most 100 entries.

The name of a key must have the form `projects/PROJECT/locations/global/keys/KEY_ID`, and a key asked for by
`key_id` must carry that ID; otherwise the answer is an invalid response and nothing is returned. Google names the
project by number, so the project segment is compared with `project_id` only where both are IDs or both are
numbers.

## Deleting and restoring

Every call needs the confirmation of a person. It names the project and the `key_id`, and for a delete says
that applications that use the key fail afterwards and that Google allows a restore for 30 days only. It does not
name the display name of the key, because a confirmation is built from the arguments alone, without a request.
The connection needs the effects `update` and `delete`.

- `api-keys.delete` sends one `DELETE`. `etag` (from `api-keys.get`) makes the delete conditional on the state
  that was read. `check_existing_usage` is `CHECK` (Google refuses if the key was used lately) or `SKIP`;
  without it Google decides. Google keeps the deleted key for 30 days.
- `api-keys.undelete` sends one `POST` with an empty request. Outside the 30 days, Google refuses.
- Both return the long-running operation as Google answered it: `name`, `done`, `metadata`, and, once done, the
  `response`. They never wait. `operations.get` with `service` `api-keys` reads the state by name. A done
  operation with an error is a provider error.
- A stale `etag` and a restore after the 30 days end as the class `provider-error` (Google status
  `FAILED_PRECONDITION`), an unknown key as `not-found`. A change is sent once and never repeated. After a
  transport failure, a 5xx answer, or an unreadable answer the message says that the outcome is unknown: read
  the key before changing it again.

## Scope and profile

Discovery `apikeys` v2 lists `cloud-platform.read-only` next to `apikeys.readonly`, `apikeys`, and
`cloud-platform` for both reads, so they belong to the profile `read` and ask for the read-only scope. For
`delete` and `undelete` it lists `apikeys` and `cloud-platform`. Both belong to the profile `api-keys-revoke`, which
holds only them, is not recommended, and asks for the narrower `apikeys`. Google uses that scope also to create
keys and read their values, so the token may do that although no tool does. The rights are listed in
[setup](setup.md).

## Left out on purpose

Creating a key, `getKeyString`, `lookupKey` (it takes the value of a key), and `patch` (it could widen the
restrictions of a key).

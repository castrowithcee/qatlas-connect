---
description: >
  Describes the zone tools of the Google Tag Manager provider: listing, reading, creating, replacing, deleting, and reverting the zones of a workspace, the members of a zone, what a child container pulls in, how the target of a connection bounds child containers, the confirmation, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: zones

The zone tools of the [Google Tag Manager provider](README.md). The update, revert, and never-cut rules shared
with tags, triggers, and variables are described there.

A zone fences tags, triggers, and variables to a part of a container. A zone can also bring another container in
through `childContainer`, which pulls the configuration of that container into this one. A change is made in the
workspace only and takes effect only with a published container version, which no tool of this provider publishes.
Every change is confirmed. The confirmation shows the name, the workspace or zone path, and the public ID of
every child container, never a condition value.

| Tool | Does |
| --- | --- |
| `zones.list` | reads the zones of a workspace, a batch per call with `cursor` |
| `zones.get` | reads one zone by its path, the fingerprint included |
| `zones.create` | creates a zone in a workspace; a repeated call creates a second zone |
| `zones.update` | replaces a zone as a whole; needs `fingerprint` |
| `zones.delete` | deletes a zone from the workspace |
| `zones.revert` | discards the workspace changes of a zone; needs `fingerprint`; only by `tools` list |

`workspace` and `zone` are paths such as `accounts/1/containers/2/workspaces/3/zones/5`; both are checked
against the target of the connection. `zones.delete` is in the profile `write` and stays revertible until a
version is published. `zones.revert` overwrites the workspace state of a zone and is in no profile.

## Members

The arguments of `create` and `update` are the members of the zone. `name` is needed. The others are:

- `boundary` with `condition` and `customEvaluationTriggerId`. A condition has the form of a trigger filter: a
  `type` and `parameter` entries such as `arg0` (a variable reference), `arg1`, `ignore_case`, and `negate`. The
  conditions together make up the boundary, and the trigger IDs make the zone evaluate it.
- `childContainer`, a list of `publicId` (such as `GTM-XXXX`) and `nickname`.
- `typeRestriction` with `enable` and `whitelistedTypeId`, the public IDs of the types allowed in the zone.
- `notes`.

Members that Google assigns (`path`, `accountId`, `containerId`, `workspaceId`, `zoneId`, `fingerprint`,
`tagManagerUrl`) are results only and are never sent. A zone has no `type`, `parameter`, or `priority`.

Names, notes, nicknames, and condition values are untrusted content of whoever edits the account. They are
returned and sent unchanged, are never run or fetched, and are never cut.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "name": "Example zone",
 "boundary": {"condition": [{"type": "contains", "parameter": [
   {"type": "template", "key": "arg0", "value": "{{Page URL}}"},
   {"type": "template", "key": "arg1", "value": "example.invalid"}]}]},
 "childContainer": [{"publicId": "GTM-EXAMPLE", "nickname": "Example child"}]}
```

## Child containers and the target

`publicId` is the public ID of a container, not its numeric path, so the target cannot be compared with it
directly. `zones.create` and `zones.update` therefore check it before they send the change:

| Target | Check |
| --- | --- |
| none | none; the connection reaches what the credential reaches |
| `accounts/ACCOUNT` | the tool reads all containers of the account and refuses a `publicId` that is not exactly the public ID of one of them; nothing is changed |
| `accounts/ACCOUNT/containers/CONTAINER` | a non-empty `childContainer` is refused without any request, because no other container lies inside the target |

The refusal is `invalid-request`. The read of the containers runs under the tool of the call, so its scope
applies, and it spends requests of the shared quota: one per page of containers before the change.

Not verified: the answer of Google to a stale fingerprint, to a `publicId` of a container the credential cannot
reach, and which conditions a boundary accepts.

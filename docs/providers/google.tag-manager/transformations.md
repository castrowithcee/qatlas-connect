---
description: >
  Describes the transformation tools of the Google Tag Manager provider: listing, reading, creating, replacing, deleting, and reverting the transformations of a server container workspace, the members of a transformation, the effect of a transformation and when it applies, the confirmation, the error of a web container, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: transformations

The transformation tools of the [Google Tag Manager provider](README.md). The update, revert, parameter, and
never-cut rules shared with tags, triggers, and variables are described there.

A transformation changes, filters, or removes event data before tags receive it. A change is made in the
workspace only and takes effect only with a published container version, which no tool of this provider publishes,
and no tool activates a transformation without publishing. Every change is confirmed, and the confirmation
shows the name, the type, and the workspace or transformation path, never a parameter value.

| Tool | Does |
| --- | --- |
| `transformations.list` | reads the transformations of a workspace, a batch per call with `cursor` |
| `transformations.get` | reads one transformation by its path, the fingerprint included |
| `transformations.create` | creates a transformation in a workspace; a repeated call creates a second one |
| `transformations.update` | replaces a transformation as a whole; needs `fingerprint` |
| `transformations.delete` | deletes a transformation from the workspace |
| `transformations.revert` | discards the workspace changes of a transformation; needs `fingerprint`; only by `tools` list |

`workspace` and `transformation` are paths such as `accounts/1/containers/2/workspaces/3/transformations/5`; both
are checked against the target of the connection. `transformations.delete` is in the profile `write` and stays
revertible until a version is published. `transformations.revert` overwrites the workspace state of a
transformation and is in no profile.

## Server containers only

Transformations exist in containers of usage context `server`. The tools do not check the usage context. For a
web container, Google answers with an error, and Qatlas passes it on in the form of every unmapped status:
`invalid-provider-response` with Google's answer as the original and no class of its own.

## Members

The arguments of `create` and `update` are the members of the transformation. `name` and `type` are needed, and
`type` is the transformation type of Tag Manager; the parameters it takes depend on it. The others are
`parameter`, `notes`, and `parentFolderId`. Members that Google assigns (`path`, `accountId`, `containerId`,
`workspaceId`, `transformationId`, `fingerprint`, `tagManagerUrl`) are results only and are never sent.

Names, notes, and parameter values are untrusted content of whoever edits the account. They are returned and
sent unchanged, are never run or fetched, and are never cut.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "name": "Example transformation", "type": "example-type",
 "parameter": [{"type": "boolean", "key": "example-key", "value": "true"}]}
```

`transformations.revert` answers with the transformation as it is in the latest container version. The member
is missing when the version holds none, so the revert removed the transformation from the workspace.

Not verified: the answer of Google to a transformation call on a web container, which parameters each
transformation type needs or accepts, and the answer to a stale fingerprint.

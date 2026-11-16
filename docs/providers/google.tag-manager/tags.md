---
description: >
  Describes the tag tools of the Google Tag Manager provider: listing, reading, creating, replacing, deleting, and reverting the tags of a workspace, the members of a tag, the confirmation, and why a tag change stays in the workspace.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Tag Manager: tags

The tag tools of the [Google Tag Manager provider](README.md). The update, revert, parameter, and never-cut
rules shared with triggers are described there.

A tag of type `html` (Custom HTML) or `img` (Custom Image) runs code or calls the network on the pages of the
container once a published version holds it, which is why every change is confirmed.

| Tool | Does |
| --- | --- |
| `tags.list` | reads the tags of a workspace, a batch per call with `cursor` |
| `tags.get` | reads one tag by its path, the fingerprint included |
| `tags.create` | creates a tag in a workspace; a repeated call creates a second tag |
| `tags.update` | replaces a tag as a whole; needs `fingerprint` |
| `tags.delete` | deletes a tag from the workspace |
| `tags.revert` | discards the workspace changes of a tag; needs `fingerprint`; only by `tools` list |

`workspace` and `tag` are paths such as `accounts/1/containers/2/workspaces/3/tags/4`; both are checked against
the target of the connection.

The arguments of `create` and `update` are the members of the tag: `name` and `type` (needed), `parameter`,
`firingTriggerId`, `blockingTriggerId`, `setupTag`, `teardownTag`, `paused`, `liveOnly`, `priority`, `notes`,
`scheduleStartMs`, `scheduleEndMs`, `parentFolderId`, `tagFiringOption`, `monitoringMetadata`,
`monitoringMetadataTagNameKey`, and `consentSettings`. Members that Google assigns (`path`, `accountId`,
`containerId`, `workspaceId`, `tagId`, `fingerprint`, `tagManagerUrl`) are results only and are never sent.
`priority` is one parameter, `monitoringMetadata` and `consentSettings.consentType` two levels.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "name": "Example tag", "type": "html",
 "paused": true,
 "parameter": [{"type": "template", "key": "html", "value": "<!-- example -->"},
               {"type": "list", "key": "rules", "list": [
                 {"type": "map", "map": [{"type": "template", "key": "k", "value": "v"}]}]}]}
```

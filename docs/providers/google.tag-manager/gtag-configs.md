---
description: >
  Describes the Google tag configuration tools of the Google Tag Manager provider: listing, reading, creating, replacing, and deleting the Google tag configurations of a workspace, the members, the destination IDs and the confirmation that shows them before and after a change, the previous_parameter check, the effect and when it applies, why no revert and no destination link exist, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: Google tag configurations

The Google tag configuration tools of the [Google Tag Manager provider](README.md). The update, parameter, and
never-cut rules shared with tags, triggers, and variables are described there.

A Google tag configuration carries measurement IDs and destination parameters, such as the ID of a Google
Analytics data stream or of an Ads account. It is a destination of the measurement data that the container
sends. A change is made in the workspace only and takes effect only with a published container version,
which no tool of this provider publishes.

| Tool | Does |
| --- | --- |
| `gtag-configs.list` | reads the configurations of a workspace, a batch per call with `cursor` |
| `gtag-configs.get` | reads one configuration by its path, the fingerprint included |
| `gtag-configs.create` | creates a configuration in a workspace; a repeated call creates a second one |
| `gtag-configs.update` | replaces a configuration as a whole; needs `fingerprint` and `previous_parameter` |
| `gtag-configs.delete` | deletes a configuration from the workspace |

`workspace` and `gtag_config` are paths such as `accounts/1/containers/2/workspaces/3/gtag_config/5`; both are
checked against the target of the connection. All five tools are in the profile `write`, the two that read also
in `read`. A configuration has no `revert` tool: the discovery document lists no such method for it. The
tools do not link destinations either: creating a link to a destination of another account (`destinations.link`)
is not part of this provider.

## Members

The arguments of `create` and `update` are the members of the configuration: `type`, which is needed and
names the Google tag configuration type (the parameters it takes depend on it), and `parameter`. Members that
Google assigns (`path`, `accountId`, `containerId`, `workspaceId`, `gtagConfigId`, `fingerprint`,
`tagManagerUrl`) are results only and are never sent. A configuration has no name, notes, folder, or priority.

Parameter values are untrusted content of whoever edits the account. They are returned and sent unchanged,
are never run or fetched, and are never cut.

## The confirmation shows the destination

Every change is confirmed. The confirmation shows the type and the workspace or configuration path, and of
the parameters only the destination IDs, never another value. A destination ID is a parameter of type
`template` whose `key` ends in `Id` or `ID`, found at any depth, also inside a `list` or `map`. This is a
heuristic on the key: a destination that Google names otherwise is not shown.

- `create` shows the destination IDs with their new value.
- `update` shows each destination ID whose value differs between `previous_parameter` and `parameter` as
  `key: "old" -> "new"`. An ID that is added or removed is shown as `(none)` on the missing side. A nested one
  is named by the keys on the way, such as `destinations/[0]/accountID`, where an unkeyed entry counts as its
  position. When no ID differs, the confirmation says so.
- `delete` shows the configuration path.

The confirmation is phrased from the arguments alone, so the old ID comes from `previous_parameter`, which
`update` requires: the `parameter` list of the configuration as last read with `gtag-configs.get`, in the same
form as `parameter`; an empty list for a configuration without parameters. The tool then makes the old ID
trustworthy: before it changes anything it reads the configuration and refuses the call as `invalid-request`,
without a write request, when `previous_parameter` is not exactly the stored `parameter` list (the same
entries in the same order). Read the configuration again and send what it returned. A call without
`previous_parameter` or without `fingerprint` is refused before any request.

```json
{"gtag_config": "accounts/1/containers/2/workspaces/3/gtag_config/5", "fingerprint": "1700000000000",
 "type": "googtag",
 "previous_parameter": [{"type": "template", "key": "tagId", "value": "G-EXAMPLE1"}],
 "parameter": [{"type": "template", "key": "tagId", "value": "G-EXAMPLE2"}]}
```

An `update` costs two requests, the read and the change, which the spacing of the quota applies to. Between
the two, a change by someone else is still caught by the `fingerprint`.

Not verified: which parameters each configuration type needs or accepts, which keys name the destinations of
a type, and the answer to a stale fingerprint.

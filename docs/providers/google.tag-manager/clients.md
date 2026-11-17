---
description: >
  Describes the client tools of the Google Tag Manager provider: listing, reading, creating, replacing, deleting, and reverting the clients of a server container workspace, the members of a client, the effect of a client and when it applies, the confirmation, the error of a web container, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: clients

The client tools of the [Google Tag Manager provider](README.md). The update, revert, parameter, and never-cut
rules shared with tags, triggers, and variables are described there.

A client receives the requests that reach a server container and decides which of them the container
processes. A change is made in the workspace only and takes effect only with a published container version,
which only the listed-only versions.publish publishes, and no tool activates a client without publishing. Every change is
confirmed, and the confirmation shows the name, the type, and the workspace or client path, never a parameter
value.

| Tool | Does |
| --- | --- |
| `clients.list` | reads the clients of a workspace, a batch per call with `cursor` |
| `clients.get` | reads one client by its path, the fingerprint included |
| `clients.create` | creates a client in a workspace; a repeated call creates a second client |
| `clients.update` | replaces a client as a whole; needs `fingerprint` |
| `clients.delete` | deletes a client from the workspace |
| `clients.revert` | discards the workspace changes of a client; needs `fingerprint`; only by `tools` list |

`workspace` and `client` are paths such as `accounts/1/containers/2/workspaces/3/clients/5`; both are checked
against the target of the connection. `clients.delete` is in the profile `write` and stays revertible until a
version is published. `clients.revert` overwrites the workspace state of a client and is in no profile.

## Server containers only

Clients exist in containers of usage context `server`. The tools do not check the usage context. For a web
container, Google answers with an error, and Qatlas passes it on in the form of every unmapped status:
`invalid-provider-response` with Google's answer as the original and no class of its own.

## Members

The arguments of `create` and `update` are the members of the client. `name` and `type` are needed, and `type`
is the client type of Tag Manager; the parameters it takes depend on it. The others are `parameter`,
`priority` (an integer between -2147483648 and 2147483647 that sets the relative order of the clients),
`notes`, and `parentFolderId`. Members that Google assigns (`path`, `accountId`, `containerId`, `workspaceId`,
`clientId`, `fingerprint`, `tagManagerUrl`) are results only and are never sent. The priority of a client is an
integer, unlike the priority of a tag, which is a parameter.

Names, notes, and parameter values are untrusted content of whoever edits the account. They are returned and
sent unchanged, are never run or fetched, and are never cut.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "name": "Example client", "type": "example-type",
 "priority": 10,
 "parameter": [{"type": "boolean", "key": "example-key", "value": "true"}]}
```

Not verified: the answer of Google to a client call on a web container, which parameters each client type
needs or accepts, and the answer to a stale fingerprint.

---
description: >
  Describes the built-in variable tools of the Google Tag Manager provider: listing, enabling, disabling, and reverting the built-in variables of a workspace, the checked type list, the absence of a fingerprint, the confirmation, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Tag Manager: built-in variables

The built-in variable tools of the [Google Tag Manager provider](README.md). Built-in variables are the
ready-made variables of a container, such as the page URL or the clicked element. A container enables the ones
its tags and triggers use. A change is made in the workspace only and goes live with a published container
version, which no tool of this provider publishes. Every change is confirmed, and the confirmation shows the types
and the workspace path.

| Tool | Does |
| --- | --- |
| `built-in-variables.list` | reads the enabled built-in variables of a workspace, a batch per call with `cursor` |
| `built-in-variables.create` | enables one or more built-in variables by `types` |
| `built-in-variables.delete` | disables one or more built-in variables by `types` |
| `built-in-variables.revert` | discards the workspace changes of one built-in variable by `type`; only by `tools` list |

`workspace` is a path such as `accounts/1/containers/2/workspaces/3` and is checked against the target of the
connection. `list` answers with `type`, `name`, `path`, `accountId`, `containerId`, and `workspaceId` per
variable.

## Types

`types` and `type` take the types of the discovery document, such as `pageUrl`, `clickElement`, `eventName`, or
`analyticsSessionId`. The list is checked before any request: an unknown type, a type named twice, and the
placeholder `builtInVariableTypeUnspecified` are `invalid-request`. Some types suit only web, mobile, AMP, or
server containers; which of them a container accepts is not checked here.

## No fingerprint

Google's methods for built-in variables take and return no fingerprint, so these tools have none. A change
cannot detect that someone else changed the variables in the meantime.

## Disabling and reverting

Disabling a variable that tags, triggers, or other variables still refer to breaks them. `delete` answers with
the workspace, the types, and `deleted: true`. The variable stays in the versions that hold it.

`revert` sets one variable back to its state in the latest container version and answers with `enabled`, which
tells whether it is enabled afterwards. It overwrites the workspace state, so it is in no profile and a
connection offers it only when its `tools` list names it.

```json
{"workspace": "accounts/1/containers/2/workspaces/3", "types": ["pageUrl", "clickElement"]}
```

Not verified: the answer to a type that a container does not support, the status Google answers to a successful
delete (200 and 204 are accepted), and whether enabling an enabled type is silent.

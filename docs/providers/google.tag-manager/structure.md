---
description: >
  Describes the account, container, and workspace tools of the Google Tag Manager provider: the six read tools, their path arguments and how a bound connection narrows them, and the bounds of their results.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Tag Manager: accounts, containers, and workspaces

The structure tools of the [Google Tag Manager provider](README.md). Credential, target, quota, and errors are
described there.

| Tool | Does |
| --- | --- |
| `accounts.list` | reads the accounts the credential reaches, a batch per call with `cursor`; `include_google_tags` adds accounts associated with a Google tag |
| `accounts.get` | reads one account |
| `containers.list` | reads the containers of an account, a batch per call |
| `containers.get` | reads one container |
| `workspaces.list` | reads the workspaces of a container, a batch per call |
| `workspaces.get` | reads one workspace |

All six read only and are in the `read` profile. `account` and `container` are paths such as `accounts/1` and
`accounts/1/containers/2`; `workspace` is `accounts/1/containers/2/workspaces/3` and is always needed. A
connection bound to an account or a container may leave `account` or `container` out, and the argument it
gives must lie inside the target.

## Result bounds

IDs and paths are cut at 100 characters, names at 300, notes and descriptions at 2000, and URLs at 2048; a cut
text ends with `…`. The name and description of a workspace are the exception: they are never cut, because
[workspaces.update](workspaces.md) writes them back. Lists inside an object keep at most 100 entries.

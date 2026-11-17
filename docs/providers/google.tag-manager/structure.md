---
description: >
  Describes the account, container, and workspace tools of the Google Tag Manager provider: the read tools, the account rename, the container snippet and the container merge, their path arguments and how a bound connection narrows them, and the bounds of their results.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-17
---

# Google Tag Manager: accounts, containers, and workspaces

The structure tools of the [Google Tag Manager provider](README.md). Credential, target, quota, and errors are
described there. Creating, changing, looking up, and deleting containers is described under
[container management](containers.md).

| Tool | Does |
| --- | --- |
| `accounts.list` | reads the accounts the credential reaches, a batch per call with `cursor`; `include_google_tags` adds accounts associated with a Google tag |
| `accounts.get` | reads one account |
| `containers.list` | reads the containers of an account, a batch per call |
| `containers.get` | reads one container |
| `workspaces.list` | reads the workspaces of a container, a batch per call |
| `workspaces.get` | reads one workspace |
| `containers.snippet` | reads the tagging snippet of a container as text |
| `accounts.update` | renames an account; needs `fingerprint` |
| `containers.combine` | merges a source container into a target container; only by `tools` list |

The first six and `containers.snippet` only read and are in the `read` profile. `account` and `container` are paths such as `accounts/1` and
`accounts/1/containers/2`; `workspace` is `accounts/1/containers/2/workspaces/3` and is always needed. A
connection bound to an account or a container may leave `account` or `container` out, and the argument it
gives must lie inside the target.

## Rename an account

`accounts.update` takes `account`, `name`, and the needed `fingerprint` from the last read. `shareData`, which
controls whether the account shares data with Google, is returned in every account result but is never an
argument: the tool cannot change that data path. Because Google replaces the whole account on update, the tool
reads the account first and sends the stored `shareData` back unchanged together with the new name. It writes
nothing when the read fails or when the stored fingerprint differs from the one given, and the fingerprint also
goes to Google with the update, so a change made between the read and the write is caught if Google checks it.
It needs the scope `tagmanager.manage.accounts`, which the `write` profile asks for, and it is refused on a
connection bound to a container. The name is the account's name for everyone who sees it. The confirmation shows
the account path and the new name.

## Container snippet

`containers.snippet` returns `snippet`, the tagging snippet of the container, and `containerConfig`, the setting
that provisions the tagging server of a server container, which belongs to that container and should stay
private. Both are text: the snippet is code for the site that embeds it, and Qatlas neither runs it nor fetches
any URL in it. `snippet` is cut at 16384 characters and `containerConfig` at 4096; `truncated` is `true` when a
cut happened.

## Combine containers

`containers.combine` merges the container in `source_container_id`, a container of the same account, into the
target container in `container`. `setting_source` chooses whose container settings remain, `current` for the
target or `other` for the source; without it Google decides. Google does not document what happens to the source
container, so the merge is treated as irreversible for both, the method is not idempotent, and Google marks it
deprecated. Both containers must lie inside the target of the connection and must differ, so a connection bound to
one container cannot merge; the check happens before any request. Options that copy or change user permissions
are not offered, so Google refuses a merge that would turn user permissions on. The confirmation names the target
and the source container.

## Result bounds

IDs and paths are cut at 100 characters, names at 300, notes and descriptions at 2000, and URLs at 2048; a cut
text ends with `…`. The name and description of a workspace are the exception: they are never cut, because
[workspaces.update](workspaces.md) writes them back. Lists inside an object keep at most 100 entries.

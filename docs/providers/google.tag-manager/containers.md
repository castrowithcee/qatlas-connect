---
description: >
  Describes the container management tools of the Google Tag Manager provider: creating, replacing, looking up by tag ID, and deleting a container, the usage contexts, the server-side URLs and why a change shows the old and the new list, how the binding narrows each tool, why deleting is only offered by a tools list, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: container management

The container management tools of the [Google Tag Manager provider](README.md). Reading containers is described
under [accounts, containers, and workspaces](structure.md).

| Tool | Does |
| --- | --- |
| `containers.create` | creates a container in an account; a repeated call creates a second container |
| `containers.update` | replaces the name, domains, notes, and server-side URLs of a container; needs `fingerprint` |
| `containers.lookup` | finds the container of a tag ID or destination ID |
| `containers.delete` | deletes a container; only by `tools` list |

A change is made to the container itself, not in a workspace, and every change needs its own confirmation.
Settings take effect only with a published container version, which no tool of this provider makes.

## Create and update

`containers.create` needs `name` and `usageContext`, a list of at least one of `web`, `android`, `ios`,
`androidSdk5`, `iosSdk5`, `amp`, and `server`. `domainName`, `notes`, and `taggingServerUrls` are optional.
Create reaches the whole account: it is refused for a connection bound to a container, and a connection bound to
an account may create only in that account.

`containers.update` replaces the container as a whole, so a member that is left out is removed: send `name`,
`domainName`, `notes`, and `taggingServerUrls` as `containers.get` returned them, with the changes made.
`usageContext` cannot be changed; the update keeps the stored value, and the argument is not accepted. Update
reads the container before it writes, and writes nothing if that read fails. `containers.get` and
`containers.list` cut long names, notes, and lists, so an update that would send a cut value back is refused
and nothing is written: send the complete value.

## Server-side URLs

`taggingServerUrls` decides where a web container sends its measurement data. `containers.update` therefore
needs `previous_tagging_server_urls`, the list as stored (an empty list if there is none). It is only compared
and never written. The confirmation shows the old and the new list. The container is read before anything is
written, and if the stored list is not exactly the previous one, the call is refused and nothing is written. A
create shows the new list only.

## Look up

`containers.lookup` takes exactly one of `tag_id` (such as `GTM-XXXX`) and `destination_id` (such as
`AW-123456789`). A container is returned only if it lies inside the target of the connection. Otherwise the
answer is `not-found`, the same as for an ID that no container has and without any word about a container
elsewhere.

## Delete

`containers.delete` removes a container with its workspaces, tags, triggers, variables, and versions. Treat it
as irreversible: it is not established that a deleted container can be restored. It is in no profile, so a
connection offers it only when its `tools` list names it, and it needs the scope
`https://www.googleapis.com/auth/tagmanager.delete.containers`, which no other tool asks for. A connection
bound to an account may delete any container of that account; one bound to a container may delete only that
container and may leave `container` out. Without a binding it reaches every container the credential reaches.

Not verified: how Google answers a stale `fingerprint`, whether a deleted container can be restored, and how
Google answers a lookup for an ID the credential cannot reach.

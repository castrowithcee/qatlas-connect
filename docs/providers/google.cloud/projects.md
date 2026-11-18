---
description: >
  Describes the Google Cloud project tools: the project list with its required parent and showDeleted, and creating, changing, moving, and restoring projects with the parent check, the confirmations, and the operation they return.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-18
---

# Google Cloud projects

`projects.list` lists the projects directly below a `parent` (`organizations/ID` or `folders/ID`, required),
with `show_deleted`, `page_size`, and `cursor`. It is the exact listing of one parent: it takes no filters. To
search by name, label, or project ID use `projects.search`, and to read one project use `projects.get`
(see [Single reads](README.md#single-reads)).

## Target check

The tool is offered to a connection without targets or with an organization or a folder among them, because a
project target holds no children. The `parent` is checked before the list is requested, with the same rules as
`folders.list`: an organization parent must be a target, a folder parent is admitted if it is a target or
proven by reading the folders above it, and a parent that is only partly bound (an organization when only a
folder inside it is a target) is refused before any request. See [folders](folders.md) for the proof and its
limit of 30 reads. Every row is checked again, and rows outside the targets are dropped.

`show_deleted` adds projects in the state `DELETE_REQUESTED`; each row carries its `state`. The cursor works as
for the other lists.

## Changing projects

| Tool | Arguments | Google method |
| --- | --- | --- |
| `projects.create` | `project_id`, `display_name`, `parent` (required), `labels` | `projects.create` |
| `projects.patch` | `project_id`, `display_name`, `labels`, `etag` | `projects.patch` |
| `projects.move` | `project_id`, `current_parent`, `destination_parent` | `projects.move` |
| `projects.undelete` | `project_id` | `projects.undelete` |

The tools belong to the profile `projects-manage` (see [Profiles](README.md#profiles)), ask for `cloud-platform`,
and need the confirmation of a person for each call. Qatlas sends one request and never repeats it; after a
timeout or a 5xx the message says that the outcome is unknown. Google answers with a long-running operation and
the tools never wait: the result is the operation with `service` set to `resource-manager`, and `operations.get`
reads its state. An operation that is done with an error is a provider error.

- `project_id` of `create` is 6 to 30 lowercase letters, digits, and hyphens, starting with a letter and not
  ending with a hyphen. A repeated call, or an ID that was used before, ends as a `provider-error` that starts
  with `conflict`. `display_name` is 4 to 30 characters of letters, digits, hyphen, quotes, space, and `!`.
  `labels` takes at most 64 entries with keys of 1 to 63 lowercase letters, digits, and hyphens (a letter
  first) and values of the same form or empty. The Preview field `tags` is not offered.
- `patch` sends exactly the set members as the update mask and refuses a call without `display_name` or
  `labels`. `labels` replaces the whole list: labels not given are removed, `{}` removes all. With `etag`
  Google refuses a change after the project changed, as a `conflict`.
- `move` changes the IAM roles and organization policies the project inherits. `current_parent` is only compared:
  Qatlas reads the project first and ends without any change if its parent differs, so the confirmation names the
  old and the new parent truthfully. It is never sent. A project that Google reports without a parent cannot be
  moved by this tool.
- `undelete` does not check the state: for an active project Google refuses and the call ends as a
  `provider-error`.
- No tool links, changes, or removes a billing account, so a created project has none. Deleting a project is not
  part of the profile.

### Target check for changes

An organization given as `parent`, `current_parent`, or `destination_parent` must be named as an organization
target of exactly that organization, also by a connection without targets, and is refused before any request
otherwise. A folder is admitted for a connection without targets, if it is a folder target, or if reading the
folders above it reaches a target (as for `folders.list`, with the same limit of 30 reads). A connection with only
project or billing account targets cannot create or move projects, and the two tools are not offered to it.
`patch`, `move`, and `undelete` need the project itself in the targets: Qatlas refuses a project target that
excludes it before any request and otherwise reads the project and the folders above it before the change. `move`
needs the current parent and the destination in the targets as well. A project created below a bound parent is in
the targets afterwards.

The confirmation holds at most 500 characters. A call with so many or such long labels that it would not fit is
refused before any request, and the unconfirmed answer already says so. The window between the read and the
change is a limit: Qatlas cannot prevent a change by someone else in between.

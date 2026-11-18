---
description: >
  Describes the Google Cloud folder tools: reading one folder and listing the folders below a parent, with the proof of the target binding, the handling of a parent that is only partly bound, and the cursor, and creating, renaming, moving, and restoring folders with the parent check, the confirmations, and the operation they return.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-18
---

# Google Cloud folders

`folders.get` reads one folder by its numeric ID (`folder_id`). `folders.list` lists the folders directly
below a `parent` (`organizations/ID` or `folders/ID`, required), with `show_deleted`, `page_size`, and
`cursor`. Both are offered only to a connection without targets or with an organization or a folder among
them, because a project holds no folder.

## Target check

- `folders.get` reads the folder and then proves it: it is a folder target, or the folders above it lead to a
  target folder or organization. A folder outside the targets is refused without a name. The proof reads at most
  30 folders.
- `folders.list` checks the `parent` before the list is requested. An organization parent is admitted only if
  that organization is a target. A folder parent that is a target is admitted at once, and any other folder
  parent is proven with `folders.get` up its chain; if the proof fails, the list is never read.
- Every row is then checked again against the targets, and rows outside them are dropped. Rows below a proven
  parent cost no further read.

**Partly bound parents.** If the parent is an organization but the connection names only a folder inside it,
the call is refused before any request. The organization is not part of the binding, and listing it would
only be filtered down afterwards. Use `folders.get` for the bound folder, or list the folder itself.

## Listing

`show_deleted` adds folders in the state `DELETE_REQUESTED`, and each row carries its `state`. An empty list
is an empty result, not an error. The `next_cursor` is bound to the tool, the targets, the parent, and
`show_deleted`; a changed cursor or one from another list is refused before a secret is read. A page can be
empty while `has_more` is true, because rows outside the targets are dropped.

## Changing folders

| Tool | Arguments | Google method |
| --- | --- | --- |
| `folders.create` | `parent`, `display_name` (both required) | `folders.create` |
| `folders.patch` | `folder_id`, `display_name`, `etag` | `folders.patch` |
| `folders.move` | `folder_id`, `current_parent`, `destination_parent` | `folders.move` |
| `folders.undelete` | `folder_id` | `folders.undelete` |

The tools belong to the profile `folders-manage` (see [Profiles](README.md#profiles)), ask for `cloud-platform`,
and need the confirmation of a person for each call. They are offered to a connection without targets or with an
organization or a folder among them. Qatlas sends one request and never repeats it; after a timeout or a 5xx the
message says that the outcome is unknown. Google answers with a long-running operation and the tools never wait:
the result is the operation with `service` set to `resource-manager`, and `operations.get` reads its state. An
operation that is done with an error is a provider error. The confirmation must stay within 500 characters, else
the call is refused before any request.

- `display_name` is 3 to 30 characters: letters and digits, with spaces, hyphens, and underscores between them,
  starting and ending with a letter or digit. It must be unique among the folders of the same parent: a name that
  is taken ends as a `provider-error` that starts with `conflict`, also for `create`, which is not idempotent.
- A parent of `create`, `current_parent`, and `destination_parent` is `organizations/ID` or `folders/ID`. An
  organization counts only if the connection names exactly that organization as a target, also a connection
  without targets is refused before any request. A folder counts without targets, as a folder target, or below a
  target, proven by reading the folders above it (at most 30 reads).
- `patch`, `move`, and `undelete` read the folder first and write only if it is a folder target or lies below a
  target. A connection with only project or billing account targets is refused before any request.
- `patch` sends exactly `display_name` as the update mask and refuses a call without it. With `etag` Google
  refuses a change after the folder changed, as a `conflict`.
- `move` changes the IAM roles and organization policies that the folder, every folder in it, and every project
  in it inherit. How many projects that concerns is not known without further calls, so the confirmation does not
  name it. `current_parent` is only compared: Qatlas reads the folder first and ends without any change if its
  parent differs, so the confirmation names the old and the new parent truthfully. It is never sent. Qatlas does
  not check whether the destination lies below the folder itself: Google refuses that as a `provider-error`.
- `undelete` does not check the state: for an active folder Google refuses and the call ends as a
  `provider-error`.
- Deleting a folder is not part of the profile.

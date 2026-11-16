---
description: >
  Describes the Google Cloud folder tools: reading one folder and listing the folders below a parent, with the proof of the target binding, the handling of a parent that is only partly bound, and the cursor.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
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

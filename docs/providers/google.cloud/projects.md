---
description: >
  Describes the Google Cloud project list tool: the required parent, its check against the targets, showDeleted, and how it differs from projects.search and projects.get.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
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

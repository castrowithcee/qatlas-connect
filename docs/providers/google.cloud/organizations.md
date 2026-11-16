---
description: >
  Describes the Google Cloud organization tool: why reading an organization needs an organization target and what the refusal does and does not cover.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Cloud organizations

`organizations.get` reads one organization by its numeric ID (`organization_id`, never an `organizations/`
name or a path).

## Targets

An organization is read only by a connection whose targets name exactly that organization. A connection
without targets is refused, as is one bound to a folder, a project, or another organization, because its
credential may see organizations the person never handed over. The refusal comes before a secret is read and
before any request, and it does not name the target that would have fit. A binding that never names an
organization does not offer the tool at all.

The tool returns the organization as Google sends it, with text cut to bounded length. Searching organizations
is described with the other [search tools](README.md#search).

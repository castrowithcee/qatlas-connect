---
description: >
  Describes the destination tools of the Google Tag Manager provider: listing and reading the Google tag destinations linked to a container, the members of a destination, why linking is not offered, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Tag Manager: destinations

The destination tools of the [Google Tag Manager provider](README.md). Both only read and are in the `read`
profile with the scope `tagmanager.readonly`.

| Tool | Does |
| --- | --- |
| `destinations.list` | reads the destinations linked to a container, a batch per call with `cursor` |
| `destinations.get` | reads one destination by its path; Google marks the method deprecated |

`container` and `destination` are paths such as `accounts/1/containers/2` and
`accounts/1/containers/2/destinations/4`; both are checked against the target of the connection before any
request. A connection bound to a container may leave `container` out.

Results carry `path`, `accountId`, `containerId`, `destinationId`, `destinationLinkId`, `name`, `fingerprint`, and
`tagManagerUrl`. Names and URLs are untrusted content of whoever edits the account; they are cut like those of
[accounts and containers](structure.md), and URLs are text that is never fetched.

Linking a destination to a container is not offered, and neither is moving a tag ID between containers.

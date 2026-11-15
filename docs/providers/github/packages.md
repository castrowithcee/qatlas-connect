---
description: >
  Describes the GitHub package tools: listing and reading the packages of a user or an organization and their versions, the owner rule, the package types, and the limits of what a package answer holds.
type: knowledge
edit: shared
created: 2026-11-14
updated: 2026-11-14
---

# GitHub packages

The package tools of the [GitHub provider](README.md). They read the GitHub Packages of a user or an
organization and the versions of a package; package content is never downloaded or uploaded, and no tool talks to
a registry. The owner is described under [the target of a call](README.md#the-target-of-a-call); the tools take
`owner` as a user, `users/LOGIN`, or as an organization, `orgs/LOGIN`, and the route follows the kind of the owner.

| Tool | Does |
| --- | --- |
| `github.packages.list` | lists the packages of one `package_type` of the owner |
| `github.packages.get` | reads one package by `package_type` and `package_name` |
| `github.package-versions.list` | lists the versions of one package |
| `github.package-versions.get` | reads one version by its numeric `package_version_id` |

```yaml
connections:
  package-reader:
    service: github
    credential: github-reader
    targets: [orgs/octo-org]
    tools: [github.packages.list, github.packages.get, github.package-versions.list, github.package-versions.get]
```

## The owner

The owner must lie inside the connection's targets as an owner target itself: `orgs/LOGIN` or `users/LOGIN`. An
owner the connection reaches only through a repository or a project target is refused, as is an excluded owner.
The check runs before the credential is read and before GitHub is contacted. Without an `owner` argument, the
only owner target of the connection is the default.

## Arguments

`package_type` is required for the list and is one of `npm`, `maven`, `rubygems`, `docker`, `nuget`, or
`container`; GitHub keeps Docker images of its Container registry as `container` and Gradle packages as `maven`.
`visibility` (`public`, `private`, or `internal`) narrows the package list. `package_name` is the name
`github.packages.list` reports, 1 to 255 characters without control characters; `.` and `..` are refused. It
travels as one URL-encoded path segment, so a `/`, `@`, or `%` in a name never addresses another route.
`package_version_id` is the positive numeric id. `github.package-versions.list` shows active versions, or the
deleted ones with `state: deleted`. Lists page by `limit` and `cursor` like the other REST lists; a cursor belongs
to its own owner, type, name, and filters.

## Answers

A package answers every member GitHub documents for it: `id`, `name`, `package_type`, `visibility`, `url` (the
web address), `version_count`, `created_at`, `updated_at`, `links` (the REST `url`), the linked repository as
`repository` (the text `OWNER/REPO`), and the two objects `owner_detail` and `repository_detail`. In a list, every
member is answered; select with `fields` to narrow. `github.packages.get` answers without the two detail objects
unless `full` or `fields` asks for them.

A package linked to a repository outside the connection's targets stays readable, since the owner is the target;
its `repository` shows as text, and its `repository_detail` keeps only `id`, `node_id`, and `full_name`.

A version answers `id`, `name`, `url`, `license`, `description`, `created_at`, `updated_at`, `deleted_at`,
`links` (the REST `url` and `package_html_url`), and `metadata`: the `package_type`, the `container` tags, and the
`docker` tags under the member `tag`, each at most 100 and marked when cut.

Package names, version names, tags, metadata, descriptions, addresses, and the linked repository are untrusted
data. Addresses are never fetched. GitHub's own text never appears in an error message.

The token rights are listed under [Packages](setup.md#packages).

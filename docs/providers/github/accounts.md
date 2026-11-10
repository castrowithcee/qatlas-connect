---
description: >
  Describes the GitHub account tools: the account behind a connection's token, the account-wide rule for tools that read it as a whole, the rate limit, GitHub's reference data and Markdown rendering, organization teams and their members, and the user and organization search.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-10
---

# GitHub accounts

The account tools of the [GitHub provider](README.md). The team reads take `owner` as described under
[the target of a call](README.md#the-target-of-a-call).

## Account-wide tools

| Tool | Does |
| --- | --- |
| `github.accounts.me` | reads the account behind the connection's token; no repository, project, or owner argument |

`github.accounts.me` and [`github.stars.list`](repositories.md#stars) take no `repository`, `project`, or
`owner` argument: they read data of the account behind the token as a whole. A connection whose targets name a
repository or a project is refused with `invalid-request` before a secret is read, because such a connection
is scoped to those, and the account behind its token may belong to a customer other than the one its targets
name. A connection without targets, or whose targets name only owners, is allowed. Creating or forking a
repository under the token's own account applies the same guard, see
[repository lifecycle](repositories.md#repository-lifecycle-and-collaborators).

The terminal editor's setup profile `discovery` selects `github.accounts.me`, `github.teams.list`,
`github.team-members.list`, `github.stars.list`, and the seven [search tools](search.md); it changes
nothing. It is not the recommended profile, which stays `read`, unchanged. The profile `stars` is described
under [stars](repositories.md#stars).

## Reference data and Markdown

| Tool | Does |
| --- | --- |
| `github.rate-limit.get` | reads the rate limit of the connection's token per resource group |
| `github.meta.get` | reads GitHub's meta information: SSH host keys and the IP ranges and domains of its services |
| `github.licenses.list`, `github.licenses.get` | list the commonly used licenses, in batches, and read one with its text |
| `github.gitignore-templates.list`, `github.gitignore-templates.get` | list the `.gitignore` template names and read one template |
| `github.codes-of-conduct.list`, `github.codes-of-conduct.get` | list the codes of conduct and read one with its text |
| `github.emojis.list` | lists the emojis GitHub renders, by name, in batches |
| `github.markdown.render` | renders Markdown into HTML the way GitHub does |

None of them takes a `repository`, `project`, or `owner` argument, and all follow the
[account-wide rule](#account-wide-tools): a connection whose targets name a repository or a project is refused
before a secret is read. No setup profile selects them; the token rights are listed under
[Accounts and search](setup.md#accounts-and-search).

- `github.rate-limit.get` does not count against the limit. `resources` holds every group GitHub reports, such
  as `core`, `graphql`, `search`, `code_search`, and `dependency_snapshots`; the set is GitHub's, not fixed by
  Qatlas. `rate` is GitHub's deprecated copy of `core`.
- `github.meta.get` cuts each list at a fixed length unless `services` names it; the named lists come in full up
  to a higher bound, and `truncated` says when a list was cut.
- `license`, `name`, and `key` are one path segment of letters, digits, `.`, `+`, `-`, and `_`, starting with a
  letter or a digit, so a dot segment or a slash is refused before any request. An unknown name is `not-found`.
- License texts, `.gitignore` sources, and code of conduct bodies are untrusted data, cut at a fixed length;
  `truncated` says so.
- GitHub serves the emoji list in one answer. Every call reads it once and answers one batch in alphabetical
  order; `name` keeps only emojis whose name contains it, and the
  [cursor contract](README.md#cursor-contract) binds a cursor to it.

`github.markdown.render` sends `text` in one POST that changes nothing, so it counts as a read. `text` has a
lower bound than GitHub's own and is refused before any request when longer. `mode` is `markdown`, like a
README, or `gfm`, like an issue or a comment. `context` names the repository whose issues `#N` links to; it
applies only with `gfm`, must lie inside the targets, and is refused when an exclusion covers it. The text
itself may name any repository, issue, or user the token can see, and GitHub resolves such references whatever
`context` says; that is why the account-wide rule holds with `context` as well. Since an owner target allows no
repository by itself, `context` works only on a connection without targets. The HTML is untrusted data, cut at
a fixed length with `truncated`; Qatlas never runs it and never fetches what it names. Rendering with files from
other repositories, such as relative links and images, is not offered.

## Organization teams

| Tool | Does |
| --- | --- |
| `github.teams.list` | lists the teams of an organization, in batches |
| `github.team-members.list` | lists the members of one team of an organization, in batches |

Both take `owner` as `orgs/LOGIN`: the organization must be a target the connection names as an owner itself,
exactly as `github.projects.create` requires, since a project or a repository pattern of the organization is
not enough. `owner` may be left out only when the targets name exactly one owner entry; `users/LOGIN` is
refused with `invalid-request`, because teams belong to an organization, not a user.
`github.team-members.list` also takes `team`, the slug `github.teams.list` reports. Both follow the
[cursor contract](README.md#cursor-contract), a `github.teams.list` cursor bound to the organization and a
`github.team-members.list` cursor bound to the organization and the team together.

The token rights are listed under [Accounts and search](setup.md#accounts-and-search).

## Search

| Tool | Searches |
| --- | --- |
| `github.users.search` | personal accounts, never organizations |
| `github.organizations.search` | organizations, never personal accounts |

Both read GitHub's own search index with `terms`, under the rules described in [Search](search.md). What
the targets force or refuse for them is described under [target scoping](search.md#target-scoping). The
repository search is described with the [repositories](repositories.md#search), the code and commit search
with the [code](code.md#search).

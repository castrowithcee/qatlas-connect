---
description: >
  Describes the GitHub account tools: the account behind a connection's token, the account-wide rule for tools that read it as a whole, the rate limit, GitHub's reference data and Markdown rendering, organization teams, their members, repositories, and, listed only, their management, reading an organization with its members, memberships, organization roles, outside collaborators, and pending invitations, and the user and organization search.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-12
---

# GitHub accounts

The account tools of the [GitHub provider](README.md). The team and organization reads take `owner` as described under
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

### Managing teams

| Tool | Does | Offered |
| --- | --- | --- |
| `github.teams.get` | reads one team by its slug | by effect |
| `github.team-memberships.get` | reads the role and state of one account in one team | by effect |
| `github.team-repositories.list` | lists the repositories of one team, in batches, with the team's permission on each | by effect |
| `github.teams.create` | creates a team: `name`, optional `description`, `privacy`, `notification_setting`, `parent_team_id` | tools list only |
| `github.teams.update` | changes the name, description, privacy, notification setting, or parent of a team; `parent_team_id` null makes it top-level | tools list only |
| `github.teams.delete` | deletes a team permanently | tools list only |
| `github.team-members.add` | adds an active member of the organization to a team, or changes the role of a team member (`member` or `maintainer`) | tools list only |
| `github.team-members.remove` | removes an account from a team; the account stays in the organization | tools list only |
| `github.team-repositories.set` | sets the permission of a team on one repository (`pull`, `triage`, `push`, `maintain`, `admin`) | tools list only |
| `github.team-repositories.remove` | withdraws a team's access from one repository | tools list only |

Every team tool takes `owner` as `orgs/LOGIN` under the rules above, and every result names it. The tools
that change something are offered only where a connection's `tools` list names them, need their own
confirmation, and send exactly one request that is never repeated; after a timeout or a server error the
message says the change may have been applied, so read the state before repeating it. GitHub's own text of a
refusal never reaches a message. `team` is a team slug and `username` a login; both are checked as fixed path
segments before anything is sent.

**No invitations.** GitHub invites an account that is not yet a member of the organization when it is added to
a team. `github.team-members.add` therefore reads the organization membership first
(`GET /orgs/{org}/memberships/{username}`) and refuses with `invalid-request`, without any change, unless the
membership is `active`: a missing account and a pending invitation are both refused. Inviting people is not
offered. `github.teams.create` offers no `maintainers` or `repo_names` for the same reason, and neither
`ldap_dn` nor the deprecated `permission` of a team.

**Repositories.** `github.team-repositories.set` and `.remove` take `repository` as `OWNER/REPO`. It must
belong to the organization named by `owner` and lie inside the connection's targets, with `exclude` applied;
both the organization and the repository are checked before a secret is read. The organization needs an owner
target and the repository a repository target (or a pattern of the organization). A custom repository role
name is not accepted as `permission`. `github.team-repositories.list` is not bound to the targets as a list
but projects each repository like the other repository reads: a repository outside the targets keeps only its
`id`, `node_id`, and `full_name`.

**Security.** Two changes raise rights and deserve review before a connection lists them: the role
`maintainer` in `github.team-members.add` lets the account manage the team, and the permission `admin` in
`github.team-repositories.set` gives the team administrative control of the repository. Deleting a team
withdraws all access it granted. Together with `github.teams.create`, which can attach a parent team, these
tools change who reaches which repository in the organization.

**Omitted fields.** The reads output every member GitHub documents for `team-full`, `team-membership`, and
`team-repository` (REST OpenAPI 1.1.4), shaped as the other team and repository reads, with these exceptions:
`temp_clone_token` of a team repository is never output, since it grants clone access without a sign-in, and
`has_downloads` of a team repository is left out like in every other repository view of the provider.
`html_url` appears as `url` and the REST addresses as `links`. Text GitHub supplies, including the
organization object inside a team with its billing contact, is untrusted data; people follow the account view
used by the member list.

## Organization, members, roles, outside collaborators, and invitations

| Tool | Does |
| --- | --- |
| `github.organizations.get` | reads one organization with its profile, plan, and settings |
| `github.organization-members.list` | lists the members of an organization by login, in batches; optional `role` (`all`, `admin`, `member`) |
| `github.organization-members.get` | reads the role and state of one account (`username`) in the organization |
| `github.organization-roles.list` | lists the organization roles of an organization, predefined and custom |
| `github.outside-collaborators.list` | lists the outside collaborators of an organization, in batches |
| `github.organization-invitations.list` | lists the pending invitations, in batches; optional `role` (`all`, `admin`, `direct_member`, `billing_manager`, `hiring_manager`) and `invitation_source` (`all`, `member`, `scim`) |

These tools only read, are offered by effect, and take `owner` as `orgs/LOGIN` under the rules of
[Organization teams](#organization-teams): an owner target of its own, checked with `exclude` before a secret is
read, and named in every result. `username` is checked as a fixed path segment before anything is sent. The lists
follow the [cursor contract](README.md#cursor-contract), a cursor bound to the organization, the tool, and its
filters. `github.organization-roles.list` has no cursor: GitHub answers that route in one piece.

GitHub decides what a token sees. The full organization object, with its settings and plan, is shown by GitHub
to an organization owner; for a token below that, GitHub leaves members out and so does the result.
`github.organization-members.get` is answered by GitHub only to a member of the organization.

**No two-factor state of other accounts.** GitHub's `filter` of the member and outside collaborator lists
(`2fa_disabled`, `2fa_insecure`) is not offered and never sent, and no output carries a two-factor member of
another account. `two_factor_requirement_enabled` of `github.organizations.get` is output: it is a setting of the
organization, not the state of any account.

**No invitation tokens or accept links.** An invitation is output from the members GitHub documents for it
(`id`, `login`, `email`, `role`, `created_at`, `failed_at`, `failed_reason`, `inviter`, `team_count`, `node_id`,
`invitation_teams_url`, `invitation_source`). Anything else GitHub might send with it, such as a token or a link
that accepts the invitation, is never output. `invitation_teams_url` is a REST address of the invitation's teams,
untrusted data and never fetched.

**Omitted fields.** The reads output every member GitHub documents for `organization-full`, `org-membership`,
`organization-role`, `organization-invitation`, and `simple-user` (REST OpenAPI 1.1.4), without exception.
`html_url` appears as `url` and the REST addresses as `links`; `organization-full`'s own REST `url` is
`links.url`. `billing_email` of `github.organizations.get` is the billing contact GitHub reports and is output
as untrusted data. People, including the account of a membership, the creator of a role, and the inviter, follow
the account view of the member lists, with the name and email address GitHub reports. Text GitHub supplies is
untrusted data, and addresses are never fetched.

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

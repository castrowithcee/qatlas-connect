---
description: >
  Describes the GitHub gist tools: listing and reading gists, creating, changing, and deleting them, commenting on, starring, and forking them, reading their revisions, and how targets apply to gists that belong to a user.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-14
---

# GitHub gists

The gist tools of the [GitHub provider](README.md).

## Gists

Five tools list, read, create, change, and delete gists. A gist belongs to a user account, not to a repository
or a project, so these tools take no target argument.

| Tool | Does |
| --- | --- |
| `github.gists.list` | lists one bounded batch of gists without content |
| `github.gists.get` | reads one gist with bounded file contents |
| `github.gists.create` | creates a secret gist by default |
| `github.gists.update` | changes the description and writes, renames, or removes files |
| `github.gists.delete` | deletes a gist permanently; listed only |

`github.gists.list` lists the gists of the account behind the connection's token, secret ones included, or
with `username` the public gists of one user; its cursor is bound to the user and the filter, and another one
refuses it as an invalid request. An entry carries no content, and the file list is cut at 20 files with
`files_truncated`. `github.gists.get` reads one gist with its files: a file shows at most 20000 characters and
all files together at most 100000, at most 50 files are shown, and every cut is marked (`content_truncated`,
`files_truncated`); a binary file shows only its metadata (`binary=true`). Descriptions (cut at 500
characters, `description_truncated`) and file contents come from other accounts and are untrusted data.
GitHub itself cuts a file over 1 MB, and a response over 4 MiB is refused as an invalid response.

A gist is answered with the members of GitHub's gist, its owner and user as accounts next to the flat owner
login, and the REST addresses of the gist and of each file in `links`. The id and the addresses of a secret
gist (`public` is `false`) are reported like those of a public one, since the id is the argument of the other
tools; whoever holds them can read a secret gist without signing in, so they are as sensitive as the
content. A gist forked from another names it in `fork_of` with its own members only. GitHub marks `forks` and
`history` of a single gist as deprecated, so `github.gists.get` leaves them out; `github.gists.list` reports
at most 10 of each, with `forks_truncated` and `history_truncated`. File contents are not read beyond the
bounded content `github.gists.get` already shows. An account inside another object carries the fields
GitHub's own REST answers give it; the full profile is read with the account tools.

`github.gists.create` makes a gist that is secret unless `public=true`. `github.gists.update` changes the
description (an empty text clears it), writes files, and removes files in one request. `github.gists.delete`
removes a gist permanently and is offered only where the connection's `tools` list names it.

The targets act as follows. A connection without targets reaches whatever its token reaches. A connection with
targets must name at least one user as `users/LOGIN` and no repository and no project; otherwise every gist
tool is an invalid request before a credential is resolved. Such a connection reaches only the gists of the
users it names: `username` of the list must be one of them, the list without `username` and `create` need the
account behind the token among them (checked with one read of the account), and `get`, `update`, and `delete`
read the gist first and answer `not-found`, without a change, for a gist whose owner is no named user, or an
anonymous one. An organization owner target does not count, since a gist belongs to a user.

An [exclusion](README.md#targets) of a user drops that user's gists from lists and makes `get`, `update`, and
`delete` answer `not-found`; `github.gists.create` is refused when the account behind the token is excluded.

Every change is sent once and is never retried, see [confirmed changes](README.md#confirmed-changes). A create
is not idempotent; an update sets the named state and leaves the same state when repeated; GitHub documents no
answer for repeating a delete, so it declares its idempotency as unknown, and a repeated delete can answer
`not-found`.

## Comments, revisions, forks, and stars

Nine more tools work on one gist by its id. None takes a target argument.

| Tool | Does |
| --- | --- |
| `github.gist-comments.list` | lists one bounded batch of the comments of a gist, oldest first |
| `github.gist-comments.get` | reads one comment by its id |
| `github.gist-comments.create` | adds a comment as the account behind the token |
| `github.gist-comments.update` | replaces the text of a comment |
| `github.gist-comments.delete` | deletes a comment permanently; listed only |
| `github.gist-commits.list` | lists one bounded batch of the revisions of a gist, newest first |
| `github.gist-forks.list` | lists one bounded batch of the forks of a gist, as gists without content |
| `github.gist-forks.create` | forks a gist into the account behind the token |
| `github.gist-stars.set` | stars or unstars a gist with `starred=true` or `starred=false` |

A comment answers with the members of GitHub's gist comment: `id`, `node_id`, `body`, `user` as an account,
the times, `author_association`, and the REST address in `links`. The text is written by other accounts and is
untrusted data: it is cut at 4000 characters, and `body_truncated` says so. A revision answers with `version`
(the commit SHA), `user`, `committed_at`, `change_status` (total, additions, deletions), and `links`. A fork
answers like an entry of `github.gists.list` in `gists`, with its owner, file metadata, and at most 10 forks and
10 history entries of its own; file contents are not part of it. The
lists take `limit` and a `cursor` that is bound to the gist and the list, and another one refuses it as an
invalid request. No address in an answer is ever fetched, and the contents of a file are never read through the
raw address.

The path of a comment binds the gist and the comment, so a comment id of another gist is `not-found`; the answer
of `github.gist-comments.get` is checked against its own address as well. A comment can only be changed or
deleted by the accounts GitHub allows to, and GitHub's refusal comes back as a permission error.

`github.gist-stars.set` reads the current state once, as `github.stars.add` does: a gist already in the
requested state is reported with `changed=false` and no further request, otherwise the star is changed once. It
is idempotent. `github.gist-comments.create` and `github.gist-forks.create` are not idempotent, a comment is
updated to the named text, and `github.gist-comments.delete` declares its idempotency as unknown.

The targets act as for the gist tools above: a connection with targets must name users as `users/LOGIN` and no
repository and no project, and every tool of this section reads the gist first and answers `not-found`, without
a change, for a gist whose owner is no named user or an excluded one, or an anonymous one. A fork needs the
account behind the token among the named users, like `github.gists.create`, and the list of forks shows only
forks whose owner the targets admit. Comments and stars are not tied to that account.

Every change is sent once and is never retried, see [confirmed changes](README.md#confirmed-changes); an
unclear outcome is reported as uncertain and has to be read back before it is repeated.

The not-recommended setup profile `gists` ticks the list, the get, the create, and the update of a gist, the
comment list, get, create, and update, the revision list, the fork list and create, and the star;
`github.gists.delete` and `github.gist-comments.delete` stay unticked, and the recommended profile `read` is
unchanged.

The token rights are listed under [Gists](setup.md#gists).

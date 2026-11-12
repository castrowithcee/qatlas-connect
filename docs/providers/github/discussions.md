---
description: >
  Describes the GitHub discussion tools: the categories, discussions, and comments of one repository, how organization discussions are read, and creating, changing, closing, locking, and deleting discussions, writing comments and replies, and marking answers, with their limits.
type: knowledge
edit: shared
created: 2026-10-22
updated: 2026-11-12
---

# GitHub discussions

The discussion tools of the [GitHub provider](README.md).

## Discussions

Four read tools and twelve write tools cover the GitHub Discussions of one repository, all through GraphQL and
all taking `repository` like the other repository tools; a repository outside the connection's targets is
refused as an invalid request before any credential is resolved.

| Tool | Does |
| --- | --- |
| `github.discussion-categories.list` | lists the discussion categories of a repository |
| `github.discussions.list` | lists one bounded batch of discussions, filtered by category, state, and answered status |
| `github.discussions.get` | reads one discussion by its number |
| `github.discussion-comments.list` | lists one bounded batch of the top-level comments of a discussion, with their replies on request |
| `github.discussions.create` | starts a discussion in a category of the repository |
| `github.discussion-comments.create` | adds a comment or a reply |
| `github.discussion-comments.update` | replaces the body of a comment or reply |
| `github.discussion-comments.delete` | deletes a comment or reply; listed only |
| `github.discussions.update` | changes the title, body, or category of a discussion |
| `github.discussions.close` / `github.discussions.reopen` | closes a discussion, optionally as resolved, outdated, or duplicate, or reopens it |
| `github.discussions.lock` / `github.discussions.unlock` | locks a discussion, optionally with a reason, or unlocks it |
| `github.discussion-comments.mark-answer` / `github.discussion-comments.unmark-answer` | chooses a top-level comment as the answer, or withdraws the choice |
| `github.discussions.delete` | deletes a discussion with its comments; listed only |

A discussion body is cut at 1000 characters in a list (`body_truncated` says so) and at 20000 characters in
`github.discussions.get`. `github.discussion-comments.list` lists the top-level comments of one discussion,
oldest first, with a body cut at 4000 characters. Replies are counted; with `include_replies=true` each
top-level comment also carries at most 100 replies, GitHub's maximum, each body cut at 4000 characters with
`body_truncated`, and `replies_truncated` says that more replies exist. The reply bodies of one answer share a
budget of 200000 characters; once it is spent, further reply bodies come back empty with `body_truncated`. A
cursor of a list without replies does not continue a list with them. Titles, bodies, and comments come from
other accounts and are untrusted data.

A discussion, a comment, and a reply answer GitHub's Discussion and DiscussionComment in full. The repository
and the discussion a call names are fixed by the call and not repeated in every entry. Rendered variants
repeat the text field in another format and add no content. A connection GitHub also serves as a list of its
own is read with that list, so the comments, reactions, and edit history of a discussion and the reactions
and edit history of a comment are reported as counts, and reaction groups only for kinds somebody gave. The
labels and the poll options are read with their first page, at most 20 labels and, in
`github.discussions.get`, 20 poll options, in `github.discussions.list` 10, each with its total and a marker
when more exist. The chosen answer is named with its id, address, author, and time; its text is read with
`github.discussion-comments.list`. An account inside another object carries the fields GitHub's own REST
answers give it; the full profile is read with the account tools. The `viewer_*` flags describe what the
token's user may do and has done.

A cursor belongs to the repository and the filters that produced it, and another filter, category, or
discussion refuses it as an invalid request. A discussion that GitHub does not hold or does not show to the
token is `not-found`, named as "discussion #N".

Organization discussions are read through the repository that stores them, usually `ORG/.github`: name that
repository as `repository`, and it must be among the connection's targets. Qatlas does not fall back to the
`.github` repository of an organization on its own.

`github.discussion-comments.create` adds a top-level comment to a discussion `number`, or, with `reply_to`
(the node `id` of a top-level comment of that discussion), a reply; GitHub allows one level of replies, so a
reply to a reply is refused. `github.discussion-comments.update` replaces the body of a comment or reply by its
node `id` (`comment_id`), and `github.discussion-comments.delete` removes one permanently. Before any mutation
the comment or category is read back and must belong to the chosen `repository`; a foreign one is refused
(`not-found`, or `invalid-request` for a reply to a reply or to a comment of another discussion) without a
change. Written bodies are not echoed. Every change is sent once and is never retried, see
[confirmed changes](README.md#confirmed-changes); an unclear outcome says it may have been applied.

The discussion tools take the discussion `number`, which is read from the chosen `repository` first; the
mutation uses the node id of that reading. `github.discussions.update` needs at least one of `title`, `body`,
and `category_id`, and a category of another repository is refused like in `github.discussions.create`.
`github.discussions.close` takes an optional `reason` (`resolved`, `outdated`, `duplicate`), and
`github.discussions.lock` an optional `lock_reason` (`off-topic`, `too heated`, `resolved`, `spam`). The
answer of these tools names the discussion with `id`, `number`, `url`, `closed`, and `locked`; written text is
not echoed. `github.discussion-comments.mark-answer` takes the node `id` of a comment (`comment_id`): the
comment must belong to a discussion of the chosen `repository`, the discussion must be in a category that
accepts answers ("Answerable", see `is_answerable` of `github.discussion-categories.list`), and the comment
must be a top-level comment, not a reply; otherwise it is refused as an invalid request without a change.
`github.discussion-comments.unmark-answer` takes the same `comment_id` and checks only the repository.
`github.discussions.delete` removes a discussion and its comments permanently.

Categories are not created or changed and polls are not written; both stay with the GitHub web interface.
Creating is not idempotent, since each request adds a new object; updating is idempotent, since the same body
leaves the same state; deleting has unknown idempotency, since GitHub answers a second delete of the same
comment with an error. Updating, closing, reopening, locking, unlocking, and marking or unmarking an answer
are described as idempotent, since they leave the same state; whether GitHub answers a repeated request with
an error is not verified. Deleting a discussion has unknown idempotency like deleting a comment. Deleting is
offered only where the connection's `tools` list names `github.discussion-comments.delete` or
`github.discussions.delete`.

The not-recommended setup profile `discussions` ticks the read tools, `github.discussions.create`,
`github.discussion-comments.create`, `github.discussion-comments.update`, and the update, close, reopen, lock,
unlock, mark-answer, and unmark-answer tools; the two deletes stay unticked, and the recommended profile `read`
is unchanged.

The token rights are listed under [Discussions](setup.md#discussions).

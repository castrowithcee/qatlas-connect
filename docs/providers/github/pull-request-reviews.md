---
description: >
  Describes the GitHub pull request review tools: conversation comments, reviews with bundled line comments, replies, review threads, requested reviewers, changes to existing reviews and line comments, and the listed-only approve, dismiss, and line comment delete.
type: knowledge
edit: shared
created: 2026-11-05
updated: 2026-11-11
---

# GitHub pull request reviews

The pull request review tools of the [GitHub provider](README.md). Every tool takes `repository` as described under
[the target of a call](README.md#the-target-of-a-call); a change needs the confirmation described under
[confirmed changes](README.md#confirmed-changes). The pull requests themselves are described under
[pull requests](pull-requests.md).

## Pull request reviews and comments

The tools of this section read and write a pull request's conversation comments, its reviews, the line
comments a review bundles in, replies to an existing line comment, its review threads, and its requested
reviewers. `github.comments.*` is unchanged and keeps refusing a pull request number; the two conversation
comment tools of this section are its mirror image, refusing an issue number instead, described below. A
line comment is never written on its own: it exists only bundled into the one review
that creates it, in `github.pull-request-reviews.create` or `.approve`, or as a reply to an existing one in
`github.pull-request-review-comments.reply`. Existing reviews and line comments change with the tools described
under [changing reviews and line comments](#changing-reviews-and-line-comments) below. The thirteen tools of this
section that neither approve, dismiss, nor delete are offered by the not-recommended setup profile
`pull-request-reviews`; approving is in no profile, offered only while a connection's `tools` list names
`github.pull-request-reviews.approve`, because an approval given on the connection's behalf is the one outcome
no other review tool can reach. `github.pull-request-reviews.dismiss` and
`github.pull-request-review-comments.delete` are listed only as well, and
`github.pull-request-reviews.delete-pending`, a delete, is in no profile.

| Tool | Does |
| --- | --- |
| `github.pull-request-comments.list` | lists the conversation comments of a pull request, oldest first |
| `github.pull-request-comments.create` | writes exactly one conversation comment |
| `github.pull-request-reviews.list` | lists the reviews of a pull request |
| `github.pull-request-reviews.create` | submits a review as `comment` or `request_changes`, with its line comments bundled in |
| `github.pull-request-reviews.approve` | approves, with an optional body and line comments; offered only where a connection's `tools` list names it |
| `github.pull-request-reviews.update` | replaces the text of a pending review or of a review the token's user wrote |
| `github.pull-request-reviews.dismiss` | dismisses an approval or a request for changes, with a required message; offered only where a connection's `tools` list names it |
| `github.pull-request-reviews.delete-pending` | deletes a pending review that was not submitted yet |
| `github.pull-request-review-comments.list` | lists the line comments of a pull request, across every review |
| `github.pull-request-review-comments.reply` | writes exactly one reply in the thread of one line comment |
| `github.pull-request-review-comments.update` | replaces the body of a line comment |
| `github.pull-request-review-comments.delete` | deletes a line comment permanently; offered only where a connection's `tools` list names it |
| `github.pull-request-review-threads.list` | lists the review threads of a pull request, each with its resolved state and its comments |
| `github.pull-request-review-threads.resolve` | marks one review thread resolved |
| `github.pull-request-review-threads.unresolve` | opens one resolved review thread again |
| `github.pull-request-reviewers.request` | requests users, teams, or both as reviewers |
| `github.pull-request-reviewers.remove` | removes users, teams, or both from the requested reviewers |

```yaml
connections:
  pull-reviewer:
    service: github
    credential: github-operator
    target: repos/octo-org/example
    tools: [github.pull-request-comments.list, github.pull-request-comments.create,
      github.pull-request-reviews.list, github.pull-request-reviews.create, github.pull-request-reviews.update,
      github.pull-request-review-comments.list, github.pull-request-review-comments.reply,
      github.pull-request-review-comments.update,
      github.pull-request-review-threads.list, github.pull-request-review-threads.resolve,
      github.pull-request-review-threads.unresolve, github.pull-request-reviewers.request,
      github.pull-request-reviewers.remove]
  pull-approver:
    service: github
    credential: github-operator
    target: repos/octo-org/example
    tools: [github.pull-request-reviews.approve]
```

`github.pull-request-comments.list` and `.create` read and write a pull request's conversation comments through
the same Issue Comments REST route `github.comments.*` uses; an issue number is refused as `invalid-request`,
naming the number and the repository, the mirror image of the refusal `github.comments.*` gives a pull
request number.

`github.pull-request-reviews.create` and `.approve` require `sha`, the pull request's head commit exactly as
`github.pull-requests.get` reports it, sent to GitHub as `commit_id`, so a review never lands on a head the
caller did not see. `event` is required for `.create` and always an approval for `.approve`; `line_notes` bundles the review's line
comments into this one call, at most 50. The argument is named `line_notes`, not
`comments`, because a change tool's input schema may not offer an argument by that name. A head that changed
since it was read, GitHub reviewing one's own pull request, an unknown commit, or a line comment naming a
line outside the diff all answer 422; Qatlas re-reads the pull request once to tell a changed head apart
and refuses `invalid-request` naming the expected and the current commit, or, for another reason, answers
`provider-error` naming the likely causes. Neither is ever retried automatically.

`github.pull-request-review-comments.list` reads a pull request's line comments across every review. `sort`,
`direction`, and `since` (an RFC 3339 time, sent to GitHub in UTC) are part of the cursor binding when set, so a
continuation never pages with other filters.
`github.pull-request-review-comments.reply` takes `comment_id`, as the list reports it, and writes exactly one
reply in its thread; a `comment_id` GitHub does not hold answers not found, naming the review comment.

### Changing reviews and line comments

`github.pull-request-reviews.update`, `.dismiss`, and `.delete-pending` take `number` and `review_id`, the `id`
`github.pull-request-reviews.list` reports. Every one reads the review first, below that pull request of the
chosen repository: a review that GitHub does not hold there, such as one of another pull request or another
repository, answers `not-found` and nothing is sent. The change itself is one request that is never repeated;
a timeout, a server error, or an unreadable answer is reported as an uncertain outcome.

`.update` replaces the review's summary (`body`, required). GitHub decides who may change a review, a pending
one or one the token's user wrote; its rejection is reported as `provider-error` naming that rule, and a
refusal for lack of a right as `permission`. `.dismiss` takes a required `message`, 1 to 65536 characters, which
GitHub shows with the dismissal; it discards an approval or a request for changes of another reviewer and is
listed only for that reason. GitHub dismisses only an approval or a request for changes that is not dismissed
yet and rejects anything else. `.delete-pending` deletes the review and the line comments bundled in it, and only
when the read before it shows the state `PENDING`; a submitted review is refused as `invalid-request` without a
request. Reviews are still created in one call and never as a multi-step draft, so a pending review exists only
where it was started outside Qatlas.

`github.pull-request-review-comments.update` and `.delete` take `comment_id`, the `id`
`github.pull-request-review-comments.list` reports, and an optional `number`. The comment is read first, below
the chosen repository; a comment of another repository answers `not-found`. With `number` it must also belong to
that pull request, otherwise it answers `not-found` too and nothing is sent; without `number` the repository
alone binds it. GitHub changes only comments the token's user wrote or that its rights reach. `.delete` removes
the comment permanently. The texts of a change are untrusted data, bounded to 65536 characters, and GitHub's own
text never appears in a message.

GitHub keeps whether a review thread is resolved only in GraphQL, never in the REST line comments read
above, so `github.pull-request-review-threads.list`, `.resolve`, and `.unresolve` read and change that state on
their own, by the thread's `id` as the list reports it. The list reports the first 50 comments of a thread, with
`comments_total_count` and `comments_truncated`: "A connection GitHub also serves as a list of its own is read with
that list; this tool reports its count." The line comments of a pull request are read with
`github.pull-request-review-comments.list`. `.resolve` and `.unresolve` also take `number`: before
either changes anything, Qatlas reads the thread node itself and confirms it is a thread of `number` in the
bound repository; a `thread_id` of another pull request or another repository is refused not found, naming
"this review thread", and nothing is sent. Resolving an already-resolved thread, or unresolving an
already-open one, is left as it is and reported in its current state.

A conversation comment, a review, or a line comment is hidden and shown again with `github.comments.hide` and
`github.comments.unhide` (kind `pull-request-comment` or `review`, each with its `pull_number`, or
`review-comment`), described under
[Changes](issues.md#changes).

`github.pull-request-reviewers.request` and `.remove` take users, teams of the repository's organization, or both,
at least one and at most 15 together, and answer with the pull request itself, the same shape
`github.pull-requests.get` reports, so `requested_reviewers` is read back after the change. A reviewer already
requested, one not requested, and the pull request's own author are all left as they are.

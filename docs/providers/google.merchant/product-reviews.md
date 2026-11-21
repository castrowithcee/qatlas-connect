---
description: >
  Describes the product review tools of the Google Merchant provider: listing and reading the product reviews of an account, deleting one through the tools list of a connection, the reviewer identifiers they show as personal data of third parties, the bounds on untrusted review text, the review ID form, the exclusion of submitting reviews, delayed effect, error and unknown-outcome handling, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant: product reviews

The product review tools of the [Google Merchant provider](README.md). The account argument, targets, quotas,
cursors, and errors are described there.

A product review is a review of a product that a publisher delivers to the account through a product review data
source. These tools read the reviews Google holds and delete one.

| Tool | Does |
| --- | --- |
| `product-reviews.list` | reads the product reviews of an account, a batch per call with `cursor` |
| `product-reviews.get` | reads one product review |
| `product-reviews.delete` | deletes one product review |

`product-reviews.list` and `product-reviews.get` are in the `read` profile. `product-reviews.delete` is in no
profile: a connection offers it only when its `tools` list names it, and every call needs a confirmation that shows
the account and the review ID. `account` is checked against the targets before any secret is read; a sub-account of a
bound advanced account is proven by one read of the sub-accounts before a review is read or deleted, and an account
outside the targets is refused without naming it and without any further request.

**Submitting reviews is not offered.** No tool of this provider calls `productReviews.insert`, because a review
carries personal data of a reviewer and replaces a review as a whole. The tools take no data source.

## Reviewer identifiers

A review holds content and identifiers of third parties. The results show `reviewerId`, `reviewerUsername`, and
`reviewerImageLinks` as Google sends them, together with `reviewerIsAnonymous` and the other members of the 31
documented attributes. These members are **personal data of third parties**: the ID and the name identify the
reviewer, and the image links point at pictures the reviewer made. The data sensitivity of the three tools says so.
Treat the results accordingly and do not pass them on without need. `reviewerId` is cut at 100 characters,
`reviewerUsername` at 300, `reviewerImageLinks` at 20 addresses of at most 2048 characters. `transactionId` is the
publisher's ID of a transaction and is shown as text. A review is addressed by its own ID, `productReviewId`, which
is not an identifier of the reviewer.

## Review ID

`product_review_id` is 1 to 128 ASCII letters, digits, dots, dashes, and underscores, starting with a letter or
digit. It is checked before any secret is read. Google documents it as the permanent identifier in the publisher's
system and gives no form, so this is Qatlas' narrow form, chosen so that an ID can never alter a path or a query: no
`/`, no space, no `%`, no leading `.`. A review whose ID has another form can be listed but not read or deleted
through these tools. The path of every request is built from the fixed sub-API path, the checked account ID, and
the checked review ID.

## Reading

`product-reviews.list` takes `account`, `page_size` (1 to 100, 25 when left out), and `cursor`. Qatlas always sends
a page size, because Google documents no default. A cursor is tied to the tool, the account, the page size, and the
targets. `product-reviews.get` takes `account` and `product_review_id`.

A review shows `name`, `productReviewId`, `productReviewAttributes`, `customAttributes` (`name`, `value`, and one
level of `groupValues`), `dataSource`, and `productReviewStatus` (`destinationStatuses`, `itemLevelIssues` with
`code`, `severity`, `resolution`, `attribute`, `reportingContext`, `description`, `detail`, `documentation`, and
`createTime`, `lastUpdateTime`). Reviews and their texts are untrusted content of reviewers and the publisher, and
addresses in them (`reviewLink`, `productLinks`, `publisherFavicon`, `reviewerImageLinks`, `documentation`) are
text that is never fetched or followed, and a user name or password inside one is removed. Qatlas cuts them:

- `title` and `content` at 2000 characters, each advantage in `pros` and each disadvantage in `cons` at 500,
- names and other texts at 300 characters, codes and IDs at 100, addresses at 2048,
- every list of a review (`pros`, `cons`, `productNames`, `productLinks`, `reviewerImageLinks`, `asins`, `gtins`,
  `mpns`, `skus`, `brands`) at 20 entries, lists of custom attributes and issues at 300,
- a list of reviews at 100.

A cut text ends with `…`. A member that is not documented is dropped, and so is a value of the wrong type.
`rating` is the number Google sends. The caps are Qatlas' own: Google documents no length limit for a text member.

## Deleting

`product-reviews.delete` takes `account` and `product_review_id` and sends one `DELETE` of that review. Deleting
removes the review from the account. The result says only that Google accepted the request, with `accepted` and the
`name` of the review. Google applies the deletion afterwards, so the review can still show for some time; read it
again later. The request is sent once and never repeated. A failure that leaves the outcome open (no answer, a
timeout, a `5xx`, or an unreadable answer) says that the outcome of the change is unknown, so read the review before
deleting again. `429` is `rate-limited` and `403` is `permission`; neither is repeated. Every call counts against the
quota.

## Checked and not verified

Checked against the discovery document of the reviews API v1 (revision 20261118) and the reference pages of
`productReviews.list`, `get`, and `delete`: the paths `GET reviews/v1/accounts/ACCOUNT/productReviews`, `GET` and
`DELETE` of `.../productReviews/REVIEW`, the scope `content` for all of them, an empty request body, an empty object
as the answer of `delete`, `pageSize` and `pageToken` of `list` (with the rule that the other arguments must match
the call that made the token), and the 31 members of the attributes. The discovery document marks no member as
deprecated. `customAttributes` have `name`, `value`, and `groupValues`, of which exactly one of `value` and
`groupValues` is set.

Not verified: no live call was made. Google documents no default and no maximum for `pageSize`, no length limit
for a text member, and no form for a review ID, so the caps above are Qatlas' own. It is not documented which role
the methods need, whether deleting a review that does not exist is an error, whether a deleted review can be
restored, whether a review that a feed delivers again returns, or how long Google takes to apply a deletion; the
tools make no claim about them.

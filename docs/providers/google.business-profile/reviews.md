---
description: >
  Describes the review tools of the Google Business Profile provider and the My Business API v4 client behind them: listing and reading reviews of one or several locations, replying to a review and deleting the reply with the previous values and the confirmation text, the v4 location path and how targets bind it, the bounds on untrusted review text and reviewer data, and what the v4 documentation leaves unverified.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: reviews

The review tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The tools use the My Business API v4 at
`https://mybusiness.googleapis.com/v4`, which has to be enabled in the Cloud project like the other APIs. Google
documents every review method only for a **verified** location.

## The v4 client

Google keeps reviews, local posts, media, and a few more groups in v4 and publishes no discovery document for
it. The parameters and answer members of the tools come from the reference at
`developers.google.com/my-business/reference/rest/v4` (the Review resource and the methods `reviews.list`,
`reviews.get`, `reviews.updateReply`, `reviews.deleteReply`, and `locations.batchGetReviews`), read on
2026-11-18; no live call was made. The client shares the credential, the request spacing, the cursor, and the
error classes of the other tools, and it never follows a redirect.

A v4 resource is named below its account: `accounts/ACCOUNT/locations/LOCATION`, with decimal IDs. The tools
check such a path against that form and the target before a secret is read:

| Target | Rule |
| --- | --- |
| none | any well-formed path |
| account | the account part must be the bound account, and the location is first proved to be in the account's location list, as for `locations.get` |
| location | only the location ID is compared; the account part is a free argument, since a location names no account |

A refusal never names the other object. The `reviews` tools are offered on every target.

## Reading

All three tools are part of the profile `read`, are safe to repeat, and return personal data: the name and
profile photo link of reviewers, the text and rating of their reviews, and the reply of the business.

- `reviews.list`: `location` as `accounts/ACCOUNT/locations/LOCATION`, `page_size` 1 to 50, `order_by`
  (`rating`, `rating desc`, or `updateTime desc`, Google's default), and `cursor`. The result holds `reviews`,
  `averageRating`, `totalReviewCount` of the whole location, and `next_cursor`. A cursor belongs to the location
  and order it came from.
- `reviews.get`: `review` as `accounts/ACCOUNT/locations/LOCATION/reviews/REVIEW`.
- `reviews.batch-get`: `account`, `location_names` (1 to 50 locations, each below `account`, no repeats),
  `page_size` 1 to 50, `order_by`, `ignore_rating_only_reviews` (deprecated by Google), and `cursor`. It is a
  POST that only reads. The result holds `locationReviews` with the `name` of each location and its `review`.
  Google answers the batch as a whole: a location it refuses fails the call, there is no partial result. Entries
  for a location that was not asked for are dropped. On an account target every location is proved with one
  pass over the account's location list.

A review carries `name`, `reviewId`, `reviewer` (`displayName`, `profilePhotoUrl`, `isAnonymous`), `starRating`
(`ONE` to `FIVE`), `comment`, `createTime`, `updateTime`, `reviewReply` (`comment`, `updateTime`,
`reviewReplyState`, and `policyViolation` while the reply is rejected), and `reviewMediaItems` (`thumbnailUrl`,
`thumbnailLabel`, `videoUrl`).

## Untrusted text

Review texts, reply texts, reviewer names, labels, and links are written by anyone, and a review is a typical
carrier of embedded instructions. They are returned unchanged as data: texts are cut at 4000 characters
(names and labels at 500, links at 2048, codes at 100) and lists at 250 entries, members that are not documented
are dropped, and nothing in a result is followed. Profile photo, thumbnail, and video links are text and are
never fetched. A reviewer with `isAnonymous` true is returned as anonymous, without `displayName` and
`profilePhotoUrl`, whatever Google sends in them.

## Replying

`reviews.update-reply` (`PUT .../reviews/REVIEW/reply`) publishes the reply of the business, or replaces the
current one. It is public, appears in the name of the business, and needs confirmation. It belongs to the
profile `write`.

| Argument | Meaning |
| --- | --- |
| `review` | required, the review path |
| `comment` | required, the new reply, at most 4096 bytes |
| `previous_star_rating` | required, `starRating` as last read; only compared, never sent |
| `previous_comment` | required, `comment` of the review as last read, empty for a rating-only review; only compared, never sent |
| `previous_reply` | `comment` of the current reply as last read; required when the review has a reply, which the new one replaces |

The tool reads the review first and sends nothing unless rating, text, and reply are exactly what the arguments
name; the body holds only `comment`. The confirmation shows the location, the rating, an excerpt of the review
(40 characters), the full reply, every URL in the reply, and the excerpt of a reply that is replaced. The
confirmation of this tool may hold 9216 characters, which fits a reply of Google's 4096 bytes with every line
break and address counted. A reply that would not fit with that context is refused before anything is sent, and so is a reply with control, invisible formatting, or separator
characters other than line breaks and tabs. Google may hold a reply back: the result shows
`reviewReplyState` (`PENDING`, `REJECTED`, or `APPROVED`). The time between the read and the change is a limit
of the comparison.

`reviews.delete-reply` (`DELETE .../reviews/REVIEW/reply`) removes the reply of the business, publicly, and
needs confirmation; it also belongs to the profile `write`. It removes only the reply: no tool deletes or reports
a review.

**Spacing.** Both changes count against the 10 changes per minute and profile; Qatlas spaces them by 6 seconds
per location, like the other changes. A change whose outcome is unclear (timeout, `5xx`, unreadable answer) is
reported as such and never repeated.

## Not verified

- No live call was made; the reference gives no limits for the length of an ID, so Qatlas accepts review IDs of
  up to 256 letters, digits, `_`, and `-`.
- `batchGetReviews` names no maximum page size; Qatlas keeps the 50 of `reviews.list`. It also names no rule for
  locations of another account, so Qatlas requires every location to lie below `account`.
- Whether the locations of a location group answer below the group's account in v4.
- How `reviewReplyState` and `policyViolation` appear in the answer of `updateReply`; Qatlas returns what Google
  sends.

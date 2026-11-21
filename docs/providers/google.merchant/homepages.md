---
description: >
  Describes the Google Merchant tools for the home page of an account: reading it, setting its address, claiming and unclaiming it, who may use them, the https check, the overwrite warning, the delayed processing, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant home page

The home page of a Merchant Center account is the address of the store website, and claiming it tells Google
that the account holder owns that website. Four tools cover it. Targets, quotas, results, and errors are as
described in the [provider overview](README.md).

| Tool | Method | Effect |
| --- | --- | --- |
| `google.merchant.homepages.get` | `accounts.homepage.get` | reads `name`, `uri`, and `claimed` |
| `google.merchant.homepages.update` | `accounts.homepage.update` | sets `uri` |
| `google.merchant.homepages.claim` | `accounts.homepage.claim` | claims the home page |
| `google.merchant.homepages.unclaim` | `accounts.homepage.unclaim` | removes the claim |

## Access

`homepages.get` is a read and belongs to the profile `read`. The three tools that change data are offered only to
a connection whose `tools` list names them, no effect profile selects them, and they need a confirmation on every
call. They need the role Admin in Merchant Center; Google names it for each of them, and `get` has no role
requirement documented. Google answers a missing role with `403`, which Qatlas reports as `permission`.

The `account` argument is the account ID. The three changing tools require it, `get` takes it from a connection
with exactly one target. It is checked against the targets before any secret is read or a request is sent. An
account that no target names is changed only after the proof read of the sub-accounts of a bound advanced account,
as for `accounts.get`; a refused account is never changed.

## Home page address

`uri` of `update` is required and must be an absolute `https` address with a host, without user information, white
space, control characters, or backslashes, at most 2048 characters. Qatlas refuses anything else before a secret is
read or a request is sent, because the address steers customers. The address is text handed to Google and never
fetched. Google documents no length for it; 2048 is the bound of Qatlas and of a shown result. Only `uri` is
changed, through the update mask `uri`.

A result shows `uri` as text, cut at 2048 characters. `claimed` is also `true` when the account inherits the claim
from its advanced account or is exempt from claiming, and it is shown when `false`.

## Claim and unclaim

- `claim` verifies the website of the account. An already claimed home page is verified again and the call
  succeeds. When ownership cannot be verified, Google answers with an error and keeps an existing claim. A call
  fails with `FAILED_PRECONDITION` when the account is no Merchant Center account, has no home page, or the claim
  failed; the error detail of Google carries the reason.
- `overwrite` of `claim` is off unless the argument says `true`. With it Google removes an existing claim on the
  website held by another account and transfers it to this account, so the other account loses its claim. The
  description of the tool and the confirmation say so.
- `unclaim` removes only a claim made directly on the account. When the claim is inherited from an advanced
  account, the call succeeds and `claimed` stays `true`. An account that is exempt from claiming is refused by
  Google. The method takes no body.

## Delayed processing and no repetition

The answer of a changing tool only tells that Google accepted the change. It can take several minutes until
`homepages.get` shows it. Each call counts against the quota of the Accounts group, a failed one too. Qatlas sends
a change once and never repeats it, also not after a renewed token. A failure that may have reached Google, such as
a server error, a lost connection, or an unreadable answer, says that the outcome is unknown: read the home page
before changing it again. A `4xx` answer rejected the request before any change.

## Boundary

Checked against the reference pages of the methods in the accounts API v1: the path of each method is
`accounts/{account}/homepage` with the verbs `:claim` and `:unclaim`; `update` is a `PATCH` whose only supported
update mask is `uri`; `claim` takes the optional boolean `overwrite`; `unclaim` takes no body; every method uses
the scope `content`, and `update`, `claim`, and `unclaim` need admin access. The resource has the members `name`,
`uri` (required), and `claimed` (output only).

Not verified:

- No live call was made.
- Whether a changed address drops an existing claim, and how long verification takes after a claim.
- Which Merchant Center role `get` needs; Google documents none.
- The answers of Google for an account that has no home page or one that is no Merchant Center account, beyond
  the error codes named above.

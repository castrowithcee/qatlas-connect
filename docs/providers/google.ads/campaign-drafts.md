---
description: >
  Describes the campaign draft tools of the Google Ads provider: campaign-drafts.create, campaign-drafts.update, campaign-drafts.remove, campaign-drafts.promote, and campaign-drafts.list-async-errors, with the Mutate service and its bound of 10 operations, the name-only update, the composite resource name, validate_only, confirmation, how create and update are reached by effect while remove and promote need the tools list, the promotion as a long-running operation without a poll tool, the read of asynchronous errors with paging, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: campaign draft tools

These tools create, rename, and remove the campaign drafts of a Google Ads account, promote a draft into its base
campaign, and list the errors of a promotion. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-drafts.create` | create | creates drafts of base campaigns of the account |
| `google.ads.campaign-drafts.update` | update | renames drafts |
| `google.ads.campaign-drafts.remove` | delete | removes drafts |
| `google.ads.campaign-drafts.promote` | execute | promotes the changes of one draft into its base campaign |
| `google.ads.campaign-drafts.list-async-errors` | read | lists the errors of the promotion of one draft |

Reading drafts is not a tool of this page: run `reports.search` on the `campaign_draft` resource, which also shows
`campaign_draft.status` and the draft campaign. The changes inside a draft are made on its draft campaign with the
campaign tools.

## How the tools are offered

- **`campaign-drafts.create` and `campaign-drafts.update`** are offered by a connection whose effects include
  `create` or `update`, and by a connection whose `tools` list names them. They belong to no profile.
- **`campaign-drafts.remove` and `campaign-drafts.promote`** are offered only through the `tools` list. A connection
  with the `delete` or `execute` effect and no `tools` list does not have them.
- **`campaign-drafts.list-async-errors`** reads only. It is part of the `read` profile and needs no confirmation.
- **Confirmed per call.** The four changing tools need the confirmation of that one call. The confirmation text
  names the account and the drafts, with the texts cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request, in all five tools. The `login_customer_id` of the service is never an argument.

## What every change does

- **One atomic request.** `create`, `update`, and `remove` send one request to
  `CampaignDraftService.MutateCampaignDrafts` with an `operations` list and without `partial_failure`: Google
  applies all operations or none. A list holds 1 to 10 entries. The 10 are Qatlas's own bound; the reference
  states none.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so
  the answer of `create`, `update`, and `remove` then holds `validate_only: true` and no `results`. Validation
  needs the same confirmation and the same checks, and costs the same operations as the change.
- **No `response_content_type`.** The request has no such field in v26, so a result holds `base_campaign_id`,
  `draft_id`, and `resource_name` only.
- **Names come from checked arguments.** A draft is named by `customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID`
  of `customer_id`, as `reports.search` returns it. Both IDs must be positive numbers; a name of another account
  is refused before any request. A caller never passes a field mask or an operation. The base campaign of a new
  draft is `customers/ID/campaigns/N` of the same `customer_id`.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or
  not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the
  drafts with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole
  request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/campaignDrafts/BASE~DRAFT` of the `customer_id` of the call. For `update` and `remove` the results
  must be exactly the drafts asked for, in the order asked for; a `create` must answer one distinct draft per
  operation, each of the base campaign asked for in that position. Anything else is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made. A
  `partial_failure_error` in an answer is treated the same way.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too; a
  promotion counts one. A call may add one operation for the membership read below a manager target and one for the
  check that the account is no manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Promotion

`campaign-drafts.promote` sends one `POST customers/ID/campaignDrafts/BASE~DRAFT:promote`. The base campaign then
serves with the changes of the draft, which can change its targeting, ads, and spend, so the tool is offered only
through the `tools` list and always confirmed. It is never repeated. A promotion that fails partway cannot be
retried safely: Google marks the draft `PROMOTE_FAILED` and the problem is fixed in the base campaign.

- **Result.** Google answers with a long-running operation and returns only a done state. The tool returns
  `operation` (its name, untrusted provider text, control characters replaced and cut to 1024 bytes, absent when
  Google gave none), `done`, and `error` with `code` and a cut `message` when the operation reports one. The
  `details` of a status are not returned.
- **No poll tool.** No tool reads an operation. The outcome is the status of the draft: read
  `campaign_draft.status` with `reports.search` (`PROMOTING`, `PROMOTED`, or `PROMOTE_FAILED`).
- **`validate_only`.** With `true`, Google validates the promotion and does not make it.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `campaign_drafts` | 1 to 10 entries |
| `create` | `campaign_drafts[].base_campaign` | required, `customers/ID/campaigns/N` of `customer_id`, positive number; cannot be changed afterwards |
| `create`, `update` | `campaign_drafts[].name` | required, 1 to 255 characters, no control characters |
| `update` | `campaign_drafts` | 1 to 10 entries, each `campaign_draft` once |
| `update` | `campaign_drafts[].campaign_draft` | `customers/ID/campaignDrafts/BASE_CAMPAIGN_ID~DRAFT_ID` of `customer_id` |
| `remove` | `campaign_drafts` | 1 to 10 distinct draft resource names of `customer_id` |
| `promote`, `list-async-errors` | `campaign_draft` | required, resource name of a draft of `customer_id` |
| `create`, `update`, `remove`, `promote` | `validate_only` | true validates only |
| `list-async-errors` | `page_size` | 1 to 1000; Google may return fewer |
| `list-async-errors` | `cursor` | the `next_cursor` of a previous page of the same call on the same connection |

The tools offer no `status` argument. In v26, `CampaignDraft.status` is output only. `REMOVED` is what `remove`
does and `PROMOTING`, `PROMOTED`, and `PROMOTE_FAILED` are what `promote` causes, so no update can bypass the
`tools` list that those two need. An `update` mask names `name` alone: `base_campaign` is immutable and the other
fields are output only.

The rules come from the reference pages of `CampaignDraftService`, its three methods, `CampaignDraft`,
`CampaignDraftOperation`, and `CampaignDraftStatus` for v26. They differ from the research behind the tools in
these points:

- **Status.** Research expected a writable status. The reference marks it output only, so none is offered.
- **Name.** The reference requires `name` on creation, not empty, without NUL, line feed, and carriage return. It
  states no length. The 255 characters are Qatlas's own, and the tools refuse every control character, which is
  stricter than the reference.
- **Operation bound.** The reference states no bound for `operations[]`. The 10 are Qatlas's own.
- **Reference pages.** The pages of the methods list the errors and the scope (`adwords`), but no request fields or
  paths on the page itself; the request shape comes from `campaign_draft_service.proto` of v26.
- **Order of calls.** Google refuses `ListCampaignDraftAsyncErrors` before the draft is promoted.

## Errors of the promotion

`campaign-drafts.list-async-errors` sends one `GET customers/ID/campaignDrafts/BASE~DRAFT:listAsyncErrors` and
returns the errors Google recorded for the promotion of the draft, one page per call.

- **Result.** `errors` holds `code` (the gRPC status code as a number) and `message` per status. `next_cursor` is
  present while more pages follow.
- **Untrusted data.** Messages are provider text: control characters become spaces and each message is cut to 1024
  bytes. The `details` of a status are free-form and are not returned.
- **Paging.** The cursor wraps the page token of Google, bound to the tool, the account, the draft, and the
  connection. A cursor of another draft or connection, a forged one, and one longer than the bound is refused as
  `invalid-request` before any request. A page token or a page of Google beyond the bounds (1000 bytes, 1000
  errors) is `invalid-provider-response`.
- **Quota.** One operation of the daily quota, plus the same checks of a manager target as for a change.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts the camelCase body and mask Qatlas sends, and the shape of the operation of a promotion in
  `validate_only` mode. No live call has been made.
- Which states of a draft allow a rename, a removal, or a promotion. Google validates that.

Left out on purpose:

- Any write of `status`, `partial_failure`, and any free operation, mask, or resource name.
- Reading or polling the operation of a promotion.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the draft family.

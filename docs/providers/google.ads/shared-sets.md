---
description: >
  Describes the shared set tools of the Google Ads provider: shared-sets.create, shared-sets.update, shared-sets.remove, campaign-shared-sets.create, and campaign-shared-sets.remove, with the Mutate services and their operations lists, the bound of 50 operations, the checks on name, type, and vertical against the reference of v26, the links of campaigns to shared sets, validate_only, confirmation, how create and update are reached by effect while both removals need the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, the quota per operation, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: shared set tools

These tools create, rename, and remove the shared sets of a Google Ads account and attach shared sets to
campaigns or detach them. A shared set is a list that several campaigns use together, such as negative keywords or
negative placements. They add to the read tools of the [Google Ads provider](README.md), whose configuration,
targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.shared-sets.create` | create | creates one or more shared sets |
| `google.ads.shared-sets.update` | update | renames existing shared sets |
| `google.ads.shared-sets.remove` | delete | removes existing shared sets |
| `google.ads.campaign-shared-sets.create` | create | attaches shared sets to campaigns |
| `google.ads.campaign-shared-sets.remove` | delete | detaches shared sets from campaigns |

Reading shared sets and their links is not a tool of this page: run `reports.search` on `shared_set` and
`campaign_shared_set`. The criteria inside a shared set, such as its keywords or placements, are not offered.

## How the tools are offered

- **`shared-sets.create`, `shared-sets.update`, and `campaign-shared-sets.create`** are offered by a connection
  whose effects include `create` or `update` (`update` for `shared-sets.update`, `create` for the other two), and
  by a connection whose `tools` list names them. They belong to no profile: the `read` profile holds none of them.
  A connection with a `tools` list gets exactly the tools the list names, whatever its effects are.
- **`shared-sets.remove` and `campaign-shared-sets.remove`** are offered only through the `tools` list. A
  connection with the `delete` effect and no `tools` list does not have them.
- **Confirmed per call.** All five need the confirmation of that one call. The confirmation text names the account
  and the objects, with texts cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `SharedSetService.MutateSharedSets` or
  `CampaignSharedSetService.MutateCampaignSharedSets` with an `operations` list and without `partial_failure`:
  Google applies all operations or none. A list holds 1 to 50 entries. The 50 are Qatlas's own bound; the pages of
  both methods state no limit.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so the
  answer then holds `validate_only: true` and no `results`. Validation needs the same confirmation and the same
  checks, and counts the same operations as the change.
- **`response_content_type`** (`shared-sets.create`, `shared-sets.update`, `campaign-shared-sets.create`).
  `MUTABLE_RESOURCE` adds the object after the change to each result: for a shared set `id`, `name`, `status`,
  `type`, `vertical_ads_item_vertical_type`, `member_count`, `reference_count`; for a link `campaign`,
  `shared_set`, `status`. Without it a result holds the IDs and `resource_name` only. The removals have no such
  argument.
- **Names come from checked IDs.** The resource names `customers/ID/sharedSets/SET_ID`,
  `customers/ID/campaigns/CAMPAIGN_ID`, and `customers/ID/campaignSharedSets/CAMPAIGN_ID~SET_ID` are built from
  `customer_id`, `shared_set_id`, and `campaign_id`, each a positive number. A caller never passes a resource name,
  a field mask, or an operation, and a campaign or shared set of another account cannot be named.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or not
  readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the objects
  with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole request
  and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must belong to the `customer_id` of the
  call. For `update`, `remove`, and the link tools the results must be exactly the objects asked for, in the order
  asked for; a `shared-sets.create` must answer one distinct shared set per operation. A returned object must have
  the resource name of its result, a returned `id` must match it, and a returned link must name the campaign and
  the shared set asked for. Anything else is `invalid-provider-response` with the note about the unknown outcome,
  since the change may have been made. A `partial_failure_error` in an answer is treated the same way, as the request
  asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and code in
  the message, such as `sharedSetError DUPLICATE_NAME` or `campaignSharedSetError`, and the field path in the
  original. The other codes of the README apply.
- **Quota.** Google counts each operation of a mutate list as one operation of the daily quota, also in a request
  it refuses. A call with 10 entries costs 10, plus one for the membership read below a manager target and one for
  the check that the account is no manager.
- **Manager accounts.** A manager account has shared sets of its own. On a connection with targets, such an account
  needs a manager target that names it (`managers/ID`): a customer target that names it, and a manager that is only
  reached below a manager target, are refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `shared-sets.create` | `shared_sets` | 1 to 50 entries |
| `shared-sets.create` | `shared_sets[].name` | required, 1 to 255 UTF-8 bytes, no control characters |
| `shared-sets.create` | `shared_sets[].type` | required, one of `NEGATIVE_KEYWORDS`, `NEGATIVE_PLACEMENTS`, `BRANDS`, `RETAIL_FILTER`, `VERTICAL_ADS_ITEM_GROUP_RULE_LIST`, `ACCOUNT_LEVEL_NEGATIVE_KEYWORDS`; cannot be changed afterwards |
| `shared-sets.create` | `shared_sets[].vertical_ads_item_vertical_type` | `HOTELS`, `FLIGHTS`, `EVENTS`, `RENTAL_CARS`, `THINGS_TO_DO`, or `VACATION_RENTALS`; required for `VERTICAL_ADS_ITEM_GROUP_RULE_LIST`, refused for any other type |
| `shared-sets.update` | `shared_sets` | 1 to 50 entries, each `shared_set_id` once |
| `shared-sets.update` | `shared_sets[].shared_set_id` | positive number, the `shared_set.id` that `reports.search` returns |
| `shared-sets.update` | `shared_sets[].name` | required, 1 to 255 bytes, no control characters |
| `shared-sets.remove` | `shared_set_ids` | 1 to 50 distinct shared set IDs |
| `campaign-shared-sets.create`, `.remove` | `links` | 1 to 50 pairs, each pair once |
| `campaign-shared-sets.create`, `.remove` | `links[].campaign_id`, `links[].shared_set_id` | positive numbers |
| all | `validate_only` | true validates only |
| `shared-sets.create`, `.update`, `campaign-shared-sets.create` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

`shared-sets.update` sends the mask `name` and nothing else: the reference marks `type` and
`vertical_ads_item_vertical_type` immutable and `status`, `id`, `member_count`, and `reference_count` output only,
so no tool takes them. Removing a shared set or a link is the way to end it; a removed object shows `REMOVED`. A
link has no field a request can set besides the campaign and the shared set, which are immutable.

The limits are those of the reference pages of `SharedSet`, `SharedSetType`, `VerticalAdsItemVerticalType`,
`CampaignSharedSet`, `MutateSharedSets`, and `MutateCampaignSharedSets` for v26, checked on 2026-11-22. The name limit
is in bytes as the reference says, so a name of 128 two-byte characters is refused. Google documents that a name must
be unique among the active shared sets of the same type; whether a name is taken is Google's to validate, use
`validate_only` to see it. Google also documents that only negative keyword shared sets can be used with Shopping
campaigns and only negative placement shared sets with Display mobile app campaigns; Qatlas does not check that and
leaves it to Google.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google documents no role per call.
- Whether Google lets an account create a shared set of each listed type, in particular
  `ACCOUNT_LEVEL_NEGATIVE_KEYWORDS`, which the reference describes as account-wide. No live call has been made.
- What Google answers when a shared set that campaigns still use is removed. `reference_count` shows the number of
  campaigns that use it.
- Whether the answer to a removal of a link or a shared set can hold more than the resource name. The pages read
  say it does not.

Left out on purpose:

- The criteria of a shared set (`SharedCriterion`) and reading shared sets and links.
- The type `WEBPAGES`, which the reference says is not publicly available, and the values `UNSPECIFIED` and
  `UNKNOWN`, which are no values to send.
- `partial_failure` and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate services of the two resource families.

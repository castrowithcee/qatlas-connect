---
description: >
  Describes the campaign group tools of the Google Ads provider: campaign-groups.create, campaign-groups.update, and campaign-groups.remove, with the Mutate service and its operations list, the bound of 50 operations, the checks on name and status against the reference of v26, validate_only, confirmation, how create and update are reached by effect while remove needs the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-23
---

# Google Ads: campaign group tools

These tools create, rename, and remove the campaign groups of a Google Ads account. A campaign group collects
campaigns, for example for shared reporting. They add to the read tools of the [Google Ads provider](README.md),
whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-groups.create` | create | creates one or more campaign groups |
| `google.ads.campaign-groups.update` | update | renames existing campaign groups or sets them ENABLED |
| `google.ads.campaign-groups.remove` | delete | removes existing campaign groups |

Reading campaign groups is not a tool of this page: run `reports.search` on the `campaign_group` resource.
Assigning a campaign to a group is a change of the campaign and is not offered here.

## How the tools are offered

- **`campaign-groups.create` and `campaign-groups.update`** are offered by a connection whose effects include
  `create` or `update`, and by a connection whose `tools` list names them. They belong to no profile: the `read`
  profile holds none of them. A connection with a `tools` list gets exactly the tools the list names, whatever its
  effects are.
- **`campaign-groups.remove`** is offered only through the `tools` list. A connection with the `delete` effect
  and no `tools` list does not have it.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account and the groups, with the names cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignGroupService.MutateCampaignGroups` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50
  entries. The 50 are Qatlas's own bound; Google accepts up to 10,000 mutate operations per request.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and an empty `results`. Validation needs the same
  confirmation and the same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the campaign group after the
  change to each result: `id`, `name`, `status`. Without it a result holds `campaign_group_id` and
  `resource_name` only. `remove` has no such argument.
- **Names come from checked IDs.** `update` and `remove` build `customers/ID/campaignGroups/GROUP_ID` from
  `customer_id` and `campaign_group_id`. A caller never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the groups with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/campaignGroups/N` of the `customer_id` of the call. For `update` and `remove` the results must be
  exactly the groups asked for, in the order asked for; a `create` must answer one distinct group per operation.
  A returned group must have the resource name of its result and, when it has an `id`, the same number. Anything
  else is `invalid-provider-response` with the note about the unknown outcome, since the change may have been
  made. A `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message, and the field path in the original. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target,
  are refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `campaign_groups` | 1 to 50 entries |
| `create` | `campaign_groups[].name` | required, 1 to 255 characters, no null, line feed, or carriage return |
| `create`, `update` | `campaign_groups[].status` | `ENABLED` only |
| `update` | `campaign_groups` | 1 to 50 entries, each `campaign_group_id` once |
| `update` | `campaign_groups[].campaign_group_id` | positive number, the `campaign_group.id` that `reports.search` returns |
| `update` | `campaign_groups[].name` | 1 to 255 characters, same characters as `create` |
| `remove` | `campaign_group_ids` | 1 to 50 distinct campaign group IDs |
| `create`, `update`, `remove` | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

An `update` entry needs at least one of name and status, and the mask Qatlas sends names exactly those fields.

The rules come from the reference pages of `CampaignGroup`, `CampaignGroupStatus`, and `CampaignGroupService` for
v26 and from the quotas page of Google for the 10,000 operations. They differ from the research behind the
tools in these points:

- **Status.** Google lists `ENABLED` and `REMOVED` as the settable statuses. A tool takes `ENABLED` only, which is
  the default of a new group. `REMOVED` is what the remove operation does, so an update with it would bypass the
  `tools` list that `remove` needs.
- **Name length.** The reference states no length, only that the name is required and not empty on create. The
  255 characters are Qatlas's own bound and can be wider than what Google accepts. Whether Google counts
  characters or bytes is not stated; Qatlas counts characters (code points).

Which names Google accepts beyond these rules, for example a name that is already taken or one of only blanks,
is Google's to validate. Use `validate_only` to see it.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- The limit on the number of campaign groups of an account, and whether a name may repeat, which the pages read
  do not state.
- What happens to the campaigns of a group when the group is removed. The pages read do not say.
- Whether Google accepts the mask in camelCase and in the order Qatlas sends. No live call has been made.

Left out on purpose:

- Assigning a campaign to a group, and reading groups.
- `status` `REMOVED` on update, `partial_failure`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the campaign group family.

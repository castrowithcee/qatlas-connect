---
description: >
  Describes the label assignment tools of the Google Ads provider: campaign-labels.create and remove, which attach a label to a campaign or detach it, and ad-group-labels.create and remove for an ad group, with the Mutate services and their operations lists, the bound of 10 entries, the composite resource names and how they are built and checked, the absence of an update and of response_content_type, validate_only, confirmation, how both create and remove are reached by effect without the tools list, the unknown outcome, the binding of the label, the object and the resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: label assignment tools

These tools put labels, which the [label tools](labels.md) create, onto campaigns and ad groups. A label assignment
is a link resource of v26 that ties two objects of one account: `CampaignLabel` ties a label to a campaign and
`AdGroupLabel` ties a label to an ad group. A link has no member that can be changed, so each family has a create and a
remove tool and no update tool. They add to the read tools of the [Google Ads provider](README.md), whose
configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-labels.create` | create | attaches one to 10 labels to campaigns |
| `google.ads.campaign-labels.remove` | delete | detaches labels from campaigns again |
| `google.ads.ad-group-labels.create` | create | attaches one to 10 labels to ad groups |
| `google.ads.ad-group-labels.remove` | delete | detaches labels from ad groups again |

Reading the assignments is not a tool of this page: run `reports.search` on `campaign_label` or `ad_group_label`.
Labels on ads, on ad group criteria, and on the account are not offered.

## How the tools are offered

- **All four tools** are offered by a connection whose effects include the effect of the tool (`create` for the
  create tools, `delete` for the remove tools), and by a connection whose `tools` list names them. They belong to
  no profile: the `read` profile holds none of them. A connection with a `tools` list gets exactly the tools the
  list names, whatever its effects are. Unlike `labels.remove`, the remove tools of this page need no `tools`
  list: they end a link only and delete no label, campaign, or ad group.
- **Confirmed per call.** Every call needs the confirmation of that one call. The confirmation text names the
  account and, per entry, the parts of the link or its resource name, with texts cut and without control
  characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignLabelService.MutateCampaignLabels` or
  `AdGroupLabelService.MutateAdGroupLabels` with an `operations` list and without `partial_failure`: Google applies
  all operations or none. A list holds 1 to 10 entries. The 10 are Qatlas's own bound; the reference of v26 names
  none.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation and the same checks and costs the same operations as the change.
- **No `response_content_type`.** The request of both services has no such member, so no tool offers the argument.
  A result holds the `resource_name` only.
- **Names come from checked arguments.** A create entry names the object and the label, each as
  `customers/ID/<collection>/N` of the `customer_id` of the call. A remove entry is the link's resource name,
  `customers/ID/campaignLabels/CAMPAIGN_ID~LABEL_ID` or `customers/ID/adGroupLabels/AD_GROUP_ID~LABEL_ID`, which
  `reports.search` returns as `resource_name`. A foreign account, another collection, a zero ID, a leading zero, a
  temporary negative ID, and a key of the wrong number of IDs are refused. A caller never passes a field mask or an
  operation. The same link twice in one call is refused.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the links with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be the link collection of the
  `customer_id` with the key of two IDs. The results must be exactly the links asked for, in the order asked for:
  for a create call the resource name follows from the two IDs of each entry. Anything else is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made. A
  `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused, for example with a `LabelError`, ends as
  `provider-error` with the error category and code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `campaign-labels.create` | `campaign_labels` | 1 to 10 entries, each an object of `campaign` (`customers/ID/campaigns/N`) and `label` (`customers/ID/labels/N`), both required, no other member, each pair once |
| `ad-group-labels.create` | `ad_group_labels` | 1 to 10 entries, each an object of `ad_group` (`customers/ID/adGroups/N`) and `label` (`customers/ID/labels/N`), both required, no other member, each pair once |
| `campaign-labels.remove` | `resource_names` | 1 to 10 distinct `customers/ID/campaignLabels/N~M` of the account |
| `ad-group-labels.remove` | `resource_names` | 1 to 10 distinct `customers/ID/adGroupLabels/N~M` of the account |
| all | `validate_only` | true validates only |

Whether a label exists, and whether a link may exist twice, is Google's to validate. Use `validate_only` to see it.

### Qatlas bounds

The reference names no bound for these requests. The 10 entries per call are Qatlas's own and refuse nothing
Google documents.

## Answer

A result holds `resource_name` only: the reference of v26 lists no other member for `MutateCampaignLabelResult` and
`MutateAdGroupLabelResult`, and the links have no `status`. A removed link is answered by its resource name too.

## Boundary

Left out on purpose:

- The update of a link. The services of v26 offer create and remove only, and every member of a link is immutable.
- Labels on ads, on ad group criteria, and on the account.
- Reading the links, and everything of the labels themselves: see the [label tools](labels.md).
- `partial_failure`, any free operation, mask, or resource name, and `GoogleAdsService.Mutate`.

Checked against the Google Ads API reference of v26: the pages `CampaignLabel`, `CampaignLabelService`,
`CampaignLabelService/MutateCampaignLabels`, `MutateCampaignLabelsRequest`, `CampaignLabelOperation`,
`MutateCampaignLabelResult`, `AdGroupLabel`, `AdGroupLabelService`, `AdGroupLabelService/MutateAdGroupLabels`,
`MutateAdGroupLabelsRequest`, `AdGroupLabelOperation`, and `MutateAdGroupLabelResult`. The requests hold
`customer_id`, `operations`, `partial_failure`, and `validate_only` and no `response_content_type`, the operations
hold `create` and `remove` only, and the pages name no limit on the number of operations.

Not verified:

- Which role in Google Ads an account needs for these calls. Google documents it per call nowhere.
- Limits on the number of labels per campaign or ad group. The pages read do not state them. No live call has been made.
- Whether removing a link that does not exist is answered as success or as an error.

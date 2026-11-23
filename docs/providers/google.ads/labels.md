---
description: >
  Describes the label tools of the Google Ads provider that change the labels of an account: labels.create, labels.update, and labels.remove, with the Mutate service and its operations list, the bound of 50 operations, the checks on name, description, and color against the reference of v26, validate_only, confirmation, how create and update are reached by effect while remove needs the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-23
---

# Google Ads: label tools

These tools create, change, and remove the labels of a Google Ads account. A label is a name with an optional
description and color that organizes campaigns, ad groups, ads, and keywords. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.labels.create` | create | creates one or more labels |
| `google.ads.labels.update` | update | changes name, description, or color of existing labels |
| `google.ads.labels.remove` | delete | removes existing labels |

Reading labels is not a tool of this page: run `reports.search` on the label resource. Attaching a label to a
campaign, an ad group, an ad, a keyword, or an account is not offered either.

## How the tools are offered

- **`labels.create` and `labels.update`** are offered by a connection whose effects include `create` or
  `update`, and by a connection whose `tools` list names them. They belong to no profile: the `read` profile holds
  none of them, so a connection that only has that profile cannot call them. A connection with a `tools` list
  gets exactly the tools the list names, whatever its effects are.
- **`labels.remove`** is offered only through the `tools` list. A connection with the `delete` effect and no
  `tools` list does not have it.
- **Confirmed per call.** All three need the confirmation of that one call. The confirmation text names the
  account and the labels, with the texts cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `LabelService.MutateLabels` with an `operations` list and
  without `partial_failure`: Google applies all operations or none. A list holds 1 to 50 entries. The 50 are
  Qatlas's own bound; Google accepts up to 10,000 mutate operations per request.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and an empty `results`. Validation needs the same
  confirmation, the same checks, and costs the same operations as the change.
- **`response_content_type`** (`create` and `update`). `MUTABLE_RESOURCE` adds the label after the change to each
  result: `id`, `name`, `status`, `description`, `background_color`. Without it a result holds `label_id` and
  `resource_name` only. `remove` has no such argument; the result of a removal is the resource name.
- **Names come from checked IDs.** `update` and `remove` build `customers/ID/labels/LABEL_ID` from `customer_id`
  and `label_id`. A caller never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the labels with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be `customers/ID/labels/N` of
  the `customer_id` of the call. For `update` and `remove` the results must be exactly the labels asked for, in
  the order asked for; a `create` must answer one distinct label per operation. A returned label must have the
  resource name of its result and, when it has an `id`, the same number. Anything else is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made. A
  `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and code
  in the message, such as `labelError DUPLICATE_NAME`, and the field path in the original. The other codes of the
  README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call
  may add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** A manager account has labels of its own. On a connection with targets, such an account
  needs a manager target that names it (`managers/ID`): a customer target that names it, and a manager that is
  only reached below a manager target, are refused as `invalid-request`. Google documents that the background
  color may not be visible for manager accounts.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `labels` | 1 to 50 entries |
| `create` | `labels[].name` | required, 1 to 80 characters |
| `create`, `update` | `labels[].description` | at most 200 characters; for `update`, an empty text clears it |
| `create`, `update` | `labels[].background_color` | hex color, `#RRGGBB` or `#RGB`, matching `^#([a-fA-F0-9]{6}\|[a-fA-F0-9]{3})$` |
| `update` | `labels` | 1 to 50 entries, each `label_id` once |
| `update` | `labels[].label_id` | positive number, the `label.id` that `reports.search` returns |
| `update` | `labels[].name` | 1 to 80 characters |
| `remove` | `label_ids` | 1 to 50 distinct label IDs |
| `create`, `update`, `remove` | `validate_only` | true validates only |
| `create`, `update` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

An `update` entry needs at least one of name, description, and color, and the mask Qatlas sends names exactly
those: `name`, `textLabel.description`, `textLabel.backgroundColor`. Text must not hold control characters. The
status of a label is output only in Google's reference, so no tool takes it; a removed label shows `REMOVED`.
The color cannot be cleared, since the pattern requires a value.

The limits are those of the reference pages of `Label`, `TextLabel`, and `MutateLabels` for v26, checked on
2026-11-22. The 10,000 operations come from the quotas page of Google. Whether the length is counted in
characters or bytes is not stated; Qatlas counts characters (code points), which cannot refuse a text Google
accepts.

Which names Google accepts beyond these limits, for example a name that is already taken or one of only blanks,
is Google's to validate. Use `validate_only` to see it.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts the mask in camelCase and in the order Qatlas sends, whether it accepts an update that
  sets an empty description, and what color a label without one gets. No live call has been made.
- The limit on the number of labels of an account, which the pages read do not state.
- What happens to the labels attached to campaigns, ad groups, ads, or keywords when a label is removed. The
  pages read do not say.

Left out on purpose:

- The label associations (`CampaignLabel`, `AdGroupLabel`, `AdGroupAdLabel`, `AdGroupCriterionLabel`,
  `CustomerLabel`) and reading labels.
- `status` of a label, `partial_failure`, and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate service of the label family.

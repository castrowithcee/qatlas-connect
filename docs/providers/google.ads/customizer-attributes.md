---
description: >
  Describes the customizer attribute tools of the Google Ads provider: customizer-attributes.create and customizer-attributes.remove, with the Mutate service and its operations list, the bound of 50 operations, the checks on name and type against the v26 definitions, the absence of an update, response_content_type on create only, validate_only, confirmation, how create is reached by effect while remove needs the tools list, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: customizer attribute tools

These tools create and remove the customizer attributes of a Google Ads account. A customizer attribute defines a
name and a type, such as a text or a price, for values that customizers attach to campaigns, ad groups, keywords,
or the account and that ads insert with a customizer tag. The tools of this page define the attributes only: they set
no value. They add to the read tools of the [Google Ads provider](README.md), whose configuration, targets, quotas,
and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customizer-attributes.create` | create | creates one to 50 customizer attributes |
| `google.ads.customizer-attributes.remove` | delete | removes existing customizer attributes |

Reading attributes is not a tool of this page: run `reports.search` on the `customizer_attribute` resource. There is
no update tool: the service of v26 offers create and remove only, and name and type are immutable.

## How the tools are offered

- **`customizer-attributes.create`** is offered by a connection whose effects include `create`, and by a connection
  whose `tools` list names it. It belongs to no profile: the `read` profile holds neither tool. A connection with a
  `tools` list gets exactly the tools the list names, whatever its effects are.
- **`customizer-attributes.remove`** is offered only through the `tools` list. A connection with the `delete`
  effect and no `tools` list does not have it. A removed attribute is REMOVED for good, no tool of this provider
  brings it back, and the values and ads that use it are affected, so the removal is as guarded as that of a label.
- **Confirmed per call.** Both need the confirmation of that one call. The confirmation text names the account and,
  per entry, the name and type or the ID, with texts cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CustomizerAttributeService.MutateCustomizerAttributes` with an
  `operations` list and without `partial_failure`: Google applies all operations or none. A list holds 1 to 50
  entries. The 50 are Qatlas's own bound; the definition of v26 names none.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers errors
  only, so the answer then holds `validate_only: true` and an empty `results`. Validation needs the same
  confirmation and the same checks and costs the same operations as the change.
- **`response_content_type`** (`create`). `MUTABLE_RESOURCE` adds the attribute after the change to each result:
  `id`, `name`, `type`, `status`. Without it a result holds `customizer_attribute_id` and `resource_name` only.
  `remove` has no such argument.
- **Names come from checked IDs.** `remove` builds `customers/ID/customizerAttributes/ATTRIBUTE_ID` from
  `customer_id` and `customizer_attribute_id`. A caller never passes a resource name, a field mask, or an operation.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or not
  readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the
  attributes with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole
  request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be
  `customers/ID/customizerAttributes/N` of the `customer_id` of the call. For `remove` the results must be exactly
  the attributes asked for, in the order asked for, and a `create` must answer one distinct attribute per operation.
  A returned attribute must have the resource name of its result and, when it has an `id`, the same number. Anything
  else is `invalid-provider-response` with the note about the unknown outcome, since the change may have been made.
  A `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused, for example a duplicate name, ends as `provider-error`
  with the error category and code in the message. The other codes of the README apply.
- **Quota.** Each entry of the list counts as one operation of the daily quota, in a refused request too. A call may
  add one operation for the membership read below a manager target and one for the check that the account is no
  manager.
- **Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
  (`managers/ID`): a customer target that names it, and a manager that is only reached below a manager target, are
  refused as `invalid-request`.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `create` | `customizer_attributes` | 1 to 50 entries, each an object of `name` and `type`, both required, no other member |
| `create` | `customizer_attributes[].name` | 1 to 40 characters, no control characters. Among the enabled attributes of an account a name is unique without regard to case, which Google checks |
| `create` | `customizer_attributes[].type` | `TEXT`, `NUMBER`, `PRICE`, or `PERCENT` |
| `create` | `response_content_type` | `MUTABLE_RESOURCE` or `RESOURCE_NAME_ONLY` |
| `remove` | `customizer_attribute_ids` | 1 to 50 distinct IDs of existing attributes, positive numbers as `reports.search` returns them |
| all | `validate_only` | true validates only |

A new attribute is ENABLED. `status` is output only and no argument. A removed attribute is REMOVED.

### Qatlas bounds

The 50 entries per call are Qatlas's own; Google documents no limit for this request. The 40 characters of the name
are counted as characters, not bytes. `type` is required by the tool although the definition does not mark it
required: an attribute created without a type is of no use, and `UNSPECIFIED` and `UNKNOWN` are not accepted.

## Boundary

Left out on purpose:

- The update of an attribute: name and type are immutable and the service has no update operation.
- Setting values: the customizers of campaigns, ad groups, ad group criteria, and the account are separate tools.
- Reading attributes.
- `status` as an argument, `partial_failure`, any free operation, mask, or resource name, and
  `GoogleAdsService.Mutate`.

Checked against the v26 definitions of `CustomizerAttribute`, `CustomizerAttributeService`,
`MutateCustomizerAttributesRequest`, `CustomizerAttributeOperation`, `MutateCustomizerAttributeResult`,
`CustomizerAttributeTypeEnum`, and `CustomizerAttributeStatusEnum`: the request holds `customer_id`, `operations`,
`partial_failure`, `validate_only`, and `response_content_type`, the operation holds `create` and `remove` and an
`update_mask` that no tool uses, the name is required, immutable, and 1 to 40 characters, the type is immutable with
the four values above, and the status is output only with `ENABLED` and `REMOVED`.

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google counts the 40 characters of a name as characters, and which characters it refuses besides control
  characters. A refused name ends as `provider-error`.
- The limit on the number of attributes of an account, and what happens to the values and ads that use an attribute
  when it is removed. The pages read do not state them. No live call has been made.

---
description: >
  Describes the customizer value tools of the Google Ads provider: campaign-customizers.create and remove, which set the value of a customizer attribute for a campaign or end it, and ad-group-customizers.create and remove for an ad group, with the Mutate services and their operations list, the bound of 10 entries, the value of type and string_value and what the tool checks of it, the absence of an update, response_content_type on create only, validate_only, confirmation, how both tools are reached by effect, the unknown outcome, the binding of resource names to the account, the manager account rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: customizer value tools

These tools set the value of a [customizer attribute](customizer-attributes.md) for a campaign or an ad group and
end that value again. A customizer value is a link resource of v26 that ties an attribute to an object of one account
and carries a value that ads insert with a customizer tag: `CampaignCustomizer` for a campaign and
`AdGroupCustomizer` for an ad group. The services offer create and remove only, so each family has a create and a
remove tool and no update tool. They add to the read tools of the [Google Ads provider](README.md), whose
configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.campaign-customizers.create` | create | sets one to 10 customizer values for campaigns |
| `google.ads.campaign-customizers.remove` | delete | ends customizer values of campaigns |
| `google.ads.ad-group-customizers.create` | create | sets one to 10 customizer values for ad groups |
| `google.ads.ad-group-customizers.remove` | delete | ends customizer values of ad groups |

Reading the values is not a tool of this page: run `reports.search` on `campaign_customizer` or
`ad_group_customizer`. The customizers of other levels are not part of these tools.

## How the tools are offered

- **All tools** are offered by a connection whose effects include the effect of the tool (`create` for the
  create tools, `delete` for the remove tools), and by a connection whose `tools` list names them. They belong to
  no profile: the `read` profile holds none of them. A connection with a `tools` list gets exactly the tools the
  list names, whatever its effects are. The remove tools need no `tools` list: they end a value only and delete
  neither the attribute nor the object.
- **Confirmed per call.** Every call needs the confirmation of that one call. The confirmation text names the
  account and, per entry, the object, the attribute, and the type and a cut of the value, or the resource name,
  with texts cut and without control characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CampaignCustomizerService.MutateCampaignCustomizers` or
  `AdGroupCustomizerService.MutateAdGroupCustomizers` with an `operations` list and without `partial_failure`:
  Google applies all operations or none. A list holds 1 to 10 entries. The 10 are Qatlas's own bound; the
  reference of v26 names none.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only, so the answer then holds `validate_only: true` and no `results`. Validation needs the same
  confirmation and the same checks and costs the same operations as the change.
- **`response_content_type`** (`create`). `MUTABLE_RESOURCE` adds the value after the change to each result
  under `campaign_customizer` or `ad_group_customizer`: the object, `customizer_attribute`, `value` (`type` and
  `string_value`), `resource_name`, and `status`. Without it a result holds the `resource_name` only. The `remove`
  tools have no such argument.
- **Names come from checked arguments.** A create entry names the object and the attribute, each as
  `customers/ID/<collection>/N` of the `customer_id` of the call, and the value. A remove entry is the resource
  name of the value, `customers/ID/campaignCustomizers/CAMPAIGN_ID~ATTRIBUTE_ID` or
  `customers/ID/adGroupCustomizers/AD_GROUP_ID~ATTRIBUTE_ID`, which `reports.search` returns as `resource_name`.
  A foreign account, another collection, a zero ID, a leading zero, a temporary negative ID, and a key of the wrong
  number of IDs are refused. A caller never passes a field mask or an operation. The same value twice in one call
  is refused.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON
  or not readable leaves open whether Google took the change, the error says that the outcome is unknown. Read
  the values with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the
  whole request and say so.
- **Resource names stay with the account.** Every `resource_name` of a result must be the collection of the
  `customer_id` with the key of its two IDs. The results must be exactly the values asked for, in the order asked
  for: for a create call the resource name follows from the IDs of each entry. A returned value must have the
  resource name of its result, and the object and attribute it names must be the ones of that key. Anything else
  is `invalid-provider-response` with the note about the unknown outcome, since the change may have been made. A
  `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused, for example with a `CustomizerError`, ends as
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
| `campaign-customizers.create` | `campaign_customizers` | 1 to 10 entries, each an object of `campaign` (`customers/ID/campaigns/N`), `customizer_attribute` (`customers/ID/customizerAttributes/N`), and `value`, all required, no other member, each campaign and attribute once |
| `ad-group-customizers.create` | `ad_group_customizers` | 1 to 10 entries, each an object of `ad_group` (`customers/ID/adGroups/N`), `customizer_attribute`, and `value`, all required, no other member, each ad group and attribute once |
| `create` | `value` | an object of `type` and `string_value`, both required, no other member |
| `create` | `value.type` | `TEXT`, `NUMBER`, `PRICE`, or `PERCENT` |
| `create` | `value.string_value` | 1 to 1024 characters, no control characters |
| `create` | `response_content_type` | `MUTABLE_RESOURCE` or `RESOURCE_NAME_ONLY` |
| `campaign-customizers.remove` | `resource_names` | 1 to 10 distinct `customers/ID/campaignCustomizers/N~M` of the account |
| `ad-group-customizers.remove` | `resource_names` | 1 to 10 distinct `customers/ID/adGroupCustomizers/N~M` of the account |
| all | `validate_only` | true validates only |

A new value is ENABLED. `status` is output only and no argument. An ended value is REMOVED.

### Qatlas bounds

The 10 entries per call and the 1024 characters of `string_value` are Qatlas's own: the definitions of v26 name no
limit for the requests or for the string, and say only that the content of `string_value` must match the constraints
of the type. The tool refuses a `type` outside the four of a customizer attribute and an empty value or one with
control characters. It does not read the attribute, so it cannot tell whether `type` matches the type of the
attribute and whether `string_value` has the form that type requires: Google checks both, and `validate_only` shows
the result.

## Boundary

Left out on purpose:

- The update of a value. The services of v26 offer create and remove only. Whether a create for an attribute that
  has a value replaces it or is refused, the pages read do not state; to change a value, end it with `remove` and
  set it again.
- Reading the values, and everything of the attributes themselves: see the
  [customizer attribute tools](customizer-attributes.md).
- The customizers of ad group criteria and of the account.
- `status` as an argument, `partial_failure`, any free operation, mask, or resource name, and
  `GoogleAdsService.Mutate`.

Checked against the v26 definitions of `CampaignCustomizer`, `AdGroupCustomizer`, `CampaignCustomizerService`,
`AdGroupCustomizerService`, their `Mutate...Request`, `...Operation`, and `Mutate...Result` messages,
`CustomizerValue`, and `CustomizerValueStatusEnum`: the requests hold `customer_id`, `operations`,
`partial_failure`, `validate_only`, and `response_content_type`, the operations hold `create` and `remove` only, the
`customizer_attribute` and the `value` are required, the object and the attribute are immutable, the status is
output only with `ENABLED` and `REMOVED`, and `CustomizerValue` holds a required `type` that must match the
attribute and a required `string_value`. The pages name no limit on the number of operations. The limit of 10,000
operations per request that is known for the Mutate services is not on these pages.

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- The form `string_value` must have for each type, its maximum length, and what happens when a value exists
  already. The pages read do not state them. No live call has been made.

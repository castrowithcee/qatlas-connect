---
description: >
  Describes the settings tools of the Google Ads provider that change an account: customers.update for name, tracking template, final URL suffix, auto-tagging and call reporting, and customer-budget-network-settings.create and update for the default distribution of budgets over network channels, with the mutation path, confirmation, tools list requirement, validate_only, the atomic single request, the unknown outcome, the checks on addresses and resource names, the manager account rule, the field excluded on purpose, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: settings tools

These tools change the settings of a Google Ads account. They add to the read tools of the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customers.update` | update | changes name, tracking template, final URL suffix, auto-tagging, and call reporting of an account |
| `google.ads.customer-budget-network-settings.create` | create | creates the default budget network settings of an account |
| `google.ads.customer-budget-network-settings.update` | update | replaces those settings |

## What every change tool does

- **Offered only through the `tools` list.** No effect profile offers these tools: a connection has them only
  when its `tools` list names each one. The `read` profile holds none of them.
- **Confirmed per call.** Every call needs the confirmation of that one call. The confirmation text names the
  account and the values.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before
  any request, as for the read tools. The `login_customer_id` of the service is never an argument.
- **One atomic request.** A tool sends one request to the Mutate service of its resource family with one
  `operation`, without `partial_failure`: Google applies all of it or nothing. The two services of this page
  take a single operation, so no call carries a list of operations.
- **`validate_only`.** With `true`, Google validates the request completely and changes nothing. It answers
  errors only and no result, so the answer then holds `validate_only: true` and no resource name. Validation
  needs the same confirmation, the same checks, and costs the same operations as the change.
- **`response_content_type`.** `MUTABLE_RESOURCE` adds the mutable fields of the object after the change to
  the answer; without it the answer holds the resource name only.
- **No repetition.** The request is sent once. A refused token is not renewed within the call. After a rate
  limit the next request of the credential waits as described in the README, and the change is not repeated.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), an answer that is not JSON or not
  readable leaves open whether Google took the change, the error says that the outcome of the change is
  unknown. Read the object, with `customers.get` for an account, before changing it again. A 4xx answer and a
  `GoogleAdsFailure` refuse the whole request and say so in the message.
- **Resource names stay with the account.** The resource name of a result, of the returned object, and a
  conversion action in it must belong to the `customer_id` of the call. An answer that names another account is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and
  code in the message, such as `fieldError REQUIRED`, and the field path in the original. The other codes of
  the README apply; `budgetNetworkSettingError.CUSTOMER_NOT_ALLOWLISTED` is `permission`.
- **Quota.** Each change counts as one operation, a refused one too. A call may add one operation for the
  membership read below a manager target and one for the check that the account is no manager.

## customers.update

Calls `CustomerService.MutateCustomer` with an update of `customers/ID` and a field mask that names exactly the
fields that were given. At least one field is required.

| Argument | Field of the customer | Rule |
| --- | --- | --- |
| `descriptive_name` | `descriptive_name` | 1 to 1024 bytes, no control characters |
| `tracking_url_template` | `tracking_url_template` | an `https` address without user info, up to 2048 bytes; empty clears it |
| `final_url_suffix` | `final_url_suffix` | parameters such as `utm_source=x`, up to 2048 bytes, no address; empty clears it |
| `auto_tagging_enabled` | `auto_tagging_enabled` | true or false |
| `call_reporting_enabled` | `call_reporting_setting.call_reporting_enabled` | true or false |
| `call_conversion_reporting_enabled` | `call_reporting_setting.call_conversion_reporting_enabled` | true or false |
| `call_conversion_action` | `call_reporting_setting.call_conversion_action` | a conversion action of this account, `customers/ID/conversionActions/N`; empty clears it |

The 1024 and 2048 byte bounds are Qatlas's own; Google documents none for these fields.

**Addresses.** The tracking template sends the clicks on the ads of the whole account through the address it
names, and the final URL suffix is appended to every final URL: both can pass click data to a third party.
Qatlas therefore checks them before a secret is read. A template must start with `https://`, have a plain host
name (no braces, no `@`), carry no user info, and hold no space, backslash, or control character. Value Track
parameters such as `{lpurl}` and `{campaignid}` are fine after the host. A template that starts with a
parameter, such as `{lpurl}?x=1`, is refused: it names no address to check. A suffix that holds `//` or starts
with `/` is refused. There is no allow-list of addresses per connection.

**Left out on purpose.** `conversion_tracking_setting.google_ads_conversion_customer`, which makes another
account the owner of the conversion tracking of this one, is not an argument, and no mask names it. The answer
with `MUTABLE_RESOURCE` shows its current value as read-only information. The other mutable fields of the
customer resource that no tool of this package names (the Local Services redemption provider, the video brand
safety suitability, and the video verification partners) stay as they are.

**Manager accounts.** On a connection with targets, a manager account needs a manager target that names it
(`managers/ID`): a customer target, and a manager that is only reached below a manager target, are refused as
`invalid-request`. To tell, Qatlas reads `customer.manager` of the account once before the change. A connection
without targets reaches what its credential reaches and makes no such read. An answer to that read that is not
exactly the one row of the account refuses the call.

## customer-budget-network-settings

Calls `CustomerBudgetNetworkSettingService.MutateCustomerBudgetNetworkSetting`. The resource is one per account,
`customers/ID/customerBudgetNetworkSettings`. `create` sends a `create` operation, which Google refuses when the
setting exists. `update` sends an `update` operation with the mask `budgetNetworkSettings`. Both replace the whole
list: an entry or a cap that is left out is gone afterwards.

The settings are the account defaults for every campaign budget whose own `budget_network_settings` is unset.
They decide how the budget is spread over the network channels: which channels spend first, and how much of
the budget a secondary channel may spend at most. This changes where an account spends, so both tools are
offered only through the `tools` list.

| Argument | Rule |
| --- | --- |
| `budget_network_settings` | one to 12 entries, one per `advertising_channel_type` |
| `advertising_channel_type` | `SEARCH`, `DISPLAY`, `SHOPPING`, `HOTEL`, `VIDEO`, `MULTI_CHANNEL`, `LOCAL`, `SMART`, `PERFORMANCE_MAX`, `LOCAL_SERVICES`, `TRAVEL`, `DEMAND_GEN` |
| `budget_caps` | per entry at most one cap per network channel: `network_channel` and `cap_percent_millis` |
| `cap_percent_millis` | 0 to 100000, milli-percent of the budget: 100000 is 100 percent, 0 is a complete opt-out |
| `channel_priorities` | per entry at most one per tier: `priority_tier` `TIER1` (primary) or `TIER2`, and its `network_channels` |
| `network_channels` | `OWNED_AND_OPERATED`, `SEARCH_PARTNER`, `GOOGLE_DISPLAY`; a channel appears in one tier only |

Qatlas checks the structure Google documents: each type once, each channel once among the caps and once across
the tiers, and the range of a cap. Every other rule is Google's to validate, among them that TIER1 holds a
channel, that caps apply to secondary tiers only, that all channels of a type are assigned, and which advertising
channel types the setting supports. Use `validate_only` to see them. The account must be allowlisted by Google
for this setting; otherwise the error is `permission`.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- The behavior of the budget network settings in a running account, the allowlisting, and which advertising
  channel types Google accepts: Google's reference gives `SEARCH`, `SHOPPING`, `PERFORMANCE_MAX`, and `DEMAND_GEN`
  as examples only. Qatlas offers every value of the enum except the unspecified and unknown ones.
- Whether Google accepts a field mask in camelCase and in the order Qatlas sends. No live call has been made.
- Documented bounds for the name, the tracking template, and the suffix, and the range of values of the
  final URL suffix.

Left out on purpose:

- `google_ads_conversion_customer`.
- `CustomerService.CreateCustomerClient`, which creates an account and may invite a user, and every
  other service of links and billing, and the invitation of users.
- `partial_failure`, a list of operations, and `GoogleAdsService.Mutate`: the tools use the Mutate service of
  their resource family with one operation.
- Smart campaign settings.

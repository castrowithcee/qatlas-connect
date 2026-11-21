---
description: >
  Describes the Google Merchant tools for online return policies: listing and reading them, creating and deleting one, who may use them, every argument with its checks, the https check of the policy address, the results, the delayed processing and the missing repetition, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant return policies

An online return policy of a Merchant Center account tells shoppers of one or more countries how they may return
what they bought: the return window, the methods, the fees, and the page that states the policy. A policy
without a label is the default of its countries; a policy with a label is an exception that products can be
assigned to. Four tools cover them. Targets, quotas, results, and errors are as described in the
[provider overview](README.md).

| Tool | Method | Effect |
| --- | --- | --- |
| `google.merchant.online-return-policies.list` | `accounts.onlineReturnPolicies.list` | reads the policies of an account |
| `google.merchant.online-return-policies.get` | `accounts.onlineReturnPolicies.get` | reads one policy |
| `google.merchant.online-return-policies.create` | `accounts.onlineReturnPolicies.create` | creates a policy |
| `google.merchant.online-return-policies.delete` | `accounts.onlineReturnPolicies.delete` | deletes a policy |

There is no tool to change a policy. Google offers none: a policy is replaced by creating a new one and deleting
the old one.

## Access

`list` and `get` are reads and belong to the profile `read`. `create` and `delete` are offered only to a
connection whose `tools` list names them, no effect profile selects them, and they need a confirmation on every
call: a policy is shown to shoppers and its address steers them to a page of the shop. Google documents no
Merchant Center role for these methods; a missing role is answered with `403`, which Qatlas reports as
`permission`.

The `account` argument is the account ID. `create` and `delete` require it, the reads take it from a connection
with exactly one target. It is checked against the targets before any secret is read or a request is sent. An
account that no target names is read or changed only after the proof read of the sub-accounts of a bound advanced
account, as for `accounts.get`; a refused account is never changed. An account target is enough: these tools act
on one account, not on the advanced account as a whole.

## Arguments

`get` and `delete` name the policy by `return_policy_id`, the `returnPolicyId` that Google generated and that
`list` and `create` show. It is 1 to 100 letters, digits, hyphens, or underscores; Qatlas builds the path of the
request from the checked ID and never from free text. Google documents no form for the ID, so this is the
bound of Qatlas. `create` takes no ID: Google generates it.

Every writable member of the resource is a structured argument. Objects are closed: a member that is not listed
is refused, also inside a nested object.

| Argument | Meaning |
| --- | --- |
| `label` | optional, 1 to 50 characters; unique per country; without it the policy is the default of its countries |
| `countries` | required, 1 to 250 two-letter uppercase country codes, each once |
| `policy` | `type` is `NUMBER_OF_DAYS_AFTER_DELIVERY` (with `days`, 1 to 3650), `NO_RETURNS`, or `LIFETIME_RETURNS` |
| `seasonal_overrides` | up to 100 entries of `label`, `start_date`, `end_date` (inclusive), and one of `return_days` or `return_until_date` |
| `restocking_fee` | exactly one of `fixed_fee` or `micro_percent` (0 to 100000000; 15000000 is 15 percent) |
| `return_methods` | `BY_MAIL`, `IN_STORE`, `AT_A_KIOSK`, each at most once |
| `item_conditions` | `NEW`, `USED`, each at most once |
| `return_shipping_fee` | `type` `FIXED` with an optional `fixed_fee`, or `CUSTOMER_PAYING_ACTUAL_FEE` |
| `return_policy_uri` | required, the address of the page that states the policy |
| `accept_defective_only` | whether only defective products are accepted |
| `process_refund_days` | days needed to process a refund, 0 to 365 |
| `accept_exchange` | whether exchanges are allowed |
| `return_label_source` | `DOWNLOAD_AND_PRINT`, `IN_THE_PACKAGE`, or `CUSTOMER_RESPONSIBILITY` |

A price is `amount_micros`, digits as a string (one million is one unit of the currency), and `currency_code`,
three uppercase letters. A date is `year`, `month`, and `day` and must be a real day. `days` is a number and
belongs to `NUMBER_OF_DAYS_AFTER_DELIVERY` only; Qatlas sends it to Google as the string the API takes.
`return_shipping_fee.fixed_fee` belongs to the type `FIXED` only. Google says to set a return shipping fee only
when customers download and print the return label; Qatlas does not check that combination.

Google requires `return_methods` and `item_conditions` to be non-empty unless the policy type is `NO_RETURNS`.
Qatlas applies this literally, so a call without a `policy` needs both. It also refuses what Google documents as
exclusive or overlapping: both `return_days` and `return_until_date` in one override, both members of a
restocking fee, and seasonal overrides whose days overlap. It refuses an end date before its start date. Qatlas
does not repeat any other rule; it leaves the rest to Google, including the uniqueness of a label per country.
The members `name` and `returnPolicyId` are output only and are no arguments. The countries and the label cannot
be changed after creation, which is why a policy is replaced and not changed.

The bounds on the number of countries and overrides, on `days`, `process_refund_days`, and `return_days`, and on
the length of an override label (100 characters) are those of Qatlas; Google documents none for them.

## Address

`return_policy_uri` must be an absolute `https` address with a host, without user information, white space,
control characters, or backslashes, at most 2048 characters. It is the address that Google uses to sanity-check
the policy and that shoppers can follow. Qatlas refuses anything else before a secret is read or a request is
sent, so a refused address never reaches Google. The address is text handed to Google and never fetched. Google
documents that the address must be a valid URL and no length for it; 2048 is the bound of Qatlas and of a shown
result. Google may also require that the page is reachable without a sign-in; Qatlas does not check this.

## Results

A policy shows `name`, `returnPolicyId`, `label`, `countries`, `policy` (`type`, `days`), `seasonalOverrides`
(`label`, `startDate`, `endDate`, `returnDays`, `returnUntilDate`), `restockingFee` (`fixedFee`,
`microPercent`), `returnMethods`, `itemConditions`, `returnShippingFee` (`type`, `fixedFee`), `returnPolicyUri`,
`acceptDefectiveOnly`, `processRefundDays`, `acceptExchange`, and `returnLabelSource`. Everything is untrusted
content of whoever runs the account: texts are cut, members that are not documented are dropped, lists keep at most
100 entries, and addresses are text that is never fetched. Values are shown as Google names them, including its
spelling of `days` and `amountMicros` as strings.

`list` takes `page_size` from 1 to 100 and a `cursor`; Google sends 10 policies when `page_size` is left out. A
cursor is tied to the tool, the account, the page size, and the targets. `create` shows the policy as Google
answered. `delete` answers with `account`, `return_policy_id`, and `deleted`, which only tells that Google accepted
the deletion.

## Delayed processing and no repetition

The answer of `create` and `delete` only tells that Google accepted the call. Google's guide says that a policy
created in Merchant Center can take up to 10 business days to be reviewed and available and names no time for the
API, so a policy may not show everywhere at once. The effect of a deletion on listings that used the policy is not
documented. Each call counts against the quota of the Merchant API, a failed one too. Qatlas sends a change once
and never repeats it, also not after a renewed token. A failure that may have reached Google, such as a server
error, a lost connection, or an unreadable answer, says that the outcome is unknown: read the policies before
changing them again. A `4xx` answer rejected the request before any change; a `429` names the daily or the
per-minute quota by its reason.

## Boundary

Checked against the reference pages of the resource and of the four methods in the accounts API v1: the
collection path is `accounts/{account}/onlineReturnPolicies`, `create` is a `POST` to it whose body is the policy,
`get` and `delete` use `accounts/{account}/onlineReturnPolicies/{return_policy}`, `delete` has no body and answers
an empty object, `list` takes `pageSize` (10 by default, at most 100, larger values are cut to 100) and
`pageToken`, every method uses the scope `content`, `label` is at most 50 characters, `countries` and
`returnPolicyUri` are required, and `name` and `returnPolicyId` are output only. A price has `amountMicros` and
`currencyCode`, a date has `year`, `month`, and `day`.

Where the pages differ, Qatlas follows the reference page of the method:

- The guide to online return policies describes the creation as a `POST` to the path of a chosen ID. The reference
  page of `create` has no ID in the path, and its resource marks `returnPolicyId` as generated by Google. Qatlas
  posts to the collection and takes no ID.
- Client library documentation of an earlier API version marks `label` as required. The reference page of v1 marks
  it optional and says that a policy without it is the default of its countries. Qatlas treats it as optional.

Not verified:

- No live call was made.
- The form of a `returnPolicyId`, and whether Google refuses a second default policy for a country or a repeated
  label.
- Whether Google answers a `delete` with `200` and an empty object, as the reference says, or with `204`; Qatlas
  accepts both.
- How long Google needs before a policy created through the API shows, and what a deletion does to listings that
  used it.
- The quota group of these methods and their per-minute and daily limits.
- Which Merchant Center role the methods need.

---
description: >
  Describes the user list tools of the Google Ads provider: user-lists.create, update and remove for audience lists (basic, rule based, logical, lookalike, and the empty shell of a Customer Match list) and user-list-customer-types.create and remove for the customer type labels of such lists, with the list of operations and its bound, the single atomic request without partial_failure, validate_only, the missing response_content_type, which tool only the tools list offers, the fields and types that are left out, what is checked against the account, the manager target rule, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-23
---

# Google Ads: user list tools

These tools create, change, and remove the audience lists of a Google Ads account and label them with customer
types. They add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply
here too. The rules of the [settings tools](settings.md) hold as well (confirmation per call, one request sent
once, unknown outcome, resource names bound to the account); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.user-lists.create` | create | creates up to 20 user lists |
| `google.ads.user-lists.update` | update | changes up to 20 user lists |
| `google.ads.user-lists.remove` | delete | removes up to 20 user lists |
| `google.ads.user-list-customer-types.create` | create | attaches up to 50 customer type categories to user lists |
| `google.ads.user-list-customer-types.remove` | delete | detaches up to 50 customer type categories |

None of the tools belongs to a profile. `user-lists.remove` is offered only when the `tools` list of a connection
names it, because a removed list is not reactivated and every campaign that uses it loses the audience. The
other four are offered by a connection whose effects include `create` or `update` or `delete`, as their effect
says, or by its `tools` list. Every call of every tool needs the confirmation of that call; the text names every
list or pair that is affected.

## Several operations in one request

`UserListService.MutateUserLists` and `UserListCustomerTypeService.MutateUserListCustomerTypes` take
`operations[]`. Each tool takes a list and sends all its entries as one request without `partial_failure`, so
Google applies all of them or none, and a failed entry fails the whole call. The bound per call is Qatlas's own:

| Tool | Argument | Entries at most | Why |
| --- | --- | --- | --- |
| `user-lists.create`, `update` | `user_lists` | 20 | an entry can carry a large rule tree, and the confirmation has to name every list |
| `user-lists.remove` | `user_list_ids` | 20 | the same bound for the same service |
| `user-list-customer-types.create`, `remove` | `customer_types` | 50 | an entry is a pair of an ID and a category |

Google's own bound is 10,000 operations for any Mutate request; the services document no smaller one. A list
may name an object once only (`user_lists`, `user_list_ids`, and each pair of `customer_types`), so one request
never changes the same object twice. The answer holds the lists or pairs in the order of the call, each with the
resource name Google answered.

**Answer binding.** The answer must hold exactly one result per operation, in the order sent. A result of an
update, a removal, or a customer type must carry exactly the resource name that was built from the checked
arguments. The result of a creation must be a new `customers/ID/userLists/N` of the account of the call, and no
two results may be the same. A partial failure, another number of results, another account, another object, or
an unreadable answer is `invalid-provider-response` with the note that the outcome is unknown, since the change
may have been made. Text of an answer is never copied into the output.

**validate_only.** With `true`, Google validates the whole request and answers errors only, so the answer
holds `validate_only: true` and no entries. It needs the same confirmation, the same checks, and costs the same
operations as the change. **No response_content_type.** The requests of both services have no such member
(`MutateUserListsRequest` and `MutateUserListCustomerTypesRequest` list only `customer_id`, `operations`,
`partial_failure`, and `validate_only`), so the tools offer none and Google answers resource names only.

**Manager accounts.** The tools act on the account that owns the lists, so the rule of the paragraph "Manager
accounts" in the [settings tools](settings.md) applies: on a connection with targets, a manager account needs a
manager target that names it, and a customer target that names a manager is refused as `invalid-request`
before any request is sent.

## user-lists

`create` needs a `name` per entry (1 to 1024 bytes without control characters, unique among the account's lists
except for some shared ones; a clash is Google's `userListError INVALID_NAME`) and exactly one list type.
`update` needs a `user_list_id` per entry and at least one field, and sends an update with the mask of exactly
the fields that were given. An empty `description` or `integration_code` clears the field by the mask alone. The
resource name `customers/ID/userLists/N` is built from the checked `customer_id` and ID; no argument takes a
resource name of a list that the call acts on. `remove` takes `user_list_ids` and sends one `remove` per ID.

| Argument of an entry | Field of the list | Rule |
| --- | --- | --- |
| `name` | `name` | 1 to 1024 bytes (Qatlas's bound) |
| `description` | `description` | up to 4096 bytes (Qatlas's bound) |
| `membership_status` | `membership_status` | `OPEN` or `CLOSED` |
| `integration_code` | `integration_code` | up to 1024 bytes (Qatlas's bound) |
| `membership_life_span` | `membership_life_span` | 0 to 540 days; Google ignores it for rule based and logical lists |
| `eligible_for_search` | `eligible_for_search` | true or false |
| `account_user_list_status` | `account_user_list_status` | `ENABLED` or `DISABLED` |
| `closing_reason` | `closing_reason` | `UNUSED`; Google sets and clears it |

The list types are the members of the `user_list` oneof, of which exactly one is given on a creation and at most
one on an update. On an update the given type member replaces that part of the list as a whole, and its name
goes into the mask (`basicUserList`, `ruleBasedUserList`, `logicalUserList`, `crmBasedUserList`).

| Member | Content | Checked by Qatlas |
| --- | --- | --- |
| `basic_user_list` | `actions`, 1 to 20, each exactly one of `conversion_action` and `remarketing_action` | each is `customers/ID/conversionActions/N` or `customers/ID/remarketingActions/N` of `customer_id` |
| `rule_based_user_list` | `flexible_rule_user_list`: `inclusive_rule_operator` `AND` or `OR`, `inclusive_operands` 1 to 20, `exclusive_operands` up to 20; optional `prepopulation_status` `REQUESTED` | see below |
| `logical_user_list` | `rules`, 1 to 10, each with `operator` `ALL`, `ANY` or `NONE` and 1 to 20 `rule_operands` | each `user_list` is `customers/ID/userLists/N` of `customer_id` |
| `crm_based_user_list` | `upload_key_type` (`CONTACT_INFO`, `CRM_ID`, `MOBILE_ADVERTISING_ID`), `data_source_type` (`FIRST_PARTY` by default), `app_id` | enums and `app_id` up to 256 bytes |
| `lookalike_user_list` (create only) | `seed_user_list_ids` 1 to 20, `expansion_level` `NARROW`, `BALANCED` or `BROAD`, `country_codes` 1 to 250 | distinct positive IDs, two-letter capital ISO-3166 codes |

**Rules.** An operand is a `rule` (1 to 20 `rule_item_groups`, each with 1 to 20 `rule_items`, `rule_type`
`OR_OF_ANDS` only, which Google documents as the only type of a flexible rule list) and an optional
`lookback_window_days`. A rule item has a `name`, the key of the tag variable, which starts with a letter, an
underscore, or a character above ASCII (up to 128 bytes; Google documents the start only), and exactly one of
`number_rule_item` (operator and value), `string_rule_item` (operator and value of up to 2048 bytes), and
`date_rule_item` (operator and exactly one of `value` as `YYYY-MM-DD` and `offset_in_days`; Google lets `value`
win when both are given, Qatlas refuses both). On `url__` and `ref_url__` a string value must hold no quote and
no parenthesis, which Google documents as illegal URL characters; newlines and tabs are refused for every text.
`prepopulation_status` offers only `REQUESTED` (adds visitors of the last 30 days on the Display Network); the
other values are set by Google. A URL in a rule is only data that Google compares: Qatlas Connect never fetches
it.

**References stay with the account.** Every reference to another object (action, operand, seed list, and the
user list of a customer type) must belong to `customer_id`. Google's reference pages state no case in which
these fields may name an object of another account, so none is accepted.

**Left out on purpose.**

- Customer Match uploads and everything that carries members: no tool uploads, adds, or removes a member,
  creates an offline data job, or returns hashed or other personal data. `crm_based_user_list` creates the
  empty shell only. `match_rate_percentage`, the sizes, and the other output-only fields are read with
  `reports.search` on `user_list`, which Qatlas does not restrict for them.
- `similar_user_list`, which is output only and created by Google.
- A change of `lookalike_user_list`, which Google documents as immutable.
- `partial_failure`, `response_content_type`, a free list of operations, and any resource name of the object that a
  call acts on.

## user-list-customer-types

A customer type is the label `customers/ID/userListCustomerTypes/USER_LIST_ID~CATEGORY` that says which kind of
customers a Customer Match list holds. `create` sends, per pair, a `create` with the resource name of the list
(built from `customer_id` and `user_list_id`) and the category; `remove` sends the name above. The categories
are `ALL_CUSTOMERS`, `CART_ABANDONERS`, `CONVERTED_LEADS`, `DISENGAGED_CUSTOMERS`, `HIGH_VALUE_CUSTOMERS`,
`LOYALTY_TIER_1_MEMBERS` to `LOYALTY_TIER_7_MEMBERS`, `PAID_SUBSCRIBERS`, `PURCHASERS`, and `QUALIFIED_LEADS`.
The tools label lists; they upload nothing and read no member.

## Quota and errors

Each entry of the list counts as one operation of the daily quota, a refused request too, plus the membership read
and the manager check when they apply. A change that Google validated and refused ends as
`provider-error` with the error category and code in the message (such as `userListError INVALID_NAME`), and
the field path of the failing operation in the original; the other codes of the README apply.

## Boundary

Not verified:

- Which role an account needs for these calls, which accounts may create which list types (Google allows
  third-party CRM sources and some lookalike lists only for allowlisted accounts), and the error then. No live
  call has been made.
- Whether Google accepts a field mask that names a whole type member (`ruleBasedUserList`) and in camelCase, as
  Qatlas sends it, and whether it accepts `account_user_list_status` and `closing_reason` on a creation.
- Whether `REQUESTED` is the only value of `prepopulation_status` that a request may carry: the reference of the
  enum lists `FAILED`, `FINISHED`, and `REQUESTED`, and the message names `NONE` as the default.
- Documented bounds for names, descriptions, rule texts, and the count of rules and operands: the reference
  states none, so the bounds above are Qatlas's own and Google may refuse sooner.
- The reference of `UserListCustomerTypeService` and `UserListService` states no REST path; Qatlas uses
  `POST /v26/customers/ID/userLists:mutate` and `/userListCustomerTypes:mutate`.
- Whether a lookalike list can still be created in v26; Qatlas offers it as the reference documents it.

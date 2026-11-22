---
description: >
  Describes the exclusion tools of the Google Ads provider: customer-negative-criteria.create and remove for the exclusions of a whole account, and shared-criteria.create and remove for the entries of shared sets, with up to 50 entries per call, how create is reached by effect while both removals need the tools list, the criterion types offered and the members of each against the reference of v26, the checks on retail filters, brands and Vertical Ads rules, the fixed form of Google constants, the binding of shared sets and answers to the account, validate_only, the unknown outcome, the quota per entry, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: exclusion tools

These tools add and remove exclusions in a Google Ads account. A customer negative criterion excludes something from
every campaign of the account, such as a website, an IP block, or a negative keyword list. A shared criterion is one
entry of a shared set, such as a negative keyword or a placement, and affects every campaign that uses the set. They
add to the read tools of the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply
here too, and to the [shared set tools](shared-sets.md), which create the sets.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customer-negative-criteria.create` | create | adds exclusions to the whole account |
| `google.ads.customer-negative-criteria.remove` | delete | removes exclusions of the whole account |
| `google.ads.shared-criteria.create` | create | adds entries to shared sets |
| `google.ads.shared-criteria.remove` | delete | removes entries from shared sets |

Reading criteria is not a tool of this page: run `reports.search` on `customer_negative_criterion` and
`shared_criterion`. Neither resource has an update: Google changes none of their members, so an entry is removed and
created again.

## How the tools are offered

- **Both `create` tools** are offered by a connection whose effects include `create` and by a connection whose
  `tools` list names them. They belong to no profile: the `read` profile holds none of them. A connection with a
  `tools` list gets exactly the tools the list names, whatever its effects are.
- **Both `remove` tools** are offered only through the `tools` list. A connection with the `delete` effect and no
  `tools` list does not have them.
- **Confirmed per call.** All four need the confirmation of that one call. The confirmation text names the account,
  the type of each entry with its first text member, and the shared set, with texts cut and without control
  characters.
- **`customer_id` is checked first.** Its form and the targets are checked before a secret is read and before any
  request. The `login_customer_id` of the service is never an argument.

## What every call does

- **One atomic request.** A call sends one request to `CustomerNegativeCriterionService.MutateCustomerNegativeCriteria`
  or `SharedCriterionService.MutateSharedCriteria` with an `operations` list and without `partial_failure`: Google
  applies all operations or none. A list holds 1 to 50 entries. The 50 are Qatlas's own bound; the reference pages state
  none.
- **`validate_only`.** With `true`, Google validates the request and changes nothing. It answers errors only, so the
  answer then holds `validate_only: true` and no `results`. Validation needs the same confirmation and the same checks,
  and counts the same operations as the change.
- **`response_content_type`** (the `create` tools). `MUTABLE_RESOURCE` adds the criterion after the change to each
  result, with the members the tool offers and `id` and `type` (`criterion_id`, `type`, and `shared_set` for a shared
  criterion). Without it a result holds the `resource_name` only. The removals have no such argument.
- **Removals take resource names of the account.** `resource_names` holds
  `customers/ID/customerNegativeCriteria/N` or `customers/ID/sharedCriteria/SHARED_SET~CRITERION`, each once, with
  positive numbers and the account of `customer_id`. Nothing else is sent: a caller never passes an operation, a
  field mask, or a free resource name.
- **References stay in the account.** A `shared_set` of a criterion, and the `shared_set` of a `negative_keyword_list`
  or `placement_list`, must be `customers/ID/sharedSets/N` of the account of `customer_id`. A temporary negative ID is
  refused. Constants of Google (`mobile_app_category_constant`, and the geo target constants of a Vertical Ads rule)
  are accepted and returned only as `mobileAppCategoryConstants/N` or `geoTargetConstants/N`.
- **Placement addresses are targets.** The `url` of a `placement` is where ads are or are not shown. Qatlas Connect
  never calls it. It must be an address of 1 to 1024 bytes with the scheme `http`, `https`, or none, and no user info.
- **No repetition.** The request is sent once, also after a refused token.
- **Unknown outcome.** When a timeout, a cut connection, a server error (5xx), or an answer that is not JSON or not
  readable leaves open whether Google took the change, the error says that the outcome is unknown. Read the criteria
  with `reports.search` before changing them again. A 4xx answer and a `GoogleAdsFailure` refuse the whole request and
  say so.
- **Answers stay with the account.** Every `resource_name` of a result must belong to the `customer_id` of the call.
  For a removal the results must be exactly the criteria asked for, in the order asked for. For a creation there must
  be one distinct result per entry, and for a shared criterion it must lie in the shared set asked for. A returned
  criterion must have the resource name of its result, a returned `id` or `criterion_id` must match it, a returned
  `shared_set` must be the set of the name, and every constant must have its fixed form. Anything else is
  `invalid-provider-response` with the note about the unknown outcome, since the change may have been made. A
  `partial_failure_error` in an answer is treated the same way, as the request asked for none.
- **Errors.** A change that Google validated and refused ends as `provider-error` with the error category and code in
  the message, such as `criterionError`, and the field path in the original. The other codes of the README apply.
- **Quota.** Google counts each operation of a mutate list as one operation of the daily quota, also in a request it
  refuses. A call with 10 entries costs 10, plus one for the membership read below a manager target and one for the
  check that the account is no manager.
- **Manager accounts.** A manager account has exclusions and shared sets of its own. On a connection with targets,
  such an account needs a manager target that names it (`managers/ID`): a customer target that names it, and a manager
  that is only reached below a manager target, are refused as `invalid-request`.

## Types of `customer-negative-criteria.create`

`criteria` holds 1 to 50 objects, each with exactly one of these members. No member can be set besides the type: the
`id` and the `type` of a criterion are output only.

| Type | Members |
| --- | --- |
| `content_label` | `type`, required, a value of `ContentLabelType` |
| `ip_block` | `ip_address`, required, one IPv4 or IPv6 address or one CIDR block; Google allows up to 500 per account |
| `mobile_app_category` | `mobile_app_category_constant`, required, `mobileAppCategoryConstants/N` |
| `mobile_application` | `app_id`, required, `1-` or `2-` and the app ID such as `2-com.example.app`; `name`, up to 255 bytes |
| `placement` | `url`, required, see above |
| `placement_list` | `shared_set`, required, a shared set of this account |
| `negative_keyword_list` | `shared_set`, required, a shared set of this account |
| `youtube_channel` | `channel_id`, required, 1 to 64 letters, digits, `_` or `-` |
| `youtube_video` | `video_id`, required, the 11 characters of the YouTube watch page |

Whether a shared set has the type that a `placement_list` or a `negative_keyword_list` needs, how many lists an
account can have, and the 500 IP addresses are Google's to decide. Qatlas sends the request once and shows a
refusal as one.

## Types of `shared-criteria.create`

Each entry of `criteria` names the `shared_set` it belongs to, exactly one type, and optionally `negative` (`true`
excludes, `false` or left out targets; it cannot be changed afterwards). The `criterion_id` and the `type` are output
only.

| Type | Members |
| --- | --- |
| `keyword` | `text`, required, up to 80 characters and 10 words; `match_type`, required, `EXACT`, `PHRASE`, or `BROAD` |
| `mobile_app_category` | `mobile_app_category_constant`, required, `mobileAppCategoryConstants/N` |
| `mobile_application` | `app_id`, required; `name` |
| `placement` | `url`, required, see above |
| `webpage` | `criterion_name`, required; `conditions`, required, a list that may be empty, each with `operand`, `operator`, and `argument` |
| `youtube_channel` | `channel_id`, required |
| `youtube_video` | `video_id`, required |
| `brand` | `entity_id`, required, the Commercial Knowledge Graph MID of the brand |
| `retail_filter` | exactly one of `expression` (`name`, required: 1 to 20 characters, lowercase latin letters, digits, and underscores, unique in the shared set) and `tag` (`value`, required: 1 to 50 characters; `expression_name`, the expression the tag belongs to, left out for a top-level condition) |
| `vertical_ads_item_group_rule` | exactly one of `city_criterion_id`, `country_criterion_id`, `region_criterion_id` (`geoTargetConstants/N`), `event_participant_display_name`, `item_code`, `venue` (up to 255 bytes), `hotel_class` (1 to 5), `user_rating` (1 to 5) |

The reference allows any character except a comma and a newline in the `value` of a tag. Qatlas also refuses other
control characters. Which type fits which shared set (a keyword in a set of negative keywords, a brand in a set of
brands) is Google's to decide. A `webpage` criterion belongs to the shared set type `WEBPAGES`, which the reference
says is not publicly available and which `shared-sets.create` does not offer, so Google is expected to refuse it.
The output-only members `display_name`, `primary_url`, `rejection_reason`, and `status` of a brand, and the
`coverage_percentage` and `sample` of a webpage, appear in an answer with `MUTABLE_RESOURCE` and are not arguments.

## Arguments

| Tool | Argument | Rule |
| --- | --- | --- |
| all | `customer_id` | ten digits without hyphens |
| `*.create` | `criteria` | 1 to 50 entries |
| `shared-criteria.create` | `criteria[].shared_set` | required, `customers/ID/sharedSets/N` of the account |
| `shared-criteria.create` | `criteria[].negative` | boolean |
| `*.remove` | `resource_names` | 1 to 50 distinct names of the account, in the form of the tool |
| all | `validate_only` | true validates only |
| `*.create` | `response_content_type` | `RESOURCE_NAME_ONLY` or `MUTABLE_RESOURCE` |

The members are those of the reference pages of `CustomerNegativeCriterion`, `SharedCriterion`, the criterion
messages (`PlacementListInfo`, `NegativeKeywordListInfo`, `BrandInfo`, `RetailFilter`, `RetailFilterExpression`,
`RetailTag`, `VerticalAdsItemGroupRuleInfo`, and the others), and of the two Mutate methods for v26, checked on
2026-11-22. The pages of the operation, response, and result messages of the two services were read too. Where the
reference lists a field as output only (`id`, `criterion_id`, `type`, the status members of a brand), it is not an
argument.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google documents no role per call.
- Which types a shared set accepts, and how Google answers a type that does not fit. No live call has been made.
- Whether Google refuses a duplicate of an exclusion, and the exact limits for `placement_list`,
  `negative_keyword_list`, and shared criteria per set.
- Whether the answer to a removal can hold more than the resource name. The pages read say it does not.

Left out on purpose:

- Reading criteria, and any update: neither resource has one.
- `partial_failure` and any free operation, mask, or resource name.
- `GoogleAdsService.Mutate`: the tools use the Mutate services of the two resource families.

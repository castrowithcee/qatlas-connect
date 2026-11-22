---
description: >
  Describes the audience tools of the Google Ads provider: custom-interests.create and update for audiences built from keywords and URLs, and audiences.create and update for audiences built from demographic dimensions, segments and exclusions, with the effect-based offer without a tools list entry, the list of at most ten operations sent as one atomic request, validate_only, the binding of every reference and result to the account, the documented values and Qatlas bounds, what the tools never do, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-22
updated: 2026-11-22
---

# Google Ads: audience tools

These tools create and change audiences of a Google Ads account. They add to the
[Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too. The change
tools follow the rules of the [settings tools](settings.md) (confirmation per call, one request sent once,
unknown outcome, resource names bound to the account); this page names only what differs. A manager account
needs a manager target that names it, as the paragraph "Manager accounts" of the [settings tools](settings.md)
says.

## Custom interests

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.custom-interests.create` | create | creates custom interests |
| `google.ads.custom-interests.update` | update | changes name, type, description, or members of custom interests |

A custom interest is an audience that Google builds from keywords and URLs. The tools call
`CustomInterestService.MutateCustomInterests`. No custom interest is removed: the service offers no remove
operation, and `status`, whose only other value `REMOVED` would remove the object, is not an argument.

### What differs from the settings tools

- **Offered by effect.** These tools need no entry in the `tools` list: a connection that allows the effect
  `create` or `update` offers them, and a connection may also name them in its `tools` list. They belong to no
  profile, so the `read` profile never offers them. Every call needs the confirmation of that one call, which
  names the account and every custom interest.
- **A list of operations.** `custom_interests` holds 1 to 10 entries, each one operation, and the tool sends
  them as one request to the service. The bound of 10 is Qatlas's own, since Google documents none: it keeps the
  confirmation readable and the reach of one call small. The request never sets `partial_failure`, and the
  service has none, so Google applies all operations or none.
- **No `response_content_type`.** The service accepts none and answers the resource names only, so the answer
  holds `results` with one `resource_name` per entry, in the order sent. After `update`, each result also names
  the fields the change named in `updated`.
- **`validate_only`** validates the whole list and answers no results, as for the settings tools.

### Arguments

| Argument | Rule |
| --- | --- |
| `custom_interests[].name` | create: required, 1 to 1024 bytes; update: optional. Google asks for a unique name |
| `custom_interests[].type` | `CUSTOM_AFFINITY` (Google's default) or `CUSTOM_INTENT` |
| `custom_interests[].description` | up to 4096 bytes; on update an empty text clears it |
| `custom_interests[].members` | up to 100 members, each `member_type` `KEYWORD` or `URL` and a `parameter` of 1 to 2048 bytes |
| `custom_interests[].id` | update only: the number of `customers/ID/customInterests/N`, each ID once per call |

The bounds of name, description, members, and parameter are Qatlas's own. Google documents no limit for them.
`update` names the object by its ID: Qatlas builds `customers/ID/customInterests/N` from the checked
`customer_id` and the ID, so an update never reaches another account. Giving `members` on update replaces all
members, since Google treats the members of an update as the new list; it needs at least one member. `update`
needs at least one field per entry.

**Keywords and URLs are data.** Qatlas sends them to Google and never fetches a URL or looks up a keyword. App
members do not exist in v26: the only member types are keyword and URL.

**Results are untrusted.** A result of `create` must name a new custom interest of the account, no name twice.
A result of `update` must be the object named at that position. The number of results must equal the number of
operations. Anything else is `invalid-provider-response` with the note that the outcome of the change is
unknown.

## Audiences

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.audiences.create` | create | creates audiences from dimensions and exclusions |
| `google.ads.audiences.update` | update | changes name, description, dimensions, exclusions, or scope of audiences |

The tools call `AudienceService.MutateAudiences` and follow the rules above: offered by effect, a list of 1 to
10 `audiences`, one atomic request, no `partial_failure`, `validate_only`, and results bound to the operations.
Here `response_content_type` is offered: `MUTABLE_RESOURCE` adds `audience` to each result with `id`, `status`,
`name`, `description`, `dimensions`, `exclusion_dimension`, `scope`, and `asset_group` as Google returns them.
No audience is removed: the service has no remove, and `status` is output only.

| Argument | Rule |
| --- | --- |
| `audiences[].name` | 1 to 255 characters, unique in the account; required with the scope `CUSTOMER`, not allowed with `ASSET_GROUP` |
| `audiences[].description` | up to 4096 bytes; on update an empty text clears it |
| `audiences[].scope` | `CUSTOMER` (default) or `ASSET_GROUP`; update accepts only `CUSTOMER` |
| `audiences[].asset_group` | create only: `customers/ID/assetGroups/N` of the account; required with `ASSET_GROUP`, otherwise not allowed |
| `audiences[].dimensions` | up to 10 dimensions, each exactly one of the members below; update needs at least one and replaces all |
| `audiences[].exclusion_dimension` | `exclusions`, each with a `user_list`; update replaces them, and an empty list clears them |
| `audiences[].id` | update only: the number of `customers/ID/audiences/N`, each ID once per call |

Google lets a scope change from `ASSET_GROUP` to `CUSTOMER` and never the other way round, and the asset group
cannot be changed, so `update` offers neither `asset_group` nor the scope `ASSET_GROUP`. A create needs at least
one dimension or one exclusion; the length of the name is Google's documented bound, the other counts are
Qatlas's own.

### Dimensions

| Dimension | Members and values |
| --- | --- |
| `age` | `age_ranges` (1 to 6) of `min_age` 18, 25, 35, 45, 55, or 65 and an optional `max_age` 24, 34, 44, 54, or 64 above `min_age`; `include_undetermined` |
| `gender` | `genders`: `MALE`, `FEMALE`, `UNDETERMINED`; `include_undetermined` |
| `household_income` | `income_ranges`: `INCOME_RANGE_0_50`, `_50_60`, `_60_70`, `_70_80`, `_80_90`, `_90_UP`, `INCOME_RANGE_UNDETERMINED`; `include_undetermined` |
| `parental_status` | `parental_statuses`: `PARENT`, `NOT_A_PARENT`, `UNDETERMINED`; `include_undetermined` |
| `audience_segments` | `segments` (1 to 100), each exactly one reference below |

A demographic dimension needs a value or `include_undetermined`. Every other rule, such as which combinations
Google accepts, is Google's to validate; `validate_only` shows them.

| Segment member | Resource name |
| --- | --- |
| `user_list` | `customers/ID/userLists/N` |
| `user_interest_category` | `customers/ID/userInterests/N` (Google's affinity and in-market categories) |
| `life_event` | `customers/ID/lifeEvents/N` |
| `detailed_demographic` | `customers/ID/detailedDemographics/N` |
| `custom_audience` | `customers/ID/customAudiences/N` |

An exclusion in `exclusion_dimension` is a `user_list` only. A custom interest is no segment type of an audience
in v26.

**References stay with the account.** Every reference in a dimension, in an exclusion, and the asset group must
have exactly the form above and the `customer_id` of the call. A reference to another account, or to a name of
another form such as a Google constant, is refused as `invalid-request` before a secret is read. Google's
reference does not say that any of these fields takes a name of another account, so Qatlas does not allow it.
The same holds for the audience in an answer: a reference or an asset group of another account makes the answer
`invalid-provider-response` with the note about the unknown outcome. Text of the audience is cut and codes are
bounded, and lists end at 500 entries.

## Boundary

Not verified:

- Whether Google accepts a reference to another account, or a category of the global taxonomy under another
  customer number, in a segment or an exclusion. Qatlas refuses both; no live call has been made.
- Whether an empty list of exclusions clears the field through the mask, and whether an audience without a
  positive dimension is valid. Qatlas lets Google decide the second and sends the first as a mask without a value.
- Limits of Google on the number of members of a custom interest, of dimensions, and of segments.
- How Google handles a name that is already used in the account: the error is Google's and ends as
  `provider-error`.

Left out on purpose:

- Removing a custom interest or an audience, and the `status` of both.
- `partial_failure` and `response_content_type` for custom interests, which the service does not have.
- Any fetch of the URLs and keywords of custom interests.

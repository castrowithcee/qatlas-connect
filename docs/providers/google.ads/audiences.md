---
description: >
  Describes the audience tools of the Google Ads provider: custom-audiences.create and update, which need a confirmation per call but no tools list entry, and custom-audiences.remove, which only the tools list offers, with the list of up to 10 operations in one atomic request, the members and their checks, the update mask that replaces all members, validate_only, the missing partial_failure and response_content_type, the binding of results to the request, the unknown outcome, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Ads: audience tools

These tools manage the audiences of a Google Ads account. They add to the [Google Ads provider](README.md),
whose configuration, targets, quotas, and errors apply here too, and follow the rules of the
[settings tools](settings.md) (confirmation per call, `customer_id` checked first, one request sent once,
unknown outcome, resource names bound to the account) except where this page says otherwise.

## Custom audiences

| Tool | Effect | Offered by |
| --- | --- | --- |
| `google.ads.custom-audiences.create` | create | a connection with the effect `create` |
| `google.ads.custom-audiences.update` | update | a connection with the effect `update` |
| `google.ads.custom-audiences.remove` | delete | only a connection whose `tools` list names it |

A custom audience is a target group that an advertiser describes with keywords, URLs, apps, and place
categories. Campaigns and ad groups can target it. `create` and `update` belong to no profile, but unlike the
settings tools they need no `tools` entry: the effect of the connection decides, and every call still needs its
confirmation. `remove` is the one change that cannot be taken back, so only the `tools` list offers it.

### Manager accounts

A manager account has custom audiences of its own. On a connection with targets, a manager account needs a
manager target that names it; a customer target that names a manager is refused after one read of
`customer.manager` and before any change. The rule is the one in the "Manager accounts"
paragraph of the [settings tools](settings.md).

### One request, up to 10 operations

`CustomAudienceService.MutateCustomAudiences` takes a list of operations and has no `partial_failure`. Each tool
takes a list and sends it as one request:

| Tool | List argument | Entry |
| --- | --- | --- |
| `create` | `custom_audiences`, 1 to 10 | `name` (required), `description`, `type`, `members` |
| `update` | `custom_audiences`, 1 to 10 | `custom_audience_id` (required), and at least one of `name`, `description`, `type`, `members` |
| `remove` | `custom_audience_ids`, 1 to 10 distinct IDs | the ID of an existing audience |

Google applies all operations or none, so one invalid entry refuses the whole call and nothing is changed. The
limit of 10 is Qatlas' own: Google documents no maximum for the operations of this request on its reference pages. The confirmation names every audience of the call: the names and member
counts for `create`, the IDs and the fields for `update`, the IDs for `remove`. An ID or a name may appear only
once in a request.

Every tool also takes `validate_only`. With `true`, Google validates the request and changes nothing; the answer
then holds `validate_only: true` and no audiences. Validation needs the same confirmation, the same checks, and
costs the same operations as the change.

### Members

Each member has a `member_type` and a `value`:

| `member_type` | `value` | Checked before any request |
| --- | --- | --- |
| `KEYWORD` | a keyword or phrase | at most 10 words and 80 characters, no control characters |
| `URL` | an address of a page | `http` or `https`, with a host, at most 2048 bytes, no spaces, backslashes, or control characters |
| `PLACE_CATEGORY` | the number of a place type that users visit | a positive int64 |
| `APP` | the package name of an Android app | dotted name such as `com.example.app`, at most 256 bytes |

Google allows 40 instead of 80 characters for keywords in Chinese, Japanese, and Korean; Qatlas checks 80 and
leaves the stricter case to Google. A call takes at most 100 members for an audience, a limit of Qatlas: Google
documents none. The members are data. Qatlas never opens a URL, looks up an app, or resolves a place category,
and a URL member is no address the provider calls. The answer holds no members.

### create

Creates the audiences with a `create` operation each. The `name` must be unique among the custom audiences of
the account; Google answers `customAudienceError.NAME_ALREADY_USED` otherwise, and the call is refused as a
whole. `type` is `AUTO` or `SEARCH`. Google does not allow `INTEREST` and `PURCHASE_INTENT` for new audiences,
so the tool does not offer them. Without `type`, Google decides.

### update

Sends an `update` operation per audience with a field mask that names exactly the fields that were given. The
resource name `customers/ID/customAudiences/AUDIENCE_ID` is built from the checked `customer_id` and
`custom_audience_id`; no argument takes a resource name. An empty `description` clears it.

**`members` replaces all members.** Google documents that members of an update replace the existing ones, and
that members can only be created or removed, never changed. Give the whole list to keep, and read the audience
with `reports.search` on `custom_audience` before. `type` is `AUTO` or `SEARCH` here as well.

### remove

Sends a `remove` operation per ID with the name built from `customer_id` and the ID. A removed audience has the
status `REMOVED` and cannot be used for targeting any more. No tool of this provider brings it back.

### Answer

The service answers with the resource names only. It has no `response_content_type`, so the tools offer no such
argument, and it returns no audience. The answer holds `customer_id`, `validate_only`, and `custom_audiences`
with `resource_name` and `custom_audience_id` for each audience that Google changed. Read audiences with
`reports.search` on `custom_audience` (`id`, `name`, `description`, `type`, `status`, `members`).

Google's answer is untrusted. It must hold one result per operation, each a resource name of this account's
`customAudiences` with a numeric ID, without duplicates, and for `update` and `remove` exactly the names that
were sent. Anything else is `invalid-provider-response` with the note that the outcome is unknown, since the
request was sent and may have been applied.

### Boundary

Not verified:

- Whether Google applies an update of `members` as a replacement when `members` is the only masked field, and
  which limit it sets for the members of an audience. The reference gives none.
- The REST reference pages of v26 for the method could not be read; the request and result shapes follow the
  RPC reference and the proto definition.
- The role an account needs for the service. No live call has been made.

Left out on purpose:

- `partial_failure` and `response_content_type`, which this service does not have.
- The types `INTEREST` and `PURCHASE_INTENT` for new audiences.
- Custom audiences are not attached to campaigns here; that belongs to the criteria and signal tools.

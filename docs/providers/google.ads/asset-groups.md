---
description: >
  Describes the asset group tools of the Google Ads provider for Performance Max campaigns: asset-groups.create, asset-groups.update and asset-groups.remove for up to ten asset groups per call, with the tools list requirement, the required name, campaign and status, the members offered and left out on purpose, the checks on resource names and addresses, field masks and clearing, validate_only, the atomic request, the answer that holds resource names only, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: asset group tools

These tools create, change, and remove asset groups of Performance Max campaigns in a Google Ads account. An
asset group that is enabled serves in its campaign and spends within the budget of the campaign. The tools add
to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors apply here too, and
follow the rules of the [settings tools](settings.md) (tools list, confirmation per call, one request sent once,
unknown outcome, `validate_only`, the manager account rule) and of the [ad group tools](ad-groups.md), whose
list of entries and field tree they share; this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.asset-groups.create` | create | creates up to 10 asset groups |
| `google.ads.asset-groups.update` | update | changes the given members of up to 10 asset groups |
| `google.ads.asset-groups.remove` | delete | removes up to 10 asset groups |

No profile offers them: a connection has a tool only when its `tools` list names it.

## What differs from the ad group tools

- **A list of asset groups, one atomic request.** The tools call `AssetGroupService.MutateAssetGroups` with up
  to 10 operations and without `partial_failure`: Google applies all of them or none. Ten is the bound of
  Qatlas; the reference of v26 names none for this call.
- **No `response_content_type`.** The request of `MutateAssetGroups` in v26 has only `customer_id`,
  `operations`, and `validate_only`, and its result holds the resource name only. The answer therefore holds
  one entry per asset group with its `resource_name`, in the order given; `reports.search` reads the asset
  group afterwards. After `validate_only` the answer has no entries.
- **Required members.** `create` requires `name`, `campaign`, and `status` (`ENABLED` or `PAUSED`). Google
  documents only the name as required: Qatlas asks for the status so that no asset group starts serving by a
  default, and for the campaign because an asset group belongs to one and the reference calls it immutable.
  `update` takes `ENABLED` or `PAUSED`; an asset group is removed only with `asset-groups.remove`, and a removed
  asset group cannot be enabled again.
- **Resource names stay with the account.** The `campaign` in an argument must be `customers/ID/campaigns/N` of
  the `customer_id` of the call, with a positive ID, and the asset groups of `update` and `remove` must be
  `customers/ID/assetGroups/N` of it. A temporary (negative) ID is refused. This is checked before a secret is
  read. A result in the answer that names another account, or for `update` and `remove` another asset group
  than the one asked for, is `invalid-provider-response` with the note about the unknown outcome.
- **Creation-only members.** `campaign` and `google_local_services_info.category_id` are arguments of `create`
  only, as the reference marks them immutable; the schema of `update` does not offer them.
- **The mask names what was given.** `update` sends a field mask with the path of each member given. A list
  member (final URLs, custom parameters, callouts) replaces the whole list. An empty text for
  `tracking_url_template` or `final_url_suffix` clears the member: it is in the mask and has no value.

## asset-groups.create and asset-groups.update

The members are those of the `AssetGroup` resource of v26, nested as the resource nests them, with the names in
snake case: name, status, campaign, final URLs, final mobile URLs, `path1`, `path2`, tracking template, final
URL suffix, custom parameters, and the Local Services information (`category_id`, callouts). The descriptor
lists every member with its type and bounds.

- **Name:** 1 to 128 characters, no control character. The name is unique within a campaign, which Google
  checks.
- **`path1` and `path2`:** the reference states no length. Qatlas keeps its own bound of 64 characters, and
  `create` refuses `path2` without `path1`, as the reference requires. On `update` Google decides.
- **At most 20 final URLs** and 20 final mobile URLs per asset group, and 50 custom parameters and callouts,
  are bounds of Qatlas; the reference states none.

**Addresses.** Final URLs, final mobile URLs, the tracking template, and the final URL suffix can pass click
data to a third party. Each final URL and the template follow the checks of
[customers.update](settings.md#customersupdate): an `https` address without user info, in the template with a
plain host name. The suffix is parameters and no address. Qatlas never calls these addresses. Performance Max
may expand a final URL, which the reference notes.

**Left out on purpose.** The members Google fills itself (output only), which `reports.search` reads: `id`,
`primary_status` and its reasons, `ad_strength`, and `asset_coverage`. The resource has no deprecated member.

## asset-groups.remove

Calls `MutateAssetGroups` with remove operations for asset groups named by `resource_names`. An asset group
appears once. Removing stops serving at once. The history of a removed asset group stays readable with
`reports.search`.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds. No live call has been made.
- Whether `MutateAssetGroups` has an upper limit of operations; Qatlas keeps its own.
- The limits of `path1`, `path2`, and the number of final URLs: the reference of v26 states none, so Google's
  own validation decides.
- That the request has no `response_content_type` and no `partial_failure` follows from the reference and the
  proto of v26; the matrix of an earlier source review assumed the standard form with both.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- The assets, signals, and listing group filters of an asset group have their own tools or none.

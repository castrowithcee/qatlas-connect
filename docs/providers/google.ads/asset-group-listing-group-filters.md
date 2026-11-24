---
description: >
  Describes the listing group filter tools of the Google Ads provider for Performance Max asset groups: asset-group-listing-group-filters.create, update and remove for up to 100 nodes per call, with the tools list requirement, the tree built in one request with temporary IDs, all dimensions of case_value, the immutable members, the field mask, response_content_type, validate_only, the atomic request, the checks on resource names and answers, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: listing group filter tools

The listing group filters of a Performance Max asset group form a tree. It decides which products (from a
Merchant Center feed or retail source) or pages (from a page feed or the advertiser domain) the asset group
advertises. A node of type `UNIT_INCLUDED` serves within the budget of the campaign. These tools create,
change, and remove nodes. They add to the [Google Ads provider](README.md), whose configuration, targets,
quotas, and errors apply here too, and follow the rules of the [asset group tools](asset-groups.md) (tools
list, confirmation per call, one atomic request sent once, unknown outcome, `validate_only`, the manager
account rule); this page names what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.asset-group-listing-group-filters.create` | create | creates up to 100 nodes, a whole tree at once |
| `google.ads.asset-group-listing-group-filters.update` | update | changes the `case_value` of up to 100 nodes |
| `google.ads.asset-group-listing-group-filters.remove` | delete | removes up to 100 nodes |

No profile offers them: a connection has a tool only when its `tools` list names it.

## What differs from the asset group tools

- **One atomic request.** The tools call
  `AssetGroupListingGroupFilterService.MutateAssetGroupListingGroupFilters` and send no `partial_failure`.
  The reference states that the service does not support it, because the tree is validated as a whole. A
  hundred nodes per call is a bound of Qatlas; the reference of v26 names none.
- **Resource names have a composite key.** A node is
  `customers/ID/assetGroupListingGroupFilters/ASSET_GROUP_ID~FILTER_ID`. The nodes of `update` and `remove`
  must belong to the `customer_id` of the call and have positive IDs. This is checked before a secret is read.
- **`response_content_type`.** The request has it, so `create` and `update` take it. With `MUTABLE_RESOURCE`
  each result carries `listing_group_filter`: `resource_name`, `asset_group`, `id`, `type`, `listing_source`,
  `case_value`, `parent_listing_group_filter`, and `path`, as far as Google returns them (the reference says
  it returns the mutable members). `remove` has no resource to return.
- **The answer is bound to the call.** Each result must be a node of the account, in the order given. For
  `update` and `remove` it must be the node asked for, for `create` a node of the asset group of its entry.
  A node in the answer whose `asset_group`, `id`, resource name, parent, or shared set belongs to another
  account, asset group, or node is `invalid-provider-response` with the note about the unknown outcome. Texts
  from Google are cut.

## Building a tree: create

A tree is usually created in one request. Google's sample builds it that way, with temporary IDs that are
negative and unique within the request.

- **`temp_id`** (an argument of `create` only) gives a node the resource name
  `customers/ID/assetGroupListingGroupFilters/ASSET_GROUP_ID~TEMP_ID`. Qatlas takes the asset group from the
  entry's `asset_group`. It is a negative whole number from -2147483648 to -1 (the lower bound is Qatlas's),
  and each name is used once.
- **`parent_listing_group_filter`** names the subdivision a node refines. It must be a node of the account.
  A negative ID is accepted only when another entry of the same request creates that exact name with
  `temp_id`. A temporary ID that no entry creates, and any temporary ID in `update`, `remove`, or
  `asset_group`, is refused. Qatlas does not require parents to come before their children in the list, and
  does not check the tree: Google's sample orders the parents first, and Google decides which tree is valid.
  The root has no parent and no `case_value` and is a `SUBDIVISION`.
- **`asset_group`** is the only member Qatlas requires, because it forms the resource name. The reference
  marks no member as required, so `type`, `listing_source`, and `case_value` are not required by Qatlas.
  Google's sample sets all three for each non-root node.
- **`type`:** `SUBDIVISION`, `UNIT_INCLUDED`, `UNIT_EXCLUDED`. **`listing_source`:** `SHOPPING`, `RETAIL`,
  `WEBPAGE`. Both are immutable after creation, like `asset_group` and the parent.

The real IDs come back in the results; read the tree afterwards with `reports.search` on
`asset_group_listing_group_filter`.

## case_value: the dimensions

`case_value` holds exactly one dimension, nested as the resource nests them, names in snake case. All nine
dimensions of v26 are arguments:

| Dimension | Members |
| --- | --- |
| `product_brand` | `value` |
| `product_category` | `category_id` (the `google_product_category` ID), `level` (`LEVEL1` to `LEVEL5`) |
| `product_channel` | `channel` (`LOCAL`, `ONLINE`) |
| `product_condition` | `condition` (`NEW`, `REFURBISHED`, `USED`) |
| `product_custom_attribute` | `index` (`INDEX0` to `INDEX4`), `value` |
| `product_item_id` | `value` |
| `product_type` | `level` (`LEVEL1` to `LEVEL5`), `value` |
| `retail_filter_bundle` | `shared_set`, a shared set of this account |
| `webpage` | `conditions`, each with exactly one of `custom_label` or `url_contains` |

An empty dimension, such as `{"product_condition":{}}`, is "everything else" of its subdivision, as in
Google's sample. Siblings use the same dimension, categories and product types the same level, and the
conditions of one `webpage` node are combined with AND and must be of one type: Qatlas does not check this,
Google does. Text values are up to 1000 bytes and at most 20 webpage conditions (bounds of Qatlas; the reference
states none). `url_contains` and `custom_label` are filters, no addresses, and Qatlas never calls them.

## update

`update` takes `resource_name` and `case_value` and sends a field mask with the path of each member given, such
as `caseValue.productBrand.value`, or `caseValue.productBrand` for an empty dimension. A `webpage` list
replaces the whole list. The reference marks the type, listing source, asset group, and parent immutable, so
`update` does not offer them: to change one, remove the node and create it again.

## remove

A node can be removed only when no other node has it as its parent. Qatlas keeps the order given and does not
reorder: list the children before their parent.

## Boundary

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether Google accepts a create without `resource_name` (no `temp_id`): the reference says no resource name is
  expected, while its sample sets a temporary one for every node. Qatlas leaves `temp_id` optional.
- Whether Google accepts a field mask in camelCase with the paths Qatlas builds. No live call has been made.
- Whether the service has an upper limit of operations; Qatlas keeps its own.
- The rules of the tree (one root, `SUBDIVISION` for nodes with children, siblings of one dimension, no
  multiple roots) and the length limits of the dimension texts: Google's own validation decides.
- That the temporary IDs may come in any order in the list: Qatlas allows it, Google decides.
- That the request has no `partial_failure` follows from the reference of the request; the matrix of an
  earlier source review assumed the standard form with it.

Left out on purpose:

- `partial_failure` and `GoogleAdsService.Mutate`.
- `id` and `path`, which Google fills (output only): `reports.search` reads them.
- The assets and signals of an asset group are not part of these tools.

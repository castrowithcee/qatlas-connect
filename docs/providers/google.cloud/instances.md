---
description: >
  Describes the Google Cloud Compute instance tools instances.list, instances.aggregated-list, and instances.get: their arguments, the compute.readonly scope and the profile read, the target check, the structured filter, the cursor and page size, partial results with unreachables and warning, the rule that metadata values are never returned, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud Compute instances

Three tools read the virtual machine instances of a project from Compute Engine (`compute` v1, discovery
revision 20261106). They change nothing. Every request goes to the fixed origin `https://compute.googleapis.com`
with the base path `/compute/v1`; no argument takes a path, URL, header, or body. [The provider page](README.md)
holds errors and rate spacing, [machine types](machine-types.md) the other Compute tool.

| Tool | Google method | Arguments |
| --- | --- | --- |
| `instances.list` | `instances.list` | `project_id`, `zone`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `instances.aggregated-list` | `instances.aggregatedList` | as above without `zone`, plus `include_all_scopes` |
| `instances.get` | `instances.get` | `project_id`, `zone`, `instance` |

## Arguments

- `project_id` is a project ID or number, `zone` a zone name such as `us-central1-a`, `instance` an instance name
  or its numeric ID. Each is checked against Compute's name form before any secret is read or request is sent;
  a path, a `projects/` or `zones/` name, a traversal, or a query is refused. A project outside the targets is
  refused without a name, as for the other project tools (see [Targets](README.md#targets)). Where the targets do
  not decide, the project is read with the scope of `projects.get`, so a connection bound to an organization or
  a folder needs `projects.get`, which the profile `read` holds.
- `filter` is Google's own filter expression, AIP-160 terms or a regular expression with `eq` or `ne`, the two not
  mixed. Qatlas does not parse it and sends it only as the `filter` parameter, encoded, so it cannot add or change
  another parameter. It holds at most 1024 characters without control characters.
- `max_results` is 1 to 500 (Google documents 0 to 500). Without it a page of `instances.list` and
  `instances.aggregated-list` asks for 100, because an instance is large and 500 would exceed what one answer
  may hold. `order_by` is `name` or `creationTimestamp`, optionally followed by ` desc`.
- `include_all_scopes` of `instances.aggregated-list` is Google's `includeAllScopes`. The Google parameter
  `serviceProjectNumber` is not offered: it names another project and is documented for subnetworks only.

## Cursor

`next_cursor` carries Google's `pageToken` and is bound to the tool, the targets, the project, the zone, the
filter, `order_by`, `return_partial_success`, and `include_all_scopes`. A cursor that is altered, cut short, or
issued for another list or project is refused before a secret is read. `max_results` stays out of the binding, so
a next page may ask for another size. `has_more` is false when Google sends no `nextPageToken`. An empty list
is a result without `instances`, not an error.

## Partial results

With `return_partial_success` Google answers a list in which a part cannot be read with the readable part, a
`warning`, and for the aggregated list the `unreachables`. The tools pass `warning` and `unreachables` on as
untrusted, bounded data and never hide them. `instances.aggregated-list` groups the instances in `scopes`, each
entry `{scope, instances, warning}` with `scope` as `zones/ZONE`, sorted by scope. Scopes without instances whose
only warning is `NO_RESULTS_ON_PAGE` are left out, because Google sends one for every empty zone; a scope with
another warning stays.

## Result

Every documented, not deprecated member of the Instance resource is returned, among them `name`, `id`,
`status`, `statusMessage`, `machineType`, `zone`, `cpuPlatform`, `networkInterfaces` (with external addresses),
`disks`, `serviceAccounts` (email and scopes), `scheduling`, `labels`, `tags`, `deletionProtection`, and
`creationTimestamp`. Text is untrusted third-party content: strings are cut at 1024 characters, lists and
objects at 100 entries, and one instance holds at most 2000 values at six levels. A row must name an instance,
and a row that names a zone must name the zone asked for, otherwise the answer is an invalid response.

**Metadata.** `metadata.items` can hold start-up scripts, credentials, or keys. The tools return only
`metadata.keys`, the names of the entries (at most 100), and the `fingerprint`, never a value. This is the one
exception to the full breadth of the result. Of the customer-supplied encryption keys, which Google marks
deprecated, `rawKey`, `rsaEncryptedKey`, and `sha256` are never returned either; the KMS key name stays.

## Scope and rights

The tools ask for the scope `compute.readonly`, the smallest the discovery document lists. They belong to the
recommended profile `read` and to `read-iam`; `compute.readonly` is a read scope, so `read` still asks for read
scopes only. The account needs the permissions `compute.instances.list` or
`compute.instances.get` (the Compute Viewer role has them), and the **Compute Engine API** must be enabled in
the project; otherwise Google refuses the request and the error says so.

## Not verified

- The rights of the account and the exact answers of Compute Engine for a disabled API were not seen live.
  Compute answers errors in the older form with a list of `errors` and a `reason`; Qatlas reads `notFound`,
  `forbidden`, and `authError` of it like the status strings `NOT_FOUND`, `PERMISSION_DENIED`, and
  `UNAUTHENTICATED`, and every other 401, 403, or 404 without a known form is an invalid response.
- Which fields of an instance Google fills depends on the instance; absent fields are absent in the result.

---
description: >
  Describes the Google Cloud Compute instance tools instances.list, instances.aggregated-list, instances.get, and the lifecycle tools instances.start, instances.stop, instances.reset, and instances.set-machine-type: their arguments, the compute.readonly scope and the profile read, the target check, the structured filter, the cursor and page size, partial results with unreachables and warning, the rule that metadata values are never returned, and the profile compute-lifecycle with its confirmations, the request ID and the returned operation, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud Compute instances

Three tools read the virtual machine instances of a project from Compute Engine (`compute` v1, discovery
revision 20261106). They change nothing; the four tools of [Lifecycle](#lifecycle) change the state of an instance. Every request goes to the fixed origin `https://compute.googleapis.com`
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

## Lifecycle

Four tools change an existing instance. They belong to the profile `compute-lifecycle`, which is not recommended,
asks for the scope `compute` (the smallest the discovery document lists, which Google also uses to change
Compute resources), needs the effect `update`, and holds `projects.get` as well. Every call needs the confirmation
of a person. None creates or deletes an instance or changes its network access.

| Tool | Google method | Arguments | Effect named in the confirmation |
| --- | --- | --- | --- |
| `instances.start` | `instances.start` | `project_id`, `zone`, `instance`, `request_id` | a running instance is billed |
| `instances.stop` | `instances.stop` | as above plus `discard_local_ssd` | the workload ends; with `discard_local_ssd` the data on local SSDs is lost |
| `instances.reset` | `instances.reset` | `project_id`, `zone`, `instance`, `request_id` | hard reset without clean shutdown, memory contents are lost |
| `instances.set-machine-type` | `instances.setMachineType` | `project_id`, `zone`, `instance`, `machine_type`, `request_id` | the instance must be stopped, the price changes |

- The confirmation names project, zone, instance, and the effect. `instance` is a name, never an ID or a path.
- `machine_type` is the name of a machine type such as `e2-standard-4`. A path or URL, also one of another zone or
  project, is refused; the tool builds the reference from the project and zone of the call. Google refuses the
  change of a running instance, and the tool passes that error on.
- Project, zone, instance, and machine type are checked against their form and the targets before any secret is
  read or request is sent. A refusal does not name the target it refused.
- Each call sends one POST and never repeats it. `request_id` is a UUID that is not all zero; without it the tool
  creates one and returns it. Repeating a call with the same `request_id` is idempotent: Google ignores a request
  whose ID it already received.
- The tool never waits. The result is `{operation, scope: "zone", zone, request_id}` with the operation as Google
  returned it; read its state with `compute-operations.get` (scope `zone`). An operation that is already `DONE`
  with an error is reported as a provider error. An answer without an operation of the zone, or a failure after
  which the request may have arrived, says that the outcome is unknown: read the instance with `instances.get`.
- The account needs `compute.instances.start`, `.stop`, `.reset`, or `.setMachineType` (the Compute Instance Admin
  role has them), and the **Compute Engine API** must be enabled.

## Not verified

- The rights of the account and the exact answers of Compute Engine for a disabled API were not seen live.
  Compute answers errors in the older form with a list of `errors` and a `reason`; Qatlas reads `notFound`,
  `forbidden`, and `authError` of it like the status strings `NOT_FOUND`, `PERMISSION_DENIED`, and
  `UNAUTHENTICATED`, and every other 401, 403, or 404 without a known form is an invalid response.
- The lifecycle tools were checked against a simulated Compute Engine only, not live.
- Which fields of an instance Google fills depends on the instance; absent fields are absent in the result.

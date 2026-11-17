---
description: >
  Describes the Google Cloud Compute instance tools instances.list, instances.aggregated-list, instances.get, the lifecycle tools instances.start, instances.stop, instances.reset, and instances.set-machine-type, instances.insert, and the listed-only instances.delete: their arguments, the compute.readonly and compute scopes and the profiles read, compute-lifecycle, and compute-instances-create, the target check, the structured filter, the cursor and page size, partial results with unreachables and warning, the rule that metadata values are never returned, the confirmations, the request ID and the returned operation, the limits on creating an instance (no external address, no ssh-keys, no foreign service account, no key material), and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud Compute instances

Three tools read the virtual machine instances of a project from Compute Engine (`compute` v1, discovery
revision 20261106). They change nothing; the four tools of [Lifecycle](#lifecycle) change the state of an instance,
`instances.insert` creates an instance and `instances.delete` deletes one (see [Creating an instance](#creating-an-instance) and [Deleting an instance](#deleting-an-instance)). Every request goes to the fixed origin `https://compute.googleapis.com`
with the base path `/compute/v1`; no argument takes a path, URL, header, or body. [The provider page](README.md)
holds errors and rate spacing, [machine types](machine-types.md) the other Compute tool.

| Tool | Google method | Arguments |
| --- | --- | --- |
| `instances.list` | `instances.list` | `project_id`, `zone`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |
| `instances.aggregated-list` | `instances.aggregatedList` | as above without `zone`, plus `include_all_scopes` |
| `instances.get` | `instances.get` | `project_id`, `zone`, `instance` |
| `instances.insert` | `instances.insert` | `project_id`, `zone`, `name`, `machine_type`, `source_instance_template`, `source_machine_image`, `disks`, `network_interfaces`, `service_accounts`, `metadata`, `labels`, `tags`, `scheduling`, `guest_accelerators`, `min_cpu_platform`, `shielded_instance_config`, `confidential_instance_config`, `can_ip_forward`, `deletion_protection`, `reservation_affinity`, `display_device`, `advanced_machine_features`, `description`, `request_id` |
| `instances.delete` | `instances.delete` | `project_id`, `zone`, `instance`, `request_id` |

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

## Creating an instance

`instances.insert` sends one POST to `/compute/v1/projects/PROJECT/zones/ZONE/instances` with the query
`requestId`. The tool builds the body itself from structured arguments; no object is passed through, and every
argument has a closed schema. It has the effect `create`, needs the confirmation of a person, and costs money:
the confirmation names the project, zone, name, machine type, the disk sizes, the accelerators, and the
scheduling model (`STANDARD`, or `SPOT` when `scheduling.provisioning_model` says so). The tool never waits. The
result is the zonal Compute operation as Google returned it, with `scope` `zone`, the `zone`, and the
`request_id`; `compute-operations.get` with that scope and zone reads its state. An operation that is already
done with an error, a quota for example, is reported as a provider error. Without a `request_id` the tool
creates one and returns it, and repeating the call with the same one is safe because Google ignores a request
whose ID it already received. The request is never repeated by the tool, and an unclear outcome is reported as
such: read the instance with `instances.get` before creating it again.

**Names and paths.** `machine_type`, `disk_type`, `source_image`, disk `source`, `network`, `subnetwork`,
`source_instance_template`, `source_machine_image`, and the accelerator `type` are names or relative paths of
the project and the zone or region of the call, such as `e2-medium`, `zones/ZONE/machineTypes/NAME`, or
`projects/PROJECT/global/networks/NAME`. The tool turns each into a fixed path; a URL, a path into another
zone or region, or a resource of another project is refused. The one resource of another project is an image
family given as a project and family pair, `projects/PROJECT/global/images/family/FAMILY` such as
`projects/debian-cloud/global/images/family/debian-12`; an image of another project by name is refused.

**What an instance cannot get.**

- No external address. `network_interfaces` has no `accessConfigs` and no `ipv6AccessConfigs`, and the handler
  refuses either name, in any spelling, at any depth of the arguments.
- No key of a person. The metadata keys `ssh-keys`, `sshKeys`, and `windows-keys` are refused whatever their case,
  because a guest accepts the public key of a person through them. `block-project-ssh-keys` is allowed, as it only
  closes access. Other metadata, including `enable-oslogin`, is allowed.
- No service account of another project. `service_accounts[].email` is `NAME@PROJECT_ID.iam.gserviceaccount.com`
  of the project of the call, or the Compute Engine default account (`PROJECT_NUMBER-compute` in the domain `developer.gserviceaccount.com`); the
  project ID and number come from reading the project (`projects.get`), never from an argument. Without an email
  Google uses the default account; without `service_accounts` the instance has none. At most one account is
  accepted, and each scope is a URL `https://www.googleapis.com/auth/NAME`.
- No key material. The `rawKey` and `rsaEncryptedKey` of a customer-supplied encryption key are no argument;
  `kms_key_name` is allowed.
- With `source_instance_template` or `source_machine_image` the tool cannot read what the template or image holds,
  so `network_interfaces` and `service_accounts` (which may be empty) must be given in the same call, and
  `source_instance_template` and `source_machine_image` exclude each other.

**What the tool does not check.** A start-up script or other metadata value is not examined. A start-up script
can create accounts and reach whatever the instance can reach, and that limit lies with the person who confirms the
call. The same holds for the metadata that a template or machine image brings. Nor does the tool check that a
service account exists, or the quota of the project.

Not every field of the Instance resource has an argument. The ones offered are those of the table above, with the
members of each nested object named in the tool description; alias IP ranges, node affinities, resource policies,
and the other members are not offered.

## Deleting an instance

`instances.delete` sends one DELETE to the instance and returns the zonal operation like the creation. It has
the effect `delete`, needs confirmation, and is **listed-only**: it belongs to no profile and a connection offers it
only when its `tools` list names `google.cloud.instances.delete` exactly. The instance is removed with the disks
that are set to `autoDelete`, which is the default of a boot disk, and this cannot be undone. An instance with
`deletion_protection` is refused by Google, and the tool reports that error. `instance` is a name or numeric ID.

## Scope and rights of creating and deleting

Both tools ask for the scope `compute`, the smallest the discovery document lists, which Google also uses to change
other Compute resources, so the token may do that although no tool does. `instances.insert` belongs to the profile
`compute-instances-create`, which also holds `projects.get`; the profile is not recommended and needs the effects
`create` and `read`. The account needs `compute.instances.create` and the permissions the new instance uses, for
example `compute.disks.create`, `compute.subnetworks.use`, and `iam.serviceAccounts.actAs` on the service account (the
Compute Instance Admin role has the first ones), and for the default account `resourcemanager.projects.get`.
`instances.delete` needs `compute.instances.delete`. The **Compute Engine API** must be enabled in the project.

## Not verified

- The rights of the account and the exact answers of Compute Engine for a disabled API were not seen live.
  Compute answers errors in the older form with a list of `errors` and a `reason`; Qatlas reads `notFound`,
  `forbidden`, and `authError` of it like the status strings `NOT_FOUND`, `PERMISSION_DENIED`, and
  `UNAUTHENTICATED`, and every other 401, 403, or 404 without a known form is an invalid response.
- The lifecycle tools were checked against a simulated Compute Engine only, not live.
- Which fields of an instance Google fills depends on the instance; absent fields are absent in the result.

---
description: >
  Describes the Google Cloud Compute tools compute-operations.get and compute-operations.list: the scope choice zone, region, or global, the argument checks before any request, the fields returned, the mapping of the error of a finished operation, the warning about the personal data in user, the scope and the profile read, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud Compute operations

Two tools read the operations of Compute Engine (`compute` v1, discovery revision 20261106), which track the
changes that Compute tools and the console start. They change nothing. Compute operations are not the
long-running operations that [`operations.get`](README.md) reads: their state is `status` (`PENDING`,
`RUNNING`, or `DONE`), and a failure is `error.errors[]`.

| Tool | Google method | Arguments |
| --- | --- | --- |
| `compute-operations.get` | `zoneOperations.get`, `regionOperations.get`, or `globalOperations.get` | `project_id`, `scope`, `zone` or `region`, `operation` |
| `compute-operations.list` | `zoneOperations.list`, `regionOperations.list`, or `globalOperations.list` | `project_id`, `scope`, `zone` or `region`, `filter`, `max_results`, `order_by`, `return_partial_success`, `cursor` |

`scope` is `zone`, `region`, or `global` and picks the method. `zone` is required for `scope=zone`, `region`
for `scope=region`, and neither is allowed for `global` or for the other scope. The combination is refused before
any secret is read or request is sent. `operation` is the name that a change returned (such as
`operation-1700000000000-abc123-def456`) or its numeric ID. The checks, the target rule, the structured `filter`,
the cursor (also bound to the scope), and the handling of `warning` are those of [instances](instances.md). A list
asks for 100 operations without `max_results`.

The result of `get` is `operation`, the result of `list` is `operations`. Each holds the documented, not
deprecated members of the Operation resource: `name`, `id`, `operationType`, `targetLink`, `targetId`, `status`,
`statusMessage`, `progress`, `insertTime`, `startTime`, `endTime`, `error`, `warnings`, `httpErrorStatusCode`,
`httpErrorMessage`, `description`, `clientOperationId`, `operationGroupId`, `user`, `zone`, `region`,
`selfLink`, `kind`, and the metadata members of bulk insert, health, version, and common instance metadata
operations. `creationTimestamp` is left out because Google marks it deprecated. An answer whose zone or region is
not the one asked for is refused. The content is untrusted third-party text and bounded.

## Error of a finished operation

`DONE` is a lasting state, with or without `error`. `get` reports a `DONE` operation with `error.errors[]` as a
provider error: the message names only the HTTP status the operation carries (`httpErrorStatusCode`), and the
original entries carry the `code`, `location`, and `message` of each error as Google wrote them. The class is
`not-found` for status 404, `rate-limited` for 429, and a provider error otherwise; 401 and 403 are not mapped
to authentication or permission errors, because Compute reports a quota failure with 403 too. A `RUNNING` or
`PENDING` operation is returned as it is, also with an `error` object. `list` never turns a row into an error:
it returns `error` as data.

## Personal data

`user` is the email address of the person or account that started the operation. It is personal data. The
tools carry the sensitivity `cloud-operation-personal-data`, not the `cloud-infrastructure` of the other Compute
tools, and their descriptions say so. They stay in the profile `read`; a connection that must not reveal who
changed what leaves them out of `tools`.

The tools ask for the scope `compute.readonly` and belong to the profiles `read` and `read-iam`. The account
needs `compute.zoneOperations.get` or `.list`, `compute.regionOperations.get` or `.list`, or
`compute.globalOperations.get` or `.list` (the Compute Viewer role has them), and the Compute Engine API must be
enabled in the project.

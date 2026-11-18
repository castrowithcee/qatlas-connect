---
description: >
  Describes the Google Cloud tool catalog-services.list, which reads the services of the public Cloud Billing Catalog: its arguments, page size and cursor, the fields, the scope and profile, and the lack of a target.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud billing catalog services

`catalog-services.list` reads the services of the public Cloud Billing Catalog (`services.list` of `cloudbilling`
v1, discovery revision 20261106). It changes nothing and is a GET to `https://cloudbilling.googleapis.com/v1/services`.
The service IDs it returns are the input of [`catalog-skus.list`](catalog-skus.md).

| Tool | Arguments |
| --- | --- |
| `catalog-services.list` | `page_size`, `cursor` |

**No target.** The catalog is public and belongs to no organization, folder, project, or billing account, so any
connection of the provider may read it and the tool is always offered. The connection's targets do not limit it.

**Pages.** `page_size` is 1 to 100. Google's own default is 5000 rows, more than a page may hold here, so a call
without `page_size` asks for 100. A page that holds more rows than twice the largest page size is an invalid
response. `has_more` and `next_cursor` continue the list; a cursor continues only the list that issued it, on the
same connection.

**Fields.** Every documented field is returned: `name` (`services/ID`), `serviceId`, `displayName`, and
`businessEntityName`. Texts are untrusted: control characters become spaces and strings are cut to 256 bytes.

**Scope and profile.** The tool asks for `cloud-billing.readonly`, the smallest scope the discovery document lists
for `services.list`, and belongs to the profile `read`. The Cloud Billing API (`cloudbilling.googleapis.com`) must be
enabled in the project of the credential.

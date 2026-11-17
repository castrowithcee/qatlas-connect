---
description: >
  Describes the Google Cloud service tools: reading the enabled and available APIs of a project from Service Usage with services.list, services.get, and services.batch-get, their arguments, the target check, the required API, and the scope.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud services

The three tools read which Google APIs a project has enabled or could enable, from Service Usage (discovery
`serviceusage` v1). Each takes the `project_id` (project ID or number, never a `projects/` name) and each result
row holds `name`, `parent`, `state` (`ENABLED`, `DISABLED`, `STATE_UNSPECIFIED`), and the `config` of the
service.

| Tool | Arguments | Google method |
| --- | --- | --- |
| `services.list` | `project_id`, `filter`, `page_size`, `cursor` | `services.list` |
| `services.get` | `project_id`, `service` | `services.get` |
| `services.batch-get` | `project_id`, `names` (1 to 20) | `services.batchGet` |

- `filter` is `state:ENABLED` or `state:DISABLED`, the only values Google documents. Anything else is refused
  before a request. `page_size` is 1 to 100 (Google allows 200). The `next_cursor` is bound to the tool, the
  targets, the project, and the filter.
- `service` and each of `names` are service names such as `compute.googleapis.com`: lowercase DNS-like names with
  at least one dot. A path, a `projects/` name, or any other form is refused; Qatlas builds the resource name
  itself. Google allows 30 names per `batchGet`; Qatlas takes 20 and refuses duplicates.

## Config

`config` carries the documented members of the service config (`name`, `title`, `apis`, `documentation`,
`quota`, `authentication`, `usage`, `endpoints`, `monitoredResources`, `monitoring`). Members Google marks
deprecated (`deprecationDescription` of a documentation rule, `audiences` of an authentication requirement) are
left out. The content is untrusted third-party text: strings are cut, lists and nesting are bounded, and one
config holds at most 1000 values, so a very large config (many API methods) is truncated.

## Target check

The project must lie in the targets. A connection with project targets alone refuses another project before a
secret is read. If an organization or a folder is a target, Qatlas reads the project and the folders above it
first, as `projects.get` does, which needs Cloud Resource Manager enabled. Billing account targets alone reach no
project.

## Required API and scope

Service Usage (`serviceusage.googleapis.com`) must be enabled in the Cloud project of the credential. If it is
not, Google answers `SERVICE_DISABLED` and the tool fails with the class `permission` and a message that names
the Service Usage API; a person enables it there. A missing right on the project is also `permission`, an unknown
project or service `not_found`.

The discovery document lists `cloud-platform.read-only` next to `cloud-platform` for all three methods, so the
tools belong to the profile `read` and ask for the read-only scope. The discovery document names no
IAM permission for these methods, so none is claimed here.

Changing the state of a service (enable, disable) is not part of these tools.

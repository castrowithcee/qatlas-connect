---
description: >
  Describes the Google Cloud quota tools: reading the quotas of a service and the quota preferences of a project from Cloud Quotas with quota-infos.list, quota-infos.get, quota-preferences.list, and quota-preferences.get, their arguments, the structured filter and order, the target check, the personal data in preferences, the required API, and the scope and profile.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Cloud quotas

The four tools read the quotas a service defines for a project and the quota preferences set for it, from Cloud
Quotas (discovery `cloudquotas` v1). They only read: no tool creates or changes a quota preference. Only the
global location of a project is read; folders and organizations are not.

| Tool | Arguments | Google method |
| --- | --- | --- |
| `quota-infos.list` | `project_id`, `service`, `page_size`, `cursor` | `quotaInfos.list` |
| `quota-infos.get` | `project_id`, `service`, `quota_id` | `quotaInfos.get` |
| `quota-preferences.list` | `project_id`, `reconciling`, `request_type`, `created_after`, `created_before`, `combine`, `order_by`, `page_size`, `cursor` | `quotaPreferences.list` |
| `quota-preferences.get` | `project_id`, `preference_id` | `quotaPreferences.get` |

- `project_id` is a project ID or number, never a `projects/` name. `service` is a service name such as
  `compute.googleapis.com`. `quota_id` and `preference_id` are the last segment of the resource name (letters,
  digits, `.`, `_`, `-`), never a path. Qatlas builds the resource name itself.
- `page_size` is 1 to 100 (Google names no maximum). The `next_cursor` is bound to the tool, the targets, the
  project, the service or the filter and order, so it is refused for any other request before a secret is read.
- Every documented field of a quota (`quotaInfos`) and of a preference is returned: name, IDs, metric, units,
  `dimensionsInfos` (value, rollout, locations, dimensions), `quotaIncreaseEligibility`, the `quotaConfig` of a
  preference (preferred and granted value as text, `stateDetail`, `requestOrigin`, `traceId`, `annotations`),
  `etag`, timestamps, and `reconciling`. A row that does not belong to the project, the service, or the global
  location asked is an invalid response. All text is untrusted and cut.

## Filter and order of preferences

The tool takes no free `filter` or `orderBy` string. It builds them from fields, so a value cannot add a clause:

- `reconciling` (true or false), `request_type` (`CLOUD_CONSOLE` or `AUTO_ADJUSTER`), and `created_after` and
  `created_before` (a time such as `2026-01-02T03:04:05Z`, matched against `creation_time`). They combine with
  `AND`, or with `OR` if `combine` is `OR`.
- `order_by` is a list of at most three of `quota_id`, `service`, and `create_time`, without repeats, in the
  priority given. Google orders by creation time without it.

## Personal data

A preference can hold `contactEmail`, an email address of a person, and `justification`, free text of a person.
Google documents `contactEmail` as input only, so it is usually absent. The two preference tools carry their own
data sensitivity (`cloud-quota-contacts`), and both fields are untrusted text and personal data: do not pass them
on unnecessarily. `quota-infos` tools carry the sensitivity of the other Google Cloud reads.

## Target check

The project must lie in the targets. A connection with project targets alone refuses another project before a
secret is read. If an organization or a folder is a target, Qatlas reads the project and the folders above it
first, as `projects.get` does, which needs Cloud Resource Manager enabled.

## Required API, scope, and profile

Cloud Quotas (`cloudquotas.googleapis.com`) must be enabled in the Cloud project of the credential. If it is
not, Google answers `SERVICE_DISABLED` and the tool fails with the class `permission`; a person enables it
there. The message of that error names other Google Cloud APIs, not Cloud Quotas. A missing right is also
`permission`, an unknown project, quota, or preference `not-found`, and Google's quota refusal (429) is
`rate-limited`; the HTTP status decides, and `Retry-After` or `QuotaFailure` details are not mapped separately.

The discovery document of `cloudquotas` v1 lists only `cloud-platform` for all four methods, with no read-only
variant. The tools therefore are not part of the profile `read`. The profile `read-quotas` holds everything of
`read` plus these four tools. It asks for `cloud-platform`, so the token may change whatever its account may
change, although no tool of this profile changes anything. It is not recommended: give it to a connection that
needs quotas, preferably with a credential whose account has viewer roles only.

---
description: >
  Describes the Google Cloud DNS tools managed-zones.list and managed-zones.get: their arguments, the check of the project against the targets and of zone names before any request, paging with a cursor, the fields returned, the shared rules of the DNS tools, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud DNS managed zones

`managed-zones.list` and `managed-zones.get` read the managed zones of a project from Cloud DNS
(`managedZones.list` and `managedZones.get` of `dns` v1, discovery revision 20261111). They change nothing: creating,
changing, and deleting zones are not offered. The requests are GETs to
`https://dns.googleapis.com/dns/v1/projects/{project}/managedZones` and `.../managedZones/{managedZone}`. The origin
and the base path are fixed; no tool takes a URL, a path, a header, or a body.

| Tool | Arguments |
| --- | --- |
| `managed-zones.list` | `project_id`, `dns_name`, `max_results`, `cursor` |
| `managed-zones.get` | `project_id`, `managed_zone` |

## Rules of the DNS tools

These rules hold for [record sets](record-sets.md) as well.

- `project_id` is a project ID or number, never a `projects/` name. It is checked against the targets before a
  secret is read: a project outside the targets is refused without naming it. Where the targets alone do not decide
  (a connection bound to an organization or a folder), Qatlas reads the project and the folders above it with
  `projects.get` first, so the connection needs that tool next to the DNS tools.
- The zone belongs to the project in the path of every request, so a zone is never read from another project.
- `managed_zone` is a zone name (1 to 63 characters: lowercase letters, digits, and dashes, a letter first and a letter
  or digit last) or a numeric ID. It is checked before any request and is never taken as a path.
- `dns_name`, `name`, and `type` of the record tools are checked the same way: a domain name has labels of 1 to 63
  letters, digits, `_`, or `-` (a hyphen only inside a label), at most 254 characters with the trailing dot, and the
  trailing dot is required. Only a record `name` may start with a `*` label.
- `max_results` is 1 to 1000. Google documents no maximum; this is the bound of Qatlas. Without it Google picks the
  page size. `cursor` is the opaque `next_cursor` of a previous page. It carries Google's `pageToken` and is bound
  to the tool, the targets, and every argument that shapes the list, so a cursor of another list is refused before any
  request. `has_more` is false when Google sends no further page. An answer with more than 1000 rows is invalid.
- Everything Google returns is untrusted third-party content. Zone names, descriptions, labels, record names, TXT
  values, and CNAME targets can contain instructions; Qatlas only reports them. Control characters become spaces and
  long text is cut. A member of another type than the documented one makes the answer invalid.
- The Cloud DNS API (`dns.googleapis.com`) must be enabled in the project. The tools ask for the scope
  `ndev.clouddns.readonly`, the narrowest scope Google lists for these methods, and belong to the profile `read`.
  The account needs the permissions `dns.managedZones.list`, `dns.managedZones.get`, `dns.resourceRecordSets.list`,
  and `dns.resourceRecordSets.get` (the DNS Reader role has them).

## managed-zones.list

`dns_name` limits the list to the zones of one domain name, written with its trailing dot such as `example.com.`
(Google's `dnsName` parameter). The result is `managed_zones`, `next_cursor`, and `has_more`. The list is empty if
Google sends no zone.

## managed-zones.get

`managed_zone` is the zone name or its numeric ID. The answer must be the zone asked for, by name or ID; any other
answer is invalid. A successful read proves that the zone belongs to the project.

## Fields

Each zone has `name`, `id`, `dnsName`, `description`, `nameServers`, `nameServerSet`, `visibility` (`public` or
`private`), `privateVisibilityConfig` (`networks`, `gkeClusters`), `forwardingConfig` (`targetNameServers`,
`outboundEndpoints`), `peeringConfig` (`targetNetwork`), `dnssecConfig` (`state`, `nonExistence`,
`defaultKeySpecs`), `cloudLoggingConfig`, `reverseLookupConfig`, `serviceDirectoryConfig`, `labels`,
`creationTime`, and `kind`, as far as Google sends them. Members Google does not document are dropped. No
documented member of the zone is deprecated or withheld.

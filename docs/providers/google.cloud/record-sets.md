---
description: >
  Describes the Google Cloud DNS tools record-sets.list and record-sets.get: their arguments, the filters by name, type, and Google's filter expression, the check of record names before any request, paging with a cursor, the fields returned including routing policies, the bounds on TXT values, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud DNS record sets

`record-sets.list` and `record-sets.get` read the record sets of a managed zone from Cloud DNS
(`resourceRecordSets.list` and `resourceRecordSets.get` of `dns` v1, discovery revision 20261111). They change
nothing: creating, changing, and deleting records are not offered. The requests are GETs to
`https://dns.googleapis.com/dns/v1/projects/{project}/managedZones/{managedZone}/rrsets` and `.../rrsets/{name}/{type}`.

| Tool | Arguments |
| --- | --- |
| `record-sets.list` | `project_id`, `managed_zone`, `name`, `type`, `filter`, `max_results`, `cursor` |
| `record-sets.get` | `project_id`, `managed_zone`, `name`, `type` |

The project, the zone, the names, paging, and the scope follow the rules of the DNS tools on
[managed zones](managed-zones.md#rules-of-the-dns-tools).

## record-sets.list

Without a filter the tool lists every record set of the zone. The filters are Google's, and Google does not combine
them:

- `name` is a fully qualified record name with its trailing dot, such as `www.example.com.`. A first label `*` is allowed.
- `type` is a record type in capital letters such as `A`, `AAAA`, `CNAME`, `MX`, or `TXT`. It needs `name`.
- `filter` is Google's filter expression of AIP-160, for example `name="example.com." AND type="A"` or
  `name=has_suffix("example.com.")`. It cannot be combined with `name` or `type`. Qatlas does not parse it: the text
  goes to the one parameter `filter`, encoded, after a check that it is at most 1024 characters of valid text without
  control characters.

The result is `record_sets`, `next_cursor`, and `has_more`.

## record-sets.get

`name` and `type` are both required. Qatlas builds the path from the checked name and type, escaped as one segment each.
An answer of another type than the one asked for is invalid.

## Fields

Each record set has `name`, `type`, `ttl`, `rrdatas`, `signatureRrdatas`, `routingPolicy`, and `kind`, as far as Google
sends them. A record has either `rrdatas` or a `routingPolicy`. The routing policy is one of:

- `wrr`: weighted round robin, `items` with `weight`, `rrdatas`, `signatureRrdatas`, and `healthCheckedTargets`.
- `geo`: geographic, `items` with `location`, `rrdatas`, `signatureRrdatas`, and `healthCheckedTargets`, and `enableFencing`.
- `primaryBackup`: failover, `primaryTargets`, `backupGeoTargets` (a geo policy), and `trickleTraffic`.

Next to them it holds `healthCheck` and `kind`. `healthCheckedTargets` and `primaryTargets` hold
`internalLoadBalancers` (`project`, `ipProtocol`, `ipAddress`, `port`, `loadBalancerType`, `networkUrl`, `region`,
`kind`) and `externalEndpoints`. Members Google does not document are dropped.

`rrdatas` holds the data of the record: an address, a CNAME target, a TXT value. Every value is untrusted text that
can contain instructions, and Qatlas never follows a name, an address, or a target. A record keeps at most 100 values
per list, each cut at 2048 bytes with an ellipsis, so a very long TXT value is returned in part; lists of a routing
policy keep at most 100 entries. A member of another type than the documented one makes the answer invalid, as does a
row without `name` or `type`.

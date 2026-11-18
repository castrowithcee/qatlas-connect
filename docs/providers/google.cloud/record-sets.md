---
description: >
  Describes the Google Cloud DNS tools record-sets.list, record-sets.get, record-sets.create, record-sets.patch, and record-sets.delete: their arguments, the filters by name, type, and Google's filter expression, the check of record names, types, and values before any request, the protection of the SOA record and the NS records of the zone apex, paging with a cursor, the fields returned including routing policies, the bounds on TXT values, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud DNS record sets

`record-sets.list` and `record-sets.get` read the record sets of a managed zone from Cloud DNS
(`resourceRecordSets.list` and `resourceRecordSets.get` of `dns` v1, discovery revision 20261111) and change
nothing. `record-sets.create`, `record-sets.patch`, and `record-sets.delete` change one record set
(`resourceRecordSets.create`, `patch`, and `delete`), each with the confirmation of a person. The requests go to
`https://dns.googleapis.com/dns/v1/projects/{project}/managedZones/{managedZone}/rrsets` and `.../rrsets/{name}/{type}`.
Zones are never created or deleted.

| Tool | Arguments |
| --- | --- |
| `record-sets.list` | `project_id`, `managed_zone`, `name`, `type`, `filter`, `max_results`, `cursor` |
| `record-sets.get` | `project_id`, `managed_zone`, `name`, `type` |
| `record-sets.create` | `project_id`, `managed_zone`, `name`, `type`, `ttl`, `rrdatas`, `routingPolicy`, `client_operation_id` |
| `record-sets.patch` | `project_id`, `managed_zone`, `name`, `type`, `ttl`, `rrdatas`, `routingPolicy`, `expected_ttl`, `expected_rrdatas`, `client_operation_id` |
| `record-sets.delete` | `project_id`, `managed_zone`, `name`, `type`, `expected_ttl`, `expected_rrdatas`, `client_operation_id` |

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

## Changing records

`record-sets.create` creates one record set with either `rrdatas` and `ttl`, or a `routingPolicy`; Google refuses a name
and type that exist already, with a conflict. `record-sets.patch` changes the `ttl`, `rrdatas`, or `routingPolicy` of an
existing record set, at least one: a member that is given replaces the whole member, so `rrdatas` replaces the complete
list of values, and members left out stay. `record-sets.delete` deletes the record set of `name` and `type`. Patch and
delete read the zone and the current record set first and return it as `previous`; a record set that does not exist ends
the call with a not-found failure before anything is changed. The confirmation text is built from the arguments alone
and names the project, the zone, the name, the type, and the new values.

Every value is checked before a secret is read, and a failed check names the rule, never the value:

- The project must lie in the targets of the connection, and the project and the zone come from the arguments alone.
- `name` is a fully qualified name with its trailing dot, `type` one of `A`, `AAAA`, `ALIAS` (a preview type of
  Cloud DNS), `CAA`, `CNAME`, `DNSKEY`, `DS`, `HTTPS`, `IPSECKEY`, `MX`, `NAPTR`, `NS`, `PTR`, `SPF`, `SRV`, `SSHFP`,
  `SVCB`, `TLSA`, and `TXT`. `SOA` is never changed.
- `ttl` is 0 to 2147483647 and is required with `rrdatas`. `rrdatas` holds 1 to 100 values of at most 2048 bytes. `A`
  takes IPv4 and `AAAA` IPv6 addresses, `ALIAS`, `CNAME`, `NS`, and `PTR` fully qualified names with the trailing dot (a `CNAME`
  has exactly one value), `MX` takes `priority name.` (`0 .` is the null MX), `SRV` takes `priority weight port name.`,
  `CAA` takes `flags tag "value"`, and `TXT` and `SPF` take one or more double-quoted strings of at most 255 characters
  separated by spaces, or one unquoted string of at most 255 characters without spaces and quotes; an unquoted value
  with spaces is refused because Google would split it. Google limits the length of a whole TXT record. The other types are held to bounded printable text, and Google checks their syntax.
- A record has either `rrdatas` or a `routingPolicy`. The policy is exactly one of `wrr` (items with `weight` of 0 or
  more), `geo` (items with `location`), or `primaryBackup` (`primaryTargets`, `backupGeoTargets`, `trickleTraffic` 0 to
  1), with the same fields as [below](#fields). The `rrdatas` of its items and the `externalEndpoints` follow the type.
  `kind` and `signatureRrdatas` are set by Google and cannot be written.
- `client_operation_id` is optional, 1 to 64 letters, digits, dots, underscores, or dashes, and goes to Google's
  `clientOperationId`. Repeating a call with the same value lets Google recognise it.

**Zone apex.** The tools never change the `SOA` record and never change the `NS` records of the zone apex, because they
can move the delegation of the zone to foreign name servers. The apex is the `dnsName` of the zone as Google describes
it. `NS` records below the apex, which delegate a subzone, are allowed. Private and public zones are treated alike, and
the result names the `visibility` of the zone; a record in a public zone is visible on the internet. The result of
`create` and `patch` is `record_set` (and `previous` for `patch`), that of `delete` is `deleted` and `previous`.

Records decide where a name resolves and where mail for it goes. Qatlas opens no network access, and what a record makes
reachable is the responsibility of the person who confirms. The request is sent once and never repeated by the tool.
After an unclear outcome (a server error, an answer that cannot be read) the message says so: read the record with
`record-sets.get` before changing it again. A change is applied by Google after the call; `changes.get` reads whether
it is `done`. A conflict (HTTP 409) is reported as such. The account needs `dns.resourceRecordSets.create`,
`dns.resourceRecordSets.update`, or `dns.resourceRecordSets.delete` (the DNS Administrator role has them) and
`dns.managedZones.get`, plus `dns.resourceRecordSets.get` for `patch` and `delete`.

**Expectations.** `record-sets.patch` and `record-sets.delete` take the optional `expected_ttl` and `expected_rrdatas`.
The confirmation names them. After the confirmation the tool reads the record set, and when a given expectation does not
match, it fails with a provider error before any change is sent and nothing is written. `expected_rrdatas` is compared
as a set, because the order of values carries no meaning: another order or a repeated value still matches. For an
exact binding of a deletion to the old record set use [`changes.create`](changes.md#changescreate) with `deletions`,
where Google requires an exact match.

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

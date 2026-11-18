---
description: >
  Describes the Google Cloud DNS tools dns-keys.list and dns-keys.get: their arguments, the digest types, paging, the fields returned for a DNSSEC key, why the public key and digests are not secrets, the empty list of a zone without DNSSEC, the scope and profile, and what is not offered.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Cloud DNS DNSSEC keys

`dns-keys.list` and `dns-keys.get` read the DNSSEC keys of a managed zone from Cloud DNS (`dnsKeys.list` and
`dnsKeys.get` of `dns` v1, discovery revision 20261111). They change nothing: creating keys and changing the DNSSEC
state of a zone are not offered. The requests are GETs to
`https://dns.googleapis.com/dns/v1/projects/{project}/managedZones/{managedZone}/dnsKeys` and `.../dnsKeys/{dnsKeyId}`.

| Tool | Arguments |
| --- | --- |
| `dns-keys.list` | `project_id`, `managed_zone`, `digest_type`, `max_results`, `cursor` |
| `dns-keys.get` | `project_id`, `managed_zone`, `dns_key_id`, `digest_type` |

The project, the zone, paging, and the scope follow the rules of the DNS tools on
[managed zones](managed-zones.md#rules-of-the-dns-tools).

## Arguments

`digest_type` is a comma-separated list of `sha1`, `sha256`, and `sha384`: the digests Google computes for key signing
keys. Without it Google computes the recommended digest type. A cursor belongs to the digest types it was issued for.
`dns_key_id` is the numeric ID of a key as `dns-keys.list` returns it, checked before any request. An answer for another
key than the one asked for is invalid.

## Fields

Each key has `id`, `keyTag`, `algorithm`, `type`, `isActive`, `creationTime`, `publicKey`, `digests`, `keyLength`,
`description`, and `kind`, as far as Google sends them. `type` is `keySigning` or `zoneSigning`. `digests` holds
`digest` and `type` and is computed for key signing keys only. Members Google does not document are dropped, and a
member of another type than the documented one makes the answer invalid, as does a key without `id`.

The public key and the digests are public material of the zone signature, from which the DS record at the parent zone
is built. They are no secret. `description` is untrusted text.

A zone without DNSSEC has no keys: the list is empty and `has_more` is false.

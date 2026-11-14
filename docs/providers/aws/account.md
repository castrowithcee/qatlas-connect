---
description: >
  Describes the Account tools for regions: listing the regions of the bound account with their opt-in status, reading one status, enabling and disabling opt-in regions, the account argument, and how opt-in relates to the region list of a connection.
type: knowledge
edit: shared
created: 2026-11-14
updated: 2026-11-14
---

# Account regions

Group `account`. The tools read and change which regions the bound account has opted in to. Account is a global
service: the calls go to `account.us-east-1.amazonaws.com` and ignore the region list of the connection, so the tools
take no `region` argument and a connection needs only its account target. The permissions are listed in
[Set up AWS](setup.md#account). Reads need no confirmation; enable and disable need confirmation.

## Tools

| Tool | AWS operation | Effect | Confirmation |
| --- | --- | --- | --- |
| `aws.account.region.list` | `ListRegions` | read | no |
| `aws.account.region.get-opt-status` | `GetRegionOptStatus` | read | no |
| `aws.account.region.enable` | `EnableRegion` | update | yes |
| `aws.account.region.disable` | `DisableRegion` | update | yes |

`region_opt_status` is one of `ENABLED_BY_DEFAULT`, `ENABLED`, `ENABLING`, `DISABLING`, or `DISABLED`. `list` takes
the optional `region_opt_status_contains` (up to five of these values) and pages by cursor with `page_size` from 1 to
50. `get-opt-status`, `enable`, and `disable` take `region_name`, a region of the `aws` partition by form; other
partitions and malformed names are refused before any request.

## The account argument

Every tool accepts an optional `account_id`. It must be the account the connection is bound to; any other value is
refused before any request. AWS accepts the argument only for management and member accounts of an organization, and
without it the call acts on the bound account.

## Opt-in and the region list

Enabling or disabling a region changes the account, not the connection. The tools never change the region list, so
a regional tool uses a newly enabled region only after you add `region/<name>` to the targets of the connection. The
result of `enable` and `disable` carries `allowlist_note`, which says this, and for `disable` whether the region is
still in the list.

The opt-in takes a while: the status goes through `ENABLING` to `ENABLED`, or through `DISABLING` to `DISABLED`.
Read it with `get-opt-status`. Both calls are repeatable; AWS answers a change that is already under way with a
conflict error. Disabling a region removes access to the region and its resources; the tool removes nothing itself.

The change of the root user email address (`StartPrimaryEmailUpdate`, `AcceptPrimaryEmailUpdate`) is not a tool.
Region names in answers come from AWS, are untrusted, and are shortened to a fixed length.

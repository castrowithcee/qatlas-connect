---
description: >
  Describes the region tools of the Google Merchant provider: listing and reading regions, creating, updating, and deleting them, singly or in batches of up to 100 in one request, through the tools list of a connection, the region ID form, the three kinds of area and their bounds, the update mask allowlist, administrator role, confirmation, delayed effect, error and unknown-outcome handling, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-21
---

# Google Merchant: regions

The region tools of the [Google Merchant provider](README.md). The account argument, targets, quotas, cursors, and
errors are described there.

A region is a named area, made of postal codes, predefined geotargets, or a radius, that shipping settings and
regional inventory refer to. Changing one changes where offers are shown and shipped.

| Tool | Does |
| --- | --- |
| `regions.list` | reads the regions of an account, a batch per call with `cursor` |
| `regions.get` | reads one region |
| `regions.create` | creates a region with exactly one area |
| `regions.update` | replaces the display name or the area of a region |
| `regions.delete` | deletes one region |
| `regions.batch-create` | creates 1 to 100 regions in one request |
| `regions.batch-update` | replaces members of 1 to 100 regions in one request |
| `regions.batch-delete` | deletes 1 to 100 regions in one request |

`regions.list` and `regions.get` are in the `read` profile. The six changing tools are in no profile: a connection
offers them only when its `tools` list names them, and every call needs a confirmation that shows the account, the
region ID or the number and IDs of the regions, and what is created, replaced, or deleted. `account` is checked against the targets before any secret is read; a sub-account of a bound advanced
account is proven by one read of the sub-accounts before the region is read or changed, and an account outside the
targets is refused without naming it and without any further request.

**Changing regions needs the administrator role** in the Merchant Center account. Google refuses the call
otherwise, and Qatlas reports it as `permission`. It cannot check the role beforehand.

## Region ID

`region_id` is 1 to 64 ASCII letters, digits, dashes, and underscores, starting with a letter or digit. It is checked
before any secret is read. Google documents no form for it (the reference says only that it is unique over the
regions of the account), so this is Qatlas' narrow form, chosen so that an ID can never alter a path or a query:
no `/`, no `.`, no space. A region whose ID has another form, for example one with a space, can be listed but not
read, created, or updated through these tools. The path of every request is built from the fixed sub-API path, the
checked account ID, and the checked region ID; `regionId` of `create` travels as an encoded query value.

## Reading

`regions.list` takes `account`, `page_size` (1 to 1000), and `cursor`. Without a page size Google sends up to 50.
A cursor is tied to the tool, the account, the page size, and the targets. `regions.get` takes `account` and
`region_id`.

A region shows `name`, `displayName`, one of `postalCodeArea` (`regionCode`, `postalCodes` with `begin` and `end`),
`geotargetArea` (`geotargetCriteriaIds`), or `radiusArea` (`radius`, `latLng`, `regionCode`, `radiusUnits`), and
the output-only `shippingEligible` and `regionalInventoryEligible`. The display name comes from whoever runs the
account and is untrusted: it is cut at 300 characters, a text of an area at 100, a list of postal code ranges at
250 entries, a list of geotarget IDs at 100, and a list of regions holds at most 1000 entries. A member that is not
documented is dropped, and so is a value of the wrong type.

## Creating and updating

Both tools take structured arguments only and build the body themselves; nothing is passed through.

- `postal_code_area`: `regionCode` (two upper case letters) and 1 to 250 `postalCodes`, each with `begin` and an
  optional `end`. A code is up to 20 letters, digits, spaces, and dashes, or a pattern of the same followed by `*`
  (`94108`, `9410*`, `9*`). Google requires `end` to be a pattern exactly when `begin` is one, with the same prefix
  length; Qatlas checks the first and leaves the length to Google.
- `geotarget_area`: 1 to 100 distinct `geotargetCriteriaIds` as decimal strings. Google requires one location type
  and offers geotargets only in some countries; Google checks both.
- `radius_area`: `radius` from 0.01 to 100000, `latLng` (latitude -90 to 90, longitude -180 to 180), `regionCode`, and
  optional `radiusUnits` `MILES` or `KILOMETERS`.
- `display_name`: 1 to 200 characters without control characters.

The numeric caps and the shapes of postal codes are Qatlas' own bounds: the discovery document gives none of them.
`create` takes `region_id`, an optional `display_name`, and exactly one area. A repeated ID is refused by Google.

`update` takes `update_mask`, a list of the paths `displayName`, `postalCodeArea`, `geotargetArea`, and `radiusArea`,
each at most once. This list is the whole allowlist; no other path, no sub-path, no comma-separated text, and no
output-only member is accepted. Each path replaces the whole member. `displayName` needs `display_name`. Because a
region has exactly one area, a mask that names an area path needs exactly one area block, and that block must be named
in the mask; to change the kind of area, name the old area and the new one and give the new block, which clears the
old one. A mask without an area path leaves the area untouched. Nothing else can be cleared.

## Deleting

`regions.delete` takes `account` and `region_id` and sends one `DELETE` of that region. Deleting a region removes its
definition from the account. Shipping settings and regional inventory that use the region can be affected. Neither
the discovery document nor the guides say what happens to them, and neither says that a deleted region can be
restored, so treat a deletion as permanent; Qatlas offers no undo. The batching guide says that deleting a region that
does not exist is no error. The request is sent once and never repeated; a failure that leaves the outcome open says
so, and the region is then read before anything is done again.

## Batches

`regions.batch-create` and `regions.batch-update` take `account` and `regions`, a list of 1 to 100 entries. An entry
has the arguments of `regions.create` (`region_id`, optional `display_name`, one area block) or `regions.update`
(`region_id`, `update_mask`, and the members it names), with the same bounds and the same mask allowlist.
`regions.batch-delete` takes `account` and `region_ids`, 1 to 100 IDs of the form of `region_id`.

**A batch is exactly one request.** Whatever the number of entries, Qatlas sends one `POST` to
`regions:batchCreate`, `regions:batchUpdate`, or `regions:batchDelete` and never splits, retries, or repeats it. The
source review found that the three batch methods count as one call in the quota; the quota documentation of the
guide names only the quota groups of the single methods (`regions.create`, `regions.update`, `regions.delete`) and
gives no number, so the count is not verified here. A tool test pins the request count: a batch of 100 entries
produces one request.

Everything is checked before any secret is read or any request is made: the account, every region ID, and every entry
in full, with the rules of the single tools. One invalid entry refuses the whole call, and the error names the
index of the entry without quoting it. A region ID may appear only once in a batch, compared without regard to case
because Google documents no rule for the case of an ID. A list of more than 100 entries or an empty one is refused.
Google applies a batch all or none, according to the guide: if one operation fails, the whole batch fails and nothing
is changed.

The body is built from the checked arguments only. For create each request holds `parent` (the checked account,
`accounts/ID`), `regionId`, and `region`. For update each holds `region` (with `name` set to the checked ID) and the
`updateMask` of the allowlist paths. For delete each holds `name`. The result of a create or update is `accepted`,
`requested` (the number of entries sent), and `regions` as Google returned them, at most 100, each cut like a read
region. The result of a delete is `accepted`, `requested`, and `names` (`accounts/ACCOUNT/regions/REGION` of the
request). A member of an answer that is not documented is dropped.

## Delay and result

All changing tools send exactly one request and are never repeated. The result says only that Google accepted the request
(`accepted` and, for create and update, the region as Google returned it). The region, and the shipping settings and regional inventory that
use it, can take some minutes to follow; read the region again later to see it.

A failure that leaves the outcome open (no answer, a timeout, a `5xx`, or an unreadable answer) says that the outcome of
the change is unknown, so read the regions before changing them again. `429` is `rate-limited` and `403` is
`permission`; neither is repeated.

## Checked and not verified

Checked against the discovery document of the accounts API v1 (revision 20261118) and the region guide: `regions.list`
returns 50 regions by default and at most 1000 (the findings of the source review gave no figure), `regionId` is a
query parameter of `create` without a documented form, `update_mask` is a comma-separated field mask with
`displayName`, `postalCodeArea`, `geotargetArea`, and `radiusArea` as the members that can be written, and create,
update, delete, and the three batch methods need the administrator role. The discovery document (revision 20261118)
and the batching guide both give 100 as the maximum of regions in a batch; the batch tools use it as their cap. The
batch methods answer with `regions` (create, update) or an empty object (delete) according to the discovery document.

Not verified:

- No live call was made.
- A documented range for the radius, a limit on postal code ranges or geotargets, and a delay before a region is
  usable. The discovery document and the guide state none.
- Whether Google accepts a field mask with sub-paths such as `postalCodeArea.regionCode`; Qatlas offers only the four
  paths of whole members.
- Whether Google rejects a region that ends with two areas or none; Qatlas prevents both before the call.
- The form of the region name in a batch. The batch tools send `name` as `accounts/ACCOUNT/regions/REGION` and the
  `parent` of a create request as `accounts/ACCOUNT`, as the discovery document describes them, built from the checked
  account and IDs. The batching guide shows shorter examples; which forms Google also accepts was not tried live.
- The batch error form: neither source describes which entry a failed batch names. The guide gives no number for the
  quota cost of a batch.
- That deleting a region does not fail when it does not exist, and what a deletion does to shipping settings and
  regional inventory (the guide says the first for batches only, and nothing on the second).

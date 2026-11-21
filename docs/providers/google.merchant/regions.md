---
description: >
  Describes the region tools of the Google Merchant provider: listing and reading regions, creating and updating them through the tools list of a connection, the region ID form, the three kinds of area and their bounds, the update mask allowlist, administrator role, confirmation, delayed effect, error and unknown-outcome handling, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
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

`regions.list` and `regions.get` are in the `read` profile. `regions.create` and `regions.update` are in no profile:
a connection offers them only when its `tools` list names them, and every call needs a confirmation that shows the
account, the region ID, and what is created or replaced. Deleting regions and the batch methods are not part of
these tools. `account` is checked against the targets before any secret is read; a sub-account of a bound advanced
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

## Delay and result

Both tools send exactly one request and are never repeated. The result says only that Google accepted the request
(`accepted` and the region as Google returned it). The region, and the shipping settings and regional inventory that
use it, can take some minutes to follow; read the region again later to see it.

A failure that leaves the outcome open (no answer, a timeout, a `5xx`, or an unreadable answer) says that the outcome of
the change is unknown, so read the region before changing it again. `429` is `rate-limited` and `403` is
`permission`; neither is repeated.

## Checked and not verified

Checked against the discovery document of the accounts API v1 (revision 20261118) and the region guide: `regions.list`
returns 50 regions by default and at most 1000 (the findings of the source review gave no figure), `regionId` is a
query parameter of `create` without a documented form, `update_mask` is a comma-separated field mask with
`displayName`, `postalCodeArea`, `geotargetArea`, and `radiusArea` as the members that can be written, and create,
update, and delete need the administrator role. The guide documents batches of up to 100 regions; they are not offered.

Not verified:

- No live call was made.
- A documented range for the radius, a limit on postal code ranges or geotargets, and a delay before a region is
  usable. The discovery document and the guide state none.
- Whether Google accepts a field mask with sub-paths such as `postalCodeArea.regionCode`; Qatlas offers only the four
  paths of whole members.
- Whether Google rejects a region that ends with two areas or none; Qatlas prevents both before the call.

---
description: >
  Describes the lodging tools of the Google Business Profile provider: reading the lodging data of a location with a required read mask, reading what Google changed with its diff mask, and changing it with the required update mask and previous values, the Google methods behind them, how the groups map to arguments, which fields are left out, the confirmation and its length limit, and how targets narrow them.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: lodging

The lodging tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The lodging data of a location are the amenities, rooms, times,
policies, and services of a business with accommodations. They are public on Google.

All three tools use the My Business Lodging API, `https://mybusinesslodging.googleapis.com`, which is enabled in
the Cloud project on its own. A location without lodging data answers `not-found`.

## Fields and masks

The lodging has one group per top-level field of the API: for example `pools`, `parking`, `policies`,
`services`, and `guestUnits`. Each group is a typed object whose members are the fields of the API with its
names, booleans, enumerations, whole numbers, and, for times and units, small objects and lists. The input and
output schemas of each tool list every field; the tool descriptions carry the rest. A field Google holds nothing
for, an empty group, a value of the wrong type, and a field the API does not document are left out of a result.
Texts are cut, see [Results](README.md#results); the label of a guest unit at 200 characters and a unit code at
100.

A mask names fields as dotted paths of the result, such as `pools.hotTub`, or a whole group such as `pools`. A
list, such as `guestUnits` or `services.languagesSpoken`, is one field: it is read and replaced as a whole. A
mask has no wildcard, names no path twice, and names no path inside another. Every path is checked against the
fields of the API before anything is sent.

Left out on purpose: the deprecated sustainability certifications. `allUnits`, `someUnits`, and the
Google-assessed `greenBuildingDesign` are answered but cannot be changed. The location name of the API is
`locations/LOCATION/lodging` and comes from `location`.

## google.business-profile.lodging.get

`GET https://mybusinesslodging.googleapis.com/v1/{location}/lodging?readMask=...`. Reads the lodging with the
fields `read_mask` names, at least one. A connection bound to a location may leave `location` out, and then it
must be that location. A connection bound to an account first proves that the location is in the account's
location list, see [Target](README.md#target).

## google.business-profile.lodging.get-google-updated

`GET https://mybusinesslodging.googleapis.com/v1/{location}/lodging:getGoogleUpdated?readMask=...`. Reads the
lodging as Google updated it, with `lodging` and `diffMask`, the comma-separated paths Google changed. Arguments,
target, and results are those of `lodging.get`. Only paths the lodging has stay in `diffMask`; Google names a
repeated field as a whole there.

## google.business-profile.lodging.update

`PATCH https://mybusinesslodging.googleapis.com/v1/{location}/lodging?updateMask=...`. Changes the named
fields. It needs confirmation and belongs to the profile `write`.

- `update_mask` is required, with at most 20 paths. It cannot name `metadata`, `name`, or a field Google only
  answers. A value is accepted only inside a named path. A named path without a value clears that field.
- Each group argument holds the new values. `metadata.updateTime` is the time the values are asserted true, in
  RFC 3339 form; the tool sends the current time when it is left out.
- `previous_values` is required, in the shape of the groups, `{}` if none of the named fields is set. It is
  only compared and never sent. The tool first reads the named fields with `updateMask` as the read mask and
  changes nothing unless Google holds exactly these values, and it changes nothing when every named field
  already has its new value. Read the fields with `lodging.get` and send exactly what it answers.
- The call returns the lodging Google answers.

The confirmation names the location, every changed field, and its old and new value in full, `unset` for a
field without a value. A text that exceeds the 500 characters a confirmation keeps is not cut: the confirmation
says that the change is refused, and the call is refused before anything is sent. Change fewer fields per call.

A change counts against the 10 changes per minute and profile of the location, see
[account admins](account-admins.md). An account target proves the location first; a location target accepts
only its location.

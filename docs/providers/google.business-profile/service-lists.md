---
description: >
  Describes the service list tools of the Google Business Profile provider: reading the services of a location with prices and descriptions, replacing the list as a whole with a required update mask and the previous state read first, the confirmation with the number of services before and after and the changed services within 500 characters, the bounds on untrusted service text, and what the v4 documentation leaves unverified.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: service lists

The service list tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The tools use the My Business API v4 at
`https://mybusiness.googleapis.com/v4`, like the [review tools](reviews.md#the-v4-client). The parameters and
answer members come from the reference at `developers.google.com/my-business/reference/rest/v4` (the methods
`accounts.locations.getServiceList` and `accounts.locations.updateServiceList` and the ServiceList resource with
its nested types), read on 2026-11-18; no live call was made.

The services are **public**: names, descriptions, and prices appear on the Google profile of the business.

## The v4 paths

The tools take the location as `accounts/ACCOUNT/locations/LOCATION` and add `/serviceList` themselves. They check
the path against its form and the target before a secret is read, and prove the location for an account target,
exactly as the review tools do: see [the v4 client](reviews.md#the-v4-client). A refusal never names the other
object. The tools are offered on every target.

## Reading

`service-lists.get` belongs to the profile `read` and is safe to repeat. Its only argument is `location`
(required, `accounts/ACCOUNT/locations/LOCATION`); the method has no query parameters.

The result holds `name` and `serviceItems`. A service has `isOffered`, `price` (`currencyCode`, `units` as a
string, `nanos`), and exactly one of `structuredServiceItem` (`serviceTypeId`, `description`) and
`freeFormServiceItem` (`categoryId`, `label` with `displayName`, `description`, `languageCode`). A location
without services answers with `name` only, and the result then has no `serviceItems`.

### Untrusted text

Names, descriptions, and every other text of a service are written by whoever edits the profile. They are
returned unchanged as data: texts are cut at 4000 characters (codes and identifiers at 100) and lists at 250
entries, members the reference does not list are dropped, and nothing in a result is followed.

## Replacing

`service-lists.update` (`PATCH .../serviceList?updateMask=serviceItems`) replaces the service list of a location
as a whole. It belongs to the profile `write` and needs confirmation. Google replaces a repeated field only as a
whole, so `service_items` is the complete new list, and every service that is not in it is removed.

| Argument | Meaning |
| --- | --- |
| `location` | required, the location path |
| `service_items` | required, the complete new list of 1 to 250 services, with the members of the `get` result |
| `previous_service_items` | required, `serviceItems` of `service-lists.get` as last read, `[]` for a location without services; only compared, never sent |
| `update_mask` | required, exactly `["serviceItems"]` |

The reference does not say which fields a mask may name. `name` only identifies the resource, so `serviceItems`
is the one field the tool lets a mask name. A missing mask is refused, since Google would treat it as a full
update that can clear fields. The tool never clears the list: an empty list is refused.

Before anything is sent the tool refuses what Google documents as incomplete: a service needs exactly one of
`structuredServiceItem` and `freeFormServiceItem`, a structured one a `serviceTypeId`, a free-form one a
`categoryId` and a `label` with a `displayName`. `isOffered` and `price` are optional. A `displayName` has at most
140 characters and stays on one line, a structured `description` has at most 300, a label `description` at most
250 (the reference recommends these lengths for labels). Control, invisible formatting, and separator characters
are refused. A list of more than 250 entries, a text over its bound, and a body of more than 1 MiB are refused
and never cut.

### Read first

No update runs without the state it replaces. After the checks, and after the proof of the location for an
account target, the tool reads the list again and sends nothing unless it equals `previous_service_items`
exactly. Otherwise it answers `invalid-argument` and asks for a new `service-lists.get`. A live list that the tool
itself would cut (over 250 entries or a text over its bound) cannot be counted or compared, so it is not
replaced from here and has to be changed in Google's own interface.

### Confirmation

The confirmation names the location, the number of services before and after, and the name of every service that
is added, changed, or removed (a structured service by its `serviceTypeId`, a free-form one by its label), and
says when only the order changes:

```text
Replace the public service list of accounts/1000001/locations/2000002: services 3 -> 3;
added: service "Brake check"; changed: service "Tune up"; removed: service "Wheel truing"
```

A service is the same before and after when its position among services of the same name and its content match;
a renamed service shows as removed and added. A call that changes nothing is refused. The core cuts a
confirmation at 500 characters, which would hide what a person confirms, so an update whose confirmation does
not fit is refused before anything is sent. Change fewer services per call or give them shorter names.

The result is the list as Google holds it after the change. A change counts against the 10 changes per minute and
profile; Qatlas spaces them by 6 seconds per location, like the other changes. A change whose outcome is unclear
(timeout, `5xx`, unreadable answer) is reported as such and never repeated: read the list with
`service-lists.get` before changing it again.

## Not verified

- No live call was made. The reference states no limit for the number of services, `serviceTypeId`, or
  `categoryId`: Qatlas holds 250 entries per list, 100 characters per identifier, and 1 MiB per update.
- The reference does not list the fields an `updateMask` may name: Qatlas allows `serviceItems` only.
- The reference does not say whether `updateMask=serviceItems` needs `name` in the body: Qatlas sends it.
- The reference names the `freeFormServiceItem` member `categoryId`, which the older Location resource calls
  `category`; the service list tools use `categoryId` and refuse `category`.
- `locationState.canModifyServiceList` is not read: Google answers an ineligible location with an error.

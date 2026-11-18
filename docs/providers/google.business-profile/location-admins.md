---
description: >
  Describes the location admin tools of the Google Business Profile provider: listing the admins of a location with their role and pending invitations, changing the role of an existing admin or removing one with the previous name and role as required comparison arguments, the Google methods behind them, the roles that are allowed, what is refused, the personal data, and how account and location targets narrow them.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: location admins

The location admin tools of the [Google Business Profile provider](README.md). Credential, target, approval,
quotas, bounds, and errors are described there. They work like the [account admin tools](account-admins.md) and
return the same members.

## google.business-profile.location-admins.list

`GET https://mybusinessaccountmanagement.googleapis.com/v1/locations/{location}/admins`. Lists the admins of one
location, without paging and at most 250 entries. `location` is optional on a connection bound to a location,
and then it must be that location. On a connection bound to an account the location is first proved to belong to
it, like in [`locations.get`](locations.md).

`admin` is the first and last name of the person, or the email address of the invitee while an invitation is
pending. These are personal data of other people, and the tool is part of the profile `read`.

## google.business-profile.location-admins.update

`PATCH https://mybusinessaccountmanagement.googleapis.com/v1/locations/{location}/admins/{admin}?updateMask=role`.
Changes the role of an existing admin of a location. It needs confirmation and is part of the profile `write`.

Arguments, refusals, roles, and the confirmation are those of
[`account-admins.update`](account-admins.md), with the path form
`locations/LOCATION/admins/ADMIN`. An account target proves the location first, then reads the list, then sends
the change. A location target accepts only admins of that location. The change is counted per location, 10 per
minute.

## google.business-profile.location-admins.delete

`DELETE https://mybusinessaccountmanagement.googleapis.com/v1/locations/{location}/admins/{admin}`. Removes an
admin from a location, or revokes the invitation of an admin that is still pending. It needs confirmation and is
part of the profile `write`.

Arguments, refusals (an admin who holds `PRIMARY_OWNER`, an entry that differs from `previous_admin` and
`previous_role`), the confirmation, and the result are those of
[`account-admins.delete`](account-admins.md#googlebusinessprofileaccount-adminsdelete), with the path form
`locations/LOCATION/admins/ADMIN`. An account target proves the location first, then reads the list, then sends
the delete. A location target accepts only admins of that location. The delete is counted per location, 10 changes per minute.

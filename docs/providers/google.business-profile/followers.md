---
description: >
  Describes the followers tool of the Google Business Profile provider: reading the number of followers of one location through the My Business API v4, the v4 location path and how targets bind it, the answer members, and what the v4 documentation leaves unverified.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: followers

The followers tool of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. The tool uses the My Business API v4 at
`https://mybusiness.googleapis.com/v4`, which has to be enabled in the Cloud project like the other APIs. Its
parameters and answer members come from the reference at
`developers.google.com/my-business/reference/rest/v4/accounts.locations.followers/getMetadata` (the method
`getMetadata` and the `FollowersMetadata` resource), read on 2026-11-18; no live call was made. The v4 client,
the location path, and the binding are those of the [review tools](reviews.md#the-v4-client). Google documents
the method only for a location that is verified or connected to Maps.

## Reading

`followers.get` is part of the profile `read` and is safe to repeat. It takes one argument:

- `location`: required, as `accounts/ACCOUNT/locations/LOCATION` with decimal IDs. On a connection bound to an
  account the account must be that account and the location is first proved to be in its location list. On a
  connection bound to a location only the location ID is compared.

The path is checked against its form and the target before a secret is read. A refusal never names the other
object. The result holds `name` (`accounts/ACCOUNT/locations/LOCATION/followers/metadata`) and `count`, the
total number of followers. Google writes `count` as an int64, so it is returned as a decimal string.

## Not verified

- No live call was made. The reference names the error `PRECONDITION_FAILED` for a location that is neither
  verified nor connected to Maps. Qatlas maps it to the same classes as the other v4 errors.

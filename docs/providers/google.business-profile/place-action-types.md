---
description: >
  Describes the place action type tool of the Google Business Profile provider: listing the types of place action links Google offers, optionally for a location or a region and in a language, the Google method behind it, the filter forms that are accepted, paging, and how a target narrows it.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: place action types

The place action type tool of the [Google Business Profile provider](README.md). Credential, target, approval,
quotas, bounds, and errors are described there. The types are the values of `place_action_type` of the
[place action links](place-action-links.md).

## google.business-profile.place-action-types.list

`GET https://mybusinessplaceactions.googleapis.com/v1/placeActionTypeMetadata`. Lists the types of place action
links with their display name. It is part of the profile `read` and changes nothing.

| Argument | Meaning |
| --- | --- |
| `language_code` | optional, a BCP-47 code such as `de` or `pt-BR`; Google answers in English when it has no names for the language |
| `filter` | optional, `location=locations/LOCATION` for the types available at a location, or `region_code=CODE` for a CLDR region code such as `DE` or `419`; the only filters Google supports |
| `page_size` | optional, 1 to 100; Google's default is 10 |
| `cursor` | optional, `next_cursor` of the previous call with the same arguments |

Each entry holds `displayName` and `placeActionType`. A cursor belongs to the arguments and the target of the call
that issued it; Qatlas refuses it with other arguments. An empty list is left out of the result. The display
name is untrusted text and is cut at 500 characters.

## Targets

The list without a filter or with `region_code` names no location, so every connection may read it. With
`location=`, a connection bound to a location accepts only that location, and a connection bound to an account
first proves that the location belongs to it, like [`locations.get`](locations.md).

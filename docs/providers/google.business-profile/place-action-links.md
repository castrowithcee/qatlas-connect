---
description: >
  Describes the place action link tools of the Google Business Profile provider: listing, reading, creating, changing, and deleting the public links of a location (ordering, booking, reserving), the Google methods behind them, the arguments, the rules for the link address, the confirmation text and its length limit, the update mask rule, how targets narrow them, and the bounds of the results.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: place action links

The place action link tools of the [Google Business Profile provider](README.md). Credential, target, approval,
quotas, bounds, and errors are described there. The available link types are read with
[`place-action-types.list`](place-action-types.md).

A place action link is a public redirect: the Google profile of a location shows a button (order food, book an
appointment, reserve a table) that leads to an address outside Google. Whoever can set the address can send the
customers of the business to another site, so every change is confirmed by a person, and Qatlas never fetches
the address. The tools use the Google My Business Place Actions API v1 at
`https://mybusinessplaceactions.googleapis.com`, which is enabled in the Cloud project on its own.

| Tool | Google method | Profile |
| --- | --- | --- |
| `place-action-links.list` | `GET /v1/locations/{location}/placeActionLinks` | `read` |
| `place-action-links.get` | `GET /v1/locations/{location}/placeActionLinks/{link}` | `read` |
| `place-action-links.create` | `POST /v1/locations/{location}/placeActionLinks` | `write` |
| `place-action-links.update` | `PATCH /v1/locations/{location}/placeActionLinks/{link}` | `write` |
| `place-action-links.delete` | `DELETE /v1/locations/{location}/placeActionLinks/{link}` | `write` |

The tools that change something need confirmation. They are not part of the profile `read`: a link is visible on
the public profile, and it is no outbound access of Qatlas, so it is a profile change that a person confirms.

## Link path and location

`link` is `locations/LOCATION/placeActionLinks/LINK`, with a decimal location ID and a link ID of letters,
digits, `_`, and `-`, as `list` shows it in `name`. A path is checked against this form and against the target
before a secret is read; it is never passed on as given. A connection bound to a location accepts only links of
that location. A connection bound to an account first proves that the location belongs to it, like
[`locations.get`](locations.md); a location outside the account is refused without naming it.

## google.business-profile.place-action-links.list

Lists the links of one location, 10 per call unless `page_size` (1 to 100) says otherwise, with `next_cursor` to
read on.

| Argument | Meaning |
| --- | --- |
| `location` | `locations/LOCATION`; optional on a connection bound to a location |
| `filter` | optional, `place_action_type=TYPE`, the only filter Google supports |
| `page_size`, `cursor` | optional |

An empty list is left out of the result.

## google.business-profile.place-action-links.get

Reads one link by `link`.

## Result members

Every documented member is returned, the output-only ones included: `name`, `uri`, `placeActionType`
(`APPOINTMENT`, `ONLINE_APPOINTMENT`, `DINING_RESERVATION`, `FOOD_ORDERING`, `FOOD_DELIVERY`, `FOOD_TAKEOUT`,
`SHOP_ONLINE`, `SOLOPRENEUR_APPOINTMENT`), `providerType` (`MERCHANT` for a link of the business or its agency,
`AGGREGATOR_3P` for a partner), `isEditable`, `isDisabled`, `isPreferred`, `createTime`, and `updateTime`.
The address and the other texts are untrusted: they come from whoever edits the profile or from a partner. Texts
are cut like the other results of the provider (the address at 2048 characters, a cut ends with `…`), and the
address is text that is never fetched.

## google.business-profile.place-action-links.create

Creates a link at a location. Google allows one link per address, type, and location.

| Argument | Meaning |
| --- | --- |
| `location` | required, `locations/LOCATION`; on a connection bound to a location it must be that location |
| `uri` | required, the address |
| `place_action_type` | required, one of the types above except the unspecified one |
| `is_preferred` | optional, marks the link as the preferred one of its type; a preferred link of the same type loses the mark |
| `is_disabled` | optional, Google accepts `true` only for a link of a partner |

The body sent to Google holds only the given members.

## google.business-profile.place-action-links.update

Changes fields of a link.

| Argument | Meaning |
| --- | --- |
| `link` | required |
| `update_mask` | required, the fields to change: `uri`, `place_action_type`, `is_preferred`, `is_disabled` |
| `uri`, `place_action_type`, `is_preferred`, `is_disabled` | the new values, as in `create` |

`update_mask` must name exactly the fields that are given with a value. A field in the mask without a value would
be cleared by Google, and a value outside the mask would be ignored, so both are refused before anything is
sent. The mask is sent sorted, in the spelling Google documents. The tool does not read the link before it
changes it; the confirmation shows the new values only.

## google.business-profile.place-action-links.delete

Deletes a link, which disappears from the public profile. The result is `deleted: true` and the `link`.

## Address of a link

The address is checked before anything is sent, and a refusal names the rule, never the address:

- it starts with `https://` in lower case, has a host, and has no user information (`user@` or `user:password@`),
  which could disguise the real host;
- it is ASCII, so no look-alike characters hide in it; other characters are percent-encoded;
- it has no space, control, format, or line-separator character and no backslash;
- it is at most 2000 bytes.

The address is never fetched, opened, or resolved by Qatlas.

## Confirmation

Each change needs confirmation, and the confirmation shows what is changed in full text:

| Tool | The confirmation shows |
| --- | --- |
| `create` | the location, the type, the whole address, and `is_preferred` and `is_disabled` when given |
| `update` | the link (which holds the location) and each changed field with its new value, the whole address included |
| `delete` | the link (which holds the location) |

The confirmation text of the core is at most 500 characters. When the text of a change does not fit, Qatlas does
not show a cut address: the confirmation says `Refused before sending`, and the tool refuses the call with a
message that names the limit, before anything is sent. A shorter address (for example without tracking
parameters) fits. A call that the checks refuse shows the same refusal as its confirmation text.

## Changes per minute

Google counts at most 10 changes per minute and profile. Create, update, and delete of a location are spaced like
the other changes of that location, see [account admins](account-admins.md). A change is sent once and never
repeated; where its outcome is unclear, the message says so and the link should be read before it is changed
again.

## Targets

| Target | Behavior |
| --- | --- |
| none | every location the consenting account manages |
| account | the location is proved to belong to the account before each call |
| location | only that location's links; a link of another location is refused |

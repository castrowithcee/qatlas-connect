---
description: >
  Describes the attribute tools of the Google Business Profile provider: listing which attributes a location can have, reading the attributes of a location, setting and removing them with the required attribute mask and the previous values compared before the change, the Google methods behind them, value types, addresses, deprecated attributes, the confirmation, and how targets narrow them.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: attributes

The attribute tools of the [Google Business Profile provider](README.md). Credential, target, approval, quotas,
bounds, and errors are described there. Attributes are public profile data: amenities, payment types, and links
such as a booking or menu address.

## google.business-profile.attributes.list

`GET https://mybusinessbusinessinformation.googleapis.com/v1/attributes`. Lists which attributes a location can
have. Every entry names the attribute (`parent`, the name `attributes.update` takes as `attributes/ATTRIBUTE`),
its `valueType` (`BOOL`, `ENUM`, `URL`, or `REPEATED_ENUM`), the possible values, `repeatable`, the group, and
`deprecated`.

- `location` names the location whose attributes to list. Without it, `region_code` (ISO 3166-1 alpha-2 in
  capitals, such as `CH`) is required, and `category_name` (`categories/CATEGORY`) narrows the list.
- `show_all` lists every attribute and makes Google ignore `location` and `category_name`; `region_code` is
  still required without a location.
- `language_code` chooses the language of the display names.
- `page_size` asks for 1 to 200 entries, Google's default is 200. `next_cursor` reads on and is bound to the
  location, category, region, language, and `show_all`.

On a connection bound to an account, `location` is first proved to belong to it, like in
[`locations.get`](locations.md). On a connection bound to a location it must be that location. The listing
without a location is not tied to any location.

## google.business-profile.attributes.get

`GET https://mybusinessbusinessinformation.googleapis.com/v1/{location}/attributes`. Reads the attributes set on
one location: `values` (booleans or enumeration values), `uriValues`, and `repeatedEnumValue` with `setValues`
and `unsetValues`. At most 250 attributes are shown. Targets work like those of
[`locations.get`](locations.md).

## google.business-profile.attributes.update

`PATCH https://mybusinessbusinessinformation.googleapis.com/v1/{location}/attributes?attributeMask=...`. Sets
and removes attributes. It needs confirmation and is part of the profile `write`. Google counts the change
against the 10 changes per minute and profile of the location, see [account admins](account-admins.md).

- `attribute_mask` is required and names each changed attribute once as `attributes/ATTRIBUTE`, with letters,
  digits, and `_`. Every attribute in `attributes` is in it. A name in the mask without an entry in
  `attributes` removes that attribute.
- `attributes` holds the new attributes: `name` and one of `values`, `uriValues`, or `repeatedEnumValue`. Send
  `[]` when the change only removes attributes.
- `previous_attributes` is required and only compared, never sent: the attributes of the mask as last read with
  `attributes.get`, none for an attribute that is not set. The tool reads the location's attributes first and
  sends nothing unless they are exactly that. A change that leaves every attribute as it is, is refused.

Before the change the tool checks each attribute of the mask against the metadata Google lists for the location
(at most 2000 attributes are searched). An attribute that is not offered for the location, or is deprecated, is
refused, because Google does not save a change to a deprecated attribute. The value has to fit the value type:
one boolean for `BOOL`, one listed value for `ENUM`, one or more addresses for `URL`, and for `REPEATED_ENUM` a
`repeatedEnumValue` with listed values that are not both set and unset. More than one value of `BOOL` or `ENUM`
is accepted only for a repeatable attribute.

### Addresses

An address becomes a link on the public profile, so one inserted by mistake or by an untrusted text would be
shown to customers. An address must be `http` or `https`, printable ASCII without spaces or user info, and at
most 2048 characters; write other characters percent-encoded or in punycode. The tool never fetches it. The
confirmation shows every address completely, old and new.

### Confirmation

The confirmation names the location and, for each attribute of the mask, the value read before and the value to
set. The confirmation of this tool may hold 8192 characters. A change whose confirmation would be cut is
refused instead, so that what a person approves is what is sent: split it into several calls.

The change is one request without a retry. After a timeout or a 5xx answer the outcome is reported as unknown,
and the attributes are to be read before changing them again. The time between the check of the state and the
change is a limit of the comparison.

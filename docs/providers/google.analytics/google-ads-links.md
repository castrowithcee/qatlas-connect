---
description: >
  Describes the Google Analytics Google Ads link tools: google-ads-links.list reads the Google Ads links of a property and google-ads-links.delete deletes one with confirmation, both on the Admin API v1beta, the personal data of the creator email address, the result members, the effect of a deletion, how targets bind the property, untrusted values, and the limits of what is verified; creating and changing a link has no tool.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Analytics Google Ads links

The Google Ads link tool of the [Google Analytics provider](README.md). A Google Ads link joins a property to a
Google Ads account. `google-ads-links.list` reads `properties.googleAdsLinks.list` of the Analytics Admin API
`v1beta` (revision 20261116 of the Discovery document). It changes nothing, a repeat is safe, it needs the scope
`analytics.readonly`, and the profile `read` holds it; see [setup](setup.md#rights-and-scopes). It reads no
report data and uses no reporting quota. `google-ads-links.delete` deletes a link, see
[Deleting a link](#deleting-a-link). Creating and changing a link has no tool.

## Personal data

Every link names the email address of the person who created it. This is personal data. The result is
classified as such (`data_sensitivity` is `analytics-link-creators`, apart from `analytics-configuration` of
the other link tools), and the description of the tool carries a warning. Read only what the task needs and do
not pass the addresses on. The profile `read` includes this tool; leave it out with a `tools` list when that is
not wanted.

## google.analytics.google-ads-links.list

`property` (`properties/ID`) is required. `page_size` (1 to 200, Google returns at most 50 without it) and
`cursor` work as in the provider page; the cursor fits only the same connection and property. A link in the
answer that does not carry a valid name of the asked property is left out. An empty answer is an empty list.

A link has `name` (`properties/ID/googleAdsLinks/ID`, the ID is not the Google Ads customer ID), `customerId`,
`canManageClients` (true for a manager account), `adsPersonalizationEnabled`, `creatorEmailAddress` (empty when
Google cannot retrieve it), `createTime`, and `updateTime`. A member Google does not send is left out, so an
absent `adsPersonalizationEnabled` is not `false`.

## Targets and proof

The property in the argument decides, like the property tools: a bound property is used at once, a property of a
bound account only after one read that shows its account, and any other property is refused without a request or,
after the proof read, without the call. A connection bound to properties only refuses every other property
before any request. The refusal does not name the property.

## Deleting a link

`google.analytics.google-ads-links.delete` calls `properties.googleAdsLinks.delete` (Admin API `v1beta`, revision 20261116). `name` (`properties/ID/googleAdsLinks/ID`) is required; take it from
`google-ads-links.list`. Deleting the link ends its data flow: the property no longer shares its audiences and conversions with the Google Ads customer over it. Google offers no way to restore a deleted link, and Qatlas offers no tool that creates
one: only a person can link the property again, outside Qatlas. Creating and changing a link has no tool.

The tool needs the scope `analytics.edit` and the confirmation of a person for each call, which names the link.
It has the effect `delete`, and the profile `write` holds it, not the profile `read`; see
[setup](setup.md#rights-and-scopes). The link is not read before it is deleted. The property in the name follows
the rule in [Targets and proof](#targets-and-proof) and is checked before any request. The tool sends exactly one
request and never repeats it. After a timeout, a 5xx answer, or an answer that cannot be read, it reports that the
outcome is unknown, and the link has to be read again with `google-ads-links.list` before another try. A link that is
already deleted is `not-found`. The answer is `name` and `deleted: true`.

## Provider content

The customer ID, the email address, and the times are untrusted data. The email address is cut at 300
characters, and a cut text ends with `…`. The customer ID and the times are kept exactly as Google sends them,
and a value that is not printable ASCII or too long is left out. Qatlas never fetches an address from a result.

## Not verified

- No live call was made, also not for the delete. The behavior is that of the Discovery document and of fake-transport tests.
- The form of the customer ID is not documented by Google beyond a string, so Qatlas checks only the bounds above.

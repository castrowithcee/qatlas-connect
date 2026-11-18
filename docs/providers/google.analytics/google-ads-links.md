---
description: >
  Describes the Google Analytics Google Ads link tool: google-ads-links.list reads the Google Ads links of a property with the Admin API v1beta method, the personal data of the creator email address, the result members, how targets bind the property, untrusted values, and the limits of what is verified; creating, changing, and deleting a link has no tool.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics Google Ads links

The Google Ads link tool of the [Google Analytics provider](README.md). A Google Ads link joins a property to a
Google Ads account. `google-ads-links.list` reads `properties.googleAdsLinks.list` of the Analytics Admin API
`v1beta` (revision 20261116 of the Discovery document). It changes nothing, a repeat is safe, it needs the scope
`analytics.readonly`, and the profile `read` holds it; see [setup](setup.md#rights-and-scopes). It reads no
report data and uses no reporting quota. Creating, changing, and deleting a link has no tool.

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

## Provider content

The customer ID, the email address, and the times are untrusted data. The email address is cut at 300
characters, and a cut text ends with `…`. The customer ID and the times are kept exactly as Google sends them,
and a value that is not printable ASCII or too long is left out. Qatlas never fetches an address from a result.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.
- The form of the customer ID is not documented by Google beyond a string, so Qatlas checks only the bounds above.

---
description: >
  Describes the Google Analytics Firebase link tool: firebase-links.list reads the Firebase links of a property with the Admin API v1beta method, the result members, how targets bind the property, untrusted values, and the limits of what is verified; creating, changing, and deleting a link has no tool.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics Firebase links

The Firebase link tool of the [Google Analytics provider](README.md). A Firebase link joins a property to a
Firebase project. `firebase-links.list` reads `properties.firebaseLinks.list` of the Analytics Admin API
`v1beta` (revision 20261116 of the Discovery document). It changes nothing, a repeat is safe, it needs the scope
`analytics.readonly`, and the profile `read` holds it; see [setup](setup.md#rights-and-scopes). It reads no
report data and uses no reporting quota. Creating, changing, and deleting a link has no tool.

## google.analytics.firebase-links.list

`property` (`properties/ID`) is required. `page_size` (1 to 200, Google returns at most 50 without it) and
`cursor` work as in the provider page; the cursor fits only the same connection and property. Google supports
one Firebase link per property and documents that no further page exists. A link in the answer that does not
carry a valid name of the asked property is left out. An empty answer is an empty list.

A link has `name` (`properties/ID/firebaseLinks/ID`), `project` (`projects/NUMBER` of the Firebase project), and
`createTime`. A member Google does not send is left out.

## Targets and proof

The property in the argument follows the same rule as for the [Google Ads links](google-ads-links.md#targets-and-proof).

## Provider content

The project is untrusted data and is shown only in the documented form `projects/NUMBER`; any other value is
left out. The time is kept exactly as Google sends it, and a value that is not printable ASCII or too long is
left out.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.

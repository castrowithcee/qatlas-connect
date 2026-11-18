---
description: >
  Describes the Google Analytics Firebase link tools: firebase-links.list reads the Firebase links of a property and firebase-links.delete deletes one with confirmation, both on the Admin API v1beta, the result members, the effect of a deletion, how targets bind the property, untrusted values, and the limits of what is verified; creating and changing a link has no tool.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Analytics Firebase links

The Firebase link tool of the [Google Analytics provider](README.md). A Firebase link joins a property to a
Firebase project. `firebase-links.list` reads `properties.firebaseLinks.list` of the Analytics Admin API
`v1beta` (revision 20261116 of the Discovery document). It changes nothing, a repeat is safe, it needs the scope
`analytics.readonly`, and the profile `read` holds it; see [setup](setup.md#rights-and-scopes). It reads no
report data and uses no reporting quota. `firebase-links.delete` deletes a link, see
[Deleting a link](#deleting-a-link). Creating and changing a link has no tool.

## google.analytics.firebase-links.list

`property` (`properties/ID`) is required. `page_size` (1 to 200, Google returns at most 50 without it) and
`cursor` work as in the provider page; the cursor fits only the same connection and property. Google supports
one Firebase link per property and documents that no further page exists. A link in the answer that does not
carry a valid name of the asked property is left out. An empty answer is an empty list.

A link has `name` (`properties/ID/firebaseLinks/ID`), `project` (`projects/NUMBER` of the Firebase project), and
`createTime`. A member Google does not send is left out.

## Targets and proof

The property in the argument follows the same rule as for the [Google Ads links](google-ads-links.md#targets-and-proof).

## Deleting a link

`google.analytics.firebase-links.delete` calls `properties.firebaseLinks.delete` (Admin API `v1beta`, revision 20261116). `name` (`properties/ID/firebaseLinks/ID`) is required; take it from
`firebase-links.list`. Deleting the link ends the data flow between the property and the Firebase project over it. Google offers no way to restore a deleted link, and Qatlas offers no tool that creates
one: only a person can link the property again, outside Qatlas. Creating and changing a link has no tool.

The tool needs the scope `analytics.edit` and the confirmation of a person for each call, which names the link.
It has the effect `delete`, and the profile `write` holds it, not the profile `read`; see
[setup](setup.md#rights-and-scopes). The link is not read before it is deleted. The property in the name follows
the rule in [Targets and proof](#targets-and-proof) and is checked before any request. The tool sends exactly one
request and never repeats it. After a timeout, a 5xx answer, or an answer that cannot be read, it reports that the
outcome is unknown, and the link has to be read again with `firebase-links.list` before another try. A link that is
already deleted is `not-found`. The answer is `name` and `deleted: true`.

## Provider content

The project is untrusted data and is shown only in the documented form `projects/NUMBER`; any other value is
left out. The time is kept exactly as Google sends it, and a value that is not printable ASCII or too long is
left out.

## Not verified

- No live call was made, also not for the delete. The behavior is that of the Discovery document and of fake-transport tests.

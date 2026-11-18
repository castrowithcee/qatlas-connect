---
description: >
  Describes the Google Analytics rollup source link tools on the alpha version of the Admin API: rollup-links.list and rollup-links.get read the source links of a rollup property, rollup-links.delete removes a source from the rollup property with confirmation, with the result members, the exclusion of creating a link, how targets restrict each tool and prove the account of the rollup property, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Analytics rollup links

The rollup link tools of the [Google Analytics provider](README.md). A rollup property combines the data of its
source properties, and each source property is joined by a source link. All tools use the alpha version of the
Admin API (`v1alpha`, Discovery revision 20261116 for the delete tool, 20261115 for reading) and follow the [alpha tools](README.md#alpha-tools) rule:
Google can change or remove the methods without notice, and then the arguments and the answer of the tools change
with them. Each description ends with the `ALPHA:` note.

The two reading tools read and change nothing, a repeat is safe, they need the scope `analytics.readonly`, and the
profile `read` holds both. The delete tool needs `analytics.edit` and is only in the profile `write`; see
[setup](setup.md#rights-and-scopes). No tool reads report data or uses reporting quota.
Find rollup properties with [properties.list](properties.md).

## No create

Joining a source property to a rollup property changes which data a rollup property reports. There is no tool for
it, and none that creates a rollup property; a person does that in the Analytics interface. Removing a source is
[rollup-links.delete](#google-analyticsrollup-linksdelete).

## Result members

A link has `name` (`properties/ID/rollupPropertySourceLinks/ID`) and `sourceProperty` (`properties/ID`, the
property whose data the rollup property receives). A member Google does not send is left out.
`sourceProperty` is a name only: Qatlas does not read the source property, whatever the targets of the
connection are.

## google.analytics.rollup-links.list

Reads `properties.rollupPropertySourceLinks.list`. `property` (`properties/ID`, the rollup property) is
required. `page_size` (1 to 200; Google returns at most 50 without it) and `cursor` work as in the provider
page; the cursor fits only the same connection and property. A link in the answer that does not carry a valid
name of the asked property is left out. An empty answer is an empty list. Google answers a property that is not a
rollup property like a property without links or with an error; which one is Google's to decide.

## google.analytics.rollup-links.get

Reads `properties.rollupPropertySourceLinks.get`: one link with both members. `name`
(`properties/ID/rollupPropertySourceLinks/ID`) is required. An answer for another link than the one asked for is
refused. Google answers a link that is missing like one the credential cannot see; both are `not-found`.

## google.analytics.rollup-links.delete

Uses `properties.rollupPropertySourceLinks.delete`. `name` (`properties/ID/rollupPropertySourceLinks/ID`) is
required; the property in it is the rollup property. The source property is removed from the rollup property: its
data no longer flows into it, and reports of the rollup property stop including it. Only rollup properties have
source links, so Google refuses the call for any other property. No tool adds a source again. The answer is `name`
and `deleted: true`.

The delete tool changes Analytics, needs the confirmation of a person for each call, and is one request without a
retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the entry is read before it is deleted again. A refusal, a `400`, a `403`, a
`404`, and a `429` mean the change was not made. A `429` holds the next request of the credential for one second, or
the seconds of `Retry-After` up to one minute, and nothing repeats the failed one. The name is checked before any
request or secret access, and a refusal names the argument, never the value. The confirmation text names the entry.
The entry is not read before it is deleted. The property in the name follows the targets rule below.

## Targets and proof

The property in the argument or in the name decides, like the property tools: a bound property is used at once, a
property of a bound account only after one read that shows its account, and any other property is refused
without a request or, after the proof read, without the call. A connection bound to properties only refuses
every other property before any request.

## Provider content

Everything in a result is untrusted data. Names that are not in their documented form are left out. Qatlas never
fetches an address from a result.

## Not verified

- No live call was made. The behavior is that of the Discovery document and of fake-transport tests.
- The members were read from the Discovery document `analyticsadmin` v1alpha, revision 20261115, and Google may
  add members. The alpha version can change.
- What Google answers for a property that is not a rollup property is not documented.

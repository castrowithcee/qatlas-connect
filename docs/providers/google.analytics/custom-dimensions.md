---
description: >
  Describes the Google Analytics custom dimension tools: custom-dimensions.list and custom-dimensions.get read the custom dimensions of a property, and custom-dimensions.create, custom-dimensions.update, and custom-dimensions.archive create, change, and archive one, with the three scopes, which members can change, the per-property limits, how targets restrict each tool and prove the account of a property, the checked values, the result members, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics custom dimensions

The custom dimension tools of the [Google Analytics provider](README.md). A custom dimension makes an event
parameter, a user property, or an item parameter available as a dimension in reports and audiences.
`custom-dimensions.list` and `custom-dimensions.get` read and change nothing, a repeat is safe, and the profile
`read` holds both. They need the scope `analytics.readonly`. The three tools under
[Changing custom dimensions](#changing-custom-dimensions) need `analytics.edit` and are in the profile `write`;
see [setup](setup.md#rights-and-scopes). Configuration, targets, paging, and the boundary are described in the
provider page. Find property names with [accounts.list-summaries](accounts.md) and dimension names with
`custom-dimensions.list`.

## Result members

A dimension has `name` (`properties/ID/customDimensions/ID`), `parameterName`, `displayName`, `description`,
`scope`, and `disallowAdsPersonalization`. The `scope` is `EVENT`, `USER`, or `ITEM`. The `parameterName` is the
event parameter, the user property, or the item parameter that is tagged. A member Google does not send is left
out, so `disallowAdsPersonalization` is absent when it is false and Google leaves it out.

## google.analytics.custom-dimensions.list

Reads `properties.customDimensions.list`. `property` (`properties/ID`) is required. `page_size` (1 to 200,
Google returns at most 50 without it) and `cursor` work as in the provider page; the cursor fits only the same
connection and property. A dimension in the answer that does not carry a valid name of the asked property is
left out. An empty answer is an empty list.

## google.analytics.custom-dimensions.get

Reads `properties.customDimensions.get`: one dimension with all members above. `name`
(`properties/ID/customDimensions/ID`) is required. An answer for another dimension than the one asked for is
refused. Google answers a dimension that is missing like one the credential cannot see; both are `not-found`.

## Changing custom dimensions

All three tools change Analytics, need the confirmation of a person for each call, and are one request without a
retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the dimension is read before it is changed again. A refusal, a `403`, a `404`,
and a `429` mean the change was not made. A `429` holds the next request of the credential for one second, or the
seconds of `Retry-After` up to one minute, and nothing repeats the failed one.

The values are checked before any request or secret access, and a refusal names the argument and the rule, never
the value:

- `parameter_name` has letters, digits, and underscores and starts with a letter. It has at most 24 characters
  for `USER` and 40 for `EVENT` and `ITEM`.
- `display_name` is non-blank text without control characters, at most 82 characters. Google further restricts
  the characters (letters, digits, spaces, and underscores, starting with a letter) and decides.
- `description` has no control characters and at most 150 characters.
- `scope` is `EVENT`, `USER`, or `ITEM`.
- `disallow_ads_personalization` set to `true` is refused for a scope other than `USER`, because Google supports
  it for user scoped dimensions only.

### Limits per property

A property holds a limited number of custom dimensions per scope. Google's help page lists, for a standard
property, 50 event scoped, 25 user scoped, and 10 item scoped dimensions, and for an Analytics 360 property 125,
100, and 25. Google also states that after deleting dimensions one waits 48 hours before adding new ones. Qatlas
checks none of this. The form of the error Google answers when a limit is exceeded is not verified; the HTTP
status decides, see the provider page.

### google.analytics.custom-dimensions.create

Uses `properties.customDimensions.create`. Required: `property`, `parameter_name`, `display_name`, and `scope`.
`description` and `disallow_ads_personalization` are optional. The parameter name and the scope cannot be
changed later. The call is not idempotent: Google refuses a second dimension with the same parameter name and
scope, and an unknown outcome must be checked with `custom-dimensions.list` before a repeat. The answer is the new
dimension.

### google.analytics.custom-dimensions.update

Uses `properties.customDimensions.patch`. `name` is required, and at least one of `display_name`, `description`,
and `disallow_ads_personalization` must be set. Qatlas sends the update mask from exactly the arguments that are
set (`display_name`, `description`, `disallow_ads_personalization`) and no others; an empty `description` clears
it. The parameter name and the scope are immutable at Google, so there are no arguments for them, and a call
that names them is refused. The scope of the dimension is not known to the tool, so Google decides whether
`disallow_ads_personalization` fits it. The answer is the dimension after the change.

### google.analytics.custom-dimensions.archive

Uses `properties.customDimensions.archive`. `name` is required. An archived dimension can no longer be used in
reports or audiences, and no tool restores it. Google restricts creating a dimension with the same parameter name
afterwards, and an archived dimension may count towards the limit of the property for a time; this behavior
follows Google's help pages and is not verified by Qatlas. The answer carries `name` and `archived`, true once
Google accepted the call, and no provider content. Archiving one dimension is in the profile `write`, unlike
deleting an account or a property, which a connection offers only when its `tools` list names them.

### Targets and proof

The property in the name decides, like the property tools: a bound property is used at once, a property of a bound
account only after one read that shows its account, and any other property is refused without a request or, after
the proof read, without the call. A connection bound to properties only refuses every other property before any
request. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Provider content

Parameter names, display names, and descriptions are untrusted data. Texts are cut at 300 characters and a cut
text ends with `…`. Enumeration values are kept exactly as Google sends them, and one that is not printable ASCII
or too long is left out.

## Not verified

- The sets of values Google accepts for display names and parameter names are Google's; Qatlas checks only the
  forms above.
- The members were read from the Discovery document `analyticsadmin` v1beta, revision 20261115, and Google may
  add members.
- The limits per property are from Google's help page and may change.

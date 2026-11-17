---
description: >
  Describes the Google Analytics key event tools: key-events.list and key-events.get read the key events of a property, and key-events.create, key-events.update, and key-events.delete create, change, and delete one, with the arguments and checked values, the update mask, the result members, what a key event that cannot be deleted looks like, how targets restrict each tool, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics key events

The key event tools of the [Google Analytics provider](README.md). A key event (formerly a conversion) is an event
of a property that Analytics counts as important. Key events steer reports and the exports to advertising
products. `key-events.list` and `key-events.get` read and change nothing, a repeat is safe, and the profile `read`
holds both. They need the scope `analytics.readonly`. The three tools under
[Changing key events](#changing-key-events) need `analytics.edit` and are in the profile `write`; see
[setup](setup.md#rights-and-scopes). Configuration, targets, paging, and the boundary are described in the provider
page. Find property names with [accounts.list-summaries](accounts.md) and key event names with
`key-events.list`.

A change is expected to apply to data collected afterwards only, not to data that is already collected. See
[Not verified](#not-verified).

## Result members

A key event has `name` (`properties/ID/keyEvents/ID`), `eventName`, `createTime` exactly as Google sends it,
`deletable`, `custom`, `countingMethod`, and `defaultValue`:

- `deletable` is `false` for a key event that cannot be deleted. The value is shown also when it is `false`.
- `custom` is `true` for a key event of a custom event and `false` for a default event of Analytics. Custom key
  events count towards the maximum number of custom key events of a property.
- `countingMethod` is `ONCE_PER_EVENT` (every event counts) or `ONCE_PER_SESSION` (at most once per session and
  user).
- `defaultValue` has `numericValue` and `currencyCode`. It applies to occurrences of the event that carry no value
  or currency.

A member Google does not send is left out.

## google.analytics.key-events.list

Reads `properties.keyEvents.list`. `property` (`properties/ID`) is required. `page_size` (1 to 200, Google returns 50
without it) and `cursor` work as in the provider page; the cursor fits only the same connection and property. An
entry in the answer that does not carry a valid name of the asked property is left out. An empty answer is an empty
list.

## google.analytics.key-events.get

Reads `properties.keyEvents.get`: one key event with all members above. `name` (`properties/ID/keyEvents/ID`) is
required. An answer for another key event than the one asked for is refused. Google answers a key event that is
missing like one the credential cannot see; both are `not-found`.

## Changing key events

The three tools change Analytics, need the confirmation of a person for each call, and are one request without a
retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the key event is read before it is changed again. A refusal, a `400`, a `403`, a
`404`, and a `429` mean the change was not made. A `429` holds the next request of the credential for one second, or
the seconds of `Retry-After` up to one minute, and nothing repeats the failed one.

The values are checked before any request or secret access, and a refusal names the argument and the rule, never
the value:

- `event_name` is a letter followed by letters, digits, and underscores, at most 40 characters.
- `counting_method` is `ONCE_PER_EVENT` or `ONCE_PER_SESSION`.
- `default_value` is an object with `numeric_value` (a number) and `currency_code` (three capital letters, ISO
  4217). Google requires both, so a call with only one is refused.

Qatlas checks only the form of these values; Google decides whether it accepts them, for example a reserved event
name.

### google.analytics.key-events.create

Uses `properties.keyEvents.create`. Required: `property`, `event_name`, and `counting_method`; `default_value` is
optional. The event name cannot be changed later. The answer is the new key event. The call is not idempotent.

### google.analytics.key-events.update

Uses `properties.keyEvents.patch`. `name` is required, and at least one of `counting_method` and `default_value`
must be set. Qatlas sends the update mask from exactly the arguments that are set (`counting_method` and
`default_value`) and no others. The event name is immutable at Google, and a default value cannot be removed this
way. The answer is the key event after the change.

### google.analytics.key-events.delete

Uses `properties.keyEvents.delete`. `name` is required. Only a key event with `deletable` true can be deleted;
Google refuses the others with a `400`, which Qatlas reports as `provider-error` with the original answer in the
`provider` field, and the key event stays. The event itself is not deleted and data already collected stays. The
answer is `name` and `deleted: true`. The tool is in the profile `write` because it removes one key event, not a
whole root object; it needs the confirmation of a person like the others.

### Targets and proof

The property in the name decides, like the property tools: a bound property is used at once, a property of a bound
account only after one read that shows its account, and any other property is refused without a request or, after
the proof read, without the call. A connection bound to properties only refuses every other property before any
request. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Provider content

Event names are untrusted data and are cut at 300 characters; a cut text ends with `…`. Enumeration values and
timestamps are kept as Google sends them, and a value that is not printable ASCII or too long is left out.

## Not verified

- That a change applies to future data only comes from the usage described in third-party guides. The sources read
  for this page, Google's help pages on key events and the Discovery document, do not state it. Read Google's help
  page on the counting method before relying on it.
- The error Google sends for a key event that cannot be deleted was not observed; the page describes it from the
  Discovery document, which only marks the member `deletable`. Qatlas tests the fixed handling of a `400`.
- The maximum number of custom key events per property is Google's and was not verified.
- The members were read from the Discovery document `analyticsadmin` v1beta, revision 20261115, and Google may add
  members.

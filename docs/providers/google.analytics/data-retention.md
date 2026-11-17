---
description: >
  Describes the Google Analytics data retention tools: data-retention.get reads how long a property keeps event and user data, and data-retention.update changes it, is reachable only through a connection's tools list because shortening a period deletes data permanently, with the allowed values, the update mask, how targets restrict both tools, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics data retention

The data retention tools of the [Google Analytics provider](README.md). They read and change how long a property
keeps its event-level and user-level data. `data-retention.get` changes nothing, a repeat is safe, and the
profile `read` holds it. It needs the scope `analytics.readonly`. `data-retention.update` needs `analytics.edit`;
see [setup](setup.md#rights-and-scopes). Configuration, targets, and the boundary are described in the provider
page. Find property names with [accounts.list-summaries](accounts.md).

## Result members

`name` (`properties/ID/dataRetentionSettings`), `eventDataRetention`, `userDataRetention`, and
`resetUserDataOnNewActivity`. The durations are `TWO_MONTHS` and `FOURTEEN_MONTHS` for both kinds of data, and
`TWENTY_SIX_MONTHS`, `THIRTY_EIGHT_MONTHS`, and `FIFTY_MONTHS` for event data of Analytics 360 properties only.
`resetUserDataOnNewActivity` is `true` when every event of a user restarts the retention period of that user; it
is `false` when Google leaves it out. A value Google sends that Qatlas does not know is kept as sent when it is
printable ASCII.

## google.analytics.data-retention.get

Reads `properties.getDataRetentionSettings`. `property` (`properties/ID`) is required. An answer for another
property than the one asked for is refused. Reads no report data and uses no reporting quota.

## google.analytics.data-retention.update

Uses `properties.updateDataRetentionSettings`. `property` is required, and at least one of
`event_data_retention`, `user_data_retention`, and `reset_user_data_on_new_activity` must be set. Qatlas sends the
update mask from exactly the arguments that are set, in this order: `event_data_retention`,
`user_data_retention`, `reset_user_data_on_new_activity`. `event_data_retention` takes one of the five durations
above, `user_data_retention` only `TWO_MONTHS` or `FOURTEEN_MONTHS`. A value that is not listed, including
`RETENTION_DURATION_UNSPECIFIED`, is refused before any request or secret access. Whether a 360-only duration is
accepted for a property is Google's decision.

### Listed only and permanent deletion

Shortening a retention period deletes the data older than the new period permanently, and Qatlas cannot tell
before the call whether a value shortens it without an extra, non-atomic read. For that reason the whole tool is
in no profile: a connection offers `google.analytics.data-retention.update` only when its `tools` list names it,
whatever its effects are. The tool is an `update` with confirmation required, and each call needs the confirmation
of a person, whose summary states that a shorter period permanently deletes older data. The tool is one request
without a retry. After a timeout, a `5xx` answer, or an answer that cannot be read, the outcome is unknown: the
error says so, and the settings are read before they are changed again. A refusal, a `403`, a `404`, and a `429`
mean the change was not made.

### Targets and proof

Both tools follow the property tools: a bound property is used at once, a property of a bound account only after
one read that shows its account, and any other property is refused without a request or, after the proof read,
without the call. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Not verified

- The values were read from the Discovery document `analyticsadmin` v1beta, revision 20261115. Which duration a
  property accepts depends on its tariff and is decided by Google.
- Qatlas does not read the current value before a change and so does not tell whether a call shortens it.

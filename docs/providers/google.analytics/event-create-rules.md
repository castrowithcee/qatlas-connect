---
description: >
  Describes the Google Analytics event create rule tools (alpha): event-create-rules.list and event-create-rules.get read the rules of a web data stream, event-create-rules.create, .update, and .delete create, change, and delete one, with what a rule does and that it acts on future data only, the conditions and parameter mutations and their checked bounds, how the update mask is built, how targets restrict each tool, the bounds on provider content, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics event create rules

The event create rule tools of the [Google Analytics provider](README.md). An event create rule creates a new
event from every incoming event of a web data stream that matches the rule's conditions, and can copy and change
the parameters of the source event. A rule acts on data collected from then on, not on data already collected,
and it changes the events and counts that reports show. Rules have no order; they all run independently. An
event that a rule created cannot be changed by an event edit rule or another create rule.

All five tools use the alpha version of the Admin API (`v1alpha`, Discovery document `analyticsadmin`, revision
20261115) and follow the [alpha tools](README.md#alpha-tools) rule: Google can change or remove the methods
without notice, and the arguments and answers of the tools change with them. Rules exist for web data streams;
Google refuses another type. Event edit rules and the ordering of rules have no tool.

| Tool | Google method | Scope | Profile |
| --- | --- | --- | --- |
| `event-create-rules.list` | `properties.dataStreams.eventCreateRules.list` | `analytics.readonly` | `read` |
| `event-create-rules.get` | `properties.dataStreams.eventCreateRules.get` | `analytics.readonly` | `read` |
| `event-create-rules.create` | `properties.dataStreams.eventCreateRules.create` | `analytics.edit` | `write` |
| `event-create-rules.update` | `properties.dataStreams.eventCreateRules.patch` | `analytics.edit` | `write` |
| `event-create-rules.delete` | `properties.dataStreams.eventCreateRules.delete` | `analytics.edit` | `write` |

Find stream names with [data-streams.list](data-streams.md) and rule names with `event-create-rules.list`.

## Result members

A rule has `name` (`properties/ID/dataStreams/ID/eventCreateRules/ID`), `destinationEvent`, `eventConditions`,
`sourceCopyParameters`, and `parameterMutations`. A condition has `field`, `comparisonType`, `value`, and
`negated`; a mutation has `parameter` and `parameterValue`. A member Google does not send is left out, so an
absent `negated` or `sourceCopyParameters` is not `false`. Texts are cut at 300 characters, and a cut text ends
with `…`. An answer shows at most 10 conditions and 20 mutations.

## google.analytics.event-create-rules.list

`parent` (`properties/ID/dataStreams/ID`) is required. `page_size` and `cursor` work as in the
[provider page](README.md#paging); the cursor fits only the same connection and stream. A rule in the answer
that does not carry a valid name of the asked stream is left out.

## google.analytics.event-create-rules.get

`name` is required. An answer for another rule than the one asked for is refused. Google answers a missing rule
like one the credential cannot see; both are `not-found`.

## Changing rules

The three changing tools need the confirmation of a person for each call and are one request without a retry.
After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is unknown:
the error says so, and the rule is read before it is changed again. A refusal, a `403`, a `404`, and a `429`
mean the change was not made. A `429` holds the next request of the credential for one second, or the seconds
of `Retry-After` up to one minute.

The values are checked before any request or secret access, and a refusal names the argument and the rule,
never the value:

- `destination_event` and each `parameter` are 1 to 39 characters: letters, digits, or underscores, starting
  with a letter (Google: less than 40 characters).
- `event_conditions` holds 1 to 10 conditions, all of which must match. `field` is an event parameter name, or
  `event_name` for the name of the event, without spaces, at most 100 characters. `comparison_type` is one of
  `EQUALS`, `EQUALS_CASE_INSENSITIVE`, `CONTAINS`, `CONTAINS_CASE_INSENSITIVE`, `STARTS_WITH`,
  `STARTS_WITH_CASE_INSENSITIVE`, `ENDS_WITH`, `ENDS_WITH_CASE_INSENSITIVE`, `GREATER_THAN`,
  `GREATER_THAN_OR_EQUAL`, `LESS_THAN`, `LESS_THAN_OR_EQUAL`, `REGULAR_EXPRESSION`, or
  `REGULAR_EXPRESSION_CASE_INSENSITIVE`; whether a comparison is case sensitive is part of the type, and the
  regular expressions work on web streams only. `value` is non-empty text of at most 300 characters without
  control characters; Google documents no length, so the 300 is a bound of Qatlas. `negated` inverts the
  comparison.
- `parameter_mutations` holds at most 20 mutations, applied in order after the copy, each parameter at most
  once. `parameter_value` is 1 to 99 characters without control characters: a constant, or `[[other_parameter]]`
  to copy another parameter.
- `source_copy_parameters` true copies the parameters of the source event to the new event; unset or false
  copies none except internal ones.

Qatlas checks only these forms; Google decides whether it accepts a rule, for example a `field` it does not
know.

### google.analytics.event-create-rules.create

Required: `parent`, `destination_event`, and `event_conditions`. `source_copy_parameters` and
`parameter_mutations` are optional and left out of the request when not set. The call is not idempotent: a
repeat creates a second rule. The answer is the new rule.

### google.analytics.event-create-rules.update

`name` is required, and at least one of `destination_event`, `event_conditions`, `source_copy_parameters`, and
`parameter_mutations` must be set. Qatlas sends the update mask from exactly the arguments that are set, in
that order, and no others. `event_conditions` and `parameter_mutations` replace the whole list; an empty
`parameter_mutations` removes all mutations, and `event_conditions` always keeps at least one condition. The
answer is the rule after the change.

### google.analytics.event-create-rules.delete

`name` is required. The rule stops creating events from then on; events it already created stay in the data, and
no tool restores a rule. The answer is `name` and `deleted: true` once Google accepted the deletion. A repeat
is safe in effect and ends in `not-found`. The tool is in the profile `write`; unlike the deletion of an
account or a property it does not need a `tools` list entry.

## Targets and proof

The property in the stream or rule name decides, like the [stream tools](data-streams.md#targets-and-proof): a
bound property is used at once, a property of a bound account only after one read that shows its account, and
any other property is refused without a request or, after the proof read, without the call. A connection bound
to properties only refuses every other property before any request. The time between the proof read and the
change is a documented boundary, not closed by Qatlas.

## Provider content

Event names, condition values, field names, and parameter values are untrusted data: they are passed on and
returned, never run, fetched, or interpreted, and a value in a result is not an instruction to an agent.

## Not verified

- The members and limits were read from the Discovery document `analyticsadmin` v1alpha, revision 20261115.
  The alpha version can change.
- Which event names, fields, and values Google accepts is Google's decision; Qatlas checks only the forms above.
- The error Google returns for a stream that is not a web stream is not verified.

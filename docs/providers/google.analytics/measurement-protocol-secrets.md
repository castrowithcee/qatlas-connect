---
description: >
  Describes the Google Analytics measurement protocol secret tools: measurement-protocol-secrets.list and measurement-protocol-secrets.get read the secrets of a data stream without their value, and measurement-protocol-secrets.delete revokes one, with the exclusion of creating and changing a secret, the rule that the secret value is never returned and is masked, the deletion in the profile `write`, how targets restrict each tool, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics measurement protocol secrets

The measurement protocol secret tools of the [Google Analytics provider](README.md). A measurement protocol secret
lets a server send events to a [data stream](data-streams.md) through the Measurement Protocol. The tools list,
read, and revoke the secrets that exist. `measurement-protocol-secrets.list` and
`measurement-protocol-secrets.get` read and change nothing, a repeat is safe, and the profile `read` holds both.
They need the scope `analytics.readonly`. `measurement-protocol-secrets.delete` needs `analytics.edit` and is listed-only, so no profile holds it; see
[setup](setup.md#rights-and-scopes). Find stream names with `data-streams.list`.

## No secret value, no create, no change

A secret lets its holder write measurement data to the stream, so Qatlas treats its value like a credential:

- The value (`secretValue` in Google's answer) is never returned. No result, schema, message, or log carries it.
  Qatlas reads it from Google's answer only to register it with the masking of the process, which then replaces it
  wherever it would appear.
- There is no tool that creates or changes a secret, because the call would hand out a new or changed way to send
  data to the stream. A person creates a secret in the Analytics interface.

## Result members

A secret has `name` (`properties/ID/dataStreams/ID/measurementProtocolSecrets/ID`) and `displayName`. A member
Google does not send is left out.

## google.analytics.measurement-protocol-secrets.list

Reads `properties.dataStreams.measurementProtocolSecrets.list`. `parent` (`properties/ID/dataStreams/ID`) is
required. `page_size` and `cursor` work as in the provider page; Google returns at most 10 secrets without
`page_size`, and the cursor fits only the same connection and stream. An entry that does not carry a valid name of
a secret of the asked stream is left out. An empty answer is an empty list.

## google.analytics.measurement-protocol-secrets.get

Reads `properties.dataStreams.measurementProtocolSecrets.get`: one secret without its value. `name` is required.
An answer for another secret than the one asked for is refused. Google answers a secret that is missing like one
the credential cannot see; both are `not-found`.

## google.analytics.measurement-protocol-secrets.delete

Uses `properties.dataStreams.measurementProtocolSecrets.delete`. `name` is required. The call revokes the secret
for good: whoever sends events with it can no longer do so, and Qatlas has no tool that restores or recreates it.
The call needs the confirmation of a person, which names the secret, and is one request without a retry. The answer
is `name` and `deleted: true`; Google sends an empty body. After a timeout, an aborted request, a `5xx` answer, or
an answer that cannot be read, the outcome is unknown and the secret is read before anything else is done. A `404`
means the secret does not exist or the credential cannot see it, a `403` and a `429` mean nothing was changed. A
`429` holds the next request of the credential for one second, or the seconds of `Retry-After` up to one minute.

It is listed-only: no profile holds it, and a connection offers it only when its `tools` list names it.

## Targets and proof

The property in the stream or secret name decides, like the [data stream tools](data-streams.md#targets-and-proof):
a bound property is used at once, a property of a bound account only after one read that shows its account, and
any other property is refused without a request or, after the proof read, without the call. A refusal does not
name the other resource. The time between the proof read and the deletion is a documented boundary, not closed by
Qatlas.

## Provider content

Display names are untrusted data and are cut at 300 characters; a cut text ends with `…`.

## Not verified

- The members were read from the Discovery document `analyticsadmin` v1beta, revision 20261028, and Google may add
  members.

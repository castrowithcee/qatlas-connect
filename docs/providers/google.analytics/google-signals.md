---
description: >
  Describes the Google Analytics Google Signals tools: google-signals.get reads whether Google Signals is enabled for a property and whether the terms were accepted, and google-signals.update enables or disables it, with the alpha API version, the privacy effect of a change, the allowed states, how targets restrict both tools, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics Google Signals

The Google Signals tools of the [Google Analytics provider](README.md). Google Signals adds data of signed-in
Google users who allowed ad personalization to the data of a property. `google-signals.get` changes nothing, a
repeat is safe, and the profile `read` holds it. It needs the scope `analytics.readonly`.
`google-signals.update` needs `analytics.edit`, is in the profile `write`, and each call needs the confirmation of
a person; see [setup](setup.md#rights-and-scopes). Find property names with
[accounts.list-summaries](accounts.md).

Both tools are on the alpha version of the Admin API (see [alpha tools](README.md#alpha-tools)), the only one
that offers these methods. A change of Google Signals affects
privacy obligations such as consent and notices to the people whose data is collected. Qatlas does not check
them; complying with them is up to the user.

## Result members

`name` (`properties/ID/googleSignalsSettings`), `state` (`GOOGLE_SIGNALS_STATE_UNSPECIFIED`,
`GOOGLE_SIGNALS_ENABLED`, or `GOOGLE_SIGNALS_DISABLED`), and `consent` (`GOOGLE_SIGNALS_CONSENT_UNSPECIFIED`,
which Google treats as not consented, `GOOGLE_SIGNALS_CONSENT_CONSENTED`, or
`GOOGLE_SIGNALS_CONSENT_NOT_CONSENTED`), the acceptance of the terms of service.

## google.analytics.google-signals.get

Reads `properties.getGoogleSignalsSettings`. `property` (`properties/ID`) is required. An answer for another
property than the one asked for is refused. Reads no report data and uses no reporting quota.

## google.analytics.google-signals.update

Uses `properties.updateGoogleSignalsSettings`. `property` and `state` are required. `state` is
`GOOGLE_SIGNALS_ENABLED` or `GOOGLE_SIGNALS_DISABLED`; any other value, including
`GOOGLE_SIGNALS_STATE_UNSPECIFIED`, is refused before any request or secret access. Qatlas sends the update mask
`state` and nothing else. Accepting the terms of service is not possible with this tool, so enabling can be
refused by Google while `consent` is not `GOOGLE_SIGNALS_CONSENT_CONSENTED`. The tool is one request without a
retry. After a timeout, a `5xx` answer, or an answer that cannot be read, the outcome is unknown: the error says
so, and the settings are read before they are changed again. A refusal, a `403`, a `404`, and a `429` mean the
change was not made.

### Targets and proof

Both tools follow the property tools: a bound property is used at once, a property of a bound account only after
one read that shows its account, and any other property is refused without a request or, after the proof read,
without the call. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Not verified

- The members and values were read from the Discovery document `analyticsadmin` v1alpha, revision 20261115, and
  Google may change them.

---
description: >
  Describes the Google Analytics attribution settings tools: attribution-settings.get reads the reporting attribution model, the lookback windows, and the Ads export scope of a property, and attribution-settings.update changes them with an update mask from the arguments that are set, with the alpha API version, the effect of a change on historical reports, the allowed values, how targets restrict both tools, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-17
---

# Google Analytics attribution settings

The attribution settings tools of the [Google Analytics provider](README.md). They read and change how a property
assigns credit for conversions. Both use the Analytics Admin API `v1alpha` (revision 20261116 of the Discovery
document) at the fixed origin `https://analyticsadmin.googleapis.com`, which Google can change or remove without
notice, and then the arguments and the answer of the tools change with it. Both descriptions end with the
`ALPHA:` note of [alpha tools](README.md#alpha-tools). `attribution-settings.get` changes nothing, a repeat is
safe, and the profile `read` holds it. It needs the scope `analytics.readonly`. `attribution-settings.update`
needs `analytics.edit` and is in the profile `write`; see [setup](setup.md#rights-and-scopes). Configuration,
targets, and the boundary are described in the provider page. Find property names with
[accounts.list-summaries](accounts.md).

## Result members

`name` (`properties/ID/attributionSettings`), `reportingAttributionModel`,
`acquisitionConversionEventLookbackWindow`, `otherConversionEventLookbackWindow`, and
`adsWebConversionDataExportScope`. A value Google sends that Qatlas does not know is kept as sent when it is
printable ASCII, and an `UNSPECIFIED` value is Google's unset state.

| Member | Values |
| --- | --- |
| `reportingAttributionModel` | `PAID_AND_ORGANIC_CHANNELS_DATA_DRIVEN`, `PAID_AND_ORGANIC_CHANNELS_LAST_CLICK`, `GOOGLE_PAID_CHANNELS_LAST_CLICK` |
| `acquisitionConversionEventLookbackWindow` | `ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_7_DAYS`, `ACQUISITION_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS` |
| `otherConversionEventLookbackWindow` | `OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_30_DAYS`, `OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_60_DAYS`, `OTHER_CONVERSION_EVENT_LOOKBACK_WINDOW_90_DAYS` |
| `adsWebConversionDataExportScope` | `NOT_SELECTED_YET`, `PAID_AND_ORGANIC_CHANNELS`, `GOOGLE_PAID_CHANNELS` |

## google.analytics.attribution-settings.get

Reads `properties.getAttributionSettings`. `property` (`properties/ID`) is required. An answer for another
property than the one asked for is refused. Reads no report data and uses no reporting quota.

## google.analytics.attribution-settings.update

Uses `properties.updateAttributionSettings`. `property` is required, and at least one of
`reporting_attribution_model`, `acquisition_conversion_event_lookback_window`,
`other_conversion_event_lookback_window`, and `ads_web_conversion_data_export_scope` must be set. Qatlas sends the
update mask from exactly the arguments that are set, in this order: `reporting_attribution_model`,
`acquisition_conversion_event_lookback_window`, `other_conversion_event_lookback_window`,
`ads_web_conversion_data_export_scope`. Each argument takes one value of the table above. `NOT_SELECTED_YET` is
not accepted for the export scope, because Google cannot set it back, and an `UNSPECIFIED` value is refused. A
value that is not listed is refused before any request or secret access.

Changing the reporting attribution model applies to historical and future data and changes the conversion and
revenue numbers in reports; user and session data stay as they are. The update is an `update` with confirmation
required, and each call needs the confirmation of a person, whose summary names the property and the values. The
tool is one request without a retry. After a timeout, a `5xx` answer, or an answer that cannot be read, the
outcome is unknown: the error says so, and the settings are read before they are changed again. A refusal, a
`403`, a `404`, and a `429` mean the change was not made. The credential needs the Editor role on the property.

### Targets and proof

Both tools follow the property tools: a bound property is used at once, a property of a bound account only after
one read that shows its account, and any other property is refused without a request or, after the proof read,
without the call. The time between the proof read and the change is a documented boundary, not closed by Qatlas.

## Not verified

- The values were read from the Discovery document `analyticsadmin` v1alpha, revision 20261116. Which values a
  property accepts, for example for the export scope without a linked Ads account, is decided by Google.

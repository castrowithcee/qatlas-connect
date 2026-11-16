---
description: >
  Describes the Google Analytics property tools: properties.list lists the properties that match a filter, properties.get reads one, and properties.create, properties.update, and properties.delete create, change, and trash one, with the accepted filter forms, how targets restrict each tool and prove the account of a property, paging, the checked values, the trash period, the methods left out, the result members, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Analytics properties

The property tools of the [Google Analytics provider](README.md). `properties.list` and `properties.get` read
and change nothing, a repeat is safe, and the profile `read` holds both. They need the scope
`analytics.readonly`. The three tools under [Changing properties](#changing-properties) need `analytics.edit`;
see [setup](setup.md#rights-and-scopes). Configuration, targets, paging, and the boundary are described in the
provider page. Find property names with [accounts.list-summaries](accounts.md).

## Result members

A property has `name` (`properties/ID`), `propertyType`, `createTime` and `updateTime` exactly as Google sends
them, `parent` (`accounts/ID` or, for a subproperty, `properties/ID`), `displayName`, `industryCategory`,
`timeZone`, `currencyCode`, `serviceLevel` (`GOOGLE_ANALYTICS_STANDARD` or `GOOGLE_ANALYTICS_360`), `deleteTime`
and `expireTime` (only for a property in the trash), and `account` (`accounts/ID`). A member Google does not
send is left out.

## google.analytics.properties.list

Reads `properties.list`.

- `filter` is required unless the connection binds exactly one account. Google accepts `parent:accounts/ID`
  and `parent:properties/ID` (the subproperties of a property), `ancestor:accounts/ID`, and
  `firebase_project:ID` with the ID or number of a linked Firebase project. Qatlas accepts exactly these forms,
  without blanks or combinations, and refuses anything else before the request.
- `page_size`, `cursor`: as in the provider page. The cursor fits only the same `filter` and `show_deleted`.
- `show_deleted`: also lists properties in the trash. They carry `deleteTime`. The default is false.

A call without `filter` on a connection without targets is refused before any request.

With targets:

- One bound account and no `filter`: Qatlas sets `parent:accounts/ID` itself.
- Several bound accounts: `filter` is required and must be `parent:` or `ancestor:` of a bound account. Any
  other filter, also `parent:properties/ID` and `firebase_project:ID`, is refused, because Qatlas cannot show
  that its result stays inside the targets.
- A connection bound to properties only has no account to list and is refused. Read a bound property with
  `properties.get`.

An empty answer is an empty list.

## google.analytics.properties.get

Reads `properties.get`: one property with all members above.

- `property` is the name `properties/ID`. It is optional only when the connection binds exactly one property
  and no account; Qatlas then reads that one.

A malformed name is refused before any request. With targets, a bound property is read at once. A property
that is not bound is read once as proof: its `parent` (or, for a subproperty, its `account`) must be a bound
account, and the result is returned only then. A connection bound to properties only refuses any other property
before the request. Google answers a property that is missing like one the credential cannot see; both are
`not-found`.

## Changing properties

Every tool here changes Analytics, needs the confirmation of a person for each call, and is one request without
a retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the property is read before it is changed again. A refusal, a `403`, a `404`, and
a `429` mean the change was not made. A `429` holds the next request of the credential for one second, or the
seconds of `Retry-After` up to one minute, and nothing repeats the failed one.

The values are checked before any request or secret access, and a refusal names the argument and the rule,
never the value:

- `display_name` is non-blank text without control characters, at most 100 UTF-16 code units (Google's limit).
- `time_zone` is an IANA time zone name such as `Europe/Zurich`. Qatlas checks the form (every part of the name
  starts with a capital letter) and looks the name up in the zone database of the system, or in the built-in one
  where the system has none. On a system whose file names ignore case, another capitalization inside a part can
  pass this check, and Google answers it with an error. Google's own list may be shorter; it answers a zone it
  does not accept.
- `currency_code` is an ISO 4217 code of three capital letters such as `CHF`. Qatlas checks only the form and
  holds no list of currencies; Google answers a code it does not accept.
- `industry_category` is one of the categories of Google's enumeration (`AUTOMOTIVE`, `FINANCE`, `SHOPPING`, and
  so on; the tool describes all 26). `INDUSTRY_CATEGORY_UNSPECIFIED` names no category and is refused.
  The enumeration was read from the Discovery document `analyticsadmin` v1beta, revision 20261111.
- `property_type` is `PROPERTY_TYPE_ORDINARY`, which is also the default. Subproperties and rollup properties
  are created by methods this provider leaves out.

### google.analytics.properties.create

Uses `properties.create`. Required: `parent` (`accounts/ID`), `display_name`, and `time_zone`.
Optional: `currency_code`, `industry_category`, `property_type`. The answer is the new property. The call is not
idempotent: a repeat creates a second property.

With targets, `parent` must be a bound account. A connection bound to properties only has no account and is
refused, and so is any other account; both before any request.

### google.analytics.properties.update

Uses `properties.patch`. `property` is required, and at least one of `display_name`, `time_zone`,
`currency_code`, and `industry_category` must be set. Qatlas sends the update mask from exactly the arguments
that are set (`display_name`, `industry_category`, `time_zone`, `currency_code`) and no others. A member cannot be
cleared this way. Changing the time zone applies to data going forward only. The answer is the property after the
change.

### google.analytics.properties.delete

Uses `properties.delete`, which moves the property to the trash (a soft delete) and returns it with `deleteTime`
and `expireTime`. Google keeps a trashed property for 35 days according to the Analytics help; `expireTime` of
the answer is the date that applies. After it, Google deletes the property for good together with its data
streams, links, and other child resources. The Admin API has no method to restore a property: a person restores
it in the Trash Can of the Analytics interface within the period. Qatlas has no restore tool.

The tool is listed-only: a connection reaches it only when its `tools` list names
`google.analytics.properties.delete`, whatever its `effects` and profile say, and no profile holds it. Each call
needs the confirmation of a person, and the tool is destructive.

### Targets and proof

`properties.update` and `properties.delete` check the property like `properties.get`: a bound property is
changed at once, a property of a bound account only after one read that shows its account, and any other
property is refused without a request or, after the proof read, without a change. A connection bound to
properties only refuses every other property before any request. The time between the proof read and the change
is a documented boundary, not closed by Qatlas.

### Left out on purpose

These methods of the Admin API have no tool, and a test pins that no tool ID exists for them:

- `properties.acknowledgeUserDataCollection`: it records a legal consent for the use of user data, which a
  person gives in Analytics and no tool gives in a person's name.
- `properties.createRollupProperty` and `properties.createSubproperty`, and the rollup property source links:
  they link properties of other owners or properties, which creates a lasting connection to a foreign property.
  Whoever needs one names the need, and a package of its own follows.

## Not verified

- The set of values of `industryCategory` and `serviceLevel` is Google's and may grow; a new value is shown as
  sent if it is printable ASCII.
- That Google applies a `filter` as documented is relied on and not rechecked in the result.
- Which time zones and currency codes Google accepts beyond the checks above is Google's to decide.
- The 35 days of the trash period come from the Analytics help and may change; `expireTime` of the answer is
  authoritative.

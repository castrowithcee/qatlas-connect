---
description: >
  Describes the Google Analytics property tools: properties.list lists the properties that match a filter and properties.get reads one, with the accepted filter forms, how targets set or restrict the filter and prove the account of a property, paging, show_deleted, the result members, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Analytics properties

The property tools of the [Google Analytics provider](README.md). They read and change nothing, a repeat is
safe, and the profile `read` holds both. They need the scope `analytics.readonly`; see
[setup](setup.md#rights-and-scopes). Configuration, targets, paging, and the boundary are described in the
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

## Not verified

- The set of values of `industryCategory` and `serviceLevel` is Google's and may grow; a new value is shown as
  sent if it is printable ASCII.
- That Google applies a `filter` as documented is relied on and not rechecked in the result.

---
description: >
  Describes the sites tools of the Google Search Console provider: listing the properties of the account, reading one property with its permission level, adding and removing one, the Google methods behind them, how site_url is checked and escaped, and how a bound connection narrows them.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console: sites

The site tools of the [Google Search Console provider](README.md). Credential, target, quotas, bounds, and
errors are described there. `sites.list` and `sites.get` read and are safe to repeat. `sites.add` and
`sites.delete` change the account's list of properties, need the `edit` profile and a confirmation, and are
idempotent.

## google.search-console.sites.list

`GET https://searchconsole.googleapis.com/webmasters/v3/sites`. Google returns every property of the account
in one answer; there are no pages. An account without properties gets an empty list, not an error. The list
includes properties the account holds only as an unverified user, with that level; it has no access to their
data.

A connection bound to a property lists only that property, filtered from Google's answer. It is empty when the
account does not see the bound property.

## google.search-console.sites.get

`GET https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}`. The answer shows the account's
permission level for one property, which is how the connection test proves a bound property.

`site_url` is checked before anything is sent, as described under [Target](README.md#target), and is put into
the path as one escaped segment: `sc-domain:example.com` becomes `sc-domain%3Aexample.com` and
`https://www.example.com/` becomes `https%3A%2F%2Fwww.example.com%2F`. A property Google does not show to the
account is `not-found` or `permission`, depending on Google's answer.

## google.search-console.sites.add

`PUT https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}`. Adds the property to the account of
the credential. It does not verify the property: ownership is proven elsewhere, and this provider has no
verification tool, so an added property that the account has not verified stays at the level unverified user,
without access to its data. Google answers with an empty body, so the result is an empty object. Adding a
property that is already there changes nothing.

A connection bound to a property adds only that property, and `site_url` may be left out.

## google.search-console.sites.delete

`DELETE https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}`. Removes the property from the
account of the credential. Only the Search Console entry goes; the website is untouched. The account loses the
history and the permissions it holds for the property, and getting them back means adding and verifying the
property again. Google's reference names no permission level for it; a refusal is `permission`. A property
the account does not hold, also after a first delete, may answer `404`, which is `not-found`.

A connection bound to a property removes only that property.

---
description: >
  Describes the sites tools of the Google Search Console provider: listing the properties of the account and reading one property with its permission level, the Google methods behind them, how site_url is checked and escaped, and how a bound connection narrows them.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console: sites

The site tools of the [Google Search Console provider](README.md). Credential, target, quotas, bounds, and
errors are described there. Both tools read and are safe to repeat.

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

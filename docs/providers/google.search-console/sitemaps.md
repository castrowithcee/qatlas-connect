---
description: >
  Describes the sitemaps tools of the Google Search Console provider: listing the sitemaps of a property or of a sitemap index and reading one sitemap, the Google methods behind them, how the addresses are checked and escaped, and what the counts mean.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console: sitemaps

The sitemap tools of the [Google Search Console provider](README.md). Credential, target, quotas, bounds, and
errors are described there. Both tools read and are safe to repeat. `site_url` follows the rules of
[sites](sites.md).

## google.search-console.sitemaps.list

`GET https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}/sitemaps`. Without `sitemap_index`
Google lists the sitemaps submitted for the property; with it, the sitemaps that index contains, sent as the
query parameter `sitemapIndex`. Google returns them in one answer; there are no pages. A property without
sitemaps gets an empty list.

## google.search-console.sitemaps.get

`GET https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}`. `feedpath` is
the sitemap address as it was submitted, put into the path as one escaped segment like `site_url`. A sitemap
the property does not hold is `not-found`.

## Addresses and counts

`feedpath` and `sitemap_index` are absolute `http` or `https` addresses of at most 2048 characters, without
spaces, control characters, user information, or fragment; a query is allowed. Qatlas never fetches them.

`errors` counts problems of the sitemap itself, which keep Google from processing it; `warnings` counts
problems of single URLs in it. Both, and `submitted` per content type, are decimal strings as Google sends
them. The `indexed` count is left out: the discovery document marks it as deprecated and not to be used.

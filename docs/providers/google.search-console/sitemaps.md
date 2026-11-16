---
description: >
  Describes the sitemaps tools of the Google Search Console provider: listing the sitemaps of a property or of a sitemap index, reading one sitemap, submitting and removing one, the Google methods behind them, how the addresses are checked and escaped, and what the counts mean.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console: sitemaps

The sitemap tools of the [Google Search Console provider](README.md). Credential, target, quotas, bounds, and
errors are described there. `sitemaps.list` and `sitemaps.get` read and are safe to repeat. `sitemaps.submit`
and `sitemaps.delete` change the sitemap entries of a property, need the `edit` profile and a confirmation,
and are idempotent. `site_url` follows the rules of [sites](sites.md).

## google.search-console.sitemaps.list

`GET https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}/sitemaps`. Without `sitemap_index`
Google lists the sitemaps submitted for the property; with it, the sitemaps that index contains, sent as the
query parameter `sitemapIndex`. Google returns them in one answer; there are no pages. A property without
sitemaps gets an empty list.

## google.search-console.sitemaps.get

`GET https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}`. `feedpath` is
the sitemap address as it was submitted, put into the path as one escaped segment like `site_url`. A sitemap
the property does not hold is `not-found`.

## google.search-console.sitemaps.submit

`PUT https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}`. Reports the
sitemap address to Search Console, or renews the entry of one already submitted. Google reads the sitemap
afterwards, and the processing state shows in `sitemaps.get`. Google answers with an empty body, so the result
is an empty object.

## google.search-console.sitemaps.delete

`DELETE https://searchconsole.googleapis.com/webmasters/v3/sites/{siteUrl}/sitemaps/{feedpath}`. Removes the
entry from the Sitemaps report of the property. The file of the website stays, and Google may keep reading it
and the addresses it already knows. A `404` is `not-found`; it can mean that the property or the sitemap
entry is missing, and whether a repeated delete answers `404` was not observed live.

## Feedpath of a change

`sitemaps.submit` and `sitemaps.delete` check that `feedpath` lies under the property, before anything is sent:

- For a URL-prefix property the scheme and host match without regard to case, and the path starts with the
  path of the property, compared character by character. A different scheme, a different host or port, or a
  path outside the prefix is refused.
- For a domain property the host is the domain or one of its subdomains, with `http` or `https`.

`sitemaps.get` and `sitemaps.list` do not apply this check.

## Addresses and counts

`feedpath` and `sitemap_index` are absolute `http` or `https` addresses of at most 2048 characters, without
spaces, control characters, user information, or fragment; a query is allowed. Qatlas never fetches them.

`errors` counts problems of the sitemap itself, which keep Google from processing it; `warnings` counts
problems of single URLs in it. Both, and `submitted` per content type, are decimal strings as Google sends
them. The `indexed` count is left out: the discovery document marks it as deprecated and not to be used.

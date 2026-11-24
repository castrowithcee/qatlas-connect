---
description: >
  Describes the url-inspection tool of the Google Search Console provider: what it reads from Google's index, the checks made before a request, the inspection quota and how Qatlas counts it, the result bounds, and what is left out because Google deprecated it.
type: knowledge
edit: shared
created: 2026-11-16
updated: 2026-11-16
---

# Google Search Console: URL inspection

The URL inspection tool of the [Google Search Console provider](README.md). Credential, target, error
classes, and the general bounds are described there. `site_url` follows the rules of [sites](sites.md).

## google.search-console.url-inspection.inspect

`POST https://searchconsole.googleapis.com/v1/urlInspection/index:inspect`. The tool reads the index state
Google holds for one URL and is safe to repeat although Google takes it as a POST; it needs the scope
`webmasters.readonly`. It requests no indexing, and it does not test the live page: Google does not crawl the
URL for it. Qatlas never fetches the URL either. The result is the `inspectionResult` of Google's answer
with the index status, the AMP result, and the rich results; the AMP result is absent for a URL that is not
AMP, and the rich results are absent when Google found none. Enumerations keep the spelling of the
discovery document, in upper case.

### Checks before a request

Qatlas refuses these before a secret is read and before anything is sent:

- `inspection_url` is an absolute `http` or `https` address of at most 2048 characters, without user
  information, whitespace, or fragment.
- The address lies under the property. For a URL-prefix property it starts with the prefix; scheme and host
  are compared without regard to case, the path as written, so `http` does not match an `https` property. For
  a domain property its host is the domain or a subdomain of it.
- `language_code` is a BCP-47 tag such as `en-US` or `de-CH`.

Whether Google accepts every address that passes, and which error it gives for an address outside the
property, is not verified: Qatlas checks the rule first so that no request is spent on it.

### Quota and spacing

Google allows 2,000 inspections per day and 600 per minute for one property, and 10,000,000 per day and
15,000 per minute for one Cloud project. Qatlas spaces the inspections of one credential on one property by
at least 100 ms within a process, which is 600 per minute. The other tools keep their own spacing.

Google's daily count is not readable through the API, so Qatlas counts its own inspections per credential and
property within the process, at most 2,000 per day. The day ends at midnight Pacific Time
(`America/Los_Angeles`), when Google resets the quota. The 2,001st inspection of a day is refused as
`rate-limited` before a secret is read or a request is sent, with the seconds until the reset. Calls of other
processes or clients are not counted, and Google's own count decides. A `429`
ends the call as `rate-limited`, holds the next inspection of the credential for the seconds of `Retry-After`
or one second, and the failed call is not repeated.

### Result bounds

Addresses and texts are untrusted and never fetched. Addresses are cut at 2048 characters, every other text
at 100. Lists of sitemaps, referring addresses, issues, rich result types, and items are cut at 50 entries;
a cut list sets `truncated`. The answer read from Google is capped at 4 MiB.

### Left out on purpose

The mobile usability result is deprecated in the discovery document, and Google ended that report in
December 2023. Qatlas does not return it. The live test of a page (`urlTestingTools.mobileFriendlyTest`) is
discontinued and not offered.

Not verified: no live inspection was made. Parameters and answers follow the discovery document, revision
20261115.

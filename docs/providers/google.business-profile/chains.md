---
description: >
  Describes the chains tools of the Google Business Profile provider: reading one chain of businesses by its name, searching chains by name with page size, the Google methods behind them, the argument forms, the chain fields, and what the search does not offer.
type: knowledge
edit: shared
created: 2026-11-18
updated: 2026-11-18
---

# Google Business Profile: chains

The chain tools of the [Google Business Profile provider](README.md). Credential, approval, quotas, bounds, and
errors are described there. Both tools read and are safe to repeat.

A chain is a group of businesses under one brand. Chains are Google's reference data, the same for everyone.
The tools read nothing of the user, name no account or location, and stay available on a connection bound to
any target. Each chain carries `name` (`chains/CHAIN`), `chainNames` (`displayName`, `languageCode`),
`websites` (`uri`), and `locationCount`. Names and websites are untrusted content; a website is text and is
never fetched.

## google.business-profile.chains.get

`GET https://mybusinessbusinessinformation.googleapis.com/v1/{chain}`. Reads one chain. `chain` is
`chains/CHAIN`, where the ID has letters, digits, hyphens, and underscores, at most 64 characters. Any other
form, such as a path with more segments, a dot, a query, or an encoded character, is refused before a secret
is read or a request is sent. Google's discovery document names the form only as `chains/{chain_place_id}`;
Qatlas accepts this narrow form so that no argument can reach another path.

A chain that Google does not know is `not-found`.

## google.business-profile.chains.search

`GET https://mybusinessbusinessinformation.googleapis.com/v1/chains:search`. Searches chains by `chain_name`,
at most 500 characters; Google matches exact, partial, fuzzy, and related names and lists the best match
first. `page_size` asks for 1 to 500 matches; Google's default is 10. The search has no cursor, so the result
holds at most the page size. No match is an empty list.

The search text goes to Google and is not logged by Qatlas Connect.

## Limits

- An answer holds at most 500 chains; more is `invalid-provider-response`.
- A chain's lists of names and websites are cut at 250 entries.

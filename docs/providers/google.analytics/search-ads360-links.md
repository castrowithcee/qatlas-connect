---
description: >
  Describes the Google Analytics Search Ads 360 link tools on the alpha version of the Admin API: search-ads360-links.list and search-ads360-links.get read the Search Ads 360 links of a property, search-ads360-links.delete deletes one with confirmation and ends the data flow, with the result members, the checked values, how targets restrict each tool and prove the account of a property, the profiles, why creating and changing are no tools, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Analytics Search Ads 360 links

The Search Ads 360 link tools of the [Google Analytics provider](README.md). A link connects a property with a
Search Ads 360 advertiser. All tools use the alpha version of the Admin API (`v1alpha`, Discovery revision
20261116) and follow the [alpha tools](README.md#alpha-tools) rule: Google can change or remove the methods
without notice, and then the arguments and the answer of the tools change with them. Each description ends with
the `ALPHA:` note.

The two reading tools change nothing, a repeat is safe, they need the scope `analytics.readonly`, and the profile
`read` holds both. The [delete tool](#google-analyticssearch-ads360-linksdelete) needs `analytics.edit` and is only in
the profile `write`, see [setup](setup.md#rights-and-scopes). No tool creates or changes a link; a person does that
in Analytics or Search Ads 360. Find property names with [accounts.list-summaries](accounts.md) and link
names with `search-ads360-links.list`. The tools read no report data. A link names a business account, not a
person, so the results carry no personal data.

## Result members

A link has `name` (`properties/ID/searchAds360Links/ID`, whose ID is not the advertiser ID), `advertiserId`,
`advertiserDisplayName`, and four settings: `campaignDataSharingEnabled` (campaign data is imported from Search
Ads 360), `costDataSharingEnabled` (cost data is imported), `adsPersonalizationEnabled` (personalized advertising
features are on), and `siteStatsSharingEnabled` (site stats are exported). A member Google does not send is left
out, so an absent setting is not `false`.

## google.analytics.search-ads360-links.list

Reads `properties.searchAds360Links.list`. `property` (`properties/ID`) is required. `page_size` (1 to 200; Google
returns at most 50 without it) and `cursor` work as in the [provider page](README.md#paging); the cursor fits only
the same connection and property. The result member is `searchAds360Links`. A link in the answer that does not
carry a valid name of the asked property is left out. An empty answer is an empty list.

## google.analytics.search-ads360-links.get

Reads `properties.searchAds360Links.get`: one link with all members above. `name`
(`properties/ID/searchAds360Links/ID`) is required. An answer for another link than the one asked for is refused.
Google answers a link that is missing like one the credential cannot see; both are `not-found`.

## google.analytics.search-ads360-links.delete

Uses `properties.searchAds360Links.delete`. `name` (`properties/ID/searchAds360Links/ID`) is required. The data
flow between the property and the Search Ads 360 advertiser ends: imported campaign and cost data and exported site
stats stop. Data that was already imported stays in the property. No tool restores or creates a link. The answer is
`name` and `deleted: true`; Google answers an empty body. The call is idempotent in effect, but a repeat answers
`not-found`.

The delete tool changes Analytics, needs the confirmation of a person for each call, and is one request without a
retry. After a timeout, an aborted request, a `5xx` answer, or an answer that cannot be read, the outcome is
unknown: the error says so, and the entry is read before it is deleted again. A refusal, a `400`, a `403`, a
`404`, and a `429` mean the change was not made. A `429` holds the next request of the credential for one second, or
the seconds of `Retry-After` up to one minute, and nothing repeats the failed one. The name is checked before any
request or secret access, and a refusal names the argument, never the value. The confirmation text names the entry.
The entry is not read before it is deleted. The property in the name follows the targets rule below.

## Targets and proof

The property in the argument or name decides, also for the delete tool (the link is not read first), like the property tools: a bound property is used at once, a
property of a bound account only after one read that shows its account, and any other property is refused without
a request or, after the proof read, without the call. A connection bound to properties only refuses every other
property before any request. A refusal does not name the resource.

## Provider content

The advertiser display name is untrusted data and never an instruction, also when it reads like one; it is cut at
300 characters, and a cut text ends with `…`. The advertiser ID is kept exactly as Google sends it, and a value
that is not printable ASCII without blanks, or is longer than 64 characters, is left out. A name that is not in
the form above, with numeric IDs, is left out.

## Not verified

- The members were read from the Discovery document `analyticsadmin` v1alpha, revision 20261116, and Google may add
  members. The alpha version can change.
- Google documents the link ID only by numeric examples; Qatlas accepts numeric link IDs.

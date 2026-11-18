---
description: >
  Describes the Google Analytics AdSense link tools on the alpha version of the Admin API: adsense-links.list and adsense-links.get read the AdSense links of a property and adsense-links.delete deletes one with confirmation, with the result members, the effect of a deletion, the checked values, how targets restrict each tool and prove the account of a property, the profile, and the limits of what is verified.
type: knowledge
edit: shared
created: 2026-11-17
updated: 2026-11-18
---

# Google Analytics AdSense links

The AdSense link tools of the [Google Analytics provider](README.md). A link connects a property with an AdSense
ad client. Both tools use the alpha version of the Admin API (`v1alpha`, Discovery revision 20261116) and follow
the [alpha tools](README.md#alpha-tools) rule: Google can change or remove the methods without notice, and then
the arguments and the answer of the tools change with them. Each description ends with the `ALPHA:` note.

The list and get tools read and change nothing, a repeat is safe, they need the scope `analytics.readonly`, and
the profile `read` holds both, see [setup](setup.md#rights-and-scopes). `adsense-links.delete` deletes a link, see
[Deleting a link](#deleting-a-link). No tool creates or changes a link; a person does that in Analytics or
AdSense. Find property names with [accounts.list-summaries](accounts.md) and link names
with `adsense-links.list`. The tools read no report data. A link names a business account, not a person, so the
results carry no personal data.

## Result members

A link has `name` (`properties/ID/adSenseLinks/ID`) and `adClientCode`, the AdSense ad client of the link, for
example `ca-pub-1234567890`. A member Google does not send is left out.

## google.analytics.adsense-links.list

Reads `properties.adSenseLinks.list`. `property` (`properties/ID`) is required. `page_size` (1 to 200; Google
returns at most 50 without it) and `cursor` work as in the [provider page](README.md#paging); the cursor fits only
the same connection and property. The result member is `adSenseLinks`. A link in the answer that does not carry a
valid name of the asked property is left out. An empty answer is an empty list.

## google.analytics.adsense-links.get

Reads `properties.adSenseLinks.get`: one link with all members above. `name` (`properties/ID/adSenseLinks/ID`) is
required. An answer for another link than the one asked for is refused. Google answers a link that is missing like
one the credential cannot see; both are `not-found`.

## Deleting a link

`google.analytics.adsense-links.delete` calls `properties.adSenseLinks.delete` (Admin API `v1alpha`, revision 20261116). `name` (`properties/ID/adSenseLinks/ID`) is required; take it from
`adsense-links.list`. Deleting the link ends the data flow between the property and the AdSense ad client over it. Google offers no way to restore a deleted link, and Qatlas offers no tool that creates
one: only a person can link the property again, outside Qatlas. Creating and changing a link has no tool.

The tool needs the scope `analytics.edit` and the confirmation of a person for each call, which names the link.
It has the effect `delete`, and the profile `write` holds it, not the profile `read`; see
[setup](setup.md#rights-and-scopes). The link is not read before it is deleted. The property in the name follows
the rule in [Targets and proof](#targets-and-proof) and is checked before any request. The tool sends exactly one
request and never repeats it. After a timeout, a 5xx answer, or an answer that cannot be read, it reports that the
outcome is unknown, and the link has to be read again with `adsense-links.list` before another try. A link that is
already deleted is `not-found`. The answer is `name` and `deleted: true`.

The description of the tool ends with the `ALPHA:` note.

## Targets and proof

The property in the argument or name decides, like the property tools: a bound property is used at once, a
property of a bound account only after one read that shows its account, and any other property is refused without
a request or, after the proof read, without the call. A connection bound to properties only refuses every other
property before any request. A refusal does not name the resource.

## Provider content

The ad client code is untrusted data, kept exactly as Google sends it; a value that is not printable ASCII without
blanks, or is longer than 64 characters, is left out. A name that is not in the form above, with numeric IDs, is
left out.

## Not verified

- The members were read from the Discovery document `analyticsadmin` v1alpha, revision 20261116, and Google may add
  members. The alpha version can change.
- Google documents the ad client code only by an example and the link ID only by numeric examples; Qatlas accepts
  numeric link IDs.

---
description: >
  Describes the product input tools of the Google Merchant provider: inserting, updating, and deleting the product inputs of an API data source through the tools list of a connection, the product identity and the resource name built from it, the attribute schema, the replacement by insert, the update mask Qatlas builds, the address rule, bounds, delayed effect, quota, error and unknown-outcome handling, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-21
updated: 2026-11-21
---

# Google Merchant: product inputs

The product input tools of the [Google Merchant provider](README.md). The account argument, targets, quotas, and
errors are described there. A product input is what you send for one product to one [data source](data-sources.md):
Google combines the inputs of a product into the processed product that Google Shopping shows.

| Tool | Does |
| --- | --- |
| `product-inputs.insert` | uploads a product input and replaces an existing one of the same identity |
| `product-inputs.update` | sets and removes single attributes of a product input |
| `product-inputs.delete` | deletes a product input from a data source |

All three tools are in no profile. A connection offers them only when its `tools` list names them, and every call
needs a confirmation that shows the account, the data source, the product identity, and what is replaced, set,
removed, or deleted. `account` is checked against the targets before any secret is read, and a sub-account of a
bound advanced account is proven by one read of the sub-accounts first, as for the other tools. Changing products
needs a Merchant Center role that may edit product data; Google refuses the call otherwise, and Qatlas reports it as
`permission`.

The tools change what Google Shopping shows publicly: `price`, `salePrice`, `availability`, `link`, and `pause` of an
input decide what customers see and where they are sent. Test accounts are the safe place to try them. Google has no
dry run.

## Identity and data source

A product input is named by `content_language` (two lower case letters), `feed_label` (1 to 20 upper case letters,
digits, underscores, and dashes), and `offer_id` (1 to 100 characters, no control characters, no leading or trailing
space), and by `legacy_local` for a product that targets local destinations only. These three cannot be changed
afterwards. `data_source` is the numeric ID of the data source in the same account; Qatlas builds its full name from
`account` and the ID, so a data source of another account cannot be named. The ID is checked like the data source ID
of the [data source tools](data-sources.md).

`update` and `delete` need the resource name of the input. Qatlas builds it as `accounts/ACCOUNT/productInputs/ID`
where `ID` is the unpadded base64url encoding of `content_language~feed_label~offer_id`, which Google documents as
the form that works for every offer ID, with the prefix `local~` for a legacy local product. The same `ID` is the
`product` argument of `products.get`, so the processed product of an input can be read with it, see
[products](products.md#product-argument). Insert has no name: Google derives it.

The write tools accept a narrower identity than the read tools take in a product name: `content_language` is the
two-letter ISO 639-1 code in lower case and `feed_label` is as Google documents it (`A-Z`, `0-9`, hyphen,
underscore), because a write sends exactly that form, while a read also takes what Google already holds. An
`offer_id` may hold any character, as the encoded name allows.

Google accepts only API data sources for `insert` and `update`. For `delete` its reference names the data source
the input is deleted from and does not repeat the limit.

## Insert replaces

If an input with the same `content_language`, `offer_id`, and data source exists, `insert` replaces it. The body of an
insert is the whole input, and it has no update mask: every attribute that the call does not send is gone from the
input afterwards. To change single attributes use `update`. If the data source in the call differs from the one that
holds the product, the product moves to the new data source. `version_number` (an int64 as a string) keeps the order
of updates: Google refuses the insert when it is lower than the current version, and it is for primary data sources
only. Insert is not repeated by Qatlas. Repeating it with the same arguments gives the same input.

## Update and its mask

`update` takes `attributes` (attributes to set), `custom_attributes` (custom attributes to set), `delete_attributes`
(names of attributes to remove), and `delete_custom_attributes` (names of custom attributes to remove). At least one
is needed. Qatlas builds `updateMask` from them and never takes a mask from the caller:

- each member of `attributes` gives `productAttributes.NAME`, each name in `delete_attributes` the same path without
  a value in the body, which is how Google removes an attribute;
- each entry of `custom_attributes` gives `custom_attribute.NAME`, each name in `delete_custom_attributes` the same
  path without a value.

Google's mask names top-level attributes only, so a member of `attributes` replaces the whole attribute, for
example the whole `price` with `amountMicros` and `currencyCode`. A name cannot be set and removed in one call and
cannot appear twice. A path outside the 145 documented attributes, a sub-path, `*`, and a comma in a name never reach
Google: names are checked against the attribute list and the custom name form. Attributes that the mask does not name
stay as they are.

## Attributes

`attributes` takes the members of the `ProductAttributes` message of the Merchant API v1, with the API names, for
example `title`, `description`, `link`, `imageLink`, `price`, `salePrice`, `availability`, `condition`, `brand`,
`gtins`, `shipping`, and the vehicle and property attributes. These are the 145 members that
[products](products.md#result) shows for a processed product, all of them are members of the schema, and the schema is
closed: an unknown member is refused, also in nested messages. A value has the type of the API: an int64 (such as
`amountMicros`) is a decimal string, a number is a JSON number, a date and time is RFC 3339, an enum is one of its
documented values (the unspecified value is left out), and a list or message follows the API.

Bounds that Google documents are applied: `gtins` up to 10, `sizeTypes` up to 2, `questionsAndAnswers` up to 30,
`shippingLabel` and `returnPolicyLabel` up to 100 characters, the certification texts and links up to 2000, and the
range of `latitude` and `longitude`. Every other text may have at most 5000 characters, an address 2000, and a list
250 entries; these are Qatlas' own caps for what it sends, and a result is cut shorter (see Results). Google checks the rest, such as the range of weights and dimensions.

`custom_attributes` takes up to 2500 attributes. A name has letters, digits, underscores, and dashes, starts with a
letter or digit, and has at most 100 characters (Google turns underscores into spaces). An attribute has either a
`value` or `groupValues` of name and value pairs, never both. Name and value of one attribute are together at most
10240 characters and all custom attributes together at most 102400 bytes. The whole input must stay below 1 MiB.

## Addresses

Titles, descriptions, addresses, and every other value of a product are untrusted content. Qatlas hands addresses to
Google as text and never fetches them. Every address attribute needs `https` with a host and no user information, in
a list or a nested message too: `link`, `mobileLink`, `canonicalLink`, `adsRedirect`, `linkTemplate`,
`mobileLinkTemplate`, `displayAdsLink`, `imageLink`, `additionalImageLinks`, `lifestyleImageLinks`, `videoLinks`,
`documentLinks`, `virtualModelLink`, `returns[].policyUrl`, and the two certification links. A backslash, a space, or a
control character in an address is refused. A refusal names the attribute and never quotes the value.

## Results

`insert` and `update` answer `accepted` and the `productInput` as Google returned it: `name`, `product`, `offerId`,
`contentLanguage`, `feedLabel`, `legacyLocal`, `versionNumber`, `base64EncodedName`, `productAttributes`, and
`customAttributes`. `productAttributes` and `customAttributes` have the members and the cuts that
[products](products.md#result) describes for a processed product: every text is bounded, a list keeps at most 100
entries, a member the document does not list and a value of the wrong type are dropped, and an address is output
as text and never fetched. `base64EncodedProduct` is left out. `delete` answers `accepted` and the `name` of the
deleted input.

`accepted` says only that Google took the request. Google processes it afterwards: it can take several minutes
before the processed product shows the change, and a read of the product made too soon returns the old state.

## Errors and quota

Every call counts against the quota of the method group, also a failed one, and a `429` holds the next request of
the connection. A change is sent once. It is not repeated after `401`, `429`, or `5xx`, because Google may already have
applied it. A failure that leaves the outcome open (no answer, a timeout, a `5xx`, an answer that is not a JSON
object) says that the outcome of the change is unknown, so read the product before changing it again.

## Checked and not verified

Checked against the discovery document of the products API v1 (revision 20261118) and the reference pages of the
three methods: `insert` is a `POST` to `productInputs:insert` with `dataSource` as a required query parameter and API
data sources only, `patch` takes `updateMask` and `dataSource` (API data sources only), the mask can only name
top-level attributes and custom attributes with the prefix `custom_attribute.`, a name in the mask without a value is
deleted, and `*` is not supported, `delete` takes `dataSource` as a required query parameter and answers an empty
object, and the limits of the custom attributes are 2500, 10240 characters, and 102.4 kB. The guide on frequent updates
writes the mask paths as `productAttributes.NAME`.

Not verified:

- No live call was made.
- Which Merchant Center role a product change needs.
- The name form of a legacy local product in the encoded form. Qatlas encodes `local~content_language~feed_label~offer_id`
  as the reference describes the structure.
- Whether Google accepts a custom attribute name with a space in the mask. Qatlas allows underscores instead of
  spaces for this reason.
- Whether `patch` needs the identity members in the body besides the name. Qatlas sends the name only.
- The limit of `offer_id` and of the texts, which the reference does not state. The caps are Qatlas' own.
- What happens to a product that has inputs in more than one data source when one input is deleted.

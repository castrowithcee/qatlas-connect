---
description: >
  Describes the Google Merchant tools that render the issues of an account or a product: the read-only POST, the options, the product segment forms, how actions, addresses, and pre-rendered HTML are treated as untrusted text, and the limits checked against the Merchant API reference.
type: knowledge
edit: shared
created: 2026-11-20
updated: 2026-11-20
---

# Google Merchant issue rendering

Two read tools of the issue resolution API v1 render the issues of an account or of one product. Both are in the
profile `read`. The `account` argument follows the rules of [Targets](README.md#targets): digits only, checked against
the targets before any secret is read, and proven for an account that no target names.

| Tool | What it renders |
| --- | --- |
| `google.merchant.issue-resolutions.render-account-issues` | the issues of an account |
| `google.merchant.issue-resolutions.render-product-issues` | the issues of one product |

Google offers issues with resolution content and actions that are meant to be shown to a business in a third-party
application. Both render tools are reads although Google takes them as `POST`: they change nothing, need no
confirmation, and are sent like any read: once more only after a `401` with a renewed token, after no other answer.

| Argument | Meaning |
| --- | --- |
| `account` | the account whose issues, or whose product, is rendered |
| `product` | `render-product-issues` only, required, see below |
| `language_code` | BCP-47 tag such as `de` or `en-US`, at most 35 characters; Google uses `en-US` without it |
| `time_zone` | IANA name such as `Europe/Zurich`, at most 64 characters, at most three segments; Google uses UTC without it |
| `content_option` | `PRE_RENDERED_HTML`, the only value of the document |
| `user_input_action_option` | `REDIRECT_TO_MERCHANT_CENTER` (Google's default) or `BUILT_IN_USER_INPUT_ACTIONS` |

The two options form the whole request body; the two others are query parameters. Nothing else reaches the request.

### Product segment

`product` is the `{product}` segment of `accounts/{account}/products/{product}`. Two forms are accepted:

- The plain form `content_language~feed_label~offer_id`, with a leading `local~` for legacy local products. The offer ID
  may use letters, digits, dot, underscore, and hyphen only (at most 400 characters), which is narrower than the
  reference: it excludes `/`, `%`, `~`, and everything that could change a path.
- The unpadded base64url form of that string, which the reference recommends and requires when the offer ID holds
  `/`, `%`, or `~`. It must decode to valid text without control characters and with the three parts, and holds at most
  512 characters.

The segment is escaped before it becomes part of the path.

### Treatment of rendered content

Everything returned is untrusted third-party content.

- Texts are cut at 2000 characters, pre-rendered HTML at 16384, and a cut text ends with `…`. A list holds at most 100
  entries, the list of issues at most 200. Members that are not documented are dropped.
- `prerenderedContent` and `prerenderedOutOfCourtDisputeSettlement` are HTML. They are returned as plain text,
  never interpreted, rendered, or sanitised for display by Qatlas. Anything that shows them must sanitise them first.
- Every address is text and is never fetched: `externalAction.uri`, links inside the HTML, and any other address. No
  address of an answer starts a further request; the provider reaches only its fixed origin.
- `externalAction.type` names where the business would continue (Merchant Center review, identity or video
  verification, a legal appeal in the Help Center). Qatlas does not follow it.
- Actions of `builtinUserInputAction` describe a dialog with input fields (text, checkbox, choice) in `flows`. These
  forms only work through the method `triggeraction`, which this provider does not offer, because it acts on the
  account and needs an allowlist at Google. The flows are therefore a description: nothing in them can be filled in
  or sent from here. Without `BUILT_IN_USER_INPUT_ACTIONS` such actions come back as links to Merchant Center.
- The opaque `actionContext` of an action serves only `triggeraction` and is not returned.
- The nesting of an action inside a reason is shown one level deep.

## Limits

Checked against the discovery document of the issue resolution API v1 (revision 20261118):

- The render calls document no numeric limit; the bounds above are Qatlas' own.
- The product segment pattern of the reference is `[^/]+`; the forms above are narrower on purpose.

Not verified: no live call was made. Which role the methods need and what Google answers for a product without issues
are not shown by the reference.

Not offered: `triggeraction` and the aggregate product status of the issue resolution API.

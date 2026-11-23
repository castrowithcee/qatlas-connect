---
description: >
  Describes the asset group signal tools of the Google Ads provider for Performance Max: asset-group-signals.create and asset-group-signals.remove for up to ten signals per call (audience, search theme, Local Services ID, vertical ads rule list), offered by effect without the tools list, the four signal arguments, exempt_policy_violation_keys, response_content_type with the policy review fields approval_status and disapproval_reasons, the checks on resource names, validate_only, the atomic request, and the limits of what is verified against the v26 reference.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: asset group signal tools

These tools create and remove the signals of Performance Max asset groups in a Google Ads account. A signal
tells Performance Max which audience or which search topic suits the asset group. It guides the optimization
and sets no targeting limit. The exception is the vertical ads rule list, which decides which items of the
attached travel feed may serve. The tools add to the [Google Ads provider](README.md), whose configuration,
targets, quotas, and errors apply here too, and follow the rules of the [settings tools](settings.md)
(confirmation per call, one request sent once, unknown outcome, `validate_only`, the manager account rule) and
of the [asset set link tools](asset-set-links.md) in how they list entries; this page names what
differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.asset-group-signals.create` | create | creates up to 10 signals |
| `google.ads.asset-group-signals.remove` | delete | removes up to 10 signals |

Like the other links, no profile offers them. A connection has `create` with the create effect and `remove`
with the delete effect, or when its `tools` list names the tool. Every call is confirmed.

## What the tools do

- **A list of signals, one atomic request.** Both tools call
  `AssetGroupSignalService.MutateAssetGroupSignals` with up to 10 operations and without `partial_failure`:
  Google applies all of them or none. Ten is the bound of Qatlas; the reference of v26 names none. The service
  has no update: a signal is replaced by removing it and creating another.
- **`create` takes `asset_group_signals`.** Each entry names `asset_group` (`customers/ID/assetGroups/N`) and
  exactly one of the four members of the `signal` union of v26, each an object with its one member:
  `audience` (`{"audience": "customers/ID/audiences/N"}`, `AudienceInfo`), `search_theme` (`{"text": "..."}`,
  `SearchThemeInfo`), `local_services_id` (`{"service_id": "..."}`, `LocalServiceIdInfo`, a Local Services Ads
  service type such as `install_faucet`), and `vertical_ads_item_group_rule_list`
  (`{"shared_set": "customers/ID/sharedSets/N"}`, `VerticalAdsItemGroupRuleListInfo`, a shared set of type
  `VERTICAL_ADS_ITEM_GROUP_RULE_LIST`). The same signal may appear only once per call. Unlike the other signals,
  which only guide, the rule list decides which items of the attached travel feed are eligible to serve, so a
  wrong set changes what serves. The confirmation says so.
- **`remove` takes `resource_names`,** each `customers/ID/assetGroupSignals/ASSETGROUPID~CRITERIONID` as
  `reports.search` returns it, each once. Only the signal ends: the asset group and the audience stay, and the
  signal can be created again. `remove` offers no `response_content_type`.
- **Resource names stay with the account.** The asset group, the audience, the shared set, and the signals to remove
  must belong to the `customer_id` of the call, with positive IDs. A temporary (negative) ID is refused. This is
  checked before a secret is read. A result in the answer that names another account, another asset group
  than the one asked for (`create`), another signal than the one asked for (`remove`), or another number of
  results is `invalid-provider-response` with the note about the unknown outcome.
- **`response_content_type`** (`create` only). With `MUTABLE_RESOURCE` each result also holds the signal under
  `asset_group_signal`. The answer shows `resource_name`, `asset_group`, `audience`, `search_theme`,
  `local_services_id`, `vertical_ads_item_group_rule_list`, and the policy review fields `approval_status` and
  `disapproval_reasons` where Google fills them. The reference states that the review fields belong to search
  themes and are absent for an audience. Every resource name in the answer must belong to the account, and
  text from Google is cut and cleaned of control characters, since it is data and not instruction.
- **After `validate_only`** Google answers errors only and the answer has no entries.

## Search themes and policy

A search theme is checked by the policy review of Google. A rejected theme is an error of the call
(`SEARCH_THEME_POLICY_VIOLATION`, with the other errors the reference lists for `search_theme`: `TOO_MANY_WORDS`,
`TOO_LONG`, `ILLEGAL_CHARS`, `REQUIRED`, `RESOURCE_LIMIT`), mapped like every Google Ads failure without the text
of Google. An entry may carry `exempt_policy_violation_keys` (the `exempt_policy_violation_keys` of the
operation): `policy_name` and optionally `violating_text` of a violation that the error named. Google then saves
the signal although it violates the policy, and it does not serve until the policy allows it. The reference
does not limit the exemption to one signal kind and says only that not every violation can be exempted
(`is_exemptible` in the error), so Qatlas passes it for every kind and Google decides. The confirmation names
the exemption. There is no `ignorable_policy_topics`: the operation of this service has none.

## Limits and deviations from the matrix

Checked against the reference pages `AssetGroupSignal`, `AssetGroupSignalService`,
`MutateAssetGroupSignalsRequest`, `AssetGroupSignalOperation`, and `MutateAssetGroupSignalsResponse` of v26:

- **Offered:** all four members of the `signal` union: `audience`, `search_theme`, `local_services_id`, and
  `vertical_ads_item_group_rule_list`. The matrix of the earlier source review named only the audience and the
  search theme as examples.
- **Not part of the resource:** `primary_status`, `primary_status_reasons`, and the other status fields of
  `AssetGroup`. The only policy fields of `AssetGroupSignal` are `approval_status` and `disapproval_reasons`.
- **No update and no `partial_failure` argument:** the operation is `create` or `remove` ("Update is not
  supported"), and Qatlas never sets `partial_failure`.
- **`response_content_type` exists** on this service, unlike on `MutateAssetGroups`, and is offered for `create`.
- **Length of a search theme:** the reference states no length, number of words, or allowed characters, only the
  errors above. Qatlas refuses a blank text, control characters, and more than 255 bytes, and leaves the rest
  to Google. The same applies to `service_id`, where the reference states no format and Qatlas keeps its own
  bound of 128 bytes. Both bounds are Qatlas's own.
- **Number of signals per asset group:** the reference states no limit besides the error `RESOURCE_LIMIT`.

Not verified:

- Which role in Google Ads an account needs for these calls. Google does not document it per call.
- Whether a theme that the policy review limits shows `approval_status` after the change or only on a later
  read: no live call has been made.
- The exact limits of Google for the text of a search theme and for the number of signals.

Left out on purpose:

- `partial_failure`, `GoogleAdsService.Mutate`, and temporary IDs.
- Reading signals: `reports.search` on `asset_group_signal` reads them.

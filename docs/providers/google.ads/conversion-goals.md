---
description: >
  Describes the conversion goal setting tools of the Google Ads provider: customer-conversion-goals.update, campaign-conversion-goals.update, conversion-goal-campaign-configs.update and customer-sk-ad-network-conversion-value-schemas.update, with the tools list requirement and confirmation, up to 20 entries in one atomic request, the request forms checked against the v26 reference (no partial_failure, response_content_type only on the config tool, one operation and enable_warnings on the SKAdNetwork tool), validate_only, how resource names are built, the effect on bidding, the SKAdNetwork schema limits and the fields left out, the manager account rule, the unknown outcome, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-23
updated: 2026-11-23
---

# Google Ads: conversion goal setting tools

These tools change which conversions the bidding of an account or a campaign counts, and the SKAdNetwork schema of
an iOS app. They add to the [Google Ads provider](README.md), whose configuration, targets, quotas, and errors
apply here too. They follow the rules of the [settings tools](settings.md) and the
[conversion action tools](conversions.md): a connection offers them only when its `tools` list names each one,
every call needs a confirmation, the request is sent once and never repeated, and an answer or a failure that
leaves open whether Google took the change says that the outcome is unknown. This page names only what differs.

| Tool | Effect | What it does |
| --- | --- | --- |
| `google.ads.customer-conversion-goals.update` | update | sets `biddable` of one to 20 account goals |
| `google.ads.campaign-conversion-goals.update` | update | sets `biddable` of one to 20 goals of campaigns |
| `google.ads.conversion-goal-campaign-configs.update` | update | sets the goal config level and the custom conversion goal of one to 20 campaigns |
| `google.ads.customer-sk-ad-network-conversion-value-schemas.update` | update | replaces the SKAdNetwork schema of one app |

None belongs to a profile. A goal is the pair of a conversion category and a conversion origin. Setting `biddable`
or the goal config decides which conversions campaigns bid for, so a call can move spend as much as a budget
change does. Like `conversion-actions.update`, the tools therefore run only through the `tools` list, never
through an effect profile. The confirmation names the account, the goals or campaigns, and the new values.

The goals and configs are read with `reports.search` on `customer_conversion_goal`, `campaign_conversion_goal`,
`conversion_goal_campaign_config` and `customer_sk_ad_network_conversion_value_schema`; no list tool exists.

## Request

Checked against the v26 service definitions, each service differs:

| Service | Operations | `partial_failure` | `response_content_type` | Other |
| --- | --- | --- | --- | --- |
| `CustomerConversionGoalService` | `operations[]`, update only | none | none | |
| `CampaignConversionGoalService` | `operations[]`, update only | none | none | |
| `ConversionGoalCampaignConfigService` | `operations[]`, update only | none | yes | |
| `CustomerSkAdNetworkConversionValueSchemaService` | one `operation`, update only | none | none | `enable_warnings`, answer with one `result` and `warning` |

So none of the tools sends `partial_failure`, and each sends only the members of its own service. Google applies all
entries or none. At most 20 entries go in one call of the first three (a limit of Qatlas, not of Google, which names
none for these services); the SKAdNetwork tool sends one operation. An answer that carries a partial failure anyway
ends as `invalid-provider-response` with the note that the outcome is unknown. `validate_only` is an argument of all
four: Google then validates and answers errors only. The confirmation is required also then. The first two answer resource names
only. The config tool takes `response_content_type`; with `MUTABLE_RESOURCE` each result holds the config as
Google returns it.

`customer_id` is checked against the targets before a secret is read. `customer-conversion-goals.update` needs a
manager target when the account is a manager account (customer goals can belong to one); the rule and its extra
read are those of the [user access tools](user-access.md). The other three act on campaigns and iOS app links of
customer accounts and need no such read.

Resource names are built from the checked `customer_id` and checked parts. No argument takes a resource name:

| Resource | Name | Parts |
| --- | --- | --- |
| customer conversion goal | `customers/ID/customerConversionGoals/{category}~{origin}` | `category` and `origin` from the v26 enums |
| campaign conversion goal | `customers/ID/campaignConversionGoals/{campaign_id}~{category}~{origin}` | numeric `campaign_id`, enums |
| conversion goal campaign config | `customers/ID/conversionGoalCampaignConfigs/{campaign_id}` | numeric `campaign_id` |
| SKAdNetwork schema | `customers/ID/customerSkAdNetworkConversionValueSchemas/{account_link_id}` | numeric `account_link_id` of an existing link |

`category` is a `ConversionActionCategory` and `origin` one of `WEBSITE`, `GOOGLE_HOSTED`, `APP`, `CALL_FROM_ADS`,
`STORE`, `YOUTUBE_HOSTED` and `LOCAL_SERVICES_ADS` (`UNSPECIFIED` and `UNKNOWN` are not values to send). A custom
conversion goal is given as the numeric `custom_conversion_goal_id` and is expanded to
`customers/ID/customConversionGoals/ID` of the same account, so a goal of another account cannot be named. Every
name in an answer must be of the account and the one asked for at the same position, and a campaign or custom goal
named in a returned config must belong to the account; otherwise the call ends as `invalid-provider-response`
with the note that the outcome is unknown.

## Settings

- **Goals.** Each entry needs `category`, `origin` and `biddable`; the mask is exactly `biddable`. The campaign tool
  adds `campaign_id`. Each pair (and campaign) may appear once per call. A campaign inherits the account default
  until it is set; to keep a campaign from bidding on a goal, set `biddable` to `false` for it explicitly (the
  reference says so, a left-out value only inherits).
- **Configs.** Each entry needs `campaign_id` and at least one of `goal_config_level` (`CUSTOMER` or `CAMPAIGN`) and
  `custom_conversion_goal_id`; the mask holds exactly the given settings. A setting is set, never cleared. The
  campaign of a config is immutable and is not an argument beyond the ID that names the config.
- **SKAdNetwork schema.** `account_link_id`, the whole `schema`, `validate_only` and `enable_warnings`. The schema
  is checked for the limits the reference states: fine grained values 0 to 63, postback sequence index 0 to 2, at
  most one lock window trigger per postback, each value or index once. Qatlas adds its own bounds (64 fine grained
  mappings, 3 postback mappings, 20 mapped events per mapping, names of 1 to 100 characters, non-negative numbers,
  minimum not above maximum, revenue range or value, occurrence range or counter, never both). Everything else
  is Google's to validate. With `enable_warnings` a warning of Google is returned as `code` and a cut `message`.

## Left out on purpose

- **Creating the app link.** The tool never creates, accepts or changes an account link: the schema belongs to an
  existing link, and linking to a third-party analytics platform is an access to the outside that the provider
  does not offer. A schema of an unknown link ends with Google's error. `app_id` is only the Apple App Store ID
  that labels the schema.
- `partial_failure`, free resource names, `login_customer_id` as an argument, creating and removing goals (the
  services have no such operation).
- The `campaign` field of a campaign goal and a config: it is immutable and follows from the name.

## Where the reference differs

- **Request forms.** Third-party summaries give these services the standard Mutate form. The v26 service
  definitions have no `partial_failure` for any of the four, and only the config service has `response_content_type`.
- **SKAdNetwork schema.** The v26 resource marks every field of the schema as output only, which suggests that
  Google may reject or ignore a change. The request form (an `update` with the schema) is documented, so the tool
  sends it and leaves the verdict to Google; use `validate_only` first. This is the one place where the tool offers
  fields the reference calls output only.
- **Mutate limit.** The reference names no limit of entries for the list services; the cap of 20 is chosen here.

## Boundary

Not verified (no live call has been made):

- Whether Google accepts a schema change through the API for a normal account, and the role an account needs.
- Which combinations of category and origin an account has goals for; Google answers an unknown goal with an error.
- Whether `validate_only` answers are always empty; if Google sends results, they are checked like real ones.
- The format of the Apple App Store ID (Qatlas accepts 1 to 100 characters without spaces) and the allowed range
  of `measurement_window_hours`.
- Whether Google accepts the mask paths in camelCase.

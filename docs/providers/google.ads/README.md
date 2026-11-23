---
description: >
  Describes the Google Ads provider: the token credential with the adwords scope and no developer token, the fixed API version v26, the service option login_customer_id and the login-customer-id header, the manager and customer targets with the membership read, the profile read, the tools customers.list-accessible, customers.get, customer-clients.list, reports.search, fields.search, fields.get, customer-user-accesses.list, customer-user-access-invitations.list, multi-party-auth-reviews.list, customer-manager-links.list, customer-client-links.list, account-links.list, product-links.list, product-link-invitations.list, data-links.list, billing-setups.list, account-budgets.list, account-budget-proposals.list, payments-accounts.list, invoices.list, incentives.fetch, third-party-app-analytics-links.list, experiments.list-async-errors, campaign-drafts.list-async-errors, content-creator-insights.generate-creator and generate-trending, geo-target-constants.suggest, keyword-theme-constants.suggest and brand-suggestions.suggest, the settings, user access, invitation and review tools customers.update, customer-budget-network-settings.create and update, customer-user-accesses.update and remove, customer-user-access-invitations.remove, multi-party-auth-reviews.resolve, customer-manager-links.update, customer-client-links.update, conversion-actions.create, update and remove, customer-conversion-goals.update, campaign-conversion-goals.update, conversion-goal-campaign-configs.update, customer-sk-ad-network-conversion-value-schemas.update, conversion-value-rules.create, update and remove, custom-conversion-goals.create, update and remove, custom-audiences.remove, user-lists.remove, labels.remove, campaign-groups.remove, account-links.remove, product-links.remove and data-links.remove that only the tools list offers, the link tools product-link-invitations.update and data-links.update, the user list tools user-lists.create and update and user-list-customer-types.create and remove, the audience tools custom-audiences.create and update, custom-interests.create and update and audiences.create and update and the conversion variable and remarketing tools conversion-custom-variables.create and update and remarketing-actions.create and update and the label tools labels.create and update and the experiment tools experiments.create and update that a connection offers by effect, experiments.remove, experiments.schedule, experiments.end, experiments.graduate and experiments.promote that only the tools list offers and experiments.list-async-errors in the read profile, the campaign draft tools campaign-drafts.create and update that a connection offers by effect, campaign-drafts.remove and campaign-drafts.promote that only the tools list offers and campaign-drafts.list-async-errors in the read profile, and the campaign group tools campaign-groups.create and update and the asset set tools asset-sets.create and update that a connection offers by effect, asset-sets.remove and the asset group tools asset-groups.create, update and remove that only the tools list offers, the bidding tools bidding-data-exclusions.create, update and remove that only the tools list offers, the asset set assignment tools asset-set-assets.create and remove, campaign-asset-sets.create and remove, ad-group-asset-sets.create and remove and customer-asset-sets.create and remove that a connection offers by effect, the label assignment tools campaign-labels.create and remove, ad-group-labels.create and remove, ad-group-ad-labels.create and remove and ad-group-criterion-labels.create and remove that a connection offers by effect, the asset tools assets.create, assets.update and campaign-automatically-created-assets.remove that only the tools list offers, the campaign asset tools campaign-assets.create, update and remove that a connection offers by effect, the ad group asset tools ad-group-assets.create, update and remove, the asset group asset tools asset-group-assets.create, update and remove and the customer asset tools customer-assets.create, update and remove that a connection offers by effect, the campaign tools campaigns.create, campaigns.update, campaigns.remove and campaigns.enable-pmax-brand-guidelines that only the tools list offers, the campaign bid modifier tools campaign-bid-modifiers.create, update and remove that only the tools list offers, the campaign budget tools campaign-budgets.create, update and remove that only the tools list offers, the campaign criteria tools campaign-criteria.create, update and remove that only the tools list offers, the ad group tools ad-groups.create, ad-groups.update and ad-groups.remove and the ad group criterion tools ad-group-criteria.create, ad-group-criteria.update and ad-group-criteria.remove that only the tools list offers, the bidding strategy tools bidding-strategies.create, update and remove that only the tools list offers, the ad tools ad-group-ads.create, ad-group-ads.update, ad-group-ads.remove and ad-group-ads.remove-automatically-created-assets that only the tools list offers, the ad content tools ads.update and ad-parameters.remove that only the tools list offers and ad-parameters.create and ad-parameters.update that a connection offers by effect, the bidding seasonality adjustment tools bidding-seasonality-adjustments.create, update and remove that only the tools list offers, the ad group bid modifier tools ad-group-bid-modifiers.create, update and remove that only the tools list offers, the recommendation tools recommendations.generate, apply and dismiss, the reach planning tools reach-plans.list-locations, list-products, generate-forecast, generate-conversion-rates, list-user-lists and list-user-interests, the keyword idea tools keyword-plan-ideas.generate-ideas, generate-historical-metrics, generate-ad-group-themes and generate-forecast-metrics, the subscription tools recommendation-subscriptions.create and update that only the tools list offers, the shared set tools shared-sets.create and update and campaign-shared-sets.create that a connection offers by effect and shared-sets.remove and campaign-shared-sets.remove that only the tools list offers, the exclusion tools customer-negative-criteria.create and shared-criteria.create that a connection offers by effect and customer-negative-criteria.remove and shared-criteria.remove that only the tools list offers, the account label tools customer-labels.create and remove, the customizer attribute tools customizer-attributes.create that a connection offers by effect and customizer-attributes.remove that only the tools list offers, the customizer value tools campaign-customizers and ad-group-customizers, create and remove, that a connection offers by effect, the goal tools goals.create and goals.update and campaign-goal-configs.create, update and remove that only the tools list offers, the keyword plan tools keyword-plans.create, update and remove that a connection offers by effect, the GoogleAdsFailure and quota errors and their classes, rate spacing, the connection test, and what is not verified.
type: knowledge
edit: shared
created: 2026-11-19
updated: 2026-11-23
---

# Google Ads

Google Ads is a provider for the Google Ads API, version v26, over its REST interface, with one credential that
issues access tokens. It lists the accounts a credential reaches, reads the settings of an account, lists the
hierarchy below a manager account, lists the invitations and approval requests and the links of an account, runs reports on any
resource, generates, applies and dismisses recommendations, and reads the field reference of the query
language. It also reads what reach planning offers and forecasts reach. The [report tools](reports.md), the
[recommendation tools](recommendations.md), the [keyword idea tools](keyword-ideas.md) and the [reach planning tools](reach-plans.md) have their own pages. Tools that change account settings or conversion actions, turn down invitations and approval requests,
remove user lists, change bidding data exclusions, apply recommendations or subscribe to automatic ones are offered only through the `tools` list of a connection. The
[audience tools](audiences.md), the other [user list tools](user-lists.md), the
[conversion variable and remarketing tools](conversion-variables-remarketing.md) and `recommendations.dismiss`
are offered through its effects as well. All of them need a confirmation per call.

[Set up Google Ads](setup.md) leads through the Cloud project, its access level, and the credential. The fields
of a tool are in its descriptor; this page holds what the descriptor does not say.

## Configuration

The service is always the official origin `https://googleads.googleapis.com`, which applies when `base_url` is
left out. Qatlas refuses any other URL before a secret is read. The API version is fixed in the provider; a
newer version comes with a new release of Qatlas, never from the configuration. A redirect is never followed,
and a tool never takes an origin, a path, a header, a query string, or a body.

The credential takes the roles of any token credential and picks its procedure from the roles that hold a value:
a person's consent (`client_id`, optionally `client_secret`, and the `refresh_token` that
`qconnect credential consent` stores), a service account (`service_account_key`), or the impersonation of a
service account that the connection names with `service_account`. The connection holds no scope.

The Google Ads API has one scope, `adwords`, for reading and changing alike. A token with it may change whatever
its account may change in the reached Google Ads accounts, also when the connection offers only reading tools.
What a connection may do is bounded by its effects, its `tools` list, and its targets. Qatlas sends no developer
token; Google ended developer tokens, and its access level now belongs to the Cloud project of the OAuth client
or the service account.

### login_customer_id

| Setting | Where | Meaning |
| --- | --- | --- |
| `login_customer_id` | `options` of the service, not secret, optional | Ten-digit customer ID of the manager account, without hyphens |

When the credential reaches an account through a manager account, Google needs the ID of that manager in the
`login-customer-id` header and evaluates the rights of the credential in its hierarchy. Without the header it
evaluates the direct access of the credential. The value is no argument and never follows from a target: a
caller cannot set or change it. Qatlas sends it with every request on an account, also with the membership read,
and not with `customers.list-accessible`, for which it has no effect. A service takes no other option.

To work in several manager hierarchies, configure one service with its own `login_customer_id` and a
connection on it for each hierarchy. A service option is not part of the approval of a connection: changing it
asks for no new approval. The targets keep bounding what the connection reaches either way.

## Targets

A connection may be bound to accounts by their ten-digit customer ID, several at once and of both kinds:

| Form | Kind | Reaches |
| --- | --- | --- |
| `managers/MANAGER_ID` | manager | the manager account and every manager and client account below it, at any level |
| `customers/CUSTOMER_ID` | customer | this one account; below a manager target or on its own |

Targets are optional: without them the connection reaches what its credential reaches. The ID is written
without hyphens. An ID may stand in the list only once, also across both kinds. A connection takes at most 100
targets and no exclusions.

Every tool that acts on an account takes `customer_id` as a required argument. Before a secret is read and
before any request, Qatlas checks its form and the targets:

- An account that a target names passes at once, as does any account of a connection without targets.
- An account that no target names, on a connection without a manager target, is refused as `invalid-request`.
  The message does not name the account.
- An account that no target names, on a connection with manager targets, needs a membership read before the
  tool reads anything of it: Qatlas asks the hierarchy of each manager target, under the `login_customer_id` of
  the service, for exactly this client (`customer_client.id = ID`), in the order of the IDs, until one shows it.
  Only a single row with the asked ID proves the membership. No row in any manager is the same refusal as above.
  Two rows, another ID, an unreadable answer, or a failed read refuse the call with the class of that failure
  and a message that names the membership check. Each read costs one operation of the daily quota.

The membership read and the read of the tool are two requests. An account moved out of a manager in between is
still read once. A customer target that names a manager account gives the hierarchy of that manager only as far
as the targets name its accounts, because a row of `customer_client` does not say under which manager an account
sits.

## Profiles

| Profile | Tools | Recommended |
| --- | --- | --- |
| `read` | `customers.list-accessible`, `customers.get`, `customer-clients.list`, `reports.search`, `fields.search`, `fields.get`, `customer-user-accesses.list`, `customer-user-access-invitations.list`, `multi-party-auth-reviews.list`, `customer-manager-links.list`, `customer-client-links.list`, `account-links.list`, `product-links.list`, `product-link-invitations.list`, `data-links.list`, `billing-setups.list`, `account-budgets.list`, `account-budget-proposals.list`, `payments-accounts.list`, `invoices.list`, `incentives.fetch`, `third-party-app-analytics-links.list`, `experiments.list-async-errors`, `campaign-drafts.list-async-errors`, `recommendations.generate`, `reach-plans.list-locations`, `reach-plans.list-products`, `reach-plans.generate-forecast`, `reach-plans.generate-conversion-rates`, `reach-plans.list-user-lists`, `reach-plans.list-user-interests`, `content-creator-insights.generate-creator`, `content-creator-insights.generate-trending`, `geo-target-constants.suggest`, `keyword-theme-constants.suggest`, `brand-suggestions.suggest`, `keyword-plan-ideas.generate-ideas`, `keyword-plan-ideas.generate-historical-metrics`, `keyword-plan-ideas.generate-ad-group-themes`, `keyword-plan-ideas.generate-forecast-metrics` | yes |

Tools that change an account belong to no profile. A connection offers the settings, user access, invitation,
approval request, link, campaign, campaign budget, bidding strategy, campaign criteria, asset and `recommendations.apply` tools only when its `tools` list
names them, see [settings tools](settings.md), [campaign budget tools](campaign-budgets.md) and
[recommendation tools](recommendations.md); `recommendations.dismiss` is
offered by the effect `update`, the
[audience tools](audiences.md) that create and change and the [conversion variable and remarketing tools](conversion-variables-remarketing.md)
are offered by the effects `create` and `update` as well, and the [user list tools](user-lists.md) except
`user-lists.remove` by the effects `create`, `update`, and `delete`. The [label tools](labels.md) `labels.create`
and `labels.update` are offered by the effects `create` and `update`; `labels.remove` needs the `tools` list. The
[campaign group tools](campaign-groups.md) follow the same rule: `campaign-groups.create` and `.update` by effect,
`campaign-groups.remove` through the `tools` list. The [experiment tools](experiments.md) `experiments.create` and
`experiments.update` are offered by effect; `experiments.remove` and the actions `experiments.schedule`, `.end`,
`.graduate`, and `.promote` need the `tools` list. Of the
[campaign draft tools](campaign-drafts.md), `campaign-drafts.create` and `.update` are offered by effect;
`campaign-drafts.remove` and `campaign-drafts.promote` need the `tools` list. Of the
[link tools](account-links.md), `product-link-invitations.update` and `data-links.update` are offered by the
effect `update`; the three `remove` tools need the `tools` list. The [asset set tools](asset-sets.md) `asset-sets.create` and
`asset-sets.update` are offered by the effects `create` and `update`; `asset-sets.remove` needs the `tools` list.
The [asset set assignment tools](asset-set-links.md) `asset-set-assets.create`, `campaign-asset-sets.create`, `ad-group-asset-sets.create`, `customer-asset-sets.create` are offered by the effect `create`, and `asset-set-assets.remove`, `campaign-asset-sets.remove`, `ad-group-asset-sets.remove`, `customer-asset-sets.remove` by the effect `delete`.
The [label assignment tools](label-assignments.md) `campaign-labels.create`, `ad-group-labels.create`, `ad-group-ad-labels.create`, and `ad-group-criterion-labels.create` are offered by the effect `create`, `campaign-labels.remove`, `ad-group-labels.remove`, `ad-group-ad-labels.remove`, and `ad-group-criterion-labels.remove` by the effect `delete`. The same holds for `customer-labels.create` and `customer-labels.remove`.
Of the [customizer attribute tools](customizer-attributes.md), `customizer-attributes.create` is offered by the effect `create`; `customizer-attributes.remove` needs the `tools` list.
The [customizer value tools](customizers.md) `campaign-customizers.create` and `ad-group-customizers.create` are offered by the effect `create`, `campaign-customizers.remove` and `ad-group-customizers.remove` by the effect `delete`.
The [keyword plan tools](keyword-plans.md) `keyword-plans.create`, `.update`, and `.remove` are offered by the effects `create`, `update`, and `delete`.
The [campaign asset tools](campaign-assets.md), the [ad group asset tools](ad-group-assets.md), the
[asset group asset tools](asset-group-assets.md) and the
[customer asset tools](customer-assets.md) are offered by the effects `create`, `update`, and `delete`.
Of the [ad content tools](ad-content.md), `ad-parameters.create` and `ad-parameters.update` are offered by the
effects `create` and `update`; `ads.update` and `ad-parameters.remove` need the `tools` list.
The [bidding data exclusion tools](bidding-data-exclusions.md) need the `tools` list for all three.
The [seasonality adjustment tools](bidding-seasonality-adjustments.md) all need the `tools` list.
The [shared set tools](shared-sets.md) `shared-sets.create`,
`shared-sets.update`, and `campaign-shared-sets.create` are offered by the effects `create` and `update` as well;
`shared-sets.remove` and `campaign-shared-sets.remove` need the `tools` list. The
[exclusion tools](exclusions.md) `customer-negative-criteria.create` and `shared-criteria.create` are offered by the
effect `create` as well; their `remove` tools need the `tools` list.

## Tools

| Tool | Where |
| --- | --- |
| `customers.update` | changes name, tracking template, final URL suffix, auto-tagging, and call reporting, see [settings tools](settings.md) |
| `customer-budget-network-settings.create`, `.update` | set the default distribution of budgets over network channels, see [settings tools](settings.md) |
| `campaigns.create`, `.update`, `.remove`, `campaigns.enable-pmax-brand-guidelines` | create, change, and remove campaigns and prepare the brand guidelines of one, see [campaign tools](campaigns.md) |
| `campaign-bid-modifiers.create`, `.update`, `.remove` | create, change, and remove the bid modifiers of campaigns, see [campaign bid modifier tools](campaign-bid-modifiers.md) |
| `campaign-budgets.create`, `.update`, `.remove` | create, change, and remove campaign budgets with amounts in micros, only through the `tools` list, see [campaign budget tools](campaign-budgets.md) |
| `ad-groups.create`, `.update`, `.remove` | create, change, and remove ad groups, see [ad group tools](ad-groups.md) |
| `asset-groups.create`, `.update`, `.remove` | create, change, and remove the asset groups of Performance Max campaigns, only through the `tools` list, see [asset group tools](asset-groups.md) |
| `ad-group-criteria.create`, `.update`, `.remove` | create, change, and remove the criteria of ad groups, see [ad group criterion tools](ad-group-criteria.md) |
| `ad-group-ads.create`, `.update`, `.remove`, `.remove-automatically-created-assets` | create, change, and remove ads, and remove text customization from an ad, see [ad tools](ads.md) |
| `ads.update`, `ad-parameters.create`, `.update`, `.remove` | change the content of existing ads and manage the numbers in the placeholders of keyword ads, see [ad content tools](ad-content.md) |
| `bidding-strategies.create`, `.update`, `.remove` | create, change, and remove portfolio bidding strategies, see [bidding strategy tools](bidding-strategies.md) |
| `ad-group-bid-modifiers.create`, `.update`, `.remove` | create, change, and remove the bid modifiers of ad groups, see [ad group bid modifier tools](ad-group-bid-modifiers.md) |
| `customer-user-accesses.list` | lists the users of an account with role and e-mail addresses, see [user access tools](user-access.md) |
| `customer-user-accesses.update`, `.remove` | change the role of a user, remove a user, see [user access tools](user-access.md) |
| `customer-user-access-invitations.list`, `.remove` | list and revoke the invitations of an account, see [invitations and approval requests](access.md) |
| `multi-party-auth-reviews.list`, `.resolve` | list the approval requests of an account, reject or revoke one, never approve, see [invitations and approval requests](access.md) |
| `conversion-actions.create`, `.update`, `.remove` | create, change, and remove conversion actions in one atomic request, see [conversion action tools](conversions.md) |
| `customer-conversion-goals.update`, `campaign-conversion-goals.update`, `conversion-goal-campaign-configs.update` | turn conversion goals on or off for the bidding of an account or a campaign and set the goal configuration of a campaign, only through the `tools` list, see [conversion goal setting tools](conversion-goals.md) |
| `customer-sk-ad-network-conversion-value-schemas.update` | replace the SKAdNetwork conversion value schema of an iOS app, only through the `tools` list, see [conversion goal setting tools](conversion-goals.md) |
| `goals.create`, `.update`, `campaign-goal-configs.create`, `.update`, `.remove` | create and change customer lifecycle goals and attach, change, and detach them on campaigns, only through the `tools` list, see [goal tools](goals.md) |
| `custom-conversion-goals.create`, `.update`, `.remove` | create, change, and remove up to 10 custom conversion goals in one request, only through the `tools` list, see [custom conversion goal tools](custom-conversion-goals.md) |
| `conversion-custom-variables.create`, `.update` | create conversion custom variables and change their name or status, see [conversion variable and remarketing tools](conversion-variables-remarketing.md) |
| `conversion-value-rules.create`, `.update`, `.remove` | create, change, and remove up to 10 conversion value rules in one atomic request, only through the `tools` list, see [conversion value rule tools](conversion-value-rules.md) |
| `custom-audiences.create`, `.update`, `.remove` | create, change, and remove up to 10 custom audiences in one request; `remove` only through the `tools` list, see [audience tools](audiences.md) |
| `remarketing-actions.create`, `.update` | create and rename remarketing actions, never handle their tag snippets, see [conversion variable and remarketing tools](conversion-variables-remarketing.md) |
| `custom-interests.create`, `.update`, `audiences.create`, `.update` | create and change audiences from keywords and URLs, and from demographic dimensions and segments, see [audience tools](audiences.md) |
| `user-lists.create`, `.update`, `.remove` | create, change, and remove audience lists, `remove` only through the `tools` list, see [user list tools](user-lists.md) |
| `user-list-customer-types.create`, `.remove` | attach and detach customer type labels of user lists, see [user list tools](user-lists.md) |
| `customer-manager-links.list`, `customer-client-links.list` | list the manager links of an account and the client links of a manager account, see [manager and client links](manager-links.md) |
| `customer-manager-links.update`, `customer-client-links.update` | decline or end a manager link, withdraw a request or hide a client link, never make or accept one, see [manager and client links](manager-links.md) |
| `account-links.list`, `product-links.list`, `product-link-invitations.list`, `data-links.list` | list what an account is linked to, with fixed fields, see [link tools](account-links.md) |
| `account-links.remove`, `product-links.remove`, `data-links.remove`, `product-link-invitations.update`, `data-links.update` | remove a link, reject or withdraw an invitation, disable or withdraw a data link, never make or accept one; `remove` only through the `tools` list, see [link tools](account-links.md) |
| `assets.create`, `assets.update`, `campaign-automatically-created-assets.remove` | create and change assets, with image bytes from a local file or inline, and end the use of automatically created assets, see [asset tools](assets.md) |
| `billing-setups.list`, `account-budgets.list`, `account-budget-proposals.list`, `payments-accounts.list` | read the billing setups, account budgets, budget proposals and payments accounts of an account, never change them, see [billing tools](billing.md) |
| `invoices.list`, `incentives.fetch`, `third-party-app-analytics-links.list` | read the invoices of a billing setup, the incentive offer of the user and the app analytics links of an account; no incentive is applied and the PDF address and shareable link ID are withheld, see [invoices, incentives and app analytics links](invoices.md) |
| `content-creator-insights.generate-creator`, `.generate-trending`, `brand-suggestions.suggest`, `geo-target-constants.suggest`, `keyword-theme-constants.suggest` | read YouTube creator and trend insights (allowlisted accounts only), brand suggestions, geo target constants and Smart campaign keyword themes; the last two need no account, see [suggestion tools](suggestions.md) |
| `campaign-assets.create`, `.update`, `.remove` | attach assets to campaigns, pause or enable and end that use, up to 50 per request, by effect `create`, `update`, and `delete`, see [campaign asset tools](campaign-assets.md) |
| `ad-group-assets.create`, `.update`, `.remove` | attach assets to ad groups, pause or enable and end that use, up to 50 per request, by effect `create`, `update`, and `delete`, see [ad group asset tools](ad-group-assets.md) |
| `asset-group-assets.create`, `.update`, `.remove` | attach assets to Performance Max asset groups, pause or enable and end that use, up to 50 per request, by effect `create`, `update`, and `delete`, see [asset group asset tools](asset-group-assets.md) |
| `customer-assets.create`, `.update`, `.remove` | attach assets to the account as a whole, pause or enable and end that use, up to 50 per request, by effect `create`, `update`, and `delete`, see [customer asset tools](customer-assets.md) |
| `labels.create`, `.update`, `.remove` | create, change, and remove the labels of an account, see [label tools](labels.md) |
| `customer-negative-criteria.create`, `.remove`, `shared-criteria.create`, `.remove` | add and remove the exclusions of a whole account and the entries of shared sets, see [exclusion tools](exclusions.md) |
| `campaign-criteria.create`, `.update`, `.remove` | create, change, and remove the targeting criteria of campaigns, see [campaign criteria tools](campaign-criteria.md) |
| `campaign-groups.create`, `.update`, `.remove` | create, rename, and remove the campaign groups of an account, see [campaign group tools](campaign-groups.md) |
| `campaign-drafts.create`, `.update`, `.remove`, `.promote`, `campaign-drafts.list-async-errors` | create, rename, remove, and promote campaign drafts, and list the errors of a promotion; `remove` and `promote` only through the `tools` list, see [campaign draft tools](campaign-drafts.md) |
| `experiments.create`, `.update`, `.remove`, `.schedule`, `.end`, `.graduate`, `.promote`, `experiments.list-async-errors` | create, change, remove, schedule, end, graduate, and promote experiments, and list the errors of the last asynchronous operation of one; `remove` and the four actions only through the `tools` list, see [experiment tools](experiments.md) |
| `asset-sets.create`, `.update`, `.remove` | create, change, and remove the asset sets of an account, `remove` only through the `tools` list; no Business Profile credential passes, see [asset set tools](asset-sets.md) |
| `asset-set-assets.create`, `.remove`, `campaign-asset-sets.create`, `.remove`, `ad-group-asset-sets.create`, `.remove`, `customer-asset-sets.create`, `.remove` | put assets into asset sets and attach asset sets to campaigns, ad groups, and the account, and end those links; no update, see [asset set assignment tools](asset-set-links.md) |
| `campaign-labels.create`, `.remove`, `ad-group-labels.create`, `.remove`, `ad-group-ad-labels.create`, `.remove`, `ad-group-criterion-labels.create`, `.remove` | attach labels to campaigns, ad groups, ads, and ad group criteria and end those links; no update, see [label assignment tools](label-assignments.md) |
| `customer-labels.create`, `.remove` | attach labels to the account itself and end those links; no update, see [label assignment tools](label-assignments.md) |
| `customizer-attributes.create`, `.remove` | create and remove the customizer attributes of an account, `remove` only through the `tools` list; no update, see [customizer attribute tools](customizer-attributes.md) |
| `campaign-customizers.create`, `.remove`, `ad-group-customizers.create`, `.remove` | set the value of a customizer attribute for campaigns and ad groups and end it; no update, see [customizer value tools](customizers.md) |
| `bidding-data-exclusions.create`, `.update`, `.remove` | create, change, and remove the periods that automated bidding ignores, only through the `tools` list, see [bidding data exclusions](bidding-data-exclusions.md) |
| `bidding-seasonality-adjustments.create`, `.update`, `.remove` | create, change, and remove the seasonality adjustments that tell Smart Bidding about a short event; only the `tools` list offers them, see [seasonality adjustment tools](bidding-seasonality-adjustments.md) |
| `shared-sets.create`, `.update`, `.remove`, `campaign-shared-sets.create`, `.remove` | create, rename, and remove shared sets, and attach them to and detach them from campaigns, see [shared set tools](shared-sets.md) |
| `recommendation-subscriptions.create`, `.update` | subscribe the account to automatic recommendations, which Google then applies on its own, and switch subscriptions between enabled and paused; only the `tools` list offers them, see [recommendation subscription tools](recommendation-subscriptions.md) |
| `recommendations.generate`, `.apply`, `.dismiss` | generate recommendations for a campaign, apply them with the parameters of one variant, dismiss them, see [recommendation tools](recommendations.md) |
| `keyword-plan-ideas.generate-ideas`, `.generate-historical-metrics`, `.generate-ad-group-themes`, `.generate-forecast-metrics` | keyword ideas, historical search metrics, ad group themes and forecasts; Explorer access is blocked, one request per second per customer ID, see [keyword idea tools](keyword-ideas.md) |
| `keyword-plans.create`, `.update`, `.remove` | create, change, and remove the saved keyword plans of an account by name and forecast period; Explorer access may be blocked, see [keyword plan tools](keyword-plans.md) |
| `reach-plans.list-locations`, `.list-products`, `.generate-forecast`, `.generate-conversion-rates`, `.list-user-lists`, `.list-user-interests` | read what reach planning offers and forecast the reach of a product mix; all read, two take no account and are bound to no target, Basic access level needed, see [reach planning tools](reach-plans.md) |

### customers.list-accessible

Calls `GET /v26/customers:listAccessibleCustomers`. Google returns only the resource names of the accounts the
credential reaches directly, without names; an account reached through a manager is often not among them, and
`customer-clients.list` on the manager lists it. On a connection with targets, only accounts that a target
names are returned; an account reachable only below a manager target is left out, since proving it would cost
one read per manager. At most 1,000 accounts are returned, `truncated` says when more were cut. A resource name
in another form than `customers/ID` makes the answer `invalid-provider-response`.

### customers.get

Runs a GAQL search `FROM customer` on the account with every selectable field of the `customer` resource in
the fields reference of v26. The answer must hold exactly one row with the asked ID, otherwise it is
`invalid-provider-response`. The redemption provider of Local Services appears as
`external_merchant_numeric_id` and `external_merchant_alphanumeric_id`; each verification partner as `partner`
and `allow_share_cost`. Without `full` or `fields`, an invoke answers the name, currency, time zone, status,
and the manager and test account flags.

`google_ads_conversion_customer` names the account that tracks conversions for this one. It is only an
identifier and may name an account outside the targets.

### customer-clients.list

Runs a GAQL search `FROM customer_client` on the account, which lists every account below it at every level,
the account itself at level 0. A client account lists only itself. Rows come sorted by ID. Qatlas asks for
`page_size` rows plus one, which tells whether more follow, and continues after the ID of the last row with
`next_cursor`. The cursor is bound to the arguments, the targets, and `login_customer_id`; it fails before any
request if one of them changed. Rows must name the queried account and their own ID, and come in the order
asked for; anything else is `invalid-provider-response`.

Filters become conditions joined with `AND`. `name_contains` becomes `LIKE` with the characters `[`, `]`, `%`,
and `_` matched literally; whether Google matches case-sensitively is not documented. Qatlas Connect does not
escape GAQL strings: a text filter that holds a quote, a double quote, a backslash, or a control character is
refused as `invalid-request` before a secret is read.

On a manager target, or an account proved below one, every row is returned. On a customer target only rows of
accounts that a target names are returned, so a page can hold fewer rows than `page_size`, or none, while
`has_more` is true.

Every text Google returns is untrusted and is cut: names, codes, and resource names at 1024 bytes or less,
tracking templates, URL suffixes, and the global site tag at 4096 bytes, lists at 500 entries. An answer above
8 MiB is `invalid-provider-response`.

### reports.search, fields.search, fields.get

`reports.search` runs a search `FROM` any resource of the field catalog from a structured query, with a limit of
100 rows or up to 1,000, and no query string. `fields.search` and `fields.get` read the field reference of the
query language and need no account. The checks, the limits, `has_more`, the fields whose values are never
returned, and the personal data a report can hold are on the page of the [report tools](reports.md).

## Errors

Qatlas' message never carries Google's text. Google answers a failed call with the Google error object whose
details hold a `GoogleAdsFailure` with one or more errors, each with an error code of a category such as
`authorizationError`, a message, and a field path, plus the request ID. The original in the `provider` field of
the error detail (untrusted, redacted, and bounded) carries the status, every error code with its message and
field path, and the request ID; the `trigger` of an error is left out, because it echoes request values. The
first documented code decides:

| Error code | Class | The message says |
| --- | --- | --- |
| `authorizationError.CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION` | `permission` | the Cloud project has test access only and how to apply for more |
| `authorizationError.USER_PERMISSION_DENIED` | `permission` | the credential may not access the account; `login_customer_id` must name the manager |
| `authorizationError.INVALID_LOGIN_CUSTOMER_ID_SERVING_CUSTOMER_ID_COMBINATION` | `permission` | the manager of `login_customer_id` does not reach the account |
| `authorizationError.CUSTOMER_NOT_ENABLED`, `PROJECT_DISABLED`, `MISSING_TOS`, `CLOUD_PROJECT_NOT_UNDER_ORGANIZATION` | `permission` | the reason |
| any other `authorizationError` | `permission` | the code |
| `authenticationError.NOT_ADS_USER` | `auth` | the account of the credential is a user of no Google Ads account |
| `authenticationError.OAUTH_TOKEN_*` and other token errors | `auth` | the token was refused |
| `authenticationError.CUSTOMER_NOT_FOUND` | `not-found` | no account with this ID |
| `quotaError.RESOURCE_TEMPORARILY_EXHAUSTED`, `RESOURCE_EXHAUSTED`, `EXCESSIVE_*_QUERY_RESOURCE_CONSUMPTION` | `rate-limited` | wait and try again |
| `quotaError.ACCESS_PROHIBITED` | `permission` | access is not permitted |
| `headerError.INVALID_LOGIN_CUSTOMER_ID` | `provider-error` | Google refused `login_customer_id` |
| any `notAllowlistedError` | `permission` | the feature is not allowlisted |

Without a documented code, the HTTP status and the error object decide: 401 `auth`; 403 with reason
`SERVICE_DISABLED` `permission` naming `googleads.googleapis.com` as the API to enable, with
`ACCESS_TOKEN_SCOPE_INSUFFICIENT` `scope-missing`, otherwise `permission`; 404 with status `NOT_FOUND`
`not-found`; 429 `rate-limited`, which also covers a request Google found too large; 400 and any other 4xx
`invalid-provider-response`, since Qatlas built the request; 5xx by HTTP status. A failed token request ends as
`auth`, `consent-required`, or `consent-expired`, as the core decides.

A report that Google refuses as a query that cannot be run, a `queryError`, ends as `provider-error` with the code
in the original, and the message says the query is not allowed; in the tools that Qatlas builds the query for
itself, the same answer is `invalid-provider-response`.

A read times out after 20 seconds and repeats once after a 401 with a renewed token, never otherwise. A change
is sent once and never repeated, also not after a 401; see [settings tools](settings.md).

## Rate spacing

Requests of one credential are spaced by at least 250 ms within a process. This is a cautious start of Qatlas,
not a Google limit. After a rate or quota error the next request of the credential waits the `retryDelay` of
the quota error details or the seconds of `Retry-After`, at least one second and at most one minute. The failed
request is not repeated. The keyword idea tools are also spaced by one second per customer ID, see
[keyword idea tools](keyword-ideas.md#spacing).

Every search, every `listAccessibleCustomers`, and every call of the field service counts as one request, and a
search or `listAccessibleCustomers` as one operation of the daily quota of the Cloud
project, also when it fails; the membership read counts as well.

## Connection test

The connection test (`t` on a connection in `qconnect tui`) obtains an access token, checks that it holds the
`adwords` scope, and calls `listAccessibleCustomers`. That costs one operation of the daily quota and no money,
and needs no account. It proves the token, the scope, and the enabled API. It does not prove the access level
of the Cloud project for production accounts, the access to a target, or `login_customer_id`; the first call of
`customers.get` on a target shows those.

## Boundary

Not verified:

- Whether `listAccessibleCustomers` with a project of test access answers `CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION`
  for production accounts, and which role an account needs for which call. No live call has been made.
- Which Cloud project decides the access level when a person's token impersonates a service account. Google
  documents the consent and the service account key for Google Ads, not the impersonation.
- Whether `customer_client` lists hidden, canceled, or closed clients by default.
- `listAccessibleCustomers` ignoring `login-customer-id` is not stated on its reference page; Qatlas does not
  send the header there.
- The fields reference of v26 lists `customer.local_services_settings.redemption_provider_info.external_merchant_id`
  as selectable, while the reference of `LocalServicesSettings` lists only the license and insurance statuses.
  Qatlas follows the fields reference.
- The error code of the daily operation limit, and how `retryDelay` and its scope behave in practice.

Left out on purpose:

- The creation and acceptance of links, the mutation of billing, and everything else that is not in the [settings tools](settings.md), the
  [user access tools](user-access.md), the [campaign tools](campaigns.md), the [campaign budget tools](campaign-budgets.md), the [ad group tools](ad-groups.md), the [asset group tools](asset-groups.md), the [ad group criterion tools](ad-group-criteria.md), the [ad tools](ads.md), the [ad group bid modifier tools](ad-group-bid-modifiers.md), the [ad content tools](ad-content.md), the [bidding strategy tools](bidding-strategies.md), the [audience tools](audiences.md), the [user list tools](user-lists.md), the [invitations and approval requests](access.md), the [manager and client links](manager-links.md), the [link tools](account-links.md), the
  [conversion action tools](conversions.md), the [conversion goal setting tools](conversion-goals.md), the [goal tools](goals.md), the [reach planning tools](reach-plans.md), the [keyword plan tools](keyword-plans.md), the [custom conversion goal tools](custom-conversion-goals.md), the [conversion value rule tools](conversion-value-rules.md), the [conversion variable and remarketing tools](conversion-variables-remarketing.md), or the
  [asset tools](assets.md), the [asset set tools](asset-sets.md), or the [asset set assignment tools](asset-set-links.md); no member of a list is uploaded, no access is
  created, no invitation is sent, no link is made or accepted, and no request approved.
- Anything that changes billing: the [billing tools](billing.md) only read, and no billing setup, budget
  proposal, or payments account is created, changed, or removed.
- The `linked-customer-id` header, which only partners with a product link use.
- `GoogleAdsService.SearchStream`, page tokens of Google, and the `page_size` of a search, which Google no
  longer accepts. `reports.search` has no cursor; the [report tools](reports.md) say how to go on.
- Any free query, path, or header.

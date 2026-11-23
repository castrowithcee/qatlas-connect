// Package googleads implements access to the Google Ads API, version v26, over its REST interface with one
// credential that issues access tokens: a person's consent, a service account key, or the impersonation of a
// service account, all through the token core of internal/provider. The only scope is adwords; no developer
// token is sent.
//
// Every request goes to the one fixed origin with a fixed path, the access token in the Authorization header
// only, within a bounded size and time, and spaced per credential. The manager account that Google evaluates
// the rights against travels in the login-customer-id header; it is the service option login_customer_id,
// never an argument. A connection may be bound to manager and customer accounts; the account of a call is
// checked against them before any secret is read, and an account below a manager target is proved with one
// read of the manager's hierarchy before the tool reads it. Queries are GAQL that this package builds from
// checked arguments. Everything Google returns is untrusted third-party content: text and lists are bounded.
package googleads

import (
	"context"
	"errors"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.ads"

// subject names Google in the messages of this provider.
const subject = "Google Ads"

// readTools are the tools of the profile read.
var readTools = []string{customersListAccessibleID, customersGetID, customerClientsListID, reportsSearchID,
	fieldsSearchID, fieldsGetID, customerUserAccessesListID, customerUserAccessInvitationsListID,
	multiPartyAuthReviewsListID, customerManagerLinksListID, customerClientLinksListID, accountLinksListID,
	productLinksListID, productLinkInvitationsListID, dataLinksListID, billingSetupsListID, accountBudgetsListID,
	accountBudgetProposalsListID, paymentsAccountsListID, invoicesListID, incentivesFetchID,
	thirdPartyAppAnalyticsLinksListID,
	recommendationsGenerateID}

// TestConnection obtains an access token, checks its scope, and lists the accounts the credential reaches
// directly. That call costs one operation of the daily quota and nothing else, and proves the token, the
// scope and the enabled API; it proves neither the access level of the Cloud project for production accounts
// nor the access to a target.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	const op = "test connection"
	conn, err := connectionOf(resolved)
	if err != nil {
		return testClass(err)
	}
	if secrets == nil {
		return testClass(failure(op, "no credential resolver was configured"))
	}
	probe := func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
		if _, err := withSource(resolved, conn, source).accessible(ctx, op); err != nil {
			return "", err
		}
		return provider.ClassOK, nil
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(), probe)
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// Register adds the provider metadata, its connection test, its consent endpoints, and its tools.
func Register(reg *capability.Registry) error {
	token := tokenMetadata
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Ads", DefaultBaseURL: apiRoot,
		Description:    "Google Ads API v26: accounts, links, settings, access, billing, invoices, incentives, campaigns, assets, conversions, remarketing, audiences, recommendations, reports and fields, and changes",
		DefaultEffects: []config.Effect{config.EffectRead},
		ValidateBaseURL: func(raw string) error {
			if !isAPIRoot(raw) {
				return errors.New("a Google Ads service must use the official API origin " + apiRoot)
			}
			return nil
		},
		SecretRoles: config.TokenSecretRoles(),
		Token:       &token,
		Target:      targetMetadata,
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "lists the accounts the credential reaches, reads account settings, the hierarchy " +
				"below a manager account and the users of an account, lists the invitations and the multi-party approval requests of an account, lists its manager, client, account, product and data links, lists the billing setups, account budgets, budget proposals and payments accounts of an account, lists the invoices of a billing setup, fetches the incentive offer of the user and lists the third-party app analytics links of an account, generates recommendations for a campaign described in the arguments, runs reports and reads the field reference; the adwords scope is the only one Google offers and also allows changes, " +
				"although no tool of this profile changes anything",
			Tools: readTools,
		}},
	}, TestConnection); err != nil {
		return err
	}
	oauth := googleapi.OAuth
	oauth.Transport = transport
	if err := reg.RegisterOAuth(Provider, oauth); err != nil {
		return err
	}
	return reg.Register(Provider,
		capability.Operation{Descriptor: customersListAccessible, Handler: handleCustomersListAccessible},
		capability.Operation{Descriptor: customersGet, Handler: handleCustomersGet},
		capability.Operation{Descriptor: customerClientsList, Handler: handleCustomerClientsList},
		capability.Operation{Descriptor: reportsSearch, Handler: handleReportsSearch},
		capability.Operation{Descriptor: fieldsSearch, Handler: handleFieldsSearch},
		capability.Operation{Descriptor: fieldsGet, Handler: handleFieldsGet},
		capability.Operation{Descriptor: customersUpdate, Handler: handleCustomersUpdate,
			ConfirmationSummary: customerUpdateSummary},
		capability.Operation{Descriptor: conversionActionsCreate, Handler: handleConversionActionsCreate,
			ConfirmationSummary: conversionActionsSummary(conversionActionsCreateID)},
		capability.Operation{Descriptor: conversionActionsUpdate, Handler: handleConversionActionsUpdate,
			ConfirmationSummary: conversionActionsSummary(conversionActionsUpdateID)},
		capability.Operation{Descriptor: conversionActionsRemove, Handler: handleConversionActionsRemove,
			ConfirmationSummary: conversionActionsSummary(conversionActionsRemoveID)},
		capability.Operation{Descriptor: customerBudgetNetworkSettingsCreate, Handler: handleBudgetNetworkCreate,
			ConfirmationSummary: budgetNetworkSummary("Create")},
		capability.Operation{Descriptor: customerBudgetNetworkSettingsUpdate, Handler: handleBudgetNetworkUpdate,
			ConfirmationSummary: budgetNetworkSummary("Replace")},
		capability.Operation{Descriptor: conversionCustomVariablesCreate, Handler: handleConversionCustomVariablesCreate,
			ConfirmationSummary: conversionVariablesSummary(true)},
		capability.Operation{Descriptor: conversionCustomVariablesUpdate, Handler: handleConversionCustomVariablesUpdate,
			ConfirmationSummary: conversionVariablesSummary(false)},
		capability.Operation{Descriptor: remarketingActionsCreate, Handler: handleRemarketingActionsCreate,
			ConfirmationSummary: remarketingActionsSummary(true)},
		capability.Operation{Descriptor: remarketingActionsUpdate, Handler: handleRemarketingActionsUpdate,
			ConfirmationSummary: remarketingActionsSummary(false)},
		capability.Operation{Descriptor: campaignsCreate, Handler: handleCampaignsCreate,
			ConfirmationSummary: campaignSummary(campaignsCreateID)},
		capability.Operation{Descriptor: campaignsUpdate, Handler: handleCampaignsUpdate,
			ConfirmationSummary: campaignSummary(campaignsUpdateID)},
		capability.Operation{Descriptor: campaignsRemove, Handler: handleCampaignsRemove,
			ConfirmationSummary: campaignSummary(campaignsRemoveID)},
		capability.Operation{Descriptor: adGroupsCreate, Handler: handleAdGroupsCreate,
			ConfirmationSummary: adGroupSummary(adGroupsCreateID)},
		capability.Operation{Descriptor: adGroupsUpdate, Handler: handleAdGroupsUpdate,
			ConfirmationSummary: adGroupSummary(adGroupsUpdateID)},
		capability.Operation{Descriptor: adGroupsRemove, Handler: handleAdGroupsRemove,
			ConfirmationSummary: adGroupSummary(adGroupsRemoveID)},
		capability.Operation{Descriptor: adGroupAdsCreate, Handler: handleAdGroupAdsCreate,
			ConfirmationSummary: adGroupAdSummary(adGroupAdsCreateID)},
		capability.Operation{Descriptor: adGroupAdsUpdate, Handler: handleAdGroupAdsUpdate,
			ConfirmationSummary: adGroupAdSummary(adGroupAdsUpdateID)},
		capability.Operation{Descriptor: adGroupAdsRemove, Handler: handleAdGroupAdsRemove,
			ConfirmationSummary: adGroupAdSummary(adGroupAdsRemoveID)},
		capability.Operation{Descriptor: adGroupAdsRemoveAutomaticallyCreatedAssets,
			Handler: handleAdGroupAdsRemoveAutomaticallyCreatedAssets, ConfirmationSummary: adAssetRemovalSummary},
		capability.Operation{Descriptor: adsUpdate, Handler: handleAdsUpdate, ConfirmationSummary: adUpdateSummary},
		capability.Operation{Descriptor: adParametersCreate, Handler: handleAdParametersCreate,
			ConfirmationSummary: adParameterSummary(adParametersCreateID)},
		capability.Operation{Descriptor: adParametersUpdate, Handler: handleAdParametersUpdate,
			ConfirmationSummary: adParameterSummary(adParametersUpdateID)},
		capability.Operation{Descriptor: adParametersRemove, Handler: handleAdParametersRemove,
			ConfirmationSummary: adParameterSummary(adParametersRemoveID)},
		capability.Operation{Descriptor: campaignsEnablePMaxBrandGuidelines, Handler: handleCampaignsEnablePMaxBrandGuidelines,
			ConfirmationSummary: brandGuidelinesSummary},
		capability.Operation{Descriptor: customerUserAccessesList, Handler: handleUserAccessesList},
		capability.Operation{Descriptor: customerUserAccessesUpdate, Handler: handleUserAccessUpdate,
			ConfirmationSummary: userAccessSummary(true)},
		capability.Operation{Descriptor: customerUserAccessesRemove, Handler: handleUserAccessRemove,
			ConfirmationSummary: userAccessSummary(false)},
		capability.Operation{Descriptor: customerUserAccessInvitationsList, Handler: handleInvitationsList},
		capability.Operation{Descriptor: customerUserAccessInvitationsRemove, Handler: handleInvitationsRemove,
			ConfirmationSummary: invitationRemoveSummary},
		capability.Operation{Descriptor: multiPartyAuthReviewsList, Handler: handleReviewsList},
		capability.Operation{Descriptor: multiPartyAuthReviewsResolve, Handler: handleReviewsResolve,
			ConfirmationSummary: reviewResolveSummary},
		capability.Operation{Descriptor: customAudiencesCreate, Handler: handleCustomAudiencesCreate,
			ConfirmationSummary: customAudienceSummary(false)},
		capability.Operation{Descriptor: customAudiencesUpdate, Handler: handleCustomAudiencesUpdate,
			ConfirmationSummary: customAudienceSummary(true)},
		capability.Operation{Descriptor: customAudiencesRemove, Handler: handleCustomAudiencesRemove,
			ConfirmationSummary: customAudienceRemoveSummary},
		capability.Operation{Descriptor: customInterestsCreate, Handler: handleCustomInterestsCreate,
			ConfirmationSummary: customInterestsCreateSummary},
		capability.Operation{Descriptor: customInterestsUpdate, Handler: handleCustomInterestsUpdate,
			ConfirmationSummary: customInterestsUpdateSummary},
		capability.Operation{Descriptor: audiencesCreate, Handler: handleAudiencesCreate,
			ConfirmationSummary: audiencesCreateSummary},
		capability.Operation{Descriptor: audiencesUpdate, Handler: handleAudiencesUpdate,
			ConfirmationSummary: audiencesUpdateSummary},
		capability.Operation{Descriptor: customerManagerLinksList, Handler: handleManagerLinksList},
		capability.Operation{Descriptor: customerManagerLinksUpdate, Handler: handleManagerLinkUpdate,
			ConfirmationSummary: linkUpdateSummary(true)},
		capability.Operation{Descriptor: customerClientLinksList, Handler: handleClientLinksList},
		capability.Operation{Descriptor: customerClientLinksUpdate, Handler: handleClientLinkUpdate,
			ConfirmationSummary: linkUpdateSummary(false)},
		capability.Operation{Descriptor: accountLinksList, Handler: handleAccountLinksList},
		capability.Operation{Descriptor: productLinksList, Handler: handleProductLinksList},
		capability.Operation{Descriptor: productLinkInvitationsList, Handler: handleProductLinkInvitationsList},
		capability.Operation{Descriptor: dataLinksList, Handler: handleDataLinksList},
		capability.Operation{Descriptor: accountLinksRemove, Handler: handleLinkRemoval(accountLinksRemoveID),
			ConfirmationSummary: linkRemovalSummary(accountLinksRemoveID)},
		capability.Operation{Descriptor: productLinksRemove, Handler: handleLinkRemoval(productLinksRemoveID),
			ConfirmationSummary: linkRemovalSummary(productLinksRemoveID)},
		capability.Operation{Descriptor: productLinkInvitationsUpdate, Handler: handleLinkRemoval(productLinkInvitationsUpdateID),
			ConfirmationSummary: linkRemovalSummary(productLinkInvitationsUpdateID)},
		capability.Operation{Descriptor: dataLinksRemove, Handler: handleLinkRemoval(dataLinksRemoveID),
			ConfirmationSummary: linkRemovalSummary(dataLinksRemoveID)},
		capability.Operation{Descriptor: dataLinksUpdate, Handler: handleLinkRemoval(dataLinksUpdateID),
			ConfirmationSummary: linkRemovalSummary(dataLinksUpdateID)},
		capability.Operation{Descriptor: assetsCreate, Handler: handleAssetsCreate,
			ConfirmationSummary: assetSummary("Create")},
		capability.Operation{Descriptor: assetsUpdate, Handler: handleAssetsUpdate,
			ConfirmationSummary: assetSummary("Change")},
		capability.Operation{Descriptor: campaignAutomaticallyCreatedAssetsRemove,
			Handler:             handleCampaignAutomaticallyCreatedAssetsRemove,
			ConfirmationSummary: removalSummary},
		capability.Operation{Descriptor: campaignAssetsCreate, Handler: handleCampaignAssetsCreate,
			ConfirmationSummary: campaignAssetsCreateSummary},
		capability.Operation{Descriptor: campaignAssetsUpdate, Handler: handleCampaignAssetsUpdate,
			ConfirmationSummary: campaignAssetsUpdateSummary},
		capability.Operation{Descriptor: campaignAssetsRemove, Handler: handleCampaignAssetsRemove,
			ConfirmationSummary: campaignAssetsRemoveSummary},
		capability.Operation{Descriptor: adGroupAssetsCreate, Handler: handleAdGroupAssetsCreate,
			ConfirmationSummary: adGroupAssetsCreateSummary},
		capability.Operation{Descriptor: adGroupAssetsUpdate, Handler: handleAdGroupAssetsUpdate,
			ConfirmationSummary: adGroupAssetsUpdateSummary},
		capability.Operation{Descriptor: adGroupAssetsRemove, Handler: handleAdGroupAssetsRemove,
			ConfirmationSummary: adGroupAssetsRemoveSummary},
		capability.Operation{Descriptor: userListsCreate, Handler: userListsHandleCreate,
			ConfirmationSummary: userListsSummary(true)},
		capability.Operation{Descriptor: userListsUpdate, Handler: userListsHandleUpdate,
			ConfirmationSummary: userListsSummary(false)},
		capability.Operation{Descriptor: userListsRemove, Handler: userListsHandleRemove,
			ConfirmationSummary: userListsRemoveSummary},
		capability.Operation{Descriptor: userListCustomerTypesCreate, Handler: userListHandleCustomerTypesCreate,
			ConfirmationSummary: userListCustomerTypesSummary(true)},
		capability.Operation{Descriptor: userListCustomerTypesRemove, Handler: userListHandleCustomerTypesRemove,
			ConfirmationSummary: userListCustomerTypesSummary(false)},
		capability.Operation{Descriptor: billingSetupsList, Handler: handleBillingSetupsList},
		capability.Operation{Descriptor: accountBudgetsList, Handler: handleAccountBudgetsList},
		capability.Operation{Descriptor: accountBudgetProposalsList, Handler: handleAccountBudgetProposalsList},
		capability.Operation{Descriptor: paymentsAccountsList, Handler: handlePaymentsAccountsList},
		capability.Operation{Descriptor: invoicesList, Handler: handleInvoicesList},
		capability.Operation{Descriptor: incentivesFetch, Handler: handleIncentivesFetch},
		capability.Operation{Descriptor: thirdPartyAppAnalyticsLinksList, Handler: handleAppAnalyticsLinksList},
		capability.Operation{Descriptor: campaignBudgetsCreate, Handler: handleCampaignBudgetsCreate,
			ConfirmationSummary: campaignBudgetsCreateSummary},
		capability.Operation{Descriptor: campaignBudgetsUpdate, Handler: handleCampaignBudgetsUpdate,
			ConfirmationSummary: campaignBudgetsUpdateSummary},
		capability.Operation{Descriptor: campaignBudgetsRemove, Handler: handleCampaignBudgetsRemove,
			ConfirmationSummary: campaignBudgetsRemoveSummary},
		capability.Operation{Descriptor: labelsCreate, Handler: handleLabelsCreate,
			ConfirmationSummary: labelsCreateSummary},
		capability.Operation{Descriptor: labelsUpdate, Handler: handleLabelsUpdate,
			ConfirmationSummary: labelsUpdateSummary},
		capability.Operation{Descriptor: labelsRemove, Handler: handleLabelsRemove,
			ConfirmationSummary: labelsRemoveSummary},
		capability.Operation{Descriptor: campaignGroupsCreate, Handler: handleCampaignGroupsCreate,
			ConfirmationSummary: campaignGroupsCreateSummary},
		capability.Operation{Descriptor: campaignGroupsUpdate, Handler: handleCampaignGroupsUpdate,
			ConfirmationSummary: campaignGroupsUpdateSummary},
		capability.Operation{Descriptor: campaignGroupsRemove, Handler: handleCampaignGroupsRemove,
			ConfirmationSummary: campaignGroupsRemoveSummary},
		capability.Operation{Descriptor: biddingStrategiesCreate, Handler: handleBiddingStrategiesCreate,
			ConfirmationSummary: biddingStrategySummary(biddingStrategiesCreateID)},
		capability.Operation{Descriptor: biddingStrategiesUpdate, Handler: handleBiddingStrategiesUpdate,
			ConfirmationSummary: biddingStrategySummary(biddingStrategiesUpdateID)},
		capability.Operation{Descriptor: biddingStrategiesRemove, Handler: handleBiddingStrategiesRemove,
			ConfirmationSummary: biddingStrategySummary(biddingStrategiesRemoveID)},
		capability.Operation{Descriptor: bdeCreate, Handler: handleBiddingDataExclusionsCreate,
			ConfirmationSummary: bdeCreateSummary},
		capability.Operation{Descriptor: bdeUpdate, Handler: handleBiddingDataExclusionsUpdate,
			ConfirmationSummary: bdeUpdateSummary},
		capability.Operation{Descriptor: bdeRemove, Handler: handleBiddingDataExclusionsRemove,
			ConfirmationSummary: bdeRemoveSummary},
		capability.Operation{Descriptor: recommendationsGenerate, Handler: handleRecommendationsGenerate},
		capability.Operation{Descriptor: recommendationsApply, Handler: handleRecommendationsApply,
			ConfirmationSummary: recommendationApplySummary},
		capability.Operation{Descriptor: recommendationsDismiss, Handler: handleRecommendationsDismiss,
			ConfirmationSummary: recommendationDismissSummary},
		capability.Operation{Descriptor: assetSetsCreate, Handler: handleAssetSetsCreate,
			ConfirmationSummary: assetSetSummary(assetSetsCreateID)},
		capability.Operation{Descriptor: assetSetsUpdate, Handler: handleAssetSetsUpdate,
			ConfirmationSummary: assetSetSummary(assetSetsUpdateID)},
		capability.Operation{Descriptor: assetSetsRemove, Handler: handleAssetSetsRemove,
			ConfirmationSummary: assetSetSummary(assetSetsRemoveID)},
		capability.Operation{Descriptor: assetSetAssetsCreate, Handler: assetSetAssetsFamily.handler("create"),
			ConfirmationSummary: assetSetAssetsFamily.summary("create")},
		capability.Operation{Descriptor: assetSetAssetsRemove, Handler: assetSetAssetsFamily.handler("remove"),
			ConfirmationSummary: assetSetAssetsFamily.summary("remove")},
		capability.Operation{Descriptor: campaignAssetSetsCreate, Handler: campaignAssetSetsFamily.handler("create"),
			ConfirmationSummary: campaignAssetSetsFamily.summary("create")},
		capability.Operation{Descriptor: campaignAssetSetsRemove, Handler: campaignAssetSetsFamily.handler("remove"),
			ConfirmationSummary: campaignAssetSetsFamily.summary("remove")},
		capability.Operation{Descriptor: adGroupAssetSetsCreate, Handler: adGroupAssetSetsFamily.handler("create"),
			ConfirmationSummary: adGroupAssetSetsFamily.summary("create")},
		capability.Operation{Descriptor: adGroupAssetSetsRemove, Handler: adGroupAssetSetsFamily.handler("remove"),
			ConfirmationSummary: adGroupAssetSetsFamily.summary("remove")},
		capability.Operation{Descriptor: customerAssetSetsCreate, Handler: customerAssetSetsFamily.handler("create"),
			ConfirmationSummary: customerAssetSetsFamily.summary("create")},
		capability.Operation{Descriptor: customerAssetSetsRemove, Handler: customerAssetSetsFamily.handler("remove"),
			ConfirmationSummary: customerAssetSetsFamily.summary("remove")},
		capability.Operation{Descriptor: campaignCriteriaCreate, Handler: handleCampaignCriteriaCreate,
			ConfirmationSummary: campaignCriteriaSummary(campaignCriteriaCreateID)},
		capability.Operation{Descriptor: campaignCriteriaUpdate, Handler: handleCampaignCriteriaUpdate,
			ConfirmationSummary: campaignCriteriaSummary(campaignCriteriaUpdateID)},
		capability.Operation{Descriptor: campaignCriteriaRemove, Handler: handleCampaignCriteriaRemove,
			ConfirmationSummary: campaignCriteriaSummary(campaignCriteriaRemoveID)})
}

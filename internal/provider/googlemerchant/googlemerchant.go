// Package googlemerchant implements access to the Google Merchant API v1 with a token credential: a person's
// consent through the user's own OAuth client, a service account key, or the impersonation of a service
// account. The access token comes from provider.TokenSource and travels only in the Authorization header to
// the one fixed origin of the API; no request follows a redirect.
//
// A connection reaches what the credential reaches, or is bound to advanced accounts and accounts. Every
// account argument is checked against the ID form and the targets before any secret is resolved; an account
// that no target names is bound to an advanced account by one read of its sub-accounts. The tools that act on
// an advanced account as a whole need that account as a target. Everything Google returns is untrusted
// content.
package googlemerchant

import (
	"context"
	"maps"
	"net/url"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.merchant"

// TestConnection obtains an access token, which tells a missing, expired, or revoked consent and a missing
// scope apart, and then reads one account without cost: the first bound account, or the first account the
// credential lists. When that account is a bound advanced account, or the connection has no target, it also
// reads the developer registration of the account. A registration without any Cloud project is reported as
// permission, because Google refuses the calls of an unregistered project. The test proves the token, the
// scope, the enabled API, and access to one account, not the role for a particular method.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return provider.ClassProviderError, nil
	}
	if secrets == nil {
		return "", provider.Fail("test connection", "no credential resolver was configured")
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(),
		func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
			const op = "test connection"
			c, err := withSource(ctx, resolved, secrets, source)
			if err != nil {
				return "", err
			}
			id, advanced := "", false
			switch {
			case len(bound.advanced) > 0:
				id, advanced = bound.advanced[0], true
			case len(bound.accounts) > 0:
				id = bound.accounts[0]
			}
			if id != "" {
				var out account
				if err := c.get(ctx, op, accountsGetID, accountPath(id), nil, &out); err != nil {
					return "", err
				}
			} else {
				var answer accountsAnswer
				if err := c.get(ctx, op, accountsListID, accountsAPI+"accounts", url.Values{"pageSize": {"1"}},
					&answer); err != nil {
					return "", err
				}
				if len(answer.Accounts) == 0 {
					return provider.ClassNotFound, nil
				}
				var ok bool
				if id, ok = answer.Accounts[0].id(); !ok {
					return provider.ClassInvalidResponse, nil
				}
				advanced = true
			}
			if !advanced {
				return provider.ClassOK, nil
			}
			registration, err := c.developerRegistration(ctx, op, developerRegistrationsGetID, id)
			if err != nil {
				return "", err
			}
			if len(registration.GcpIDs) == 0 {
				return provider.ClassPermission, nil
			}
			return provider.ClassOK, nil
		})
}

// Register adds the provider metadata, its consent endpoints, its connection test, and its tools.
func Register(reg *capability.Registry) error {
	var readIDs []string
	for _, d := range descriptors() {
		if d.Risk.Effect == capability.EffectRead && !d.RequiresToolAllowList {
			readIDs = append(readIDs, d.ID)
		}
	}
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Merchant Center", DefaultBaseURL: apiRoot,
		Description:     "Merchant Center through the Merchant API, with a token credential from your own Google Cloud project",
		DefaultEffects:  []config.Effect{config.EffectRead},
		ValidateBaseURL: validateBaseURL,
		SecretRoles:     config.TokenSecretRoles(),
		Token:           &config.TokenMetadata{Scopes: toolScopes()},
		Target: config.TargetMetadata{
			Label: "advanced account or account",
			Description: "optional: advanced-accounts/ACCOUNT binds an advanced account with its direct sub-accounts " +
				"and the tools that act on the advanced account itself, accounts/ACCOUNT binds one account; without " +
				"a target a connection reaches what the credential reaches",
			Multiple:    true,
			Kinds:       targetKinds,
			Validate:    validateTarget,
			ValidateSet: validateTargetSet,
			Reaches:     reaches,
		},
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "reads Merchant Center accounts, the developer registration of an account, the users of an account, data sources, product reviews, products, the local and regional inventories of a product, issues, reports, quotas, and limits; changes nothing",
			Tools:       readIDs,
		}},
	}, TestConnection); err != nil {
		return err
	}
	if err := reg.RegisterOAuth(Provider, googleapi.OAuth); err != nil {
		return err
	}
	handlers := map[string]capability.Handler{
		accountsListID: handleAccountsList, accountsGetID: handleAccountsGet,
		developerRegistrationsGetID: handleDeveloperRegistrationsGet,
		termsOfServiceGetID:         handleTermsOfServiceGet, termsOfServiceRetrieveLatestID: handleTermsOfServiceRetrieveLatest,
		termsOfServiceAgreementStatesGetID:            handleAgreementStatesGet,
		termsOfServiceAgreementStatesForApplicationID: handleAgreementStatesRetrieveForApplication,
		reportsSearchID: handleReportsSearch, quotasListID: handleQuotasList, limitsListID: handleLimitsList,
		limitsGetID:    handleLimitsGet,
		programsListID: handleProgramsList, programsGetID: handleProgramsGet,
		programsEnableID: handleProgramsEnable, programsDisableID: handleProgramsDisable,
		autofeedSettingsGetID: handleAutofeedSettingsGet, autofeedSettingsUpdateID: handleAutofeedSettingsUpdate,
		automaticImprovementsGetID: handleAutomaticImprovementsGet, automaticImprovementsUpdateID: handleAutomaticImprovementsUpdate,
		usersListID: handleUsersList, usersGetID: handleUsersGet,
		usersUpdateID: handleUsersUpdate, usersDeleteID: handleUsersDelete,
		regionsListID: handleRegionsList, regionsGetID: handleRegionsGet,
		regionsCreateID: handleRegionsCreate, regionsUpdateID: handleRegionsUpdate,
		notificationSubscriptionsListID:   handleNotificationSubscriptionsList,
		notificationSubscriptionsGetID:    handleNotificationSubscriptionsGet,
		notificationSubscriptionsDeleteID: handleNotificationSubscriptionsDelete,
		accountServicesListID:             handleAccountServicesList, accountServicesGetID: handleAccountServicesGet,
		accountServicesRejectID: handleAccountServicesReject, accountRelationshipsListID: handleAccountRelationshipsList,
		conversionSourcesListID: handleConversionSourcesList, conversionSourcesGetID: handleConversionSourcesGet,
		conversionSourcesCreateID: handleConversionSourcesCreate, conversionSourcesUpdateID: handleConversionSourcesUpdate,
		conversionSourcesUndeleteID: handleConversionSourcesUndelete, conversionSourcesDeleteID: handleConversionSourcesDelete,
		renderAccountIssuesID: handleRenderAccountIssues, renderProductIssuesID: handleRenderProductIssues,
		emailPreferencesGetID: handleEmailPreferencesGet, emailPreferencesUpdateID: handleEmailPreferencesUpdate,
		regionsDeleteID: handleRegionsDelete, regionsBatchCreateID: handleRegionsBatchCreate,
		regionsBatchUpdateID: handleRegionsBatchUpdate, regionsBatchDeleteID: handleRegionsBatchDelete,
		productsListID: handleProductsList, productsGetID: handleProductsGet,
		aggregateProductStatusesListID: handleAggregateProductStatusesList, accountIssuesListID: handleAccountIssuesList,
		dataSourcesListID: handleDataSourcesList, dataSourcesGetID: handleDataSourcesGet,
		dataSourcesCreateID: handleDataSourcesCreate, dataSourcesUpdateID: handleDataSourcesUpdate,
		localInventoriesListID: handleLocalInventoriesList, localInventoriesInsertID: handleLocalInventoriesInsert,
		localInventoriesDeleteID:  handleLocalInventoriesDelete,
		regionalInventoriesListID: handleRegionalInventoriesList, regionalInventoriesInsertID: handleRegionalInventoriesInsert,
		regionalInventoriesDeleteID: handleRegionalInventoriesDelete,
		promotionsListID:            handlePromotionsList, promotionsGetID: handlePromotionsGet,
		promotionsInsertID: handlePromotionsInsert,
		businessInfosGetID: handleBusinessInfosGet, businessInfosUpdateID: handleBusinessInfosUpdate,
		businessIdentitiesGetID: handleBusinessIdentitiesGet, businessIdentitiesUpdateID: handleBusinessIdentitiesUpdate,
	}
	summaries := map[string]capability.SummaryFunc{
		programsEnableID: programSummary("Enable"), programsDisableID: programSummary("Disable"),
		autofeedSettingsUpdateID: autofeedSummary, automaticImprovementsUpdateID: improvementsSummary,
		usersUpdateID: usersUpdateSummary, usersDeleteID: usersDeleteSummary,
		regionsCreateID: regionCreateSummary, regionsUpdateID: regionUpdateSummary,
		notificationSubscriptionsDeleteID: notificationDeleteSummary,
		accountServicesRejectID:           serviceRejectSummary,
		conversionSourcesCreateID:         conversionCreateSummary, conversionSourcesUpdateID: conversionUpdateSummary,
		conversionSourcesUndeleteID: conversionUndeleteSummary, conversionSourcesDeleteID: conversionDeleteSummary,
		emailPreferencesUpdateID: emailPreferencesSummary,
		regionsDeleteID:          regionDeleteSummary,
		localInventoriesInsertID: localInventoryInsertSummary, localInventoriesDeleteID: localInventoryDeleteSummary,
		regionalInventoriesInsertID: regionalInventoryInsertSummary, regionalInventoriesDeleteID: regionalInventoryDeleteSummary,
		promotionsInsertID: promotionSummary,
		businessInfosUpdateID: businessSummary("the business info", businessInfoPaths,
			"this can change the address and the contacts Google shows to customers"),
		businessIdentitiesUpdateID: businessSummary("the business identity", businessIdentityPaths,
			"this can change how Google presents the business in promotions"),
	}
	maps.Copy(summaries, regionBatchSummaries())
	maps.Copy(handlers, accountWriteHandlers())
	maps.Copy(summaries, accountSummaries())
	maps.Copy(handlers, homepageHandlers())
	maps.Copy(summaries, homepageSummaries())
	maps.Copy(handlers, omnichannelHandlers())
	maps.Copy(summaries, omnichannelSummaries())
	maps.Copy(handlers, checkoutHandlers())
	maps.Copy(summaries, checkoutSummaries())
	maps.Copy(handlers, returnPolicyHandlers())
	maps.Copy(summaries, returnPolicySummaries())
	maps.Copy(handlers, shippingHandlers())
	maps.Copy(summaries, shippingSummaries())
	maps.Copy(handlers, productInputHandlers())
	maps.Copy(summaries, productInputSummaries())
	maps.Copy(handlers, reviewHandlers())
	maps.Copy(summaries, reviewSummaries())
	maps.Copy(handlers, relationshipHandlers())
	maps.Copy(summaries, relationshipSummaries())
	maps.Copy(handlers, linkHandlers())
	maps.Copy(handlers, dataSourceOperationHandlers())
	maps.Copy(summaries, dataSourceOperationSummaries())
	maps.Copy(handlers, developerRegistrationHandlers())
	maps.Copy(summaries, developerRegistrationSummaries())
	for _, d := range descriptors() {
		if err := reg.Register(Provider, capability.Operation{Descriptor: d, Handler: handlers[d.ID],
			ConfirmationSummary: summaries[d.ID]}); err != nil {
			return err
		}
	}
	return nil
}

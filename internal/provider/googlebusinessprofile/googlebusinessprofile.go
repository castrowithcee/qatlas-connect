// Package googlebusinessprofile implements access to the Google Business Profile APIs v1: the accounts a
// person manages, the business locations in them, and the performance of a location: daily metrics and
// search keywords.
//
// The credential is a person's consent through the user's own OAuth client; a service account key and the
// impersonation of a service account are not offered, since Google documents no service account as a
// manager of a business profile. The access token comes from provider.TokenSource and travels only in the
// Authorization header to the fixed origin of each API; no request follows a redirect.
//
// Every call needs a Cloud project that Google approved for the Business Profile APIs. Until then Google
// grants a quota of zero requests, which the connection test reports as a class of its own.
//
// A connection reaches what the credential reaches, or is bound to one account (also a location group) or
// one location. Every path argument is checked against its form and against the target before any secret
// is resolved. Everything Google returns, such as names, addresses, phone numbers and websites, is untrusted
// content: texts and lists are cut and addresses are never fetched.
package googlebusinessprofile

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
const Provider = "google.business-profile"

// scopeManage is the only scope the discovery documents of the Business Profile APIs name, for reading as
// for changing; Google offers no read-only scope.
const scopeManage = "https://www.googleapis.com/auth/business.manage"

// toolScopes maps every tool to the smallest scopes it needs.
var toolScopes = map[string][]string{
	accountsListID:  {scopeManage},
	accountsGetID:   {scopeManage},
	locationsListID: {scopeManage},
	locationsGetID:  {scopeManage},

	accountAdminsListID:    {scopeManage},
	accountAdminsUpdateID:  {scopeManage},
	locationAdminsListID:   {scopeManage},
	locationAdminsUpdateID: {scopeManage},
	accountsUpdateID:       {scopeManage},

	performanceGetDailyID:   {scopeManage},
	performanceFetchDailyID: {scopeManage},
	performanceKeywordsID:   {scopeManage},

	categoriesListID:             {scopeManage},
	categoriesBatchGetID:         {scopeManage},
	chainsGetID:                  {scopeManage},
	chainsSearchID:               {scopeManage},
	googleLocationsSearchID:      {scopeManage},
	verificationsListID:          {scopeManage},
	verificationsFetchOptionsID:  {scopeManage},
	verificationsMerchantStateID: {scopeManage},
	notificationsGetID:           {scopeManage},
}

// ClassAccessNotApproved is the outcome of a Cloud project that Google has not approved for the Business
// Profile APIs: its quota is zero requests per minute until the request for access is granted.
const ClassAccessNotApproved provider.Class = "access-not-approved"

const testOp = "test connection"

// TestConnection obtains an access token the way a call does, which tells a missing, expired, or revoked
// consent and a missing scope apart, and then lists one account, and for an account target reads that
// account. A location target reads its location with the read mask name instead and lists no account. It
// proves the token, the enabled API, and the approval of the Cloud project, not a role on a location. A rate
// limit on the first request is the missing approval.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	bound, err := checkConnection(resolved)
	if err != nil {
		return testClass(err)
	}
	if secrets == nil {
		return "", provider.Fail(testOp, "no credential resolver was configured")
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(),
		func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
			c, err := withSource(ctx, resolved, secrets, source)
			if err != nil {
				return "", err
			}
			if bound.location != "" {
				var out location
				query := readMaskQuery([]string{"name"})
				return provider.ClassOK, c.get(ctx, testOp, locationsGetID, request{root: informationRoot,
					path: locationPath(bound.location), query: query, first: true, notFound: notFoundLocation}, &out)
			}
			var list accountsAnswer
			if err := c.get(ctx, testOp, accountsListID, request{root: accountRoot, path: accountsPath,
				query: pageSizeQuery(nil, 1), first: true, notFound: notFoundAccount}, &list); err != nil {
				return "", err
			}
			if bound.account == "" {
				return provider.ClassOK, nil
			}
			var one account
			return provider.ClassOK, c.get(ctx, testOp, accountsGetID, request{root: accountRoot,
				path: accountPath(bound.account), notFound: notFoundAccount}, &one)
		})
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// readTools are the tools of the profile read.
var readTools = []string{accountsListID, accountsGetID, locationsListID, locationsGetID,
	accountAdminsListID, locationAdminsListID,
	performanceGetDailyID, performanceFetchDailyID, performanceKeywordsID,
	categoriesListID, categoriesBatchGetID, chainsGetID, chainsSearchID, googleLocationsSearchID,
	verificationsListID, verificationsFetchOptionsID, verificationsMerchantStateID, notificationsGetID}

// writeTools are the tools of the profile write.
var writeTools = []string{accountAdminsUpdateID, locationAdminsUpdateID, accountsUpdateID}

// Register adds the provider metadata, its consent endpoints, its connection test, and its tools.
func Register(reg *capability.Registry) error {
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Business Profile", DefaultBaseURL: accountRoot,
		Description: "Business accounts and locations of Google Business Profile, with a consent through your own " +
			"OAuth client from an approved Google Cloud project",
		DefaultEffects:  []config.Effect{config.EffectRead},
		ValidateBaseURL: validateBaseURL,
		SecretRoles:     config.TokenSecretRoles(),
		Token:           &config.TokenMetadata{Scopes: toolScopes},
		Target:          targetMetadata,
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "lists and reads the accounts and the business locations the consenting account manages, " +
				"and the categories, chains, and Google locations of Google's reference data, and changes nothing; " +
				"Google offers only the scope business.manage, which also allows changes, so " +
				"the token may change what its account may, although no tool of this profile changes anything; " +
				"the admin lists name other people and the verification options show masked postal addresses, phone " +
				"numbers, and email addresses, which is personal data",
			Tools: readTools,
		}, {
			ID: "write", Title: "Write",
			Description: "changes the role of existing admins of an account or a location and renames an account, " +
				"each change with the confirmation of a person; no tool invites or creates an admin, makes anyone " +
				"primary owner, or transfers a location; Google offers only the scope business.manage, so the " +
				"token may change what its account may",
			Tools: writeTools,
		}},
	}, TestConnection); err != nil {
		return err
	}
	// Only a person's consent is offered, so the consent endpoints carry no impersonation.
	oauth := googleapi.OAuth
	oauth.ImpersonateURL, oauth.ImpersonateScope = "", ""
	if err := reg.RegisterOAuth(Provider, oauth); err != nil {
		return err
	}
	return reg.Register(Provider,
		capability.Operation{Descriptor: accountsList, Handler: handleAccountsList},
		capability.Operation{Descriptor: accountsGet, Handler: handleAccountsGet},
		capability.Operation{Descriptor: locationsList, Handler: handleLocationsList},
		capability.Operation{Descriptor: locationsGet, Handler: handleLocationsGet},
		capability.Operation{Descriptor: accountAdminsList, Handler: handleAccountAdminsList},
		capability.Operation{Descriptor: locationAdminsList, Handler: handleLocationAdminsList},
		capability.Operation{Descriptor: accountAdminsUpdate, Handler: handleAccountAdminsUpdate,
			ConfirmationSummary: adminSummary},
		capability.Operation{Descriptor: locationAdminsUpdate, Handler: handleLocationAdminsUpdate,
			ConfirmationSummary: adminSummary},
		capability.Operation{Descriptor: accountsUpdate, Handler: handleAccountsUpdate,
			ConfirmationSummary: accountUpdateSummary},
		capability.Operation{Descriptor: performanceGetDaily, Handler: handleGetDailyMetric},
		capability.Operation{Descriptor: performanceFetchDaily, Handler: handleFetchDailyMetrics},
		capability.Operation{Descriptor: performanceKeywords, Handler: handleListSearchKeywords},
		capability.Operation{Descriptor: categoriesList, Handler: handleCategoriesList},
		capability.Operation{Descriptor: categoriesBatchGet, Handler: handleCategoriesBatchGet},
		capability.Operation{Descriptor: chainsGet, Handler: handleChainsGet},
		capability.Operation{Descriptor: chainsSearch, Handler: handleChainsSearch},
		capability.Operation{Descriptor: googleLocationsSearch, Handler: handleGoogleLocationsSearch},
		capability.Operation{Descriptor: verificationsList, Handler: handleVerificationsList},
		capability.Operation{Descriptor: verificationsFetchOptions, Handler: handleVerificationsFetchOptions},
		capability.Operation{Descriptor: verificationsMerchantState, Handler: handleVerificationsMerchantState},
		capability.Operation{Descriptor: notificationsGet, Handler: handleNotificationsGet},
	)
}

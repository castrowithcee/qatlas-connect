// Package googlebusinessprofile implements access to the Google Business Profile APIs v1: the accounts a
// person manages, the business locations in them, the attributes of a location, and the performance of a
// location: daily metrics and search keywords, and the place action links of a location (ordering, booking)
// with their types. Reviews come from the My Business API v4, which Google keeps for them and has no discovery
// document; see v4.go.
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

	locationsBatchGetID: {scopeManage},

	accountAdminsListID:    {scopeManage},
	accountAdminsUpdateID:  {scopeManage},
	locationAdminsListID:   {scopeManage},
	locationAdminsUpdateID: {scopeManage},
	accountsUpdateID:       {scopeManage},

	locationsCreateID:           {scopeManage},
	locationsUpdateID:           {scopeManage},
	locationsDeleteID:           {scopeManage},
	locationsGetGoogleUpdatedID: {scopeManage},

	performanceGetDailyID:   {scopeManage},
	performanceFetchDailyID: {scopeManage},
	performanceKeywordsID:   {scopeManage},

	categoriesListID:        {scopeManage},
	categoriesBatchGetID:    {scopeManage},
	chainsGetID:             {scopeManage},
	chainsSearchID:          {scopeManage},
	googleLocationsSearchID: {scopeManage},

	accountAdminsDeleteID:        {scopeManage},
	locationAdminsDeleteID:       {scopeManage},
	invitationsListID:            {scopeManage},
	invitationsDeclineID:         {scopeManage},
	lodgingGetID:                 {scopeManage},
	lodgingGoogleUpdatedID:       {scopeManage},
	lodgingUpdateID:              {scopeManage},
	verificationsListID:          {scopeManage},
	verificationsFetchOptionsID:  {scopeManage},
	verificationsMerchantStateID: {scopeManage},
	notificationsGetID:           {scopeManage},
	placeActionLinksListID:       {scopeManage},
	placeActionLinksGetID:        {scopeManage},
	placeActionLinksCreateID:     {scopeManage},
	placeActionLinksUpdateID:     {scopeManage},
	placeActionLinksDeleteID:     {scopeManage},
	placeActionTypesListID:       {scopeManage},

	reviewsListID:        {scopeManage},
	reviewsGetID:         {scopeManage},
	reviewsBatchGetID:    {scopeManage},
	reviewsUpdateReplyID: {scopeManage},
	reviewsDeleteReplyID: {scopeManage},

	attributesListID:   {scopeManage},
	attributesGetID:    {scopeManage},
	attributesUpdateID: {scopeManage},
	mediaListID:        {scopeManage},
	mediaGetID:         {scopeManage},
	mediaCreateID:      {scopeManage},
	mediaUpdateID:      {scopeManage},
	mediaDeleteID:      {scopeManage},
	localPostsListID:   {scopeManage},
	localPostsGetID:    {scopeManage},
	localPostsCreateID: {scopeManage},
	localPostsUpdateID: {scopeManage},
	localPostsDeleteID: {scopeManage},

	foodMenusGetID:    {scopeManage},
	foodMenusUpdateID: {scopeManage},
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
	accountAdminsListID, locationAdminsListID, locationsGetGoogleUpdatedID, locationsBatchGetID,
	performanceGetDailyID, performanceFetchDailyID, performanceKeywordsID,
	categoriesListID, categoriesBatchGetID, chainsGetID, chainsSearchID, googleLocationsSearchID,
	invitationsListID, attributesListID, attributesGetID, reviewsListID, reviewsGetID, reviewsBatchGetID,
	mediaListID, mediaGetID, localPostsListID, localPostsGetID, lodgingGetID, lodgingGoogleUpdatedID,
	verificationsListID, verificationsFetchOptionsID, verificationsMerchantStateID, notificationsGetID,
	placeActionLinksListID, placeActionLinksGetID, placeActionTypesListID, foodMenusGetID}

// writeTools are the tools of the profile write.
//
// locations.delete is in no profile: only a tools list offers it.
var writeTools = []string{accountAdminsUpdateID, locationAdminsUpdateID, accountsUpdateID, locationsCreateID,
	locationsUpdateID, accountAdminsDeleteID, locationAdminsDeleteID, invitationsDeclineID, attributesUpdateID,
	reviewsUpdateReplyID, reviewsDeleteReplyID, mediaCreateID, mediaUpdateID, mediaDeleteID, localPostsCreateID,
	localPostsUpdateID, localPostsDeleteID, lodgingUpdateID, placeActionLinksCreateID, placeActionLinksUpdateID,
	placeActionLinksDeleteID, foodMenusUpdateID}

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
			Description: "lists and reads the accounts, the business locations, and the place action links the " +
				"consenting account manages, and the categories, chains, and Google locations of Google's reference " +
				"data, and changes nothing; Google offers only the scope business.manage, which also allows changes, so " +
				"the token may change what its account may, although no tool of this profile changes anything; " +
				"the local posts and the food menus are public profile content, the admin lists name other people, the invitation list " +
				"names the targets of invitations, the " +
				"verification options show masked postal addresses, phone numbers, and email addresses, and the " +
				"reviews name reviewers, which is personal data",
			Tools: readTools,
		}, {
			ID: "write", Title: "Write",
			Description: "changes the role of existing admins of an account or a location, removes existing admins, " +
				"declines invitations, renames an account, sets or removes the public attributes of a location, " +
				"creates and changes business locations, whose data is public, creates, changes, and deletes place " +
				"action links, which are public on the profile, publishes or deletes the public reply of the business " +
				"to a review, adds, recategorizes, and deletes photos and videos of a location, which are public on " +
				"the profile, and creates, changes, and deletes local posts, which are public on the profile and in " +
				"Google Search, and replaces the food menus of a location, which are public on the profile, each change with the confirmation of a person; no tool of this profile deletes a " +
				"location; no tool invites or creates an admin, accepts an invitation, deletes or reports a review, " +
				"makes anyone primary owner, or transfers a location; Google offers only the scope business.manage, " +
				"so the " +
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
		capability.Operation{Descriptor: locationsBatchGet, Handler: handleLocationsBatchGet},
		capability.Operation{Descriptor: accountAdminsList, Handler: handleAccountAdminsList},
		capability.Operation{Descriptor: locationAdminsList, Handler: handleLocationAdminsList},
		capability.Operation{Descriptor: accountAdminsUpdate, Handler: handleAccountAdminsUpdate,
			ConfirmationSummary: adminSummary},
		capability.Operation{Descriptor: locationAdminsUpdate, Handler: handleLocationAdminsUpdate,
			ConfirmationSummary: adminSummary},
		capability.Operation{Descriptor: accountsUpdate, Handler: handleAccountsUpdate,
			ConfirmationSummary: accountUpdateSummary},
		capability.Operation{Descriptor: locationsGetGoogleUpdated, Handler: handleLocationsGetGoogleUpdated},
		capability.Operation{Descriptor: locationsCreate, Handler: handleLocationsCreate,
			ConfirmationSummary: locationCreateSummary},
		capability.Operation{Descriptor: locationsUpdate, Handler: handleLocationsUpdate,
			ConfirmationSummary: locationUpdateSummary},
		capability.Operation{Descriptor: locationsDelete, Handler: handleLocationsDelete,
			ConfirmationSummary: locationDeleteSummary},
		capability.Operation{Descriptor: accountAdminsDelete, Handler: handleAccountAdminsDelete,
			ConfirmationSummary: adminRemoveSummary},
		capability.Operation{Descriptor: locationAdminsDelete, Handler: handleLocationAdminsDelete,
			ConfirmationSummary: adminRemoveSummary},
		capability.Operation{Descriptor: invitationsList, Handler: handleInvitationsList},
		capability.Operation{Descriptor: invitationsDecline, Handler: handleInvitationsDecline,
			ConfirmationSummary: invitationSummary},
		capability.Operation{Descriptor: performanceGetDaily, Handler: handleGetDailyMetric},
		capability.Operation{Descriptor: performanceFetchDaily, Handler: handleFetchDailyMetrics},
		capability.Operation{Descriptor: performanceKeywords, Handler: handleListSearchKeywords},
		capability.Operation{Descriptor: categoriesList, Handler: handleCategoriesList},
		capability.Operation{Descriptor: categoriesBatchGet, Handler: handleCategoriesBatchGet},
		capability.Operation{Descriptor: chainsGet, Handler: handleChainsGet},
		capability.Operation{Descriptor: chainsSearch, Handler: handleChainsSearch},
		capability.Operation{Descriptor: googleLocationsSearch, Handler: handleGoogleLocationsSearch},
		capability.Operation{Descriptor: lodgingGet, Handler: handleLodgingGet},
		capability.Operation{Descriptor: lodgingGoogleUpdated, Handler: handleLodgingGoogleUpdated},
		capability.Operation{Descriptor: lodgingUpdate, Handler: handleLodgingUpdate,
			ConfirmationSummary: lodgingUpdateSummary},
		capability.Operation{Descriptor: verificationsList, Handler: handleVerificationsList},
		capability.Operation{Descriptor: verificationsFetchOptions, Handler: handleVerificationsFetchOptions},
		capability.Operation{Descriptor: verificationsMerchantState, Handler: handleVerificationsMerchantState},
		capability.Operation{Descriptor: notificationsGet, Handler: handleNotificationsGet},
		capability.Operation{Descriptor: reviewsList, Handler: handleReviewsList},
		capability.Operation{Descriptor: reviewsGet, Handler: handleReviewsGet},
		capability.Operation{Descriptor: reviewsBatchGet, Handler: handleReviewsBatchGet},
		capability.Operation{Descriptor: reviewsUpdateReply, Handler: handleReviewsUpdateReply,
			ConfirmationSummary: replyConfirmation},
		capability.Operation{Descriptor: reviewsDeleteReply, Handler: handleReviewsDeleteReply,
			ConfirmationSummary: deleteReplyConfirmation},
		capability.Operation{Descriptor: mediaList, Handler: handleMediaList},
		capability.Operation{Descriptor: mediaGet, Handler: handleMediaGet},
		capability.Operation{Descriptor: mediaCreate, Handler: handleMediaCreate,
			ConfirmationSummary: mediaSummary(mediaCreateID)},
		capability.Operation{Descriptor: mediaUpdate, Handler: handleMediaUpdate,
			ConfirmationSummary: mediaSummary(mediaUpdateID)},
		capability.Operation{Descriptor: mediaDelete, Handler: handleMediaDelete,
			ConfirmationSummary: mediaSummary(mediaDeleteID)},
		capability.Operation{Descriptor: localPostsList, Handler: handleLocalPostsList},
		capability.Operation{Descriptor: localPostsGet, Handler: handleLocalPostsGet},
		capability.Operation{Descriptor: localPostsCreate, Handler: handleLocalPostsCreate,
			ConfirmationSummary: localPostSummary(localPostsCreateID)},
		capability.Operation{Descriptor: localPostsUpdate, Handler: handleLocalPostsUpdate,
			ConfirmationSummary: localPostSummary(localPostsUpdateID)},
		capability.Operation{Descriptor: localPostsDelete, Handler: handleLocalPostsDelete,
			ConfirmationSummary: localPostSummary(localPostsDeleteID)},
		capability.Operation{Descriptor: placeActionLinksList, Handler: handlePlaceActionLinksList},
		capability.Operation{Descriptor: placeActionLinksGet, Handler: handlePlaceActionLinksGet},
		capability.Operation{Descriptor: placeActionLinksCreate, Handler: handlePlaceActionLinksCreate,
			ConfirmationSummary: placeActionSummary(placeActionLinksCreateID)},
		capability.Operation{Descriptor: placeActionLinksUpdate, Handler: handlePlaceActionLinksUpdate,
			ConfirmationSummary: placeActionSummary(placeActionLinksUpdateID)},
		capability.Operation{Descriptor: placeActionLinksDelete, Handler: handlePlaceActionLinksDelete,
			ConfirmationSummary: placeActionSummary(placeActionLinksDeleteID)},
		capability.Operation{Descriptor: placeActionTypesList, Handler: handlePlaceActionTypesList},
		capability.Operation{Descriptor: foodMenusGet, Handler: handleFoodMenusGet},
		capability.Operation{Descriptor: foodMenusUpdate, Handler: handleFoodMenusUpdate,
			ConfirmationSummary: foodMenusSummary},
		capability.Operation{Descriptor: attributesList, Handler: handleAttributesList},
		capability.Operation{Descriptor: attributesGet, Handler: handleAttributesGet},
		capability.Operation{Descriptor: attributesUpdate, Handler: handleAttributesUpdate,
			ConfirmationSummary: attributesSummary},
	)
}

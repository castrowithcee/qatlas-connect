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
	multiPartyAuthReviewsListID, accountLinksListID, productLinksListID, productLinkInvitationsListID,
	dataLinksListID}

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
		Description:    "Google Ads API v26: accounts, hierarchy, settings, user access, invitations, approval requests, account, product and data links, reports and the field reference of Google Ads, and changes to them",
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
				"below a manager account and the users of an account, lists the invitations and the multi-party approval requests of an account, lists its account, product and data links, runs reports and reads the field reference; the adwords scope is the only one Google offers and also allows changes, " +
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
		capability.Operation{Descriptor: customerBudgetNetworkSettingsCreate, Handler: handleBudgetNetworkCreate,
			ConfirmationSummary: budgetNetworkSummary("Create")},
		capability.Operation{Descriptor: customerBudgetNetworkSettingsUpdate, Handler: handleBudgetNetworkUpdate,
			ConfirmationSummary: budgetNetworkSummary("Replace")},
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
		capability.Operation{Descriptor: accountLinksList, Handler: handleAccountLinksList},
		capability.Operation{Descriptor: productLinksList, Handler: handleProductLinksList},
		capability.Operation{Descriptor: productLinkInvitationsList, Handler: handleProductLinkInvitationsList},
		capability.Operation{Descriptor: dataLinksList, Handler: handleDataLinksList})
}

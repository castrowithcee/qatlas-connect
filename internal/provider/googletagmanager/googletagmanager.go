// Package googletagmanager implements access to the Google Tag Manager API v2 with a token credential: a
// person's consent through the user's own OAuth client, a service account key, or the impersonation of a
// service account. The access token comes from provider.TokenSource and travels only in the Authorization
// header to the one fixed origin of the API; no request follows a redirect.
//
// A connection reaches what the credential reaches, or is bound to one account or one container. Every path
// argument is checked against the account, container, and workspace forms and against the target before any
// secret is resolved. Requests of one OAuth client are spaced, because the quota of the API belongs to the
// Cloud project of the client and is shared by all its users. Everything Google returns is untrusted content.
package googletagmanager

import (
	"context"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.tag-manager"

// TestConnection obtains an access token, which tells a missing, expired, or revoked consent and a missing
// scope apart, and then reads the first batch of accounts, or the container of a container target. It proves
// the token and the enabled API, not a role on a particular container.
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
			if bound.container != "" {
				var out container
				path := gtmPath{account: bound.account, container: bound.container}.String()
				return provider.ClassOK, c.get(ctx, op, containersGetID, path, nil, &out)
			}
			var out accountsAnswer
			return provider.ClassOK, c.get(ctx, op, accountsListID, "accounts", nil, &out)
		})
}

// Register adds the provider metadata, its consent endpoints, its connection test, and its tools.
func Register(reg *capability.Registry) error {
	ids := make([]string, 0, len(descriptors()))
	for _, d := range descriptors() {
		ids = append(ids, d.ID)
	}
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Tag Manager", DefaultBaseURL: apiRoot,
		Description:     "Tag management for websites and apps, with a token credential from your own Google Cloud project",
		DefaultEffects:  []config.Effect{config.EffectRead},
		ValidateBaseURL: validateBaseURL,
		SecretRoles:     config.TokenSecretRoles(),
		Token:           &config.TokenMetadata{Scopes: toolScopes()},
		DocPage:         "google.tag-manager.md",
		Target: config.TargetMetadata{
			Label: "target",
			Description: "one account or one container; without a target a connection reaches what the credential " +
				"reaches. A container target answers account reads with its own account and container only",
			Kinds: []config.TargetKind{{
				Name: "account", Description: "one Tag Manager account and everything in it",
				Forms: []string{formAccount},
			}, {
				Name: "container", Description: "one container; its account can be read, nothing else of it",
				Forms: []string{formContainer}, Parents: []string{"account"},
			}},
			Validate: validateTarget,
		},
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "reads accounts, containers, and workspaces; changes nothing",
			Tools:       ids,
		}},
	}, TestConnection); err != nil {
		return err
	}
	if err := reg.RegisterOAuth(Provider, googleapi.OAuth); err != nil {
		return err
	}
	handlers := map[string]capability.Handler{
		accountsListID: handleAccountsList, accountsGetID: handleAccountsGet,
		containersListID: handleContainersList, containersGetID: handleContainersGet,
		workspacesListID: handleWorkspacesList, workspacesGetID: handleWorkspacesGet,
	}
	for _, d := range descriptors() {
		if err := reg.Register(Provider, capability.Operation{Descriptor: d, Handler: handlers[d.ID]}); err != nil {
			return err
		}
	}
	return nil
}

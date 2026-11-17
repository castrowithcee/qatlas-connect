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
	var readIDs, writeIDs []string
	for _, d := range descriptors() {
		if d.Risk.Effect == capability.EffectRead {
			readIDs = append(readIDs, d.ID)
		}
		// Reverting overwrites the workspace state of an object or built-in variable, so it is in no profile: a connection offers it
		// only when its tools list names it. Deleting one workspace object is not: it stays revertible until a
		// version is published.
		if !d.RequiresToolAllowList {
			writeIDs = append(writeIDs, d.ID)
		}
	}
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Tag Manager", DefaultBaseURL: apiRoot,
		Description:     "Tag management for websites and apps, with a token credential from your own Google Cloud project",
		DefaultEffects:  []config.Effect{config.EffectRead},
		ValidateBaseURL: validateBaseURL,
		SecretRoles:     config.TokenSecretRoles(),
		Token:           &config.TokenMetadata{Scopes: toolScopes()},
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
			Description: "reads accounts, containers, workspaces, tags, triggers, variables, built-in variables, " +
				"folders, clients, transformations, and zones; changes nothing",
			Tools: readIDs,
		}, {
			ID: "write", Title: "Read and change tags, triggers, variables, built-in variables, folders, clients, transformations, " +
				"and zones",
			Description: "reads what the read profile reads, creates, updates, and deletes the tags, triggers, and " +
				"variables of a workspace, creates and updates its folders, creates, updates, and deletes its clients, " +
				"transformations, and zones, and enables and disables its built-in variables; every change needs its own " +
				"confirmation and stays in the workspace until a version is published, which no tool of this provider " +
				"does. Reverting a tag, trigger, variable, built-in variable, client, transformation, or zone stays unticked",
			Tools: writeIDs,
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
		tagsListID: handleTagsList, tagsGetID: handleTagsGet, tagsCreateID: handleTagsCreate,
		tagsUpdateID: handleTagsUpdate, tagsDeleteID: handleTagsDelete, tagsRevertID: handleTagsRevert,
		triggersListID: handleTriggersList, triggersGetID: handleTriggersGet, triggersCreateID: handleTriggersCreate,
		triggersUpdateID: handleTriggersUpdate, triggersDeleteID: handleTriggersDelete,
		triggersRevertID: handleTriggersRevert,
		variablesListID:  handleVariablesList, variablesGetID: handleVariablesGet,
		variablesCreateID: handleVariablesCreate, variablesUpdateID: handleVariablesUpdate,
		variablesDeleteID: handleVariablesDelete, variablesRevertID: handleVariablesRevert,
		builtInVariablesListID: handleBuiltInVariablesList, builtInVariablesCreateID: handleBuiltInVariablesCreate,
		builtInVariablesDeleteID: handleBuiltInVariablesDelete, builtInVariablesRevertID: handleBuiltInVariablesRevert,
		foldersListID: handleFoldersList, foldersGetID: handleFoldersGet, foldersEntitiesID: handleFoldersEntities,
		foldersCreateID: handleFoldersCreate, foldersUpdateID: handleFoldersUpdate,

		clientsListID: handleClientsList, clientsGetID: handleClientsGet, clientsCreateID: handleClientsCreate,
		clientsUpdateID: handleClientsUpdate, clientsDeleteID: handleClientsDelete, clientsRevertID: handleClientsRevert,

		transformationsListID: handleTransformationsList, transformationsGetID: handleTransformationsGet,
		transformationsCreateID: handleTransformationsCreate, transformationsUpdateID: handleTransformationsUpdate,
		transformationsDeleteID: handleTransformationsDelete, transformationsRevertID: handleTransformationsRevert,

		zonesListID: handleZonesList, zonesGetID: handleZonesGet, zonesCreateID: handleZonesCreate,
		zonesUpdateID: handleZonesUpdate, zonesDeleteID: handleZonesDelete, zonesRevertID: handleZonesRevert,
	}
	summaries := map[string]capability.SummaryFunc{
		tagsCreateID: tagSummary("Create"), tagsUpdateID: tagSummary("Replace"),
		tagsDeleteID: tagSummary("Delete"), tagsRevertID: tagSummary("Revert"),
		triggersCreateID: triggerSummary("Create"), triggersUpdateID: triggerSummary("Replace"),
		triggersDeleteID: triggerSummary("Delete"), triggersRevertID: triggerSummary("Revert"),
		variablesCreateID: variableSummary("Create"), variablesUpdateID: variableSummary("Replace"),
		variablesDeleteID: variableSummary("Delete"), variablesRevertID: variableSummary("Revert"),
		builtInVariablesCreateID: builtInSummary("Enable"), builtInVariablesDeleteID: builtInSummary("Disable"),
		builtInVariablesRevertID: builtInSummary("Revert"),
		foldersCreateID:          folderSummary("Create"), foldersUpdateID: folderSummary("Replace"),

		clientsCreateID: clientSummary("Create"), clientsUpdateID: clientSummary("Replace"),
		clientsDeleteID: clientSummary("Delete"), clientsRevertID: clientSummary("Revert"),

		transformationsCreateID: transformationSummary("Create"), transformationsUpdateID: transformationSummary("Replace"),
		transformationsDeleteID: transformationSummary("Delete"), transformationsRevertID: transformationSummary("Revert"),

		zonesCreateID: zoneSummary("Create"), zonesUpdateID: zoneSummary("Replace"),
		zonesDeleteID: zoneSummary("Delete"), zonesRevertID: zoneSummary("Revert"),
	}
	for _, d := range descriptors() {
		if err := reg.Register(Provider, capability.Operation{Descriptor: d, Handler: handlers[d.ID],
			ConfirmationSummary: summaries[d.ID]}); err != nil {
			return err
		}
	}
	return nil
}

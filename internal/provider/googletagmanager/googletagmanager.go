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
		if isUserPermissionTool(d.ID) {
			continue
		}
		if d.Risk.Effect == capability.EffectRead {
			readIDs = append(readIDs, d.ID)
		}
		// Reverting overwrites the workspace state of an object, folder, or built-in variable, and deleting a
		// workspace loses its unpublished changes, and combining containers merges one into another with an
		// effect on the source that is not documented, and deleting a container version removes a whole version,
		// so all four are in no profile: a connection offers them only when its tools list names them. Deleting
		// one workspace object is not: it stays revertible until a version is published.
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
			Reaches:  targetReaches,
		},
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "reads accounts, containers, workspaces and their changes, tags, triggers, variables, " +
				"built-in variables, templates, folders, clients, transformations, zones, Google tag " +
				"configurations, destinations, the embed snippet of a container, the environments of a container, " +
				"container versions, and their headers; " +
				"changes nothing",
			Tools: readIDs,
		}, {
			ID: "write", Title: "Read and change workspaces, tags, triggers, variables, templates, built-in variables, " +
				"folders, clients, transformations, zones, Google tag configurations, and environments, make, rename, and " +
				"restore container versions, and rename accounts",
			Description: "reads what the read profile reads, creates and updates workspaces, synchronizes a " +
				"workspace with the latest container version, resolves its merge conflicts, quick previews it, " +
				"creates, updates, and deletes the tags, triggers, variables, and templates of a workspace, creates, " +
				"updates, and deletes its folders, moves tags, triggers, and variables into a folder, creates, updates, " +
				"and deletes its clients, transformations, zones, and Google tag configurations, enables and disables " +
				"its built-in variables, creates, updates, and deletes the custom environments of a container, " +
				"creates a container version from a workspace, changes the name and notes of a version, marks a " +
				"version as the latest, and restores a deleted version; " +
				"every change needs its own confirmation. Changes of workspace content stay in the workspace until a " +
				"version is published, which no tool of this provider does. Changes to an environment and renaming an " +
				"account take effect at once; renaming an account (scope tagmanager.manage.accounts) applies for everyone " +
				"who sees the account and is not available on a connection bound to a container. Deleting a workspace, " +
				"reverting a tag, trigger, variable, template, built-in variable, folder, client, transformation, or " +
				"zone, combining containers, and deleting a container version stay unticked",
			Tools: writeIDs,
		}, {
			ID: userPermissionsProfile, Title: "User permissions",
			Description: "reads the users of an account with their email addresses and rights, changes the rights of " +
				"existing users, and removes a user from the account; no tool adds a user; these methods accept only " +
				"the scope that manages users, so the token may also change what its account may in Tag Manager, " +
				"every change needs its own confirmation, and a connection bound to a container does not offer them",
			Tools: userPermissionIDs(),
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
		workspacesCreateID: handleWorkspacesCreate, workspacesUpdateID: handleWorkspacesUpdate,
		workspacesDeleteID: handleWorkspacesDelete, workspacesStatusID: handleWorkspacesStatus,
		workspacesSyncID: handleWorkspacesSync, workspacesResolveConflictID: handleWorkspacesResolveConflict,
		workspacesQuickPreviewID: handleWorkspacesQuickPreview,
		tagsListID:               handleTagsList, tagsGetID: handleTagsGet, tagsCreateID: handleTagsCreate,
		tagsUpdateID: handleTagsUpdate, tagsDeleteID: handleTagsDelete, tagsRevertID: handleTagsRevert,
		triggersListID: handleTriggersList, triggersGetID: handleTriggersGet, triggersCreateID: handleTriggersCreate,
		triggersUpdateID: handleTriggersUpdate, triggersDeleteID: handleTriggersDelete,
		triggersRevertID: handleTriggersRevert,
		variablesListID:  handleVariablesList, variablesGetID: handleVariablesGet,
		variablesCreateID: handleVariablesCreate, variablesUpdateID: handleVariablesUpdate,
		variablesDeleteID: handleVariablesDelete, variablesRevertID: handleVariablesRevert,
		templatesListID: handleTemplatesList, templatesGetID: handleTemplatesGet,
		templatesCreateID: handleTemplatesCreate, templatesUpdateID: handleTemplatesUpdate,
		templatesDeleteID: handleTemplatesDelete, templatesRevertID: handleTemplatesRevert,
		builtInVariablesListID: handleBuiltInVariablesList, builtInVariablesCreateID: handleBuiltInVariablesCreate,
		builtInVariablesDeleteID: handleBuiltInVariablesDelete, builtInVariablesRevertID: handleBuiltInVariablesRevert,
		foldersListID: handleFoldersList, foldersGetID: handleFoldersGet, foldersEntitiesID: handleFoldersEntities,
		foldersCreateID: handleFoldersCreate, foldersUpdateID: handleFoldersUpdate,
		foldersMoveID: handleFoldersMove, foldersDeleteID: handleFoldersDelete, foldersRevertID: handleFoldersRevert,

		clientsListID: handleClientsList, clientsGetID: handleClientsGet, clientsCreateID: handleClientsCreate,
		clientsUpdateID: handleClientsUpdate, clientsDeleteID: handleClientsDelete, clientsRevertID: handleClientsRevert,

		transformationsListID: handleTransformationsList, transformationsGetID: handleTransformationsGet,
		transformationsCreateID: handleTransformationsCreate, transformationsUpdateID: handleTransformationsUpdate,
		transformationsDeleteID: handleTransformationsDelete, transformationsRevertID: handleTransformationsRevert,

		zonesListID: handleZonesList, zonesGetID: handleZonesGet, zonesCreateID: handleZonesCreate,
		zonesUpdateID: handleZonesUpdate, zonesDeleteID: handleZonesDelete, zonesRevertID: handleZonesRevert,

		gtagConfigsListID: handleGtagConfigsList, gtagConfigsGetID: handleGtagConfigsGet,
		gtagConfigsCreateID: handleGtagConfigsCreate, gtagConfigsUpdateID: handleGtagConfigsUpdate,
		gtagConfigsDeleteID: handleGtagConfigsDelete,

		accountsUpdateID: handleAccountsUpdate, containersSnippetID: handleContainersSnippet,
		containersCombineID: handleContainersCombine,
		destinationsListID:  handleDestinationsList, destinationsGetID: handleDestinationsGet,

		environmentsListID: handleEnvironmentsList, environmentsGetID: handleEnvironmentsGet,
		environmentsCreateID: handleEnvironmentsCreate, environmentsUpdateID: handleEnvironmentsUpdate,
		environmentsDeleteID: handleEnvironmentsDelete,

		userPermissionsListID: handleUserPermissionsList, userPermissionsGetID: handleUserPermissionsGet,
		userPermissionsUpdateID: handleUserPermissionsUpdate, userPermissionsDeleteID: handleUserPermissionsDelete,

		versionsGetID: handleVersionsGet, versionsLiveID: handleVersionsLive,
		versionHeadersListID: handleVersionHeadersList, versionHeadersLatestID: handleVersionHeadersLatest,

		versionsCreateID: handleVersionsCreate, versionsUpdateID: handleVersionsUpdate,
		versionsSetLatestID: handleVersionsSetLatest, versionsDeleteID: handleVersionsDelete,
		versionsUndeleteID: handleVersionsUndelete,
	}
	summaries := map[string]capability.SummaryFunc{
		workspacesCreateID: workspaceSummary("Create"), workspacesUpdateID: workspaceSummary("Replace"),
		workspacesDeleteID: deleteWorkspaceSummary, workspacesSyncID: syncSummary,
		workspacesResolveConflictID: resolveConflictSummary, workspacesQuickPreviewID: quickPreviewSummary,
		tagsCreateID: tagSummary("Create"), tagsUpdateID: tagSummary("Replace"),
		tagsDeleteID: tagSummary("Delete"), tagsRevertID: tagSummary("Revert"),
		triggersCreateID: triggerSummary("Create"), triggersUpdateID: triggerSummary("Replace"),
		triggersDeleteID: triggerSummary("Delete"), triggersRevertID: triggerSummary("Revert"),
		variablesCreateID: variableSummary("Create"), variablesUpdateID: variableSummary("Replace"),
		variablesDeleteID: variableSummary("Delete"), variablesRevertID: variableSummary("Revert"),
		templatesCreateID: templateSummary("Create"), templatesUpdateID: templateSummary("Replace"),
		templatesDeleteID: templateSummary("Delete"), templatesRevertID: templateSummary("Revert"),
		builtInVariablesCreateID: builtInSummary("Enable"), builtInVariablesDeleteID: builtInSummary("Disable"),
		builtInVariablesRevertID: builtInSummary("Revert"),
		foldersCreateID:          folderSummary("Create"), foldersUpdateID: folderSummary("Replace"),
		foldersMoveID: moveSummary, foldersDeleteID: folderSummary("Delete"), foldersRevertID: folderSummary("Revert"),

		clientsCreateID: clientSummary("Create"), clientsUpdateID: clientSummary("Replace"),
		clientsDeleteID: clientSummary("Delete"), clientsRevertID: clientSummary("Revert"),

		transformationsCreateID: transformationSummary("Create"), transformationsUpdateID: transformationSummary("Replace"),
		transformationsDeleteID: transformationSummary("Delete"), transformationsRevertID: transformationSummary("Revert"),

		zonesCreateID: zoneSummary("Create"), zonesUpdateID: zoneSummary("Replace"),
		zonesDeleteID: zoneSummary("Delete"), zonesRevertID: zoneSummary("Revert"),

		gtagConfigsCreateID: gtagConfigSummary("Create"), gtagConfigsUpdateID: gtagConfigSummary("Replace"),
		gtagConfigsDeleteID: gtagConfigSummary("Delete"),

		accountsUpdateID: accountUpdateSummary, containersCombineID: combineSummary,

		environmentsCreateID: environmentSummary("Create"), environmentsUpdateID: environmentSummary("Replace"),
		environmentsDeleteID: environmentSummary("Delete"),

		userPermissionsUpdateID: userPermissionUpdateSummary, userPermissionsDeleteID: userPermissionDeleteSummary,

		versionsCreateID: createVersionSummary, versionsUpdateID: updateVersionSummary,
		versionsSetLatestID: versionPathSummary("Mark", " as the latest container version; new workspaces "+
			"start from it, nothing is published"),
		versionsDeleteID:   versionPathSummary("Delete", "; the live version cannot be deleted"),
		versionsUndeleteID: versionPathSummary("Restore", " from deletion (nothing is published)"),
	}
	for _, d := range descriptors() {
		if err := reg.Register(Provider, capability.Operation{Descriptor: d, Handler: handlers[d.ID],
			ConfirmationSummary: summaries[d.ID]}); err != nil {
			return err
		}
	}
	return nil
}

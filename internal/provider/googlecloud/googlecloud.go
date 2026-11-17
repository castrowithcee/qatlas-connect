// Package googlecloud implements access to Google Cloud with one credential that issues access tokens: a
// person's consent, a service account key, or the impersonation of a service account, all through the token
// core of internal/provider. A connection takes no secret of its own and no scope: the scopes follow from the
// tools it offers (see scopes.go).
//
// The package starts with Cloud Resource Manager: the search of organizations, folders, and projects, one
// project, and the state of a long-running operation. It then reads IAM: roles, and the roles and permissions
// of a resource. It changes only through the profiles service-accounts (service_accounts_write.go),
// service-accounts-revoke and the listed-only service-accounts.delete (service_accounts_revoke.go), and
// iam-bindings (a single binding of the IAM policy of a project, folder, or organization, iam_bindings.go),
// each change with the confirmation of a person. Every request goes to a fixed origin of the service table,
// with the access token in the Authorization header only, once, within a bounded size and time, and spaced per
// credential.
//
// A connection may be bound to organizations, folders, and projects, which nest as Google nests them, and to
// billing accounts, which belong to none of them. A project is bound to a target by reading it and the folders
// above it; a tool for a whole organization runs only on a connection that names an organization.
// Everything Google returns is untrusted third-party content: strings and lists are bounded.
package googlecloud

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
const Provider = "google.cloud"

// TestConnection obtains an access token and reads one project with projects.search, which costs nothing. A
// target of the connection is then proved to exist by reading it. A Cloud project of the credential in which
// the Cloud Resource Manager API is disabled ends as a failure of the class permission, which the message of
// the failed request explains.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	const op = "test connection"
	if err := checkConnection(resolved); err != nil {
		return testClass(err)
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return testClass(failure(op, "the targets of the connection are not usable"))
	}
	if secrets == nil {
		return testClass(failure(op, "no credential resolver was configured"))
	}
	probe := func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
		c := withSource(resolved, source)
		if err := c.searchProjectsPage(ctx, op, projectsSearchID, 1); err != nil {
			return "", err
		}
		for _, t := range list.targets {
			var err error
			switch t.kind {
			case kindProject:
				_, err = c.project(ctx, op, projectsGetID, t.id)
			case kindFolder:
				_, err = c.folder(ctx, op, projectsGetID, t.id)
			case kindOrganization:
				err = c.organization(ctx, op, projectsGetID, t.id)
			}
			if err != nil {
				return "", err
			}
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

// readTools are the tools of the profile read: every tool that needs no more than the read-only scope.
var readTools = []string{organizationsSearch.ID, foldersSearch.ID, projectsSearch.ID, projectsGet.ID, operationsGet.ID,
	organizationsGet.ID, foldersGet.ID, foldersList.ID, projectsList.ID, servicesList.ID, servicesGet.ID,
	servicesBatchGet.ID, instancesList.ID, instancesAggregatedList.ID, instancesGet.ID, machineTypesList.ID}

// Register adds the provider metadata, its connection test, its consent endpoints, and its tools.
func Register(reg *capability.Registry) error {
	token := tokenMetadata
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Cloud", DefaultBaseURL: apiRoot,
		Description:    "Projects, folders, organizations, IAM, and service accounts of Google Cloud: read, and service accounts and single IAM bindings changed with opt-in profiles",
		DefaultEffects: []config.Effect{config.EffectRead},
		ValidateBaseURL: func(raw string) error {
			if !isAPIRoot(raw) {
				return errors.New("a Google Cloud service must use the official API origin " + apiRoot)
			}
			return nil
		},
		SecretRoles: config.TokenSecretRoles(),
		Token:       &token,
		Target:      targetMetadata,
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "searches and reads organizations, folders, and projects and reads the state of long-running " +
				"operations from Google Cloud Resource Manager and the enabled and available APIs of a project from Service " +
				"Usage with the read-only scope and the Compute Engine instances and machine types of a project with the scope " +
				"compute.readonly, and changes nothing",
			Tools: readTools,
		}, {
			ID: "read-iam", Title: "Read with IAM",
			Description: "everything of read plus IAM roles, policies, deny policies, the roles and permissions of a resource, " +
				"and service accounts with the metadata of their keys; these IAM methods accept only the broad cloud-platform " +
				"scope, so the token may change what its account may, although no tool of this profile changes anything",
			Tools: append(append([]string{}, readTools...), iamRolesList.ID, iamRolesGet.ID, iamRolesQuery.ID,
				iamPermissionsQuery.ID, iamPoliciesGet.ID, iamPermissionsTest.ID, denyPoliciesList.ID,
				denyPoliciesGet.ID, serviceAccountsList.ID, serviceAccountsGet.ID, serviceAccountKeysList.ID,
				serviceAccountKeysGet.ID),
		}, {
			ID: "service-accounts", Title: "Service accounts",
			Description: "creates service accounts, changes their display name and description, and disables and " +
				"enables them; the token holds the broad cloud-platform scope and may change what its account may, " +
				"every change needs its own confirmation, and no tool creates a key, a token, or a signature",
			Tools: []string{serviceAccountsCreate.ID, serviceAccountsPatch.ID, serviceAccountsDisable.ID,
				serviceAccountsEnable.ID},
		}, {
			ID: "iam-bindings", Title: "Change IAM bindings",
			Description: "everything of read plus reading IAM policies and adding a member to, removing a member from, " +
				"or moving a member between the bindings of a project, folder, or organization, each with the " +
				"confirmation of a person; only principals already in the policy, or service accounts of the same " +
				"project, are granted; these methods accept only the broad cloud-platform scope",
			Tools: append(append([]string{}, readTools...), iamPoliciesGet.ID, iamBindingsAdd.ID, iamBindingsRemove.ID,
				iamBindingsChange.ID),
		}, {
			ID: "service-accounts-revoke", Title: "Service account revocation",
			Description: "disables, enables, and deletes the keys of service accounts; deleting a service account itself " +
				"is not part of this profile and needs a tools list that names it; the token holds the broad " +
				"cloud-platform scope and may change what its account may, every change needs its own confirmation, " +
				"and no tool creates a key, a token, or a signature",
			Tools: []string{serviceAccountKeysDisable.ID, serviceAccountKeysEnable.ID, serviceAccountKeysDelete.ID},
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
		capability.Operation{Descriptor: organizationsSearch, Handler: handleOrganizationsSearch},
		capability.Operation{Descriptor: foldersSearch, Handler: handleFoldersSearch},
		capability.Operation{Descriptor: projectsSearch, Handler: handleProjectsSearch},
		capability.Operation{Descriptor: projectsGet, Handler: handleProjectsGet},
		capability.Operation{Descriptor: operationsGet, Handler: handleOperationsGet},
		capability.Operation{Descriptor: organizationsGet, Handler: handleOrganizationsGet},
		capability.Operation{Descriptor: foldersGet, Handler: handleFoldersGet},
		capability.Operation{Descriptor: foldersList, Handler: handleFoldersList},
		capability.Operation{Descriptor: projectsList, Handler: handleProjectsList},
		capability.Operation{Descriptor: servicesList, Handler: handleServicesList},
		capability.Operation{Descriptor: servicesGet, Handler: handleServicesGet},
		capability.Operation{Descriptor: servicesBatchGet, Handler: handleServicesBatchGet},
		capability.Operation{Descriptor: instancesList, Handler: handleInstancesList},
		capability.Operation{Descriptor: instancesAggregatedList, Handler: handleInstancesAggregatedList},
		capability.Operation{Descriptor: instancesGet, Handler: handleInstancesGet},
		capability.Operation{Descriptor: machineTypesList, Handler: handleMachineTypesList},
		capability.Operation{Descriptor: iamRolesList, Handler: handleIAMRolesList},
		capability.Operation{Descriptor: iamRolesGet, Handler: handleIAMRolesGet},
		capability.Operation{Descriptor: iamRolesQuery, Handler: handleIAMRolesQuery},
		capability.Operation{Descriptor: iamPermissionsQuery, Handler: handleIAMPermissionsQuery},
		capability.Operation{Descriptor: iamPoliciesGet, Handler: handleIAMPoliciesGet},
		capability.Operation{Descriptor: iamPermissionsTest, Handler: handleIAMPermissionsTest},
		capability.Operation{Descriptor: denyPoliciesList, Handler: handleDenyPoliciesList},
		capability.Operation{Descriptor: denyPoliciesGet, Handler: handleDenyPoliciesGet},
		capability.Operation{Descriptor: serviceAccountsList, Handler: handleServiceAccountsList},
		capability.Operation{Descriptor: serviceAccountsGet, Handler: handleServiceAccountsGet},
		capability.Operation{Descriptor: serviceAccountKeysList, Handler: handleServiceAccountKeysList},
		capability.Operation{Descriptor: serviceAccountKeysGet, Handler: handleServiceAccountKeysGet},
		capability.Operation{Descriptor: serviceAccountsCreate, Handler: handleServiceAccountsCreate,
			ConfirmationSummary: accountSummary(serviceAccountsCreateID)},
		capability.Operation{Descriptor: serviceAccountsPatch, Handler: handleServiceAccountsPatch,
			ConfirmationSummary: accountSummary(serviceAccountsPatchID)},
		capability.Operation{Descriptor: serviceAccountsDisable, Handler: handleServiceAccountsDisable,
			ConfirmationSummary: accountSummary(serviceAccountsDisableID)},
		capability.Operation{Descriptor: serviceAccountsEnable, Handler: handleServiceAccountsEnable,
			ConfirmationSummary: accountSummary(serviceAccountsEnableID)},
		capability.Operation{Descriptor: iamBindingsAdd, Handler: handleIAMBindingsAdd,
			ConfirmationSummary: bindingSummary(changeAdd)},
		capability.Operation{Descriptor: iamBindingsRemove, Handler: handleIAMBindingsRemove,
			ConfirmationSummary: bindingSummary(changeRemove)},
		capability.Operation{Descriptor: iamBindingsChange, Handler: handleIAMBindingsChange,
			ConfirmationSummary: bindingSummary(changeMove)},
		capability.Operation{Descriptor: serviceAccountKeysDisable, Handler: handleServiceAccountKeysDisable,
			ConfirmationSummary: revokeSummary(serviceAccountKeysDisableID)},
		capability.Operation{Descriptor: serviceAccountKeysEnable, Handler: handleServiceAccountKeysEnable,
			ConfirmationSummary: revokeSummary(serviceAccountKeysEnableID)},
		capability.Operation{Descriptor: serviceAccountKeysDelete, Handler: handleServiceAccountKeysDelete,
			ConfirmationSummary: revokeSummary(serviceAccountKeysDeleteID)},
		capability.Operation{Descriptor: serviceAccountsDelete, Handler: handleServiceAccountsDelete,
			ConfirmationSummary: revokeSummary(serviceAccountsDeleteID)})
}

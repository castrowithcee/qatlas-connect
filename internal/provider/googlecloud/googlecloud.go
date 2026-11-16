// Package googlecloud implements read-only access to Google Cloud with one credential that issues access
// tokens: a person's consent, a service account key, or the impersonation of a service account, all through
// the token core of internal/provider. A connection takes no secret of its own and no scope: the scopes follow
// from the tools it offers (see scopes.go).
//
// The package starts with Cloud Resource Manager: the search of organizations, folders, and projects, one
// project, and the state of a long-running operation. It then reads IAM: roles, and the roles and permissions
// of a resource. Every
// request goes to a fixed origin of the service table, with the access token in the Authorization header only,
// once, within a bounded size and time, and spaced per credential.
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
	organizationsGet.ID, foldersGet.ID, foldersList.ID, projectsList.ID}

// Register adds the provider metadata, its connection test, its consent endpoints, and its tools.
func Register(reg *capability.Registry) error {
	token := tokenMetadata
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Cloud", DefaultBaseURL: apiRoot,
		Description:    "Projects, folders, organizations, long-running operations, and IAM roles of Google Cloud, searched and read with a consent, a service account key, or an impersonated service account",
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
				"operations from Google Cloud Resource Manager with the read-only scope and changes nothing",
			Tools: readTools,
		}, {
			ID: "read-iam", Title: "Read with IAM",
			Description: "everything of read plus IAM roles, policies, deny policies, and the roles and permissions of a resource; the IAM " +
				"methods accept only the broad cloud-platform scope, so the token may change what its account may, " +
				"although no tool of this provider changes anything",
			Tools: append(append([]string{}, readTools...), iamRolesList.ID, iamRolesGet.ID, iamRolesQuery.ID,
				iamPermissionsQuery.ID, iamPoliciesGet.ID, iamPermissionsTest.ID, denyPoliciesList.ID,
				denyPoliciesGet.ID),
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
		capability.Operation{Descriptor: iamRolesList, Handler: handleIAMRolesList},
		capability.Operation{Descriptor: iamRolesGet, Handler: handleIAMRolesGet},
		capability.Operation{Descriptor: iamRolesQuery, Handler: handleIAMRolesQuery},
		capability.Operation{Descriptor: iamPermissionsQuery, Handler: handleIAMPermissionsQuery},
		capability.Operation{Descriptor: iamPoliciesGet, Handler: handleIAMPoliciesGet},
		capability.Operation{Descriptor: iamPermissionsTest, Handler: handleIAMPermissionsTest},
		capability.Operation{Descriptor: denyPoliciesList, Handler: handleDenyPoliciesList},
		capability.Operation{Descriptor: denyPoliciesGet, Handler: handleDenyPoliciesGet})
}

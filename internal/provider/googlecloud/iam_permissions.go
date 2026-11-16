package googlecloud

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const iamPermissionsTestID = Provider + ".iam-permissions.test"

const maxTestedPermissions = 100

// permissionPattern is the shape of a permission such as resourcemanager.projects.get: service, resource, and
// verb. Wildcards are not permissions, and Google refuses them.
var permissionPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+){1,5}$`)

const permissionsOutputSchema = `{"type":"object","properties":{"resource":{"type":"string"},` +
	`"permissions":` + stringArraySchema + `},"required":["resource"],"additionalProperties":false}`

var iamPermissionsTest = capability.Descriptor{
	ID: iamPermissionsTestID, Version: 1, Title: "Test permissions of the Google Cloud credential",
	Description: "Check which of up to 100 permissions the credential of the connection holds on a project, " +
		"folder, organization, or service account, and return the granted subset. It tests only the own " +
		"credential, never another identity. The request is a POST that only reads and is safe to repeat. " +
		"The resource must lie in the targets of the connection, proved before the request as for an IAM " +
		"policy; an organization only on a connection that names it. A permission Google does not know is " +
		"refused by Google as a whole.",
	Tags: []string{"google", "cloud", "iam", "permissions"}, Risk: iamRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + resourceTypeProp + `,` + resourceIDProp +
		`,"permissions":{"type":"array","minItems":1,"maxItems":100,"uniqueItems":true,` +
		`"items":{"type":"string","minLength":3,"maxLength":128,` +
		`"pattern":"^[A-Za-z][A-Za-z0-9_-]*(\\.[A-Za-z0-9_-]+){1,5}$"}}},` +
		`"required":["resource_type","resource_id","permissions"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(permissionsOutputSchema),
	Arguments: []capability.Argument{
		resourceTypeArgument, resourceIDArgument,
		{Name: "permissions", Required: true, Description: "Permissions to check, 1 to 100 distinct names " +
			"such as resourcemanager.projects.get; no wildcards"},
	},
	Fields: []capability.Field{
		resourceField,
		{Name: "permissions", Description: "The subset of the requested permissions the credential holds on the " +
			"resource; a permission that is missing here is not granted"},
	},
	Examples: []capability.Example{{
		Description: "Check two permissions on a project",
		Arguments: json.RawMessage(`{"resource_type":"project","resource_id":"my-project-123",` +
			`"permissions":["resourcemanager.projects.get","resourcemanager.projects.getIamPolicy"]}`),
	}},
}

func handleIAMPermissionsTest(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "test iam permissions"
	var arguments struct {
		iamArguments
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if n := len(arguments.Permissions); n < 1 || n > maxTestedPermissions {
		return nil, badRequest("permissions must hold 1 to 100 permissions")
	}
	for i, permission := range arguments.Permissions {
		if !permissionPattern.MatchString(permission) || len(permission) > 128 {
			return nil, badRequest("permissions must hold permission names such as resourcemanager.projects.get")
		}
		if slices.Contains(arguments.Permissions[:i], permission) {
			return nil, badRequest("permissions must not repeat a permission")
		}
	}
	c, r, err := openResource(ctx, op, iamPermissionsTestID, resolved, secrets, red, arguments.iamArguments,
		kindProject, kindFolder, kindOrganization, kindServiceAccount)
	if err != nil {
		return nil, err
	}
	svc, base := r.service()
	var wire struct {
		Permissions []string `json:"permissions"`
	}
	body := map[string][]string{"permissions": arguments.Permissions}
	if err := c.post(ctx, op, iamPermissionsTestID, svc, base+r.escaped()+":testIamPermissions", nil, body, &wire); err != nil {
		return nil, err
	}
	granted := make([]string, 0, len(wire.Permissions))
	for _, permission := range wire.Permissions {
		// Google documents the answer as a subset of the request; anything else is not an answer to it.
		if !slices.Contains(arguments.Permissions, permission) || slices.Contains(granted, permission) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		granted = append(granted, permission)
	}
	return struct {
		Resource    string   `json:"resource"`
		Permissions []string `json:"permissions"`
	}{r.name(), granted}, nil
}

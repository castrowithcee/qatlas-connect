package github

import (
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// Dependabot secrets of a repository or of an organization. They share the helpers of the Actions secret tools
// through their own secretFamily, with the same guarantees: names and metadata only, never a value. Dependabot
// has no environment, so no input schema has an environment member, a request that carries one is refused before
// a credential is resolved, and no path ever takes the environment route.

var dependabotSecretFamily = secretFamily{route: "dependabot", label: "Dependabot", grant: "Dependabot secrets"}

const dependabotTargetKeys = `"repository":` + repoSchema + `,"organization":` + ownerSchema

const dependabotTargetProperties = `"repository":{"type":"string"},"organization":{"type":"string"}`

var dependabotTargetArguments = []capability.Argument{
	{Name: "repository", Description: "Repository as OWNER/REPO whose Dependabot secrets this call addresses; " +
		"exactly one of repository or organization is required, never both; must lie inside the targets when " +
		"the connection lists any"},
	{Name: "organization", Description: "Organization as orgs/LOGIN whose Dependabot secrets this call " +
		"addresses; exactly one of repository or organization is required, never both; must be an owner " +
		"target the connection allows when it lists any"},
}

func dependabotFieldsWith(fields ...capability.Field) []capability.Field {
	return append(fields, variableTargetFields[0], variableTargetFields[2])
}

var dependabotSecretsList = capability.Descriptor{
	ID:      Provider + ".dependabot-secrets.list",
	Version: 1,
	Title:   "List GitHub Dependabot secrets",
	Description: "List one bounded batch of the names and times of the Dependabot secrets of a repository or, " +
		"with organization instead, of an organization a connection allows; never a value; exactly one of " +
		"repository or organization is required",
	Tags:         []string{"github", "dependabot", "secrets", "list"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, secretSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(dependabotTargetKeys + `,"limit":` + secretListLimitSchema + `,"cursor":` + cursorSchema),
	OutputSchema: json.RawMessage(secretListOutputFor(dependabotTargetProperties)),
	Arguments:    append(append([]capability.Argument{}, dependabotTargetArguments...), pagingArguments...),
	Fields: dependabotFieldsWith(
		capability.Field{Name: "secrets", Description: "Secrets: name, created_at, updated_at, and, for an " +
			"organization, visibility and selected_repositories_url; names are untrusted data"},
		capability.Field{Name: "total_count", Description: "Number of secrets GitHub reports at this level"},
		pagingFields[0], pagingFields[1]),
	Examples: []capability.Example{{
		Description: "List the Dependabot secrets of a repository",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example"}`),
	}, {
		Description: "List the Dependabot secrets of an organization",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org"}`),
	}},
}

var dependabotSecretsGet = capability.Descriptor{
	ID:      Provider + ".dependabot-secrets.get",
	Version: 1,
	Title:   "Get a GitHub Dependabot secret",
	Description: "Read the name and times of one Dependabot secret of a repository or, with organization " +
		"instead, of an organization a connection allows, by name; never the value; exactly one of repository " +
		"or organization is required",
	Tags:         []string{"github", "dependabot", "secrets", "get"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, secretSensitivity),
	Provider:     Provider,
	InputSchema:  inputSchema(dependabotTargetKeys+`,"name":`+secretNameSchema, "name"),
	OutputSchema: json.RawMessage(secretOutputFor(dependabotTargetProperties)),
	Arguments:    append([]capability.Argument{secretNameArgument}, dependabotTargetArguments...),
	Fields:       dependabotFieldsWith(secretMembersOf("github.dependabot-secret-repositories.list")...),
	Examples: []capability.Example{{
		Description: "Read one Dependabot secret of a repository",
		Arguments:   json.RawMessage(`{"repository":"octo-org/example","name":"REGISTRY_TOKEN"}`),
	}},
}

var dependabotSecretRepositoriesList = capability.Descriptor{
	ID:      Provider + ".dependabot-secret-repositories.list",
	Version: 1,
	Title:   "List the repositories of a GitHub Dependabot organization secret",
	Description: "List one bounded batch of the repositories that may use an organization's Dependabot secret " +
		"whose visibility is selected; the organization is required as orgs/LOGIN and must be an owner target " +
		"the connection allows; when the connection's targets name any, a repository outside them or in an " +
		"excluded repository is withheld and counted in withheld instead of being listed",
	Tags:         []string{"github", "dependabot", "secrets", "repositories", "list"},
	Risk:         guardedRisk(capability.EffectRead, capability.IdempotencySafe, secretSensitivity),
	Provider:     Provider,
	InputSchema:  actionSecretRepositoriesList.InputSchema,
	OutputSchema: actionSecretRepositoriesList.OutputSchema,
	Arguments:    actionSecretRepositoriesList.Arguments,
	Fields:       actionSecretRepositoriesList.Fields,
	Examples: []capability.Example{{
		Description: "List the repositories of an organization Dependabot secret",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","name":"REGISTRY_TOKEN"}`),
	}},
}

var dependabotSecretsDelete = capability.Descriptor{
	ID:      Provider + ".dependabot-secrets.delete",
	Version: 1,
	Title:   "Delete a GitHub Dependabot secret",
	Description: "Delete one Dependabot secret of a repository or, with organization instead, of an " +
		"organization a connection allows, permanently; exactly one of repository or organization is required; " +
		"offered only where a connection's tools list names it",
	Tags:                  []string{"github", "dependabot", "secrets", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, secretSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(dependabotTargetKeys+`,"name":`+secretNameSchema, "name"),
	OutputSchema:          json.RawMessage(secretDeletedOutputFor(dependabotTargetProperties)),
	Arguments:             append([]capability.Argument{secretNameArgument}, dependabotTargetArguments...),
	Fields: dependabotFieldsWith(
		capability.Field{Name: "deleted", Description: "True once GitHub deleted the secret"},
		capability.Field{Name: "name", Description: "Name of the deleted secret"}),
	Examples: []capability.Example{{
		Description: "Delete an organization Dependabot secret",
		Arguments:   json.RawMessage(`{"organization":"orgs/octo-org","name":"REGISTRY_TOKEN"}`),
	}},
}

// dependabotSecretsOperations binds every Dependabot secret tool to its handler.
func dependabotSecretsOperations() []capability.Operation {
	return secretOperations(dependabotSecretFamily, dependabotSecretsList, dependabotSecretsGet,
		dependabotSecretRepositoriesList, dependabotSecretsDelete)
}

package github

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/localfile"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The write tools create and change one private registry of an organization. The registry's credential is
// read from a local file the connection releases for reading, sealed locally with the public key GitHub serves
// for the organization, and only the sealed text is sent; no argument carries a value. The address must be an
// https address whose host the connection approves as a registry-hosts/HOST target. The call checks the
// organization, the address, and the selected repositories, then the file, and only then resolves a
// credential; GitHub sees the reads of the repositories and the key and exactly one change that is never
// repeated. Only the token and username_password authentication types are offered: the others carry no
// local value and name further addresses.

// privateRegistryValueSensitivity classifies the write tools: they handle the credential of a registry, though
// only sealed text ever leaves the machine.
const privateRegistryValueSensitivity = "github-private-registry-value"

const privateRegistryURLMax = 2048

const privateRegistryRejected = "GitHub rejected the private registry as invalid; the key may have been replaced " +
	"since it was read, or the registry type, the address, the visibility, or the selected repositories are not " +
	"accepted; nothing was stored"

const privateRegistryWritePermission = "GitHub refused this token the creation or change of a private registry of " +
	"this organization; writing them needs admin:org on a classic token, or the organization permission " +
	"Organization private registries: write on a fine-grained token"

var privateRegistryTypes = []string{"maven_repository", "nuget_feed", "goproxy_server", "npm_registry",
	"rubygems_server", "cargo_registry", "composer_repository", "docker_registry", "git_source", "helm_registry",
	"hex_organization", "hex_repository", "pub_repository", "python_index", "terraform_registry"}

var privateRegistryAuthTypes = []string{"token", "username_password"}

var privateRegistryWriteProperties = `"registry_type":` + enumSchema(privateRegistryTypes) +
	`,"url":{"type":"string","minLength":1,"maxLength":2048},` +
	`"username":{"type":"string","minLength":1,"maxLength":255},"replaces_base":{"type":"boolean"},` +
	`"visibility":` + variableVisibilitySchema + `,"selected_repositories":` + selectedRepositoriesSchema + `,"` +
	localfile.LocalPathArgument + `":` + localfile.LocalPathSchema

const privateRegistryWriteOutput = `{"type":"object","properties":{"state":{"type":"string","enum":["created","updated"]},` +
	`"name":{"type":"string"},"registry_type":{"type":"string"},"visibility":{"type":"string"},` +
	`"selected_repositories":{"type":"array","items":{"type":"string"}}},` +
	`"required":["state","name"],"additionalProperties":false}`

var privateRegistryWriteFields = []capability.Field{
	{Name: "state", Description: "created when GitHub made the registry, updated when it changed an existing one"},
	{Name: "name", Description: "Secret name of the registry, as the other private registry tools take it"},
	{Name: "registry_type", Description: "Registry type sent, when one was sent"},
	{Name: "visibility", Description: "Visibility sent, when one was sent"},
	{Name: "selected_repositories", Description: "Repositories sent as the selected ones, as OWNER/REPO; absent when none were sent"},
}

var (
	registryTypeArgument = capability.Argument{Name: "registry_type", Description: "The kind of registry: " +
		strings.Join(privateRegistryTypes, ", ")}
	registryURLArgument = capability.Argument{Name: "url", Description: "Address of the registry; an https " +
		"address without user information whose host, with its port when one is given, the connection approves " +
		"as registry-hosts/HOST, without regard to case; no pattern, no subdomain, and no default port; never " +
		"fetched by Qatlas"}
	registryUsernameArgument = capability.Argument{Name: "username", Description: "Username Dependabot signs in " +
		"with; omit it for a registry that needs none"}
	registryReplacesBaseArgument = capability.Argument{Name: "replaces_base", Description: "True when Dependabot " +
		"uses only this registry and no public default for its ecosystem"}
	registryVisibilityArgument = capability.Argument{Name: "visibility", Description: "all, private, or " +
		"selected repositories of the organization may use the registry; all and private are refused when the " +
		"connection's targets or exclusions narrow the repositories of the organization, since the registry " +
		"would reach repositories outside them"}
	registrySelectedArgument = capability.Argument{Name: "selected_repositories", Description: "Only with " +
		"visibility selected, and required with it: the repositories of the organization that may use the " +
		"registry, as OWNER/REPO, at most 50; each must lie inside the connection's repository targets; Qatlas " +
		"resolves their identifiers; replaces the current selection"}
	registryLocalPathArgument = capability.Argument{Name: localfile.LocalPathArgument,
		Description: "File that holds the credential of the registry, at most 48 KB, absolute or starting with ~/, " +
			"inside a directory the connection releases for reading; the whole content is the value, so a line " +
			"break at the end of the file is part of it"}
)

var privateRegistriesCreate = capability.Descriptor{
	ID:      Provider + ".private-registries.create",
	Version: 1,
	Title:   "Create a GitHub private registry",
	Description: "Create one private registry Dependabot uses in an organization an explicit connection allows, " +
		"with the token or username and password authentication; the url must be an https address whose host " +
		"the connection approves as a registry-hosts/HOST target; the credential is read from a local file the " +
		"connection releases for reading, sealed locally with the organization's public key, and only the sealed " +
		"text is sent; the value is never an argument and never part of an answer; visibility is required; " +
		"offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "dependabot", "private-registries", "organization", "create"},
	Risk:                  guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, privateRegistryValueSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	LocalFiles:            config.LocalFilesRead,
	InputSchema: inputSchema(privateRegistryWriteProperties+`,"auth_type":`+enumSchema(privateRegistryAuthTypes),
		"registry_type", "url", "visibility", localfile.LocalPathArgument),
	OutputSchema: json.RawMessage(privateRegistryWriteOutput),
	Arguments: []capability.Argument{
		withRequired(registryTypeArgument), withRequired(registryURLArgument),
		{Name: "auth_type", Description: "token (the default) or username_password"},
		registryUsernameArgument, registryReplacesBaseArgument, withRequired(registryVisibilityArgument),
		registrySelectedArgument, withRequired(registryLocalPathArgument)},
	Fields: privateRegistryWriteFields,
	Examples: []capability.Example{{
		Description: "Create a registry for selected repositories",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","registry_type":"npm_registry",` +
			`"url":"https://registry.example.com/npm","visibility":"selected","selected_repositories":` +
			`["octo-org/example"],"local_path":"~/secrets/registry-token"}`),
	}},
}

var privateRegistriesUpdate = capability.Descriptor{
	ID:      Provider + ".private-registries.update",
	Version: 1,
	Title:   "Update a GitHub private registry",
	Description: "Change one private registry of an organization an explicit connection allows by its secret " +
		"name; a new url must be an https address whose host the connection approves as a registry-hosts/HOST " +
		"target, a change without url keeps the existing address and needs no host approval; a new credential " +
		"is read from a local file the connection releases for reading, sealed locally with the organization's " +
		"public key, and only the sealed text is sent; the value is never an argument and never part of an " +
		"answer; at least one change is required; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "dependabot", "private-registries", "organization", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, privateRegistryValueSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	LocalFiles:            config.LocalFilesRead,
	InputSchema:           inputSchema(`"secret_name":`+privateRegistryNameSchema+`,`+privateRegistryWriteProperties, "secret_name"),
	OutputSchema:          json.RawMessage(privateRegistryWriteOutput),
	Arguments: []capability.Argument{privateRegistryNameArgument, registryTypeArgument, registryURLArgument,
		registryUsernameArgument, registryReplacesBaseArgument, registryVisibilityArgument, registrySelectedArgument,
		registryLocalPathArgument},
	Fields: privateRegistryWriteFields,
	Examples: []capability.Example{{
		Description: "Replace the credential of a registry",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","secret_name":"NPM_REGISTRY_1","local_path":"~/secrets/registry-token"}`),
	}},
}

func withRequired(argument capability.Argument) capability.Argument {
	argument.Required = true
	return argument
}

// privateRegistryWriteToolIDs are the two write tools; each names an organization as its owner target.
var privateRegistryWriteToolIDs = []string{privateRegistriesCreate.ID, privateRegistriesUpdate.ID}

// privateRegistryWriteArguments holds the arguments of both write tools; the input schema of each admits only
// its own. Nothing here is a value but the local path, and the file content is held only for the call.
type privateRegistryWriteArguments struct {
	SecretName           string    `json:"secret_name"`
	RegistryType         *string   `json:"registry_type"`
	AuthType             *string   `json:"auth_type"`
	URL                  *string   `json:"url"`
	Username             *string   `json:"username"`
	ReplacesBase         *bool     `json:"replaces_base"`
	Visibility           *string   `json:"visibility"`
	SelectedRepositories *[]string `json:"selected_repositories"`
	LocalPath            *string   `json:"local_path"`

	allowed  allowlist
	selected []target
	value    []byte
}

func privateRegistryWriteOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: privateRegistriesCreate, Handler: privateRegistryWriteHandler(privateRegistriesCreate.ID, true)},
		{Descriptor: privateRegistriesUpdate, Handler: privateRegistryWriteHandler(privateRegistriesUpdate.ID, false)},
	}
}

// privateRegistryWriteHandler checks the arguments, the organization, the address, and the selected
// repositories, then reads the file, and only then resolves a credential, so a refused request never becomes a
// secret read, a file read, or a provider call.
func privateRegistryWriteHandler(id string, create bool) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments privateRegistryWriteArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		defer func() { clear(arguments.value) }()
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if arguments.allowed, err = allowlistOf(resolved); err != nil {
			return nil, providerError("open", err.Error())
		}
		if err := arguments.check(bound, create); err != nil {
			return nil, err
		}
		if arguments.LocalPath != nil {
			if arguments.value, err = readSecretValue(ctx, resolved, *arguments.LocalPath); err != nil {
				return nil, err
			}
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(client.writePrivateRegistry(ctx, &arguments, create))
	}
}

func (a *privateRegistryWriteArguments) check(bound target, create bool) error {
	if create {
		if a.RegistryType == nil || a.URL == nil || a.Visibility == nil || a.LocalPath == nil {
			return invalidRequest("registry_type, url, visibility, and " + localfile.LocalPathArgument + " are required")
		}
		if a.AuthType != nil && !slices.Contains(privateRegistryAuthTypes, *a.AuthType) {
			return invalidRequest("auth_type must be token or username_password")
		}
	} else {
		if err := checkPrivateRegistryName(a.SecretName); err != nil {
			return err
		}
		if a.AuthType != nil {
			return invalidRequest("auth_type cannot be changed")
		}
		if a.RegistryType == nil && a.URL == nil && a.Username == nil && a.ReplacesBase == nil && a.Visibility == nil &&
			a.LocalPath == nil {
			return invalidRequest("at least one change is required")
		}
	}
	if a.RegistryType != nil && !slices.Contains(privateRegistryTypes, *a.RegistryType) {
		return invalidRequest("registry_type is not a registry type GitHub documents")
	}
	if a.URL != nil {
		if len(*a.URL) > privateRegistryURLMax {
			return invalidRequest("url must be at most 2048 characters")
		}
		if err := checkRegistryURL(a.allowed, *a.URL); err != nil {
			return err
		}
	}
	if a.Username != nil && !validRegistryUsername(*a.Username) {
		return invalidRequest("username must be 1 to 255 characters without control characters")
	}
	if a.LocalPath != nil && *a.LocalPath == "" {
		return invalidRequest(localfile.LocalPathArgument + " must not be empty")
	}
	if a.Visibility != nil && !slices.Contains([]string{"all", "private", "selected"}, *a.Visibility) {
		return invalidRequest("visibility must be all, private, or selected")
	}
	selected, err := checkVisibility("registry", a.Visibility, a.SelectedRepositories, a.allowed, bound)
	if err != nil {
		return err
	}
	a.selected = selected
	if a.Visibility != nil && *a.Visibility != "selected" && a.allowed.restrictsRepositories(bound.owner) {
		return invalidRequest("visibility " + *a.Visibility + " would reach repositories of the organization " +
			"outside the targets of this connection; use selected with repositories inside the targets")
	}
	return nil
}

func validRegistryUsername(value string) bool {
	n := utf8.RuneCountInString(value)
	return n >= 1 && n <= 255 && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

// PrivateRegistryChange is the answer to a created or changed private registry: what GitHub did and what was
// sent besides the sealed value, never the value or the address.
type PrivateRegistryChange struct {
	State                string   `json:"state"`
	Name                 string   `json:"name"`
	RegistryType         string   `json:"registry_type,omitempty"`
	Visibility           string   `json:"visibility,omitempty"`
	SelectedRepositories []string `json:"selected_repositories,omitempty"`
}

// writePrivateRegistry resolves the selected repositories, reads the key of the organization when a value is
// sent, seals it, and sends one change that is never repeated. GitHub answers 201 for a created registry and
// 204 for a changed one; any other success is reported as uncertain. Only the name of a created registry is
// read from the answer, and only when it has the form of a secret name.
func (c *Client) writePrivateRegistry(ctx context.Context, a *privateRegistryWriteArguments, create bool) (*PrivateRegistryChange, error) {
	op := "update private registry"
	if create {
		op = "create private registry"
	}
	result := &PrivateRegistryChange{State: "updated", Name: a.SecretName}
	body := map[string]any{}
	if a.RegistryType != nil {
		body["registry_type"] = *a.RegistryType
		result.RegistryType = *a.RegistryType
	}
	if a.AuthType != nil {
		body["auth_type"] = *a.AuthType
	}
	if a.URL != nil {
		body["url"] = *a.URL
	}
	if a.Username != nil {
		body["username"] = *a.Username
	}
	if a.ReplacesBase != nil {
		body["replaces_base"] = *a.ReplacesBase
	}
	if a.Visibility != nil {
		body["visibility"] = *a.Visibility
		result.Visibility = *a.Visibility
		if *a.Visibility == "selected" {
			ids, err := c.selectedRepositoryIDs(ctx, op, a.selected)
			if err != nil {
				return nil, err
			}
			body["selected_repository_ids"] = ids
			for _, repository := range a.selected {
				result.SelectedRepositories = append(result.SelectedRepositories, repository.argument())
			}
		}
	}
	if a.value != nil {
		var raw publicKeyRaw
		if err := c.rest(ctx, op, c.privateRegistryPath("")+"/public-key", &raw); err != nil {
			return nil, actionsFailure(err, privateRegistryWritePermission)
		}
		keyID, key, err := raw.sealingKey(op)
		if err != nil {
			return nil, err
		}
		if body["encrypted_value"], err = sealValue(op, a.value, key); err != nil {
			return nil, err
		}
		body["key_id"] = keyID
	}
	if !create {
		status, err := c.restChangeStatus(ctx, op, http.MethodPatch, c.privateRegistryPath(a.SecretName), body, nil)
		if err != nil {
			return nil, variableFailure(err, privateRegistryWritePermission, "", privateRegistryRejected)
		}
		if status != http.StatusNoContent {
			return nil, invalidResponse(op, true)
		}
		return result, nil
	}
	var created struct {
		Name *string `json:"name"`
	}
	status, err := c.restChangeStatus(ctx, op, http.MethodPost, c.privateRegistryPath(""), body, &created)
	if err != nil {
		return nil, variableFailure(err, privateRegistryWritePermission, "", privateRegistryRejected)
	}
	if status != http.StatusCreated || created.Name == nil || checkPrivateRegistryName(*created.Name) != nil {
		return nil, invalidResponse(op, true)
	}
	result.State, result.Name = "created", *created.Name
	return result, nil
}

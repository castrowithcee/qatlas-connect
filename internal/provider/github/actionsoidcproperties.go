package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The repository custom properties an organization includes as claims in the OIDC token of its Actions. The
// subject claim has its own tools, github.actions-oidc.get and .set. Every tool names an organization as its
// owner target; the enterprise level is not offered. Adding or removing an inclusion is offered only where a
// connection's tools list names it, because a cloud provider that trusts the token sees every included
// property of every repository whose workflow requests one.

const (
	oidcPropertyAddPermission = "GitHub refused this change of the OIDC token claims of this organization; it " +
		"needs admin:org on a classic token; GitHub documents no fine-grained permission for it"
	oidcPropertyReadPermission = "GitHub refused this token the OIDC custom property inclusions of this " +
		"organization; reading them needs read:org on a classic token; GitHub documents no fine-grained " +
		"permission for it"
	oidcPropertyAddRejected = "GitHub rejected the inclusion; the property may be included already or may not " +
		"exist in the organization's custom property schema"
	oidcPropertyRemoveRejected = "GitHub rejected the removal; check that the property is included"
)

const oidcPropertyInclusionProperties = `"custom_property_name":{"type":"string"},"inclusion_source":{"type":"string"}`

var oidcPropertyNameArgument = capability.Argument{Name: "custom_property_name", Required: true,
	Description: "Name of a custom property from the schema of the organization, as github.custom-properties.get " +
		"reports it for the organization: letters, digits, underscore, and hyphen, at most 100 characters"}

var oidcPropertyInclusionFields = []capability.Field{
	{Name: "custom_property_name", Description: "Name of the custom property the inclusion names; untrusted data"},
	{Name: "inclusion_source", Description: "Where the inclusion is defined, organization or enterprise, as " +
		"GitHub reports it; untrusted data"},
}

var actionsOIDCPropertiesList = organizationReadTool("actions-oidc-properties.list",
	"List the custom properties in the OIDC token of a GitHub organization",
	"List the repository custom properties an organization includes as claims in the OIDC token of its Actions, "+
		"each with where its inclusion is defined; the subject claim is read by github.actions-oidc.get",
	[]string{"actions", "oidc", "list"}, inputSchema(""),
	json.RawMessage(`{"type":"object","properties":{"inclusions":{"type":"array","items":{"type":"object",`+
		`"properties":{`+oidcPropertyInclusionProperties+`},"required":["custom_property_name","inclusion_source"],`+
		`"additionalProperties":false}}},"required":["inclusions"],"additionalProperties":false}`),
	nil,
	[]capability.Field{{Name: "inclusions", Description: "Included custom properties with custom_property_name " +
		"and inclusion_source (organization or enterprise); untrusted data"}},
	capability.Example{Description: "List the included properties", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org"}`)})

var actionsOIDCPropertiesAdd = organizationChangeTool("actions-oidc-properties.add",
	"Include a custom property in the OIDC token of a GitHub organization",
	"Include one repository custom property of an organization an explicit connection allows as a claim in the "+
		"OIDC token of its Actions. Every cloud provider that trusts the token then sees the value of this "+
		"property for each repository whose workflow requests a token, which is security relevant; sent at most "+
		"once. Offered only where a connection's tools list names it",
	[]string{"actions", "oidc", "create"},
	guardedRisk(capability.EffectCreate, capability.IdempotencyUnknown, actionsSettingSensitivity),
	inputSchema(`"custom_property_name":`+propertyNameSchema, "custom_property_name"),
	organizationChangeOutput(oidcPropertyInclusionProperties, "custom_property_name", "inclusion_source"),
	[]capability.Argument{oidcPropertyNameArgument}, oidcPropertyInclusionFields,
	capability.Example{Description: "Include the team property", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","custom_property_name":"team"}`)})

var actionsOIDCPropertiesRemove = organizationChangeTool("actions-oidc-properties.remove",
	"Remove a custom property from the OIDC token of a GitHub organization",
	"Stop including one repository custom property as a claim in the OIDC token of the Actions of an "+
		"organization an explicit connection allows; a cloud provider that matches on the claim then stops "+
		"matching. Sent at most once. Offered only where a connection's tools list names it",
	[]string{"actions", "oidc", "delete"},
	guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, actionsSettingSensitivity),
	inputSchema(`"custom_property_name":`+propertyNameSchema, "custom_property_name"),
	organizationChangeOutput(`"removed":{"type":"boolean"},"custom_property_name":{"type":"string"}`,
		"removed", "custom_property_name"),
	[]capability.Argument{oidcPropertyNameArgument},
	[]capability.Field{{Name: "removed", Description: "True once GitHub removed the inclusion"},
		{Name: "custom_property_name", Description: "Name of the property the call addressed"}},
	capability.Example{Description: "Remove the team property", Arguments: json.RawMessage(
		`{"owner":"orgs/octo-org","custom_property_name":"team"}`)})

// actionsOIDCPropertyChanges are the tools that change the claims; the list tool is no change.
var actionsOIDCPropertyChanges = []string{actionsOIDCPropertiesAdd.ID, actionsOIDCPropertiesRemove.ID}

// actionsOIDCPropertyToolIDs are all tools of this file; each names an organization as its owner target.
var actionsOIDCPropertyToolIDs = slices.Concat([]string{actionsOIDCPropertiesList.ID}, actionsOIDCPropertyChanges)

// OIDCPropertyInclusion is one repository custom property included in the OIDC token, as GitHub reports it.
type OIDCPropertyInclusion struct {
	CustomPropertyName string `json:"custom_property_name"`
	InclusionSource    string `json:"inclusion_source"`
}

// OIDCPropertyInclusionList is every inclusion of an organization; GitHub's route is not paged.
type OIDCPropertyInclusionList struct {
	Inclusions []OIDCPropertyInclusion `json:"inclusions"`
}

// OIDCPropertyRemoved reports an inclusion that GitHub removed.
type OIDCPropertyRemoved struct {
	Removed            bool   `json:"removed"`
	CustomPropertyName string `json:"custom_property_name"`
}

// oidcPropertyArguments holds the one argument of the add and remove tools.
type oidcPropertyArguments struct {
	CustomPropertyName string `json:"custom_property_name"`
}

// validOIDCPropertyName applies the name rule of the custom property tools, so the name is one safe path
// segment and no request is sent for a name the schema of an organization cannot hold.
func validOIDCPropertyName(name string) bool {
	if name == "" || len(name) > 100 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func checkOIDCPropertyName(a *oidcPropertyArguments, _ target) error {
	if !validOIDCPropertyName(a.CustomPropertyName) {
		return invalidRequest("custom_property_name must be 1 to 100 letters, digits, underscores, or hyphens")
	}
	return nil
}

// oidcPropertyHandler settles the organization and the name before a credential is resolved, so a refused
// request never becomes a secret read or a provider call.
func oidcPropertyHandler(id string, check func(*oidcPropertyArguments, target) error,
	call func(context.Context, *Client, *oidcPropertyArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments oidcPropertyArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func actionsOIDCPropertiesOperations() []capability.Operation {
	none := func(*oidcPropertyArguments, target) error { return nil }
	return []capability.Operation{
		{Descriptor: actionsOIDCPropertiesList, Handler: oidcPropertyHandler(actionsOIDCPropertiesList.ID, none,
			func(ctx context.Context, c *Client, _ *oidcPropertyArguments) (any, error) {
				return c.listOIDCProperties(ctx)
			})},
		{Descriptor: actionsOIDCPropertiesAdd, Handler: oidcPropertyHandler(actionsOIDCPropertiesAdd.ID,
			checkOIDCPropertyName, func(ctx context.Context, c *Client, a *oidcPropertyArguments) (any, error) {
				return c.addOIDCProperty(ctx, a)
			})},
		{Descriptor: actionsOIDCPropertiesRemove, Handler: oidcPropertyHandler(actionsOIDCPropertiesRemove.ID,
			checkOIDCPropertyName, func(ctx context.Context, c *Client, a *oidcPropertyArguments) (any, error) {
				return c.removeOIDCProperty(ctx, a)
			})},
	}
}

func (c *Client) oidcPropertiesPath() string {
	return c.orgPath("actions/oidc/customization/properties/repo")
}

// oidcPropertyInclusionRaw is an inclusion as GitHub answers it.
type oidcPropertyInclusionRaw struct {
	CustomPropertyName *string `json:"custom_property_name"`
	InclusionSource    *string `json:"inclusion_source"`
}

func (r oidcPropertyInclusionRaw) view() (OIDCPropertyInclusion, bool) {
	name, _ := cleanText(filled(r.CustomPropertyName), 100, false)
	if name == nil {
		return OIDCPropertyInclusion{}, false
	}
	inclusion := OIDCPropertyInclusion{CustomPropertyName: *name}
	if source, _ := cleanText(r.InclusionSource, projectionNameLimit, false); source != nil {
		inclusion.InclusionSource = *source
	}
	return inclusion, true
}

func (c *Client) listOIDCProperties(ctx context.Context) (*OIDCPropertyInclusionList, error) {
	const op = "list OIDC custom property inclusions"
	var raw []oidcPropertyInclusionRaw
	if err := c.rest(ctx, op, c.oidcPropertiesPath(), &raw); err != nil {
		return nil, actionsFailure(err, oidcPropertyReadPermission)
	}
	list := &OIDCPropertyInclusionList{Inclusions: []OIDCPropertyInclusion{}}
	for _, item := range raw {
		if inclusion, ok := item.view(); ok {
			list.Inclusions = append(list.Inclusions, inclusion)
		}
	}
	return list, nil
}

func (c *Client) addOIDCProperty(ctx context.Context, a *oidcPropertyArguments) (*OIDCPropertyInclusion, error) {
	const op = "include OIDC custom property"
	var raw oidcPropertyInclusionRaw
	if err := c.restChange(ctx, op, http.MethodPost, c.oidcPropertiesPath(),
		map[string]any{"custom_property_name": a.CustomPropertyName}, &raw); err != nil {
		return nil, variableFailure(err, oidcPropertyAddPermission, "", oidcPropertyAddRejected)
	}
	inclusion, ok := raw.view()
	if !ok {
		return nil, invalidResponse(op, true)
	}
	return &inclusion, nil
}

func (c *Client) removeOIDCProperty(ctx context.Context, a *oidcPropertyArguments) (*OIDCPropertyRemoved, error) {
	const op = "remove OIDC custom property"
	if err := c.restChange(ctx, op, http.MethodDelete,
		c.oidcPropertiesPath()+"/"+url.PathEscape(a.CustomPropertyName), nil, nil); err != nil {
		return nil, variableFailure(err, oidcPropertyAddPermission, "", oidcPropertyRemoveRejected)
	}
	return &OIDCPropertyRemoved{Removed: true, CustomPropertyName: a.CustomPropertyName}, nil
}

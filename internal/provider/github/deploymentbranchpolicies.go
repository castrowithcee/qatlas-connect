package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// The deployment branch policies of one environment: the name patterns a branch or a tag must match to deploy
// to it, which GitHub applies only while the environment's deployment_branch_policy is custom.
// github.deployment-branch-policies.list reads them, .create and .update change them, and .delete, offered
// only where a connection's tools list names it, removes one. Every route lies below the environment of the
// bound repository the call names.

const maxBranchPolicyNameLength = 255

const (
	branchPolicyNameSchema = `{"type":"string","minLength":1,"maxLength":255,"x-form":"name pattern"}`
	branchPolicyTypeSchema = `{"type":"string","enum":["branch","tag"]}`
)

var branchPolicyProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"name":{"type":"string"},` +
	`"type":{"type":"string"}`

var branchPolicyOutput = `{"type":"object","properties":{` + branchPolicyProperties +
	`,"environment":{"type":"string"}},"required":["id","name","environment"],"additionalProperties":false}`

var branchPolicyListOutput = `{"type":"object","properties":{"branch_policies":{"type":"array","items":` +
	`{"type":"object","properties":{` + branchPolicyProperties + `},"required":["id","name"],` +
	`"additionalProperties":false}},"environment":{"type":"string"},"total_count":{"type":"integer"},` +
	`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
	`"required":["branch_policies","environment","has_more"],"additionalProperties":false}`

const branchPolicyDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
	`"environment":{"type":"string"},"id":{"type":"integer"}},"required":["deleted","environment","id"],` +
	`"additionalProperties":false}`

var branchPolicyEnvironmentField = capability.Field{Name: "environment", Description: "Environment the call " +
	"named, as the call gave it"}

var branchPolicyFields = []capability.Field{
	{Name: "id", Description: "Numeric identifier of the deployment branch policy"},
	{Name: "node_id", Description: "GraphQL node id of the deployment branch policy"},
	{Name: "name", Description: "Name pattern a branch or a tag must match, untrusted data"},
	{Name: "type", Description: "branch or tag"},
	branchPolicyEnvironmentField,
}

var branchPolicyIDArgument = capability.Argument{Name: "id", Description: "Numeric deployment branch policy " +
	"identifier, as github.deployment-branch-policies.list reports it", Required: true}

var branchPoliciesList = capability.Descriptor{
	ID:      Provider + ".deployment-branch-policies.list",
	Version: 1,
	Title:   "List GitHub deployment branch policies",
	Description: "List one bounded batch of the deployment branch policies of one environment of a repository " +
		"a connection allows: the name patterns a branch or a tag must match to deploy to it, which GitHub " +
		"applies while the environment's deployment_branch_policy is custom",
	Tags:         []string{"github", "actions", "environments", "deployment-branch-policies", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"environment":`+environmentNameSchema+`,`+pagingKeys, "environment"),
	OutputSchema: json.RawMessage(branchPolicyListOutput),
	Arguments:    append([]capability.Argument{environmentArgument}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "branch_policies", Description: "Deployment branch policies: id, node_id, name (the pattern, " +
			"untrusted data), and type (branch or tag)"},
		branchPolicyEnvironmentField,
		{Name: "total_count", Description: "Number of deployment branch policies GitHub reports for the environment"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the deployment branch policies of an environment",
		Arguments: json.RawMessage(`{"environment":"production"}`)}},
}

var branchPoliciesCreate = capability.Descriptor{
	ID:      Provider + ".deployment-branch-policies.create",
	Version: 1,
	Title:   "Create a GitHub deployment branch policy",
	Description: "Create one deployment branch policy of one environment of a repository a connection allows: " +
		"a name pattern, with * not matching /, that a branch or, with type tag, a tag must match to deploy; " +
		"the environment's deployment_branch_policy must be custom; a pattern the environment already holds " +
		"is refused without a second one; not idempotent",
	Tags:     []string{"github", "actions", "environments", "deployment-branch-policies", "create"},
	Risk:     changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"environment":`+environmentNameSchema+`,"name":`+branchPolicyNameSchema+
		`,"type":`+branchPolicyTypeSchema, "environment", "name"),
	OutputSchema: json.RawMessage(branchPolicyOutput),
	Arguments: []capability.Argument{
		environmentArgument,
		{Name: "name", Description: "Name pattern a branch or a tag must match, such as release/*; GitHub " +
			"matches it like Ruby's File.fnmatch, and * does not match /", Required: true},
		{Name: "type", Description: "branch or tag; GitHub's default branch when left out"},
	},
	Fields: branchPolicyFields,
	Examples: []capability.Example{{Description: "Let release branches deploy",
		Arguments: json.RawMessage(`{"environment":"production","name":"release/*"}`)}},
}

var branchPoliciesUpdate = capability.Descriptor{
	ID:      Provider + ".deployment-branch-policies.update",
	Version: 1,
	Title:   "Update a GitHub deployment branch policy",
	Description: "Change the name pattern of one deployment branch policy of one environment of a repository " +
		"a connection allows; its type stays as it is; idempotent",
	Tags:     []string{"github", "actions", "environments", "deployment-branch-policies", "update"},
	Risk:     changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider: Provider,
	InputSchema: inputSchema(`"environment":`+environmentNameSchema+`,"id":`+actionsIDSchema+`,"name":`+
		branchPolicyNameSchema, "environment", "id", "name"),
	OutputSchema: json.RawMessage(branchPolicyOutput),
	Arguments: []capability.Argument{
		environmentArgument, branchPolicyIDArgument,
		{Name: "name", Description: "New name pattern, matched like the one github.deployment-branch-policies.create takes",
			Required: true},
	},
	Fields: branchPolicyFields,
	Examples: []capability.Example{{Description: "Narrow a branch pattern",
		Arguments: json.RawMessage(`{"environment":"production","id":361471,"name":"release/v*"}`)}},
}

var branchPoliciesDelete = capability.Descriptor{
	ID:      Provider + ".deployment-branch-policies.delete",
	Version: 1,
	Title:   "Delete a GitHub deployment branch policy",
	Description: "Delete one deployment branch policy of one environment of a repository a connection allows " +
		"permanently; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "actions", "environments", "deployment-branch-policies", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, environmentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"environment":`+environmentNameSchema+`,"id":`+actionsIDSchema, "environment", "id"),
	OutputSchema:          json.RawMessage(branchPolicyDeletedOutput),
	Arguments:             []capability.Argument{environmentArgument, branchPolicyIDArgument},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the deployment branch policy"},
		branchPolicyEnvironmentField,
		{Name: "id", Description: "Identifier of the deleted deployment branch policy"},
	},
	Examples: []capability.Example{{Description: "Delete a deployment branch policy",
		Arguments: json.RawMessage(`{"environment":"production","id":361471}`)}},
}

func checkBranchPoliciesList(a *environmentsArguments, bound target, allowed allowlist) error {
	if err := checkEnvironmentName(a, bound, allowed); err != nil {
		return err
	}
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("deployment-branch-policies", "list", bound.String(), a.Environment)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkBranchPolicyName(a *environmentsArguments, bound target, allowed allowlist) error {
	if err := checkEnvironmentName(a, bound, allowed); err != nil {
		return err
	}
	if strings.TrimSpace(a.Name) == "" || len([]rune(a.Name)) > maxBranchPolicyNameLength {
		return invalidRequest("name must be a pattern of 1 to 255 characters")
	}
	if a.Type != "" && a.Type != "branch" && a.Type != "tag" {
		return invalidRequest("type must be branch or tag")
	}
	return checkText("name", a.Name)
}

func checkBranchPolicyUpdate(a *environmentsArguments, bound target, allowed allowlist) error {
	if err := checkBranchPolicyID(a, bound, allowed); err != nil {
		return err
	}
	return checkBranchPolicyName(a, bound, allowed)
}

func checkBranchPolicyID(a *environmentsArguments, bound target, allowed allowlist) error {
	if a.ID <= 0 {
		return invalidRequest("id must be a deployment branch policy identifier")
	}
	return checkEnvironmentName(a, bound, allowed)
}

// branchPolicyMissing adds what GitHub's not-found answer of a deployment branch policy route may also mean.
const branchPolicyMissing = "; GitHub answers the same while the environment's deployment_branch_policy " +
	"is not custom"

const branchPoliciesRejected = "GitHub rejected the deployment branch policy; the pattern may be invalid; " +
	"nothing was changed"

// DeploymentBranchPolicy is one deployment branch policy.
type DeploymentBranchPolicy struct {
	ID     int64   `json:"id"`
	NodeID *string `json:"node_id,omitempty"`
	Name   string  `json:"name"`
	Type   *string `json:"type,omitempty"`
}

// DeploymentBranchPolicyResult is a created or changed deployment branch policy with the environment the call
// named.
type DeploymentBranchPolicyResult struct {
	DeploymentBranchPolicy
	Environment string `json:"environment"`
}

// DeploymentBranchPolicyList is one batch of the deployment branch policies of an environment.
type DeploymentBranchPolicyList struct {
	BranchPolicies []DeploymentBranchPolicy `json:"branch_policies"`
	Environment    string                   `json:"environment"`
	TotalCount     *int64                   `json:"total_count,omitempty"`
	NextCursor     string                   `json:"next_cursor,omitempty"`
	HasMore        bool                     `json:"has_more"`
}

// DeploymentBranchPolicyDeleted is the answer to a deleted deployment branch policy.
type DeploymentBranchPolicyDeleted struct {
	Deleted     bool   `json:"deleted"`
	Environment string `json:"environment"`
	ID          int64  `json:"id"`
}

type branchPolicyRaw struct {
	ID     *int64  `json:"id"`
	NodeID *string `json:"node_id"`
	Name   *string `json:"name"`
	Type   *string `json:"type"`
}

// project reports false for a policy without a usable identifier or pattern; a pattern is never cut, since a
// cut pattern states another rule.
func (r *branchPolicyRaw) project() (DeploymentBranchPolicy, bool) {
	name, cut := cleanText(r.Name, maxBranchPolicyNameLength, false)
	if r.ID == nil || *r.ID <= 0 || name == nil || *name == "" || cut {
		return DeploymentBranchPolicy{}, false
	}
	return DeploymentBranchPolicy{ID: *r.ID, NodeID: nameText(r.NodeID), Name: *name, Type: nameText(r.Type)}, true
}

func (c *Client) branchPolicyPath(environment string, id int64) string {
	if id == 0 {
		return c.environmentPath(environment, "deployment-branch-policies")
	}
	return c.environmentPath(environment, "deployment-branch-policies/"+strconv.FormatInt(id, 10))
}

func (c *Client) listBranchPolicies(ctx context.Context, a *environmentsArguments) (*DeploymentBranchPolicyList, error) {
	const op = "list deployment branch policies"
	var raw struct {
		TotalCount     *int64            `json:"total_count"`
		BranchPolicies []branchPolicyRaw `json:"branch_policies"`
	}
	hasNext, err := c.restPage(ctx, op, c.branchPolicyPath(a.Environment, 0), a.query(), &raw)
	if err != nil {
		return nil, environmentFailure(err, environmentsReadPermission, branchPolicyMissing, "")
	}
	result := &DeploymentBranchPolicyList{BranchPolicies: make([]DeploymentBranchPolicy, 0, len(raw.BranchPolicies)),
		Environment: a.Environment, TotalCount: raw.TotalCount}
	for i := range raw.BranchPolicies {
		policy, ok := raw.BranchPolicies[i].project()
		if !ok {
			return nil, invalidEntry(op, "a deployment branch policy")
		}
		result.BranchPolicies = append(result.BranchPolicies, policy)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// createBranchPolicy sends one POST, never repeated. GitHub answers a pattern the environment already holds
// with 303 See Other, which is never followed: nothing was created, so it is a clear refusal instead.
func (c *Client) createBranchPolicy(ctx context.Context, a *environmentsArguments) (*DeploymentBranchPolicyResult, error) {
	const op = "create deployment branch policy"
	body := map[string]any{"name": a.Name}
	if a.Type != "" {
		body["type"] = a.Type
	}
	var raw branchPolicyRaw
	err := c.restChange(ctx, op, http.MethodPost, c.branchPolicyPath(a.Environment, 0), body, &raw)
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Original != nil && failure.Original.Status == http.StatusSeeOther {
		return nil, invalidRequest("environment " + a.Environment + " already holds a deployment branch policy " +
			"with this pattern; nothing was created")
	}
	if err != nil {
		return nil, environmentFailure(err, environmentsChangePermission, branchPolicyMissing, branchPoliciesRejected)
	}
	policy, ok := raw.project()
	if !ok {
		return nil, invalidResponse(op, true)
	}
	return &DeploymentBranchPolicyResult{DeploymentBranchPolicy: policy, Environment: a.Environment}, nil
}

func (c *Client) updateBranchPolicy(ctx context.Context, a *environmentsArguments) (*DeploymentBranchPolicyResult, error) {
	const op = "update deployment branch policy"
	var raw branchPolicyRaw
	if err := c.restChange(ctx, op, http.MethodPut, c.branchPolicyPath(a.Environment, a.ID),
		map[string]any{"name": a.Name}, &raw); err != nil {
		return nil, environmentFailure(err, environmentsChangePermission, branchPolicyMissing, branchPoliciesRejected)
	}
	policy, ok := raw.project()
	if !ok {
		return nil, invalidResponse(op, true)
	}
	return &DeploymentBranchPolicyResult{DeploymentBranchPolicy: policy, Environment: a.Environment}, nil
}

func (c *Client) deleteBranchPolicy(ctx context.Context, a *environmentsArguments) (*DeploymentBranchPolicyDeleted, error) {
	const op = "delete deployment branch policy"
	if err := c.restChange(ctx, op, http.MethodDelete, c.branchPolicyPath(a.Environment, a.ID), nil, nil); err != nil {
		return nil, environmentFailure(err, environmentsChangePermission, branchPolicyMissing, "")
	}
	return &DeploymentBranchPolicyDeleted{Deleted: true, Environment: a.Environment, ID: a.ID}, nil
}

package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// The custom deployment protection rules of one environment: GitHub Apps whose approval a deployment to it
// waits for. github.deployment-protection-rules.list and .get read them, and .delete, offered only where a
// connection's tools list names it, disables one, which drops that approval. Every route lies below the
// environment of the bound repository the call names; the answers carry nothing that names the repository,
// so the route alone binds them.

var protectionRuleAppLinkKeys = []string{"integration_url"}

var protectionRuleProperties = `"id":{"type":"integer"},"node_id":{"type":"string"},"enabled":{"type":"boolean"},` +
	`"app":` + objectSchema(`"id":{"type":"integer"},"slug":{"type":"string"},"node_id":{"type":"string"},`+
	`"links":`+linksSchema(protectionRuleAppLinkKeys))

var protectionRuleOutput = `{"type":"object","properties":{` + protectionRuleProperties +
	`,"environment":{"type":"string"}},"required":["id","environment"],"additionalProperties":false}`

var protectionRuleListOutput = `{"type":"object","properties":{"custom_deployment_protection_rules":{"type":` +
	`"array","items":{"type":"object","properties":{` + protectionRuleProperties + `},"required":["id"],` +
	`"additionalProperties":false}},"environment":{"type":"string"},"total_count":{"type":"integer"}},` +
	`"required":["custom_deployment_protection_rules","environment"],"additionalProperties":false}`

const protectionRuleDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
	`"environment":{"type":"string"},"protection_rule_id":{"type":"integer"}},` +
	`"required":["deleted","environment","protection_rule_id"],"additionalProperties":false}`

const protectionRuleMembers = "id, node_id, enabled, and app (id, slug, node_id, and links with " +
	"integration_url, the REST address of the app, untrusted data, never fetched)"

var protectionRuleFields = []capability.Field{
	{Name: "id", Description: "Numeric id of the deployment protection rule"},
	{Name: "node_id", Description: "GraphQL node id of the deployment protection rule"},
	{Name: "enabled", Description: "True while the rule applies to the environment"},
	{Name: "app", Description: "GitHub App that decides the rule: id, slug (untrusted data), node_id, and " +
		"links with integration_url, its REST address, untrusted data, never fetched"},
	branchPolicyEnvironmentField,
}

var protectionRuleIDArgument = capability.Argument{Name: "protection_rule_id", Description: "Numeric id of the " +
	"deployment protection rule, as github.deployment-protection-rules.list reports it", Required: true}

var protectionRulesList = capability.Descriptor{
	ID:      Provider + ".deployment-protection-rules.list",
	Version: 1,
	Title:   "List GitHub deployment protection rules",
	Description: "List the custom deployment protection rules enabled for one environment of a repository a " +
		"connection allows: the GitHub Apps whose approval a deployment to it waits for; GitHub answers them " +
		"in one batch",
	Tags:         []string{"github", "actions", "environments", "deployment-protection-rules", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"environment":`+environmentNameSchema, "environment"),
	OutputSchema: json.RawMessage(protectionRuleListOutput),
	Arguments:    []capability.Argument{environmentArgument},
	Fields: []capability.Field{
		{Name: "custom_deployment_protection_rules", Description: "Deployment protection rules: " +
			protectionRuleMembers},
		branchPolicyEnvironmentField,
		{Name: "total_count", Description: "Number of enabled custom deployment protection rules GitHub reports"},
	},
	Examples: []capability.Example{{Description: "List the deployment protection rules of an environment",
		Arguments: json.RawMessage(`{"environment":"production"}`)}},
}

var protectionRulesGet = capability.Descriptor{
	ID:      Provider + ".deployment-protection-rules.get",
	Version: 1,
	Title:   "Get a GitHub deployment protection rule",
	Description: "Read one custom deployment protection rule enabled for one environment of a repository a " +
		"connection allows, by id",
	Tags:     []string{"github", "actions", "environments", "deployment-protection-rules", "get"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"environment":`+environmentNameSchema+`,"protection_rule_id":`+actionsIDSchema,
		"environment", "protection_rule_id"),
	OutputSchema: json.RawMessage(protectionRuleOutput),
	Arguments:    []capability.Argument{environmentArgument, protectionRuleIDArgument},
	Fields:       protectionRuleFields,
	Examples: []capability.Example{{Description: "Read one deployment protection rule",
		Arguments: json.RawMessage(`{"environment":"production","protection_rule_id":3}`)}},
}

var protectionRulesDelete = capability.Descriptor{
	ID:      Provider + ".deployment-protection-rules.delete",
	Version: 1,
	Title:   "Delete a GitHub deployment protection rule",
	Description: "Disable one custom deployment protection rule of one environment of a repository a " +
		"connection allows, so a deployment to it no longer waits for that app's approval; the rule is read " +
		"first and nothing is sent when it is missing; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "actions", "environments", "deployment-protection-rules", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, environmentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"environment":`+environmentNameSchema+`,"protection_rule_id":`+actionsIDSchema,
		"environment", "protection_rule_id"),
	OutputSchema: json.RawMessage(protectionRuleDeletedOutput),
	Arguments:    []capability.Argument{environmentArgument, protectionRuleIDArgument},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub disabled the deployment protection rule"},
		branchPolicyEnvironmentField,
		{Name: "protection_rule_id", Description: "Id of the disabled deployment protection rule"},
	},
	Examples: []capability.Example{{Description: "Disable a deployment protection rule",
		Arguments: json.RawMessage(`{"environment":"production","protection_rule_id":3}`)}},
}

func checkProtectionRuleID(a *environmentsArguments, bound target, allowed allowlist) error {
	if a.ProtectionRuleID <= 0 {
		return invalidRequest("protection_rule_id must be a deployment protection rule id")
	}
	return checkEnvironmentName(a, bound, allowed)
}

const protectionRulesReadPermission = "GitHub refused this token the deployment protection rules of this " +
	"environment; reading them needs repo on a classic token for a private repository, or Actions: read on a " +
	"fine-grained token"

// DeploymentProtectionRuleApp is the GitHub App behind a deployment protection rule; links holds its
// integration_url.
type DeploymentProtectionRuleApp struct {
	ID     *int64  `json:"id,omitempty"`
	Slug   *string `json:"slug,omitempty"`
	NodeID *string `json:"node_id,omitempty"`
	Links  Links   `json:"links,omitempty"`
}

// DeploymentProtectionRule is one custom deployment protection rule.
type DeploymentProtectionRule struct {
	ID      int64                        `json:"id"`
	NodeID  *string                      `json:"node_id,omitempty"`
	Enabled *bool                        `json:"enabled,omitempty"`
	App     *DeploymentProtectionRuleApp `json:"app,omitempty"`
}

// DeploymentProtectionRuleResult is one deployment protection rule with the environment the call named.
type DeploymentProtectionRuleResult struct {
	DeploymentProtectionRule
	Environment string `json:"environment"`
}

// DeploymentProtectionRuleList is the custom deployment protection rules of an environment.
type DeploymentProtectionRuleList struct {
	Rules       []DeploymentProtectionRule `json:"custom_deployment_protection_rules"`
	Environment string                     `json:"environment"`
	TotalCount  *int64                     `json:"total_count,omitempty"`
}

// DeploymentProtectionRuleDeleted is the answer to a disabled deployment protection rule.
type DeploymentProtectionRuleDeleted struct {
	Deleted          bool   `json:"deleted"`
	Environment      string `json:"environment"`
	ProtectionRuleID int64  `json:"protection_rule_id"`
}

type protectionRuleAppRaw struct {
	ID     *int64  `json:"id"`
	Slug   *string `json:"slug"`
	NodeID *string `json:"node_id"`
	links  Links
}

func (r *protectionRuleAppRaw) UnmarshalJSON(data []byte) (err error) {
	type plain protectionRuleAppRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, protectionRuleAppLinkKeys)
	return err
}

type protectionRuleRaw struct {
	ID      *int64                `json:"id"`
	NodeID  *string               `json:"node_id"`
	Enabled *bool                 `json:"enabled"`
	App     *protectionRuleAppRaw `json:"app"`
}

// project reports false for a rule without a usable id.
func (r *protectionRuleRaw) project() (DeploymentProtectionRule, bool) {
	if r.ID == nil || *r.ID <= 0 {
		return DeploymentProtectionRule{}, false
	}
	rule := DeploymentProtectionRule{ID: *r.ID, NodeID: nameText(r.NodeID), Enabled: r.Enabled}
	if r.App != nil {
		rule.App = &DeploymentProtectionRuleApp{ID: r.App.ID, Slug: nameText(r.App.Slug),
			NodeID: nameText(r.App.NodeID), Links: r.App.links}
	}
	return rule, true
}

func (c *Client) protectionRulePath(environment string, id int64) string {
	if id == 0 {
		return c.environmentPath(environment, "deployment_protection_rules")
	}
	return c.environmentPath(environment, "deployment_protection_rules/"+strconv.FormatInt(id, 10))
}

func (c *Client) listProtectionRules(ctx context.Context, environment string) (*DeploymentProtectionRuleList, error) {
	const op = "list deployment protection rules"
	var raw struct {
		TotalCount *int64              `json:"total_count"`
		Rules      []protectionRuleRaw `json:"custom_deployment_protection_rules"`
	}
	if err := c.rest(ctx, op, c.protectionRulePath(environment, 0), &raw); err != nil {
		return nil, environmentFailure(err, protectionRulesReadPermission, "", "")
	}
	result := &DeploymentProtectionRuleList{Rules: make([]DeploymentProtectionRule, 0, len(raw.Rules)),
		Environment: environment, TotalCount: raw.TotalCount}
	for i := range raw.Rules {
		rule, ok := raw.Rules[i].project()
		if !ok {
			return nil, invalidEntry(op, "a deployment protection rule")
		}
		result.Rules = append(result.Rules, rule)
	}
	return result, nil
}

// getProtectionRule reads one rule and refuses an answer for another id; the delete uses it as its proof read.
func (c *Client) getProtectionRule(ctx context.Context, a *environmentsArguments) (*DeploymentProtectionRuleResult, error) {
	const op = "get deployment protection rule"
	var raw protectionRuleRaw
	if err := c.rest(ctx, op, c.protectionRulePath(a.Environment, a.ProtectionRuleID), &raw); err != nil {
		return nil, environmentFailure(err, protectionRulesReadPermission, "", "")
	}
	rule, ok := raw.project()
	if !ok {
		return nil, invalidEntry(op, "a deployment protection rule")
	}
	if rule.ID != a.ProtectionRuleID {
		return nil, foreignAnswer(op, "a deployment protection rule")
	}
	return &DeploymentProtectionRuleResult{DeploymentProtectionRule: rule, Environment: a.Environment}, nil
}

// deleteProtectionRule reads the rule in the same environment first and sends the DELETE only after that
// read answered the rule; the DELETE is sent once and never repeated.
func (c *Client) deleteProtectionRule(ctx context.Context, a *environmentsArguments) (*DeploymentProtectionRuleDeleted, error) {
	const op = "delete deployment protection rule"
	if _, err := c.getProtectionRule(ctx, a); err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.protectionRulePath(a.Environment, a.ProtectionRuleID),
		nil, nil); err != nil {
		return nil, environmentFailure(err, environmentsChangePermission, "", "")
	}
	return &DeploymentProtectionRuleDeleted{Deleted: true, Environment: a.Environment,
		ProtectionRuleID: a.ProtectionRuleID}, nil
}

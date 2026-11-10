package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/redact"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
)

// Deployment environments of a repository. github.environments.list and github.environments.get read them;
// github.environments.set creates one or replaces its protection settings and github.environments.delete
// removes one with its secrets and variables, both offered only where a connection's tools list names them,
// since an environment decides who must approve a deployment and which branches may deploy at all. An
// environment is addressed by its name, escaped as one opaque path segment, so a name can never open another
// route. The deployment branch policies of an environment live in deploymentbranchpolicies.go.

// environmentSensitivity classifies the guarded environment changes: they decide who approves a deployment
// and which branches may deploy, and a delete takes the environment's secrets and variables with it.
const environmentSensitivity = "github-environment"

// Bounds GitHub documents for an environment: a name of at most 255 characters, a wait timer of 0 to 43,200
// minutes, and at most six reviewers.
const (
	maxEnvironmentNameLength = 255
	maxEnvironmentWaitTimer  = 43200
	maxEnvironmentReviewers  = 6
)

const (
	environmentNameSchema      = `{"type":"string","minLength":1,"maxLength":255}`
	environmentWaitTimerSchema = `{"type":"integer","minimum":0,"maximum":43200}`
	environmentReviewersSchema = `{"type":"array","maxItems":6,"items":{"type":"object","properties":` +
		`{"type":{"type":"string","enum":["User","Team"]},"name":{"type":"string","minLength":1,` +
		`"maxLength":100,"pattern":"` + loginPattern + `","x-form":"user login or team slug"}},` +
		`"required":["type","name"],"additionalProperties":false}}`
	environmentBranchPolicySchema = `{"type":"string","enum":["all","protected","custom"]}`
)

var environmentArgument = capability.Argument{Name: "environment", Description: "Name of the environment, " +
	"matched by GitHub without regard to case", Required: true}

var environmentLinkKeys = []string{"url"}

var environmentReviewerSchema = objectSchema(`"type":{"type":"string"},"user":` + restAccountSchema +
	`,"team":` + restPullTeamSchema)

var environmentRuleSchema = objectSchema(`"id":{"type":"integer"},"node_id":{"type":"string"},` +
	`"type":{"type":"string"},"wait_timer":{"type":"integer"},"prevent_self_review":{"type":"boolean"},` +
	`"reviewers":{"type":"array","items":` + environmentReviewerSchema + `}`)

var environmentProperties = `"name":{"type":"string"},"id":{"type":"integer"},"node_id":{"type":"string"},` +
	`"url":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"},` +
	`"protection_rules":{"type":"array","items":` + environmentRuleSchema + `},` +
	`"deployment_branch_policy":{"type":"object","properties":{"protected_branches":{"type":"boolean"},` +
	`"custom_branch_policies":{"type":"boolean"}},"additionalProperties":false},` +
	`"links":` + linksSchema(environmentLinkKeys)

var environmentOutput = `{"type":"object","properties":{` + environmentProperties +
	`},"required":["name"],"additionalProperties":false}`

var environmentListOutput = `{"type":"object","properties":{"environments":{"type":"array","items":` +
	`{"type":"object","properties":{` + environmentProperties + `},"required":["name"],` +
	`"additionalProperties":false}},"total_count":{"type":"integer"},"next_cursor":{"type":"string"},` +
	`"has_more":{"type":"boolean"}},"required":["environments","has_more"],"additionalProperties":false}`

const environmentDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},` +
	`"environment":{"type":"string"}},"required":["deleted","environment"],"additionalProperties":false}`

var environmentFields = []capability.Field{
	{Name: "name", Description: "Name of the environment, untrusted data"},
	{Name: "id", Description: "Numeric environment id"},
	{Name: "node_id", Description: "GraphQL node id of the environment"},
	{Name: "url", Description: "Web address of the environment, untrusted data, never fetched"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
	{Name: "protection_rules", Description: "Protection rules: id, node_id, type (wait_timer, " +
		"required_reviewers, branch_policy, or another type GitHub names), wait_timer in minutes, " +
		"prevent_self_review, and reviewers, each with type (User or Team) and the account as user or the team " +
		"as team; GitHub names both reviewer under one member, split here by type; untrusted data"},
	{Name: "deployment_branch_policy", Description: "protected_branches and custom_branch_policies; absent " +
		"when any branch may deploy"},
	{Name: "links", Description: "REST url of the environment by its GitHub name, untrusted data, never fetched"},
}

var environmentsList = capability.Descriptor{
	ID:      Provider + ".environments.list",
	Version: 1,
	Title:   "List GitHub environments",
	Description: "List one bounded batch of the deployment environments of a repository a connection allows, " +
		"with their protection rules and deployment branch policy setting; environments of a private or " +
		"internal repository need GitHub Pro, Team, or Enterprise",
	Tags:         []string{"github", "actions", "environments", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(pagingKeys),
	OutputSchema: json.RawMessage(environmentListOutput),
	Arguments:    pagingArguments,
	Fields: append([]capability.Field{
		{Name: "environments", Description: "Environments with the members of github.environments.get; untrusted data"},
		{Name: "total_count", Description: "Number of environments GitHub reports for the repository"},
	}, pagingFields...),
	Examples: []capability.Example{{Description: "List the environments", Arguments: json.RawMessage(`{}`)}},
}

var environmentsGet = capability.Descriptor{
	ID:      Provider + ".environments.get",
	Version: 1,
	Title:   "Get a GitHub environment",
	Description: "Read one deployment environment of a repository a connection allows, by name, with its " +
		"protection rules, reviewers, and deployment branch policy setting",
	Tags:         []string{"github", "actions", "environments", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"environment":`+environmentNameSchema, "environment"),
	OutputSchema: json.RawMessage(environmentOutput),
	Arguments:    []capability.Argument{environmentArgument},
	Fields:       environmentFields,
	Examples: []capability.Example{{Description: "Read one environment",
		Arguments: json.RawMessage(`{"environment":"production"}`)}},
}

var environmentsSet = capability.Descriptor{
	ID:      Provider + ".environments.set",
	Version: 1,
	Title:   "Create or update a GitHub environment",
	Description: "Create one deployment environment of a repository a connection allows, or replace the " +
		"protection settings of the existing one of that name; wait_timer, prevent_self_review, reviewers, " +
		"and deployment_branch_policy are all required and sent together, since GitHub does not document that " +
		"a value left out stays unchanged; reviewers name users by login and teams by slug, at most six, and " +
		"are resolved to GitHub's identifiers first, so an unknown one is refused without a change; setting " +
		"or clearing reviewers changes who must approve a deployment; idempotent; offered only where a " +
		"connection's tools list names it",
	Tags:                  []string{"github", "actions", "environments", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, environmentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"environment":`+environmentNameSchema+`,"wait_timer":`+environmentWaitTimerSchema+
		`,"prevent_self_review":{"type":"boolean"},"reviewers":`+environmentReviewersSchema+
		`,"deployment_branch_policy":`+environmentBranchPolicySchema,
		"environment", "wait_timer", "prevent_self_review", "reviewers", "deployment_branch_policy"),
	OutputSchema: json.RawMessage(environmentOutput),
	Arguments: []capability.Argument{
		environmentArgument,
		{Name: "wait_timer", Description: "Minutes a job waits before it runs, 0 to 43200; 0 for none", Required: true},
		{Name: "prevent_self_review", Description: "true keeps the person who started a job from approving it",
			Required: true},
		{Name: "reviewers", Description: "Who may approve a job, at most six, each {type, name}: type User with " +
			"a login, or Team with the slug of a team of the organization that owns the repository; [] for none", Required: true},
		{Name: "deployment_branch_policy", Description: "all (any branch may deploy), protected (only " +
			"protected branches), or custom (only branches and tags matching the deployment branch policies)",
			Required: true},
	},
	Fields: environmentFields,
	Examples: []capability.Example{{
		Description: "Require one approval from a team before deploying protected branches",
		Arguments: json.RawMessage(`{"environment":"production","wait_timer":0,"prevent_self_review":true,` +
			`"reviewers":[{"type":"Team","name":"release-managers"}],"deployment_branch_policy":"protected"}`),
	}},
}

var environmentsDelete = capability.Descriptor{
	ID:      Provider + ".environments.delete",
	Version: 1,
	Title:   "Delete a GitHub environment",
	Description: "Delete one deployment environment of a repository a connection allows permanently, together " +
		"with its secrets, variables, protection rules, and deployment branch policies; offered only where a " +
		"connection's tools list names it",
	Tags:                  []string{"github", "actions", "environments", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, environmentSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"environment":`+environmentNameSchema, "environment"),
	OutputSchema:          json.RawMessage(environmentDeletedOutput),
	Arguments:             []capability.Argument{environmentArgument},
	Fields: []capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the environment"},
		{Name: "environment", Description: "Name of the deleted environment, as the call gave it"},
	},
	Examples: []capability.Example{{Description: "Delete an environment",
		Arguments: json.RawMessage(`{"environment":"staging"}`)}},
}

// environmentsOperations binds every environment and deployment branch policy tool to its handler.
func environmentsOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: environmentsList, Handler: environmentsHandler(environmentsList.ID, checkEnvironmentsList,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.listEnvironments(ctx, a)
			})},
		{Descriptor: environmentsGet, Handler: environmentsHandler(environmentsGet.ID, checkEnvironmentName,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.getEnvironment(ctx, a.Environment)
			})},
		{Descriptor: environmentsSet, Handler: environmentsHandler(environmentsSet.ID, checkEnvironmentSet,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.setEnvironment(ctx, a)
			})},
		{Descriptor: environmentsDelete, Handler: environmentsHandler(environmentsDelete.ID, checkEnvironmentName,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.deleteEnvironment(ctx, a.Environment)
			})},
		{Descriptor: branchPoliciesList, Handler: environmentsHandler(branchPoliciesList.ID, checkBranchPoliciesList,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.listBranchPolicies(ctx, a)
			})},
		{Descriptor: branchPoliciesCreate, Handler: environmentsHandler(branchPoliciesCreate.ID, checkBranchPolicyName,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.createBranchPolicy(ctx, a)
			})},
		{Descriptor: branchPoliciesUpdate, Handler: environmentsHandler(branchPoliciesUpdate.ID, checkBranchPolicyUpdate,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.updateBranchPolicy(ctx, a)
			})},
		{Descriptor: branchPoliciesDelete, Handler: environmentsHandler(branchPoliciesDelete.ID, checkBranchPolicyID,
			func(ctx context.Context, c *Client, a *environmentsArguments) (any, error) {
				return c.deleteBranchPolicy(ctx, a)
			})},
	}
}

// environmentReviewer is one reviewer github.environments.set takes: a user login or a team slug, never a
// numeric identifier, which the tool resolves itself.
type environmentReviewer struct {
	Type string `json:"type"`
	Name string `json:"name"`
}

// environmentsArguments holds the arguments of every environment and deployment branch policy tool; the
// input schema of each tool admits only its own. page, perPage, and binding are derived by the checks.
type environmentsArguments struct {
	Environment            string                `json:"environment"`
	WaitTimer              *int64                `json:"wait_timer"`
	PreventSelfReview      *bool                 `json:"prevent_self_review"`
	Reviewers              []environmentReviewer `json:"reviewers"`
	DeploymentBranchPolicy string                `json:"deployment_branch_policy"`
	ID                     int64                 `json:"id"`
	Name                   string                `json:"name"`
	Type                   string                `json:"type"`
	Limit                  int                   `json:"limit"`
	Cursor                 string                `json:"cursor"`

	page, perPage int
	binding       []byte
}

// environmentsHandler checks the repository against the targets and exclusions and every argument before a
// credential is resolved, so a refused request never becomes a provider call.
func environmentsHandler(id string, check func(*environmentsArguments, target, allowlist) error,
	call func(context.Context, *Client, *environmentsArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments environmentsArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		allowed, err := allowlistOf(resolved)
		if err != nil {
			return nil, providerError("open", err.Error())
		}
		if err := check(&arguments, bound, allowed); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func checkEnvironmentsList(a *environmentsArguments, bound target, _ allowlist) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("environments", "list", bound.String())
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkEnvironmentName(a *environmentsArguments, _ target, _ allowlist) error {
	if strings.TrimSpace(a.Environment) == "" || len([]rune(a.Environment)) > maxEnvironmentNameLength {
		return invalidRequest("environment must name an environment of 1 to 255 characters")
	}
	return checkText("environment", a.Environment)
}

// checkEnvironmentSet applies the bounds of github.environments.set that do not need GitHub: every value is
// present, at most six reviewers of a known type without repetition, and no reviewer an owner exclusion
// takes out of the connection's reach.
func checkEnvironmentSet(a *environmentsArguments, bound target, allowed allowlist) error {
	if err := checkEnvironmentName(a, bound, allowed); err != nil {
		return err
	}
	switch {
	case a.WaitTimer == nil || a.PreventSelfReview == nil || a.Reviewers == nil || a.DeploymentBranchPolicy == "":
		return invalidRequest("give wait_timer, prevent_self_review, reviewers, and deployment_branch_policy " +
			"together, since GitHub does not document that a value left out stays unchanged")
	case *a.WaitTimer < 0 || *a.WaitTimer > maxEnvironmentWaitTimer:
		return invalidRequest("wait_timer must be 0 to 43200 minutes")
	case len(a.Reviewers) > maxEnvironmentReviewers:
		return invalidRequest("reviewers accepts at most 6 users or teams")
	}
	if _, ok := environmentBranchPolicyBody(a.DeploymentBranchPolicy); !ok {
		return invalidRequest("deployment_branch_policy must be all, protected, or custom")
	}
	seen := map[string]bool{}
	for _, reviewer := range a.Reviewers {
		if reviewer.Type != "User" && reviewer.Type != "Team" {
			return invalidRequest("a reviewer's type must be User or Team")
		}
		if !validLogin(reviewer.Name) {
			return invalidRequest("a reviewer's name must be a user login or a team slug")
		}
		key := reviewer.Type + "/" + strings.ToLower(reviewer.Name)
		if seen[key] {
			return invalidRequest("reviewers must not name " + reviewer.Name + " twice")
		}
		seen[key] = true
		if reviewer.Type == "User" && allowed.excludedOwner(reviewer.Name) {
			return invalidRequest("reviewer " + reviewer.Name + " lies outside what this connection may reach")
		}
	}
	return nil
}

// environmentBranchPolicyBody turns the deployment_branch_policy argument into GitHub's member: null lets any
// branch deploy, and the two flags are always opposite, as GitHub requires.
func environmentBranchPolicyBody(value string) (any, bool) {
	switch value {
	case "all":
		return nil, true
	case "protected":
		return map[string]bool{"protected_branches": true, "custom_branch_policies": false}, true
	case "custom":
		return map[string]bool{"protected_branches": false, "custom_branch_policies": true}, true
	}
	return nil, false
}

// environmentPlanNote names the plan GitHub requires for environments, since GitHub answers a repository
// whose plan lacks them like a missing or refused resource, without a status of its own.
const environmentPlanNote = "; environments of a private or internal repository need GitHub Pro, Team, or " +
	"Enterprise, which GitHub's answer does not tell apart"

// Permission messages of the environment tools. GitHub decides on every request; a message names what such a
// request needs without claiming what the configured token holds.
const (
	environmentsReadPermission = "GitHub refused this token the environments of this repository; reading " +
		"them needs repo on a classic token for a private repository, or Actions: read on a fine-grained token"
	environmentsChangePermission = "GitHub refused this change of an environment; it needs repo on a classic " +
		"token, or Administration: read and write on a fine-grained token"
	environmentsRejected = "GitHub rejected the environment settings; the name may be invalid, a reviewer may " +
		"lack read access to the repository, or the plan may not offer them: environments of a private or " +
		"internal repository need GitHub Pro, Team, or Enterprise, and on Free, Pro, or Team a wait timer and " +
		"reviewers only apply to a public repository; nothing was changed"
	environmentTeamPermission = "GitHub refused this token the teams of the organization that owns this " +
		"repository; resolving a team reviewer needs read:org on a classic token, or Members: read of the " +
		"organization on a fine-grained token"
)

// environmentFailure renames a refusal of an environment route: a permission refusal names what the request
// needs, a missing resource adds the plan GitHub requires, and a rejection, where given, says what GitHub
// may have rejected. Provider text stays in Original and never reaches the message.
func environmentFailure(err error, permission, missing, rejected string) error {
	var failure *provider.Error
	if !errors.As(err, &failure) {
		return err
	}
	refused := *failure
	switch {
	case failure.Class == provider.ClassPermission:
		refused.Message = permission + environmentPlanNote
	case failure.Class == provider.ClassNotFound:
		refused.Message = failure.Message + missing + environmentPlanNote
	case rejected != "" && failure.Class == provider.ClassProviderError && failure.Message == rejectedMessage:
		refused.Message = rejected
	default:
		return err
	}
	return &refused
}

// environmentPath is the REST route of the environments of the bound repository, of one environment, or of a
// path below it. The name is escaped as one opaque path segment, so a slash in it stays inside the name.
func (c *Client) environmentPath(name, rest string) string {
	if name == "" {
		return c.repoPath("environments")
	}
	path := c.repoPath("environments/" + url.PathEscape(name))
	if rest != "" {
		path += "/" + rest
	}
	return path
}

// environmentRaw is the REST environment; links holds its REST url.
type environmentRaw struct {
	ID                     *int64                   `json:"id"`
	NodeID                 *string                  `json:"node_id"`
	Name                   *string                  `json:"name"`
	HTMLURL                *string                  `json:"html_url"`
	CreatedAt              *string                  `json:"created_at"`
	UpdatedAt              *string                  `json:"updated_at"`
	ProtectionRules        []environmentRuleRaw     `json:"protection_rules"`
	DeploymentBranchPolicy *EnvironmentBranchPolicy `json:"deployment_branch_policy"`
	links                  Links
}

func (r *environmentRaw) UnmarshalJSON(data []byte) (err error) {
	type plain environmentRaw
	if err = json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.links, err = readLinks(data, environmentLinkKeys)
	return err
}

type environmentRuleRaw struct {
	ID                *int64                   `json:"id"`
	NodeID            *string                  `json:"node_id"`
	Type              *string                  `json:"type"`
	WaitTimer         *int64                   `json:"wait_timer"`
	PreventSelfReview *bool                    `json:"prevent_self_review"`
	Reviewers         []environmentReviewerRaw `json:"reviewers"`
}

// environmentReviewerRaw keeps the reviewer object until its type says whether it is an account or a team.
type environmentReviewerRaw struct {
	Type     *string         `json:"type"`
	Reviewer json.RawMessage `json:"reviewer"`
}

// Environment is the view of one environment.
type Environment struct {
	Name                   string                      `json:"name"`
	ID                     *int64                      `json:"id,omitempty"`
	NodeID                 *string                     `json:"node_id,omitempty"`
	URL                    *string                     `json:"url,omitempty"`
	CreatedAt              *string                     `json:"created_at,omitempty"`
	UpdatedAt              *string                     `json:"updated_at,omitempty"`
	ProtectionRules        []EnvironmentProtectionRule `json:"protection_rules,omitempty"`
	DeploymentBranchPolicy *EnvironmentBranchPolicy    `json:"deployment_branch_policy,omitempty"`
	Links                  Links                       `json:"links,omitempty"`
}

// EnvironmentProtectionRule is one protection rule of an environment; which members it carries depends on
// its type.
type EnvironmentProtectionRule struct {
	ID                *int64                `json:"id,omitempty"`
	NodeID            *string               `json:"node_id,omitempty"`
	Type              *string               `json:"type,omitempty"`
	WaitTimer         *int64                `json:"wait_timer,omitempty"`
	PreventSelfReview *bool                 `json:"prevent_self_review,omitempty"`
	Reviewers         []EnvironmentReviewer `json:"reviewers,omitempty"`
}

// EnvironmentReviewer is one reviewer of a required reviewers rule: an account as user or a team as team, by
// the type GitHub names.
type EnvironmentReviewer struct {
	Type *string       `json:"type,omitempty"`
	User *RESTAccount  `json:"user,omitempty"`
	Team *RESTPullTeam `json:"team,omitempty"`
}

// EnvironmentBranchPolicy is GitHub's setting of which branches may deploy to an environment.
type EnvironmentBranchPolicy struct {
	ProtectedBranches    bool `json:"protected_branches"`
	CustomBranchPolicies bool `json:"custom_branch_policies"`
}

// EnvironmentList is one batch of environments.
type EnvironmentList struct {
	Environments []Environment `json:"environments"`
	TotalCount   *int64        `json:"total_count,omitempty"`
	NextCursor   string        `json:"next_cursor,omitempty"`
	HasMore      bool          `json:"has_more"`
}

// EnvironmentDeleted is the answer to a deleted environment.
type EnvironmentDeleted struct {
	Deleted     bool   `json:"deleted"`
	Environment string `json:"environment"`
}

// project turns the decoded environment into its view, or reports false for one without a usable name. A
// name is never cut, since a cut name addresses another environment.
func (r *environmentRaw) project() (Environment, bool) {
	name, cut := cleanText(r.Name, maxEnvironmentNameLength, false)
	if name == nil || *name == "" || cut {
		return Environment{}, false
	}
	view := Environment{Name: *name, ID: r.ID, NodeID: nameText(r.NodeID), URL: address(r.HTMLURL),
		CreatedAt: nameText(r.CreatedAt), UpdatedAt: nameText(r.UpdatedAt),
		DeploymentBranchPolicy: r.DeploymentBranchPolicy, Links: r.links}
	for _, rule := range r.ProtectionRules {
		projected := EnvironmentProtectionRule{ID: rule.ID, NodeID: nameText(rule.NodeID), Type: nameText(rule.Type),
			WaitTimer: rule.WaitTimer, PreventSelfReview: rule.PreventSelfReview}
		for _, reviewer := range rule.Reviewers {
			projected.Reviewers = append(projected.Reviewers, reviewer.project())
		}
		view.ProtectionRules = append(view.ProtectionRules, projected)
	}
	return view, true
}

// project reads the reviewer object as the account or the team its type names; a reviewer of another type
// keeps only its type.
func (r environmentReviewerRaw) project() EnvironmentReviewer {
	reviewer := EnvironmentReviewer{Type: nameText(r.Type)}
	switch valueOr(r.Type) {
	case "User":
		var account accountRaw
		if json.Unmarshal(r.Reviewer, &account) == nil {
			reviewer.User = account.project()
		}
	case "Team":
		var team pullTeamRaw
		if json.Unmarshal(r.Reviewer, &team) == nil {
			reviewer.Team = team.project()
		}
	}
	return reviewer
}

func (a *environmentsArguments) query() url.Values {
	return url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
}

func (c *Client) listEnvironments(ctx context.Context, a *environmentsArguments) (*EnvironmentList, error) {
	const op = "list environments"
	var raw struct {
		TotalCount   *int64           `json:"total_count"`
		Environments []environmentRaw `json:"environments"`
	}
	hasNext, err := c.restPage(ctx, op, c.environmentPath("", ""), a.query(), &raw)
	if err != nil {
		return nil, environmentFailure(err, environmentsReadPermission, "", "")
	}
	result := &EnvironmentList{Environments: make([]Environment, 0, len(raw.Environments)), TotalCount: raw.TotalCount}
	for i := range raw.Environments {
		view, ok := raw.Environments[i].project()
		if !ok {
			return nil, invalidEntry(op, "an environment")
		}
		result.Environments = append(result.Environments, view)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

func (c *Client) getEnvironment(ctx context.Context, name string) (*Environment, error) {
	const op = "get environment"
	var raw environmentRaw
	if err := c.rest(ctx, op, c.environmentPath(name, ""), &raw); err != nil {
		return nil, environmentFailure(err, environmentsReadPermission, "", "")
	}
	view, ok := raw.project()
	if !ok {
		return nil, invalidEntry(op, "an environment")
	}
	return &view, nil
}

// setEnvironment resolves every reviewer to GitHub's identifier before it sends one PUT, never repeated, with
// all four settings: GitHub does not document that a member left out keeps its value.
func (c *Client) setEnvironment(ctx context.Context, a *environmentsArguments) (*Environment, error) {
	const op = "set environment"
	reviewers, err := c.resolveEnvironmentReviewers(ctx, a.Reviewers)
	if err != nil {
		return nil, err
	}
	policy, _ := environmentBranchPolicyBody(a.DeploymentBranchPolicy)
	body := map[string]any{"wait_timer": *a.WaitTimer, "prevent_self_review": *a.PreventSelfReview,
		"reviewers": reviewers, "deployment_branch_policy": policy}
	var raw environmentRaw
	if err := c.restChange(ctx, op, http.MethodPut, c.environmentPath(a.Environment, ""), body, &raw); err != nil {
		return nil, environmentFailure(err, environmentsChangePermission, "", environmentsRejected)
	}
	view, ok := raw.project()
	if !ok {
		return nil, invalidResponse(op, true)
	}
	return &view, nil
}

// resolveEnvironmentReviewers reads the identifier of every reviewer: a user through its public account, a
// team only below the organization that owns the bound repository, so no other organization is ever read.
// An unknown user or team ends the call before any change.
func (c *Client) resolveEnvironmentReviewers(ctx context.Context, reviewers []environmentReviewer) ([]map[string]any, error) {
	const op = "resolve environment reviewers"
	resolved := make([]map[string]any, 0, len(reviewers))
	for _, reviewer := range reviewers {
		var answer struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		}
		var failure *provider.Error
		if reviewer.Type == "Team" {
			err := c.rest(ctx, op, c.orgPath("teams/"+url.PathEscape(reviewer.Name)), &answer)
			if errors.As(err, &failure) && failure.Class == provider.ClassNotFound {
				return nil, invalidRequest("reviewer team " + reviewer.Name + " is not a team of the organization " +
					"that owns this repository, or this token cannot see it; a repository owned by a user has no " +
					"teams; nothing was changed")
			}
			if err != nil {
				return nil, actionsFailure(err, environmentTeamPermission)
			}
		} else {
			err := c.rest(ctx, op, "/users/"+url.PathEscape(reviewer.Name), &answer)
			if errors.As(err, &failure) && failure.Class == provider.ClassNotFound {
				return nil, invalidRequest("reviewer user " + reviewer.Name + " does not exist on GitHub or this " +
					"token cannot see it; nothing was changed")
			}
			if err != nil {
				return nil, actionsFailure(err, "GitHub refused this token the account of reviewer "+reviewer.Name)
			}
			if answer.ID > 0 && answer.Type != "User" {
				return nil, invalidRequest("reviewer " + reviewer.Name + " is not a user account; nothing was changed")
			}
		}
		if answer.ID <= 0 {
			return nil, invalidEntry(op, "a reviewer")
		}
		resolved = append(resolved, map[string]any{"type": reviewer.Type, "id": answer.ID})
	}
	return resolved, nil
}

func (c *Client) deleteEnvironment(ctx context.Context, name string) (*EnvironmentDeleted, error) {
	const op = "delete environment"
	if err := c.restChange(ctx, op, http.MethodDelete, c.environmentPath(name, ""), nil, nil); err != nil {
		return nil, environmentFailure(err, environmentsChangePermission, "", "")
	}
	return &EnvironmentDeleted{Deleted: true, Environment: name}, nil
}

// environmentsSubject names the environment, or the deployment branch policy of one, a repository path below
// environments addresses. It names only a name the input schema allows, unescaped, and is empty for any other
// path.
func environmentsSubject(path string) string {
	if path == "environments" {
		return "the environments of this repository"
	}
	rest, ok := strings.CutPrefix(path, "environments/")
	if !ok {
		return ""
	}
	segment, below, _ := strings.Cut(rest, "/")
	name, err := url.PathUnescape(segment)
	if err != nil || name == "" || len([]rune(name)) > maxEnvironmentNameLength || checkText("environment", name) != nil {
		return "an environment"
	}
	environment := "environment " + name
	policies, ok := strings.CutPrefix(below, "deployment-branch-policies")
	switch {
	case below == "":
		return environment
	case !ok:
		return "a resource of " + environment
	case policies == "":
		return "the deployment branch policies of " + environment
	}
	if id, err := strconv.ParseInt(strings.TrimPrefix(policies, "/"), 10, 64); err == nil && id > 0 {
		return "deployment branch policy " + strconv.FormatInt(id, 10) + " of " + environment
	}
	return "a deployment branch policy of " + environment
}

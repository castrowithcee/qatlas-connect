package github

import (
	"bytes"
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
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Branch protection of one branch and the rules that effectively apply to it. github.branch-protection.set
// replaces the whole protection of a branch, so every setting of the request is given explicitly: a setting
// left out is refused instead of defaulted, since a default could loosen the protection. set and delete are
// offered only where a connection's tools list names them. Each change is one request, never repeated.

// branchProtectionSensitivity classifies the protection of a branch as a governance setting of its repository.
const branchProtectionSensitivity = "github-branch-protection"

const (
	maxProtectionActors = 100
	maxProtectionChecks = 100
)

const (
	protectionActorListSchema = `{"type":"array","maxItems":100,"items":{"type":"string","minLength":1,` +
		`"maxLength":100,"pattern":"^[A-Za-z0-9._-]+$"}}`
	protectionActorsSchema = `{"type":"object","properties":{"users":` + protectionActorListSchema +
		`,"teams":` + protectionActorListSchema + `,"apps":` + protectionActorListSchema +
		`},"additionalProperties":false}`
	protectionRestrictionsSchema = `{"anyOf":[{"type":"object","properties":{"users":` +
		protectionActorListSchema + `,"teams":` + protectionActorListSchema + `,"apps":` +
		protectionActorListSchema + `},"required":["users","teams"],"additionalProperties":false},{"type":"null"}]}`
	protectionStatusChecksSchema = `{"anyOf":[{"type":"object","properties":{"strict":{"type":"boolean"},` +
		`"checks":{"type":"array","maxItems":100,"items":{"type":"object","properties":{"context":` +
		`{"type":"string","minLength":1,"maxLength":255},"app_id":{"type":"integer","minimum":-1}},` +
		`"required":["context"],"additionalProperties":false}}},"required":["strict","checks"],` +
		`"additionalProperties":false},{"type":"null"}]}`
	protectionReviewsSchema = `{"anyOf":[{"type":"object","properties":{"dismissal_restrictions":` +
		protectionActorsSchema + `,"dismiss_stale_reviews":{"type":"boolean"},` +
		`"require_code_owner_reviews":{"type":"boolean"},"required_approving_review_count":` +
		`{"type":"integer","minimum":0,"maximum":6},"require_last_push_approval":{"type":"boolean"},` +
		`"bypass_pull_request_allowances":` + protectionActorsSchema + `},"required":["dismiss_stale_reviews",` +
		`"require_code_owner_reviews","required_approving_review_count","require_last_push_approval"],` +
		`"additionalProperties":false},{"type":"null"}]}`
	nullableBoolSchema = `{"anyOf":[{"type":"boolean"},{"type":"null"}]}`
)

// branchProtectionRequired lists every setting a replacement names; none has a default.
var branchProtectionRequired = []string{"branch", "required_status_checks", "enforce_admins",
	"required_pull_request_reviews", "restrictions", "required_linear_history", "allow_force_pushes",
	"allow_deletions", "block_creations", "required_conversation_resolution", "lock_branch",
	"allow_fork_syncing"}

var branchProtectionOutput = json.RawMessage(`{"type":"object","properties":{"branch":{"type":"string"},` +
	`"state":{"type":"string","enum":["protected","unprotected"]},"protection":` + restBranchProtectionSchema +
	`},"required":["branch","state"],"additionalProperties":false}`)

var branchProtectionFields = []capability.Field{
	{Name: "branch", Description: "Branch the call addressed"},
	{Name: "state", Description: "protected, or unprotected where GitHub holds no protection for the branch"},
	{Name: "protection", Description: "Protection GitHub reports, absent while unprotected: required_status_checks " +
		"(strict, contexts, checks, enforcement_level), enforce_admins, required_pull_request_reviews " +
		"(dismissal_restrictions, bypass_pull_request_allowances, dismiss_stale_reviews, " +
		"require_code_owner_reviews, required_approving_review_count, require_last_push_approval), " +
		"required_signatures, restrictions (users, teams, apps), and the switches required_linear_history, " +
		"allow_force_pushes, allow_deletions, block_creations, required_conversation_resolution, lock_branch, " +
		"and allow_fork_syncing, each with its own links; users, teams, and apps at most 100 each; untrusted data"},
}

var branchProtectionGet = capability.Descriptor{
	ID:      Provider + ".branch-protection.get",
	Version: 1,
	Title:   "Get GitHub branch protection",
	Description: "Read the protection of one branch of a repository a connection allows; a branch without " +
		"protection answers the state unprotected, a missing branch not-found",
	Tags:         []string{"github", "branches", "protection", "governance", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"branch":`+refSchema, "branch"),
	OutputSchema: branchProtectionOutput,
	Arguments: []capability.Argument{
		{Name: "branch", Description: "Branch name without refs/heads/", Required: true},
	},
	Fields: branchProtectionFields,
	Examples: []capability.Example{{
		Description: "Read the protection of main",
		Arguments:   json.RawMessage(`{"branch":"main"}`),
	}},
}

var branchProtectionSet = capability.Descriptor{
	ID:      Provider + ".branch-protection.set",
	Version: 1,
	Title:   "Set GitHub branch protection",
	Description: "Replace the whole protection of one branch of a repository a connection allows with the given " +
		"settings; every setting must be given, because GitHub replaces the protection as a whole and a " +
		"setting left out would be reset; idempotent, since sending the same settings again leaves GitHub in " +
		"the same state; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "branches", "protection", "governance", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, branchProtectionSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"branch":`+refSchema+`,"required_status_checks":`+protectionStatusChecksSchema+
		`,"enforce_admins":`+nullableBoolSchema+`,"required_pull_request_reviews":`+protectionReviewsSchema+
		`,"restrictions":`+protectionRestrictionsSchema+`,"required_linear_history":{"type":"boolean"},`+
		`"allow_force_pushes":`+nullableBoolSchema+`,"allow_deletions":{"type":"boolean"},`+
		`"block_creations":{"type":"boolean"},"required_conversation_resolution":{"type":"boolean"},`+
		`"lock_branch":{"type":"boolean"},"allow_fork_syncing":{"type":"boolean"}`, branchProtectionRequired...),
	OutputSchema: branchProtectionOutput,
	Arguments: []capability.Argument{
		{Name: "branch", Description: "Branch name without refs/heads/", Required: true},
		{Name: "required_status_checks", Required: true, Description: "Object {strict, checks} with strict " +
			"(bool) and checks, a list of {context, app_id} (app_id optional, -1 for any app), or null for no " +
			"required status checks"},
		{Name: "enforce_admins", Required: true, Description: "true to apply the protection to administrators " +
			"too; false or null leaves them exempt"},
		{Name: "required_pull_request_reviews", Required: true, Description: "Object {dismiss_stale_reviews, " +
			"require_code_owner_reviews, required_approving_review_count (0 to 6), require_last_push_approval, " +
			"dismissal_restrictions, bypass_pull_request_allowances} where the last two are optional objects of " +
			"{users, teams, apps} lists of existing logins and slugs (organization repositories only), or null " +
			"for no required reviews"},
		{Name: "restrictions", Required: true, Description: "Object {users, teams, apps} of logins and slugs " +
			"of existing users, teams, and apps that may push (organization repositories only; apps optional), " +
			"or null for no push restriction"},
		{Name: "required_linear_history", Required: true, Description: "true to require a linear history"},
		{Name: "allow_force_pushes", Required: true, Description: "true to allow force pushes; false or null " +
			"blocks them"},
		{Name: "allow_deletions", Required: true, Description: "true to allow deleting the branch"},
		{Name: "block_creations", Required: true, Description: "true to block creating a matching branch"},
		{Name: "required_conversation_resolution", Required: true, Description: "true to require resolved " +
			"conversations before merging"},
		{Name: "lock_branch", Required: true, Description: "true to make the branch read-only"},
		{Name: "allow_fork_syncing", Required: true, Description: "true to let forks sync the branch while it " +
			"is locked"},
	},
	Fields: branchProtectionFields,
	Examples: []capability.Example{{
		Description: "Require one approving review and a passing check on main",
		Arguments: json.RawMessage(`{"branch":"main","required_status_checks":{"strict":true,` +
			`"checks":[{"context":"ci/test"}]},"enforce_admins":true,"required_pull_request_reviews":` +
			`{"dismiss_stale_reviews":true,"require_code_owner_reviews":false,"required_approving_review_count":1,` +
			`"require_last_push_approval":false},"restrictions":null,"required_linear_history":true,` +
			`"allow_force_pushes":false,"allow_deletions":false,"block_creations":false,` +
			`"required_conversation_resolution":true,"lock_branch":false,"allow_fork_syncing":false}`),
	}},
}

const branchProtectionDeletedOutput = `{"type":"object","properties":{"branch":{"type":"string"},` +
	`"deleted":{"type":"boolean"}},"required":["branch","deleted"],"additionalProperties":false}`

var branchProtectionDelete = capability.Descriptor{
	ID:      Provider + ".branch-protection.delete",
	Version: 1,
	Title:   "Delete GitHub branch protection",
	Description: "Remove the whole protection of one branch of a repository a connection allows; offered only " +
		"where a connection's tools list names it, because the removed settings are not kept",
	Tags:                  []string{"github", "branches", "protection", "governance", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, branchProtectionSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"branch":`+refSchema, "branch"),
	OutputSchema:          json.RawMessage(branchProtectionDeletedOutput),
	Arguments: []capability.Argument{
		{Name: "branch", Description: "Branch name without refs/heads/", Required: true},
	},
	Fields: []capability.Field{
		{Name: "branch", Description: "Branch whose protection was removed"},
		{Name: "deleted", Description: "True once GitHub removed the protection"},
	},
	Examples: []capability.Example{{
		Description: "Remove the protection of a branch",
		Arguments:   json.RawMessage(`{"branch":"release/1.0"}`),
	}},
}

const branchRulesOutput = `{"type":"object","properties":{"branch":{"type":"string"},"rules":{"type":"array",` +
	`"items":{"type":"object","properties":{"type":{"type":"string"},"parameters":{"type":"object"},` +
	`"parameters_omitted":{"type":"boolean"},"ruleset_source_type":{"type":"string"},` +
	`"ruleset_source":{"type":"string"},"ruleset_id":{"type":"integer"}},"required":["type"],` +
	`"additionalProperties":false}},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
	`"required":["branch","rules","has_more"],"additionalProperties":false}`

var branchRulesGet = capability.Descriptor{
	ID:      Provider + ".branch-rules.get",
	Version: 1,
	Title:   "Get GitHub branch rules",
	Description: "List one bounded batch of the rules that effectively apply to one branch of a repository a " +
		"connection allows, from every ruleset that covers it, repository and organization alike",
	Tags:         []string{"github", "branches", "rules", "governance", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"branch":`+refSchema+`,`+pagingKeys, "branch"),
	OutputSchema: json.RawMessage(branchRulesOutput),
	Arguments: append([]capability.Argument{
		{Name: "branch", Description: "Branch name without refs/heads/", Required: true},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "branch", Description: "Branch the call addressed"},
		{Name: "rules", Description: "Rules that apply: type, parameters (GitHub's own shape of the rule type, " +
			"left out and marked in parameters_omitted when beyond the bounds), ruleset_source_type " +
			"(Repository or Organization), ruleset_source (where the ruleset is configured), and ruleset_id; " +
			"untrusted data"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "Read the rules that apply to main",
		Arguments:   json.RawMessage(`{"branch":"main"}`),
	}},
}

func branchProtectionOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: branchProtectionGet, Handler: capability.Handler(invokeBranchProtectionGet)},
		{Descriptor: branchProtectionSet, Handler: capability.Handler(invokeBranchProtectionSet)},
		{Descriptor: branchProtectionDelete, Handler: capability.Handler(invokeBranchProtectionDelete)},
		{Descriptor: branchRulesGet, Handler: capability.Handler(invokeBranchRulesGet)},
	}
}

const (
	branchProtectionReadPermission = "GitHub refused this token the branch protection of this repository; " +
		"reading it needs repo on a classic token with the Admin role on the repository, or Administration: " +
		"read on a fine-grained token"
	branchProtectionChangePermission = "GitHub refused this change of branch protection; it needs repo on a " +
		"classic token with the Admin role on the repository, or Administration: read and write on a " +
		"fine-grained token, and a plan that offers branch protection for this repository"
	branchRulesPermission = "GitHub refused this token the branch rules of this repository; reading them " +
		"needs no scope for a public repository, or repo on a classic token, or Metadata: read on a " +
		"fine-grained token, for a private one"
)

// BranchProtection is the protection of one branch, read or just replaced.
type BranchProtection struct {
	Branch     string                `json:"branch"`
	State      string                `json:"state"`
	Protection *RESTBranchProtection `json:"protection,omitempty"`
}

// BranchProtectionDeleted is the answer to a removed branch protection.
type BranchProtectionDeleted struct {
	Branch  string `json:"branch"`
	Deleted bool   `json:"deleted"`
}

func checkProtectionBranch(branch string) error {
	switch {
	case strings.HasPrefix(branch, "refs/"):
		return invalidRequest("branch must be the branch name without refs/heads/")
	case !validRef(branch):
		return invalidRequest("branch must be a usable branch name")
	}
	return nil
}

func (c *Client) protectionPath(branch string) string {
	return c.repoPath("branches/" + url.PathEscape(branch) + "/protection")
}

func invokeBranchProtectionGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var arguments struct {
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, unreadable(branchProtectionGet.ID)
	}
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	if err := checkProtectionBranch(arguments.Branch); err != nil {
		return nil, err
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.getBranchProtection(ctx, arguments.Branch))
}

// isBranchUnprotected tells GitHub's 404 of a branch without protection from the 404 of a missing branch or
// repository by reading its text; the text never reaches a message.
func isBranchUnprotected(err error) bool {
	var failure *provider.Error
	if !errors.As(err, &failure) || failure.Class != provider.ClassNotFound || failure.Original == nil {
		return false
	}
	for _, entry := range failure.Original.Entries {
		if strings.Contains(strings.ToLower(entry.Message), "branch not protected") {
			return true
		}
	}
	return false
}

// protectionView turns the answer of GitHub into a protected branch, or refuses an answer that holds no
// protection at all.
func protectionView(branch string, raw *branchProtectionRaw, op string, change bool) (*BranchProtection, error) {
	if raw == nil {
		return nil, invalidResponse(op, change)
	}
	projected := raw.project()
	if encoded, err := json.Marshal(projected); err != nil || string(encoded) == "{}" {
		return nil, invalidResponse(op, change)
	}
	return &BranchProtection{Branch: branch, State: "protected", Protection: projected}, nil
}

func (c *Client) getBranchProtection(ctx context.Context, branch string) (*BranchProtection, error) {
	const op = "get branch protection"
	var raw *branchProtectionRaw
	if err := c.rest(ctx, op, c.protectionPath(branch), &raw); err != nil {
		if isBranchUnprotected(err) {
			return &BranchProtection{Branch: branch, State: "unprotected"}, nil
		}
		return nil, actionsFailure(err, branchProtectionReadPermission)
	}
	return protectionView(branch, raw, op, false)
}

// protectionActors are the logins and slugs of users, teams, and apps a protection setting names.
type protectionActors struct {
	Users *[]string `json:"users,omitempty"`
	Teams *[]string `json:"teams,omitempty"`
	Apps  *[]string `json:"apps,omitempty"`
}

type protectionRestrictions struct {
	Users *[]string `json:"users"`
	Teams *[]string `json:"teams"`
	Apps  *[]string `json:"apps,omitempty"`
}

type protectionStatusCheck struct {
	Context string `json:"context"`
	AppID   *int64 `json:"app_id,omitempty"`
}

type protectionStatusChecks struct {
	Strict *bool                    `json:"strict"`
	Checks *[]protectionStatusCheck `json:"checks"`
}

type protectionReviews struct {
	DismissalRestrictions        *protectionActors `json:"dismissal_restrictions,omitempty"`
	DismissStaleReviews          *bool             `json:"dismiss_stale_reviews"`
	RequireCodeOwnerReviews      *bool             `json:"require_code_owner_reviews"`
	RequiredApprovingReviewCount *int              `json:"required_approving_review_count"`
	RequireLastPushApproval      *bool             `json:"require_last_push_approval"`
	BypassPullRequestAllowances  *protectionActors `json:"bypass_pull_request_allowances,omitempty"`
}

// branchProtectionArguments holds the arguments of github.branch-protection.set. The members that GitHub
// lets be null are raw, so a null that was given tells from a member that was left out.
type branchProtectionArguments struct {
	Branch                         string          `json:"branch"`
	RequiredStatusChecks           json.RawMessage `json:"required_status_checks"`
	EnforceAdmins                  json.RawMessage `json:"enforce_admins"`
	RequiredPullRequestReviews     json.RawMessage `json:"required_pull_request_reviews"`
	Restrictions                   json.RawMessage `json:"restrictions"`
	RequiredLinearHistory          *bool           `json:"required_linear_history"`
	AllowForcePushes               json.RawMessage `json:"allow_force_pushes"`
	AllowDeletions                 *bool           `json:"allow_deletions"`
	BlockCreations                 *bool           `json:"block_creations"`
	RequiredConversationResolution *bool           `json:"required_conversation_resolution"`
	LockBranch                     *bool           `json:"lock_branch"`
	AllowForkSyncing               *bool           `json:"allow_fork_syncing"`
}

func missingSetting(name string) error {
	return invalidRequest(name + " is required; give it explicitly, since the protection is replaced as a " +
		"whole and a setting left out would be reset")
}

// decodeStrict decodes one JSON value without accepting members the structure does not name.
func decodeStrict(raw json.RawMessage, into any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into) == nil && !decoder.More()
}

// nullableSetting reports whether a member that may be null holds a value; a member left out is refused.
func nullableSetting(name string, raw json.RawMessage) (bool, error) {
	trimmed := bytes.TrimSpace(raw)
	switch {
	case len(trimmed) == 0:
		return false, missingSetting(name)
	case string(trimmed) == "null":
		return false, nil
	}
	return true, nil
}

func nullableBoolSetting(name string, raw json.RawMessage) (any, error) {
	given, err := nullableSetting(name, raw)
	if err != nil || !given {
		return nil, err
	}
	var value bool
	if !decodeStrict(raw, &value) {
		return nil, invalidRequest(name + " must be true, false, or null")
	}
	return value, nil
}

func checkActorList(name string, list *[]string) error {
	if list == nil {
		return nil
	}
	if len(*list) > maxProtectionActors {
		return invalidRequest(name + " accepts at most 100 entries")
	}
	for _, entry := range *list {
		if !validRepoName(entry) {
			return invalidRequest(name + " names users, teams, and apps by login or slug")
		}
	}
	return nil
}

func checkActors(name string, actors *protectionActors) error {
	if actors == nil {
		return nil
	}
	for suffix, list := range map[string]*[]string{"users": actors.Users, "teams": actors.Teams, "apps": actors.Apps} {
		if err := checkActorList(name+"."+suffix, list); err != nil {
			return err
		}
	}
	return nil
}

// body checks every setting before any credential is resolved and returns the request body: exactly the
// settings given, never a default.
func (a *branchProtectionArguments) body() (map[string]any, error) {
	if err := checkProtectionBranch(a.Branch); err != nil {
		return nil, err
	}
	body := map[string]any{}
	given, err := nullableSetting("required_status_checks", a.RequiredStatusChecks)
	if err != nil {
		return nil, err
	}
	body["required_status_checks"] = nil
	if given {
		var checks protectionStatusChecks
		if !decodeStrict(a.RequiredStatusChecks, &checks) || checks.Strict == nil || checks.Checks == nil {
			return nil, invalidRequest("required_status_checks needs strict and checks, or null")
		}
		if len(*checks.Checks) > maxProtectionChecks {
			return nil, invalidRequest("required_status_checks.checks accepts at most 100 entries")
		}
		for _, check := range *checks.Checks {
			if check.Context == "" || len(check.Context) > 255 || checkText("required_status_checks.checks", check.Context) != nil ||
				check.AppID != nil && *check.AppID < -1 {
				return nil, invalidRequest("each check needs a context of plain text and an app_id of -1 or more")
			}
		}
		body["required_status_checks"] = checks
	}
	if body["enforce_admins"], err = nullableBoolSetting("enforce_admins", a.EnforceAdmins); err != nil {
		return nil, err
	}
	given, err = nullableSetting("required_pull_request_reviews", a.RequiredPullRequestReviews)
	if err != nil {
		return nil, err
	}
	body["required_pull_request_reviews"] = nil
	if given {
		var reviews protectionReviews
		if !decodeStrict(a.RequiredPullRequestReviews, &reviews) || reviews.DismissStaleReviews == nil ||
			reviews.RequireCodeOwnerReviews == nil || reviews.RequiredApprovingReviewCount == nil ||
			reviews.RequireLastPushApproval == nil {
			return nil, invalidRequest("required_pull_request_reviews needs dismiss_stale_reviews, " +
				"require_code_owner_reviews, required_approving_review_count, and require_last_push_approval, or null")
		}
		if count := *reviews.RequiredApprovingReviewCount; count < 0 || count > 6 {
			return nil, invalidRequest("required_approving_review_count must be between 0 and 6")
		}
		if err := checkActors("dismissal_restrictions", reviews.DismissalRestrictions); err != nil {
			return nil, err
		}
		if err := checkActors("bypass_pull_request_allowances", reviews.BypassPullRequestAllowances); err != nil {
			return nil, err
		}
		body["required_pull_request_reviews"] = reviews
	}
	given, err = nullableSetting("restrictions", a.Restrictions)
	if err != nil {
		return nil, err
	}
	body["restrictions"] = nil
	if given {
		var restrictions protectionRestrictions
		if !decodeStrict(a.Restrictions, &restrictions) || restrictions.Users == nil || restrictions.Teams == nil {
			return nil, invalidRequest("restrictions needs users and teams, or null")
		}
		for name, list := range map[string]*[]string{"restrictions.users": restrictions.Users,
			"restrictions.teams": restrictions.Teams, "restrictions.apps": restrictions.Apps} {
			if err := checkActorList(name, list); err != nil {
				return nil, err
			}
		}
		body["restrictions"] = restrictions
	}
	if body["allow_force_pushes"], err = nullableBoolSetting("allow_force_pushes", a.AllowForcePushes); err != nil {
		return nil, err
	}
	for _, setting := range []struct {
		name  string
		value *bool
	}{{"required_linear_history", a.RequiredLinearHistory}, {"allow_deletions", a.AllowDeletions},
		{"block_creations", a.BlockCreations}, {"required_conversation_resolution", a.RequiredConversationResolution},
		{"lock_branch", a.LockBranch}, {"allow_fork_syncing", a.AllowForkSyncing}} {
		if setting.value == nil {
			return nil, missingSetting(setting.name)
		}
		body[setting.name] = *setting.value
	}
	return body, nil
}

func invokeBranchProtectionSet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var arguments branchProtectionArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, unreadable(branchProtectionSet.ID)
	}
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	body, err := arguments.body()
	if err != nil {
		return nil, err
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.setBranchProtection(ctx, arguments.Branch, body))
}

// setBranchProtection sends the whole protection in one PUT, never repeated, and reports what GitHub says
// the branch now holds.
func (c *Client) setBranchProtection(ctx context.Context, branch string, body map[string]any) (*BranchProtection, error) {
	const op = "set branch protection"
	var raw *branchProtectionRaw
	if err := c.restChange(ctx, op, http.MethodPut, c.protectionPath(branch), body, &raw); err != nil {
		return nil, actionsFailure(err, branchProtectionChangePermission)
	}
	return protectionView(branch, raw, op, true)
}

func invokeBranchProtectionDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var arguments struct {
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, unreadable(branchProtectionDelete.ID)
	}
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	if err := checkProtectionBranch(arguments.Branch); err != nil {
		return nil, err
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.deleteBranchProtection(ctx, arguments.Branch))
}

func (c *Client) deleteBranchProtection(ctx context.Context, branch string) (*BranchProtectionDeleted, error) {
	const op = "delete branch protection"
	if err := c.restChange(ctx, op, http.MethodDelete, c.protectionPath(branch), struct{}{}, nil); err != nil {
		return nil, actionsFailure(err, branchProtectionChangePermission)
	}
	return &BranchProtectionDeleted{Branch: branch, Deleted: true}, nil
}

// BranchRule is one rule that applies to a branch, with the ruleset it comes from.
type BranchRule struct {
	Type              string  `json:"type"`
	Parameters        any     `json:"parameters,omitempty"`
	ParametersOmitted bool    `json:"parameters_omitted,omitempty"`
	RulesetSourceType *string `json:"ruleset_source_type,omitempty"`
	RulesetSource     *string `json:"ruleset_source,omitempty"`
	RulesetID         *int64  `json:"ruleset_id,omitempty"`
}

// BranchRules is one batch of the rules that apply to a branch.
type BranchRules struct {
	Branch     string       `json:"branch"`
	Rules      []BranchRule `json:"rules"`
	NextCursor string       `json:"next_cursor,omitempty"`
	HasMore    bool         `json:"has_more"`
}

type branchRulesOptions struct {
	Branch string `json:"branch"`
	Limit  int    `json:"limit"`
	Cursor string `json:"cursor"`

	page, perPage int
	binding       []byte
}

func invokeBranchRulesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	var options branchRulesOptions
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, unreadable(branchRulesGet.ID)
	}
	bound, err := selectTarget(resolved, kindRepository, raw)
	if err != nil {
		return nil, err
	}
	if err := checkProtectionBranch(options.Branch); err != nil {
		return nil, err
	}
	limit, err := normalizeLimit(options.Limit)
	if err != nil {
		return nil, err
	}
	options.binding = fingerprint("branch-rules", "get", bound.String(), options.Branch)
	if options.page, options.perPage, err = pageOf(options.binding, options.Cursor, limit); err != nil {
		return nil, err
	}
	client, err := openAt(ctx, resolved, secrets, red, bound)
	if err != nil {
		return nil, err
	}
	return bound.locate(client.getBranchRules(ctx, &options))
}

func (c *Client) getBranchRules(ctx context.Context, o *branchRulesOptions) (*BranchRules, error) {
	const op = "get branch rules"
	var raw []struct {
		Type              string          `json:"type"`
		Parameters        json.RawMessage `json:"parameters"`
		RulesetSourceType *string         `json:"ruleset_source_type"`
		RulesetSource     *string         `json:"ruleset_source"`
		RulesetID         *int64          `json:"ruleset_id"`
	}
	query := url.Values{"per_page": {strconv.Itoa(o.perPage)}, "page": {strconv.Itoa(o.page)}}
	hasNext, err := c.restPage(ctx, op, c.repoPath("rules/branches/"+url.PathEscape(o.Branch)), query, &raw)
	if err != nil {
		return nil, actionsFailure(err, branchRulesPermission)
	}
	result := &BranchRules{Branch: o.Branch, Rules: make([]BranchRule, 0, len(raw))}
	for _, entry := range raw {
		if entry.Type == "" {
			return nil, invalidEntry(op, "a rule")
		}
		rule := BranchRule{Type: *nameText(&entry.Type), RulesetSourceType: shortText(entry.RulesetSourceType),
			RulesetSource: nameText(entry.RulesetSource), RulesetID: entry.RulesetID}
		var ok bool
		if rule.Parameters, rule.ParametersOmitted, ok = boundedRulesetMember[map[string]any](entry.Parameters); !ok {
			return nil, invalidEntry(op, "a rule")
		}
		result.Rules = append(result.Rules, rule)
	}
	result.HasMore, result.NextCursor = morePage(o.binding, o.page, o.perPage, hasNext)
	return result, nil
}

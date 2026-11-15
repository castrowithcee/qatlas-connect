package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The finer Actions settings of a repository or an organization: which actions and reusable workflows may
// run, which fork pull requests need approval, how long artifacts and logs are kept, which workflows outside
// a private repository may use its actions, and the subject claim of the OIDC token. Each setting has one
// tool that reads it and one that changes it, and each is offered only where a connection's tools list names
// it. Four of the settings exist at both levels and take the exclusive repository or organization argument
// the ruleset tools use; the access setting exists for a repository only and takes the usual repository
// argument. A change reads the setting first, because a request that fails there changes nothing, sends the
// change once without a retry, and reads the setting again, so the answer names the values the call gave as
// GitHub reports them before and after.

// Bounds and values of the finer Actions settings. The claim keys are those GitHub documents for the
// customization of the OIDC subject claim; the retention bound is the longest period GitHub allows for a
// private repository, and GitHub rejects what its plan or policy does not allow below that.
var (
	forkApprovalPolicies = []string{"first_time_contributors_new_to_github", "first_time_contributors",
		"all_external_contributors"}
	accessLevels  = []string{"none", "user", "organization"}
	oidcClaimKeys = []string{"actor", "actor_id", "base_ref", "check_run_id", "context", "environment", "event_name", "head_ref",
		"job_workflow_ref", "job_workflow_sha", "ref", "ref_type", "repo", "repository", "repository_id",
		"repository_owner", "repository_owner_id", "repository_visibility", "run_attempt", "run_id", "run_number",
		"runner_environment", "sha", "workflow", "workflow_ref", "workflow_sha"}
)

const (
	maxSelectedPatterns   = 1000
	maxPatternLength      = 255
	maxRetentionDays      = 400
	actionPatternAlphabet = "_./@*!+-"
)

const (
	actionPatternSchema = `{"type":"string","minLength":1,"maxLength":255,"pattern":"^[A-Za-z0-9_./@*!+-]{1,255}$",` +
		`"x-form":"such as octo-org/*, octo-org/action@v1, or !octo-org/denied"}`
	actionPatternsSchema = `{"type":"array","maxItems":1000,"uniqueItems":true,"items":` + actionPatternSchema + `}`
	retentionDaysSchema  = `{"type":"integer","minimum":1,"maximum":400}`
)

// A repository property claim is named repo_property_ and the property's name, as it stands in the token.
const (
	repoPropertyClaimPattern = `^repo_property_[A-Za-z0-9_]+$`
	maxClaimKeys             = 50
	maxClaimKeyLength        = 120
)

func claimKeysSchema() string {
	return `{"type":"array","minItems":1,"maxItems":50,"uniqueItems":true,"items":{"anyOf":[` + enumSchema(oidcClaimKeys) +
		`,{"type":"string","maxLength":120,"pattern":"` + repoPropertyClaimPattern + `"}]}}`
}

func validClaimKey(key string) bool {
	if contains(oidcClaimKeys, key) {
		return true
	}
	name, ok := strings.CutPrefix(key, "repo_property_")
	if !ok || name == "" || len(key) > maxClaimKeyLength {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

const settingTargetKeys = `"repository":` + repoSchema + `,"organization":` + ownerSchema

const settingTargetProperties = `"repository":{"type":"string"},"organization":{"type":"string"}`

var settingTargetArguments = []capability.Argument{
	{Name: "repository", Description: "Repository as OWNER/REPO whose setting this call addresses; exactly one of " +
		"repository or organization is required, never both; must lie inside the targets when the connection " +
		"lists any"},
	{Name: "organization", Description: "Organization as orgs/LOGIN whose setting this call addresses; exactly " +
		"one of repository or organization is required, never both; must be an owner target the connection " +
		"allows when it lists any"},
}

var settingTargetFields = []capability.Field{
	{Name: "repository", Description: "Repository this call addressed, as OWNER/REPO; present only for a repository"},
	{Name: "organization", Description: "Organization this call addressed, as orgs/LOGIN; present only for an " +
		"organization"},
}

// settingDefinition is what differs between the five settings; settingDescriptors builds the pair of tools.
type settingDefinition struct {
	id         string // tool segment such as actions-selected
	subject    string // what the setting is, for titles
	repoOnly   bool
	properties string // output members of one reading, as JSON schema properties
	required   string // required members of one reading
	// changeProperties and changeRequired describe what a change reports; they default to the reading's.
	changeProperties, changeRequired string
	fields                           []capability.Field
	setKeys                          string // input properties beyond the target
	setRequired                      []string
	setArgs                          []capability.Argument
	getText                          string
	setText                          string
	getExample                       capability.Example
	setExample                       capability.Example
}

func (d settingDefinition) level() string {
	if d.repoOnly {
		return "a repository"
	}
	return "a repository or, with organization instead, an organization"
}

func (d settingDefinition) descriptors() (capability.Descriptor, capability.Descriptor) {
	tags := func(verb string) []string {
		return []string{"github", "actions", "settings", d.id, verb, "admin"}
	}
	getInput, setInput := "", d.setKeys
	// A read GitHub answers with a conflict reports the setting as unavailable, so no member is required.
	getOutput := string(settingsOutput(d.properties+","+unavailableProperties, ""))
	changeProperties, changeRequired := d.properties, d.required
	if d.changeProperties != "" {
		changeProperties, changeRequired = d.changeProperties, d.changeRequired
	}
	changeOutput := settingChangeOutput(changeProperties, changeRequired)
	getArguments := []capability.Argument{}
	setArguments := d.setArgs
	getFields := append(append([]capability.Field{}, d.fields...), unavailableFields...)
	setFields := settingChangeFields
	if !d.repoOnly {
		getInput = settingTargetKeys
		setInput = settingTargetKeys + "," + d.setKeys
		getOutput = withProperties(getOutput, settingTargetProperties)
		changeOutput = withProperties(changeOutput, settingTargetProperties)
		getArguments = append(getArguments, settingTargetArguments...)
		setArguments = append(append([]capability.Argument{}, d.setArgs...), settingTargetArguments...)
		getFields = append(getFields, settingTargetFields...)
		setFields = append(append([]capability.Field{}, settingChangeFields...), settingTargetFields...)
	}
	get := capability.Descriptor{
		ID:                    Provider + "." + d.id + ".get",
		Version:               1,
		Title:                 "Get " + d.subject,
		Description:           "Read " + d.getText + " of " + d.level() + " a connection allows" + d.exclusive() + "; offered only where the connection lists it",
		Tags:                  tags("get"),
		Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, actionsSettingSensitivity),
		Provider:              Provider,
		RequiresToolAllowList: true,
		InputSchema:           inputSchema(getInput),
		OutputSchema:          json.RawMessage(getOutput),
		Arguments:             getArguments,
		Fields:                getFields,
		Examples:              []capability.Example{d.getExample},
	}
	set := capability.Descriptor{
		ID:                    Provider + "." + d.id + ".set",
		Version:               1,
		Title:                 "Change " + d.subject,
		Description:           d.setText + " of " + d.level() + " a connection allows" + d.exclusive() + "; a value left out stays as it is; idempotent, since sending the same values again leaves GitHub in the same state; offered only where the connection lists it",
		Tags:                  tags("set"),
		Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, actionsSettingSensitivity),
		Provider:              Provider,
		RequiresToolAllowList: true,
		InputSchema:           inputSchema(setInput, d.setRequired...),
		OutputSchema:          json.RawMessage(changeOutput),
		Arguments:             setArguments,
		Fields:                setFields,
		Examples:              []capability.Example{d.setExample},
	}
	return get, set
}

func (d settingDefinition) exclusive() string {
	if d.repoOnly {
		return ""
	}
	return "; exactly one of repository or organization is required"
}

// withProperties adds schema properties to the top level of an object schema, so a result can name the
// repository or the organization it addressed.
func withProperties(schema, properties string) string {
	var object map[string]json.RawMessage
	var members map[string]json.RawMessage
	var extra map[string]json.RawMessage
	if json.Unmarshal([]byte(schema), &object) != nil || json.Unmarshal(object["properties"], &members) != nil ||
		json.Unmarshal([]byte("{"+properties+"}"), &extra) != nil {
		return schema
	}
	for name, value := range extra {
		members[name] = value
	}
	object["properties"], _ = json.Marshal(members)
	out, _ := json.Marshal(object)
	return string(out)
}

const (
	selectedProperties = `"github_owned_allowed":{"type":"boolean"},"verified_allowed":{"type":"boolean"},` +
		`"patterns_allowed":{"type":"array","items":{"type":"string"}}`
	forkApprovalProperties = `"approval_policy":{"type":"string"}`
	retentionProperties    = `"days":{"type":"integer"},"maximum_allowed_days":{"type":"integer"}`
	accessProperties       = `"access_level":{"type":"string"}`
	oidcProperties         = `"use_default":{"type":"boolean"},"include_claim_keys":{"type":"array","items":{"type":"string"}},` +
		`"use_immutable_subject":{"type":"boolean"},"sub_claim_prefix":{"type":"string"}`
)

var selectedSetting = settingDefinition{
	id:         "actions-selected",
	subject:    "the selected GitHub Actions of a repository or an organization",
	properties: selectedProperties,
	fields: []capability.Field{
		{Name: "github_owned_allowed", Description: "True when actions created by GitHub are allowed"},
		{Name: "verified_allowed", Description: "True when actions of verified creators are allowed"},
		{Name: "patterns_allowed", Description: "Patterns of further allowed actions and reusable workflows, " +
			"untrusted data"},
	},
	setKeys: `"github_owned_allowed":{"type":"boolean"},"verified_allowed":{"type":"boolean"},"patterns_allowed":` +
		actionPatternsSchema,
	setArgs: []capability.Argument{
		{Name: "github_owned_allowed", Description: "true to allow actions created by GitHub"},
		{Name: "verified_allowed", Description: "true to allow actions of verified creators"},
		{Name: "patterns_allowed", Description: "Patterns of allowed actions and reusable workflows as text, such " +
			"as octo-org/*, octo-org/action@v1, or !octo-org/denied; at most 1000; replaces the current patterns, " +
			"an empty list removes them"},
	},
	getText: "which actions and reusable workflows GitHub Actions may use while allowed_actions is selected",
	setText: "Choose which actions and reusable workflows GitHub Actions may use while allowed_actions is selected " +
		"(github_owned_allowed, verified_allowed, patterns_allowed); allowing GitHub-owned or verified actions " +
		"or adding patterns loosens what workflows may run and is security relevant",
	getExample: capability.Example{Description: "Read the selected actions of a repository",
		Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
	setExample: capability.Example{Description: "Allow GitHub-owned actions and the actions of one organization",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","github_owned_allowed":true,` +
			`"patterns_allowed":["octo-org/*"]}`)},
}

var forkApprovalSetting = settingDefinition{
	id:         "actions-fork-approval",
	subject:    "the fork pull request approval policy of a repository or an organization",
	properties: forkApprovalProperties,
	required:   `"approval_policy"`,
	fields: []capability.Field{{Name: "approval_policy", Description: "first_time_contributors_new_to_github, " +
		"first_time_contributors, or all_external_contributors"}},
	setKeys:     `"approval_policy":` + enumSchema(forkApprovalPolicies),
	setRequired: []string{"approval_policy"},
	setArgs: []capability.Argument{{Name: "approval_policy", Description: "first_time_contributors_new_to_github, " +
		"first_time_contributors, or all_external_contributors: whose fork pull requests need approval before " +
		"workflows run", Required: true}},
	getText:    "which contributors' fork pull requests need approval before workflows run",
	setText:    "Choose which contributors' fork pull requests need approval before workflows run",
	getExample: capability.Example{Description: "Read the approval policy of an organization", Arguments: json.RawMessage(`{"organization":"orgs/octo-org"}`)},
	setExample: capability.Example{Description: "Require approval for every external contributor",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","approval_policy":"all_external_contributors"}`)},
}

var retentionSetting = settingDefinition{
	id:               "actions-retention",
	subject:          "the artifact and log retention of a repository or an organization",
	properties:       retentionProperties,
	required:         `"days","maximum_allowed_days"`,
	changeProperties: `"days":{"type":"integer"}`, changeRequired: `"days"`,
	fields: []capability.Field{
		{Name: "days", Description: "Days checks, workflow runs, commit statuses, artifacts, and logs are kept"},
		{Name: "maximum_allowed_days", Description: "Most days GitHub allows here"},
	},
	setKeys:     `"days":` + retentionDaysSchema,
	setRequired: []string{"days"},
	setArgs: []capability.Argument{{Name: "days", Description: "Days to keep checks, workflow runs, commit " +
		"statuses, artifacts, and logs, from 1 through 400; at most the maximum_allowed_days the read reports", Required: true}},
	getText:    "how many days checks, workflow runs, commit statuses, artifacts, and logs are kept",
	setText:    "Set how many days checks, workflow runs, commit statuses, artifacts, and logs are kept",
	getExample: capability.Example{Description: "Read the retention of a repository", Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
	setExample: capability.Example{Description: "Keep artifacts and logs for 30 days",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","days":30}`)},
}

var accessSetting = settingDefinition{
	id:          "actions-access",
	subject:     "the access of outside workflows to a private repository",
	repoOnly:    true,
	properties:  accessProperties,
	required:    `"access_level"`,
	fields:      []capability.Field{{Name: "access_level", Description: "none, user, or organization"}},
	setKeys:     `"access_level":` + enumSchema(accessLevels),
	setRequired: []string{"access_level"},
	setArgs: []capability.Argument{{Name: "access_level", Description: "none (only this repository), user (the " +
		"repositories of its owner), or organization (the repositories of its organization): which workflows " +
		"outside it may use its actions and reusable workflows", Required: true}},
	getText:    "which workflows outside a private repository may use its actions and reusable workflows",
	setText:    "Choose which workflows outside a private repository may use its actions and reusable workflows",
	getExample: capability.Example{Description: "Read the access of a repository", Arguments: json.RawMessage(`{}`)},
	setExample: capability.Example{Description: "Share the repository's actions inside its organization",
		Arguments: json.RawMessage(`{"access_level":"organization"}`)},
}

var oidcSetting = settingDefinition{
	id:         "actions-oidc",
	subject:    "the OIDC subject claim of a repository or an organization",
	properties: oidcProperties,
	fields: []capability.Field{
		{Name: "use_default", Description: "Repository only: true while the default subject claim applies"},
		{Name: "include_claim_keys", Description: "Claim keys the customized subject claim is built from, untrusted data"},
		{Name: "use_immutable_subject", Description: "True when the subject claim uses immutable identifiers"},
		{Name: "sub_claim_prefix", Description: "Repository only: prefix GitHub puts before the subject claim, " +
			"untrusted data"},
	},
	setKeys: `"use_default":{"type":"boolean"},"include_claim_keys":` + claimKeysSchema() +
		`,"use_immutable_subject":{"type":"boolean"}`,
	setArgs: []capability.Argument{
		{Name: "use_default", Description: "Repository only, and required there: true for the default subject " +
			"claim, false for one built from include_claim_keys"},
		{Name: "include_claim_keys", Description: "Claim keys of GitHub's documented list (actor, actor_id, " +
			"base_ref, check_run_id, context, environment, event_name, head_ref, job_workflow_ref, job_workflow_sha, ref, " +
			"ref_type, repo, repository, repository_id, repository_owner, repository_owner_id, " +
			"repository_visibility, run_attempt, run_id, run_number, runner_environment, sha, workflow, " +
			"workflow_ref, workflow_sha) or repo_property_ and the name of a repository property; required with use_default false and for an organization, never " +
			"with use_default true"},
		{Name: "use_immutable_subject", Description: "true to build the subject claim from immutable identifiers"},
	},
	getText: "the customization of the OIDC subject claim that workflows' tokens carry",
	setText: "Customize the OIDC subject claim that workflows' tokens carry; a cloud provider that trusts a " +
		"subject claim trusts the new form at once, so a change can lock workflows out or widen who may assume a role",
	getExample: capability.Example{Description: "Read the subject claim of a repository", Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
	setExample: capability.Example{Description: "Build the subject claim from repository and context",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","use_default":false,` +
			`"include_claim_keys":["repo","context"]}`)},
}

var (
	actionsSelectedGet, actionsSelectedSet         = selectedSetting.descriptors()
	actionsForkApprovalGet, actionsForkApprovalSet = forkApprovalSetting.descriptors()
	actionsRetentionGet, actionsRetentionSet       = retentionSetting.descriptors()
	actionsAccessGet, actionsAccessSet             = accessSetting.descriptors()
	actionsOIDCGet, actionsOIDCSet                 = oidcSetting.descriptors()
)

// actionsSettingsReads and actionsSettingsChanges are the tools of the finer Actions settings. The reads
// join the Actions administrator profile; the changes stay unticked, since each is offered only where a
// connection's tools list names it.
var (
	actionsSettingsReads = []string{actionsSelectedGet.ID, actionsForkApprovalGet.ID, actionsRetentionGet.ID,
		actionsAccessGet.ID, actionsOIDCGet.ID}
	actionsSettingsChanges = []string{actionsSelectedSet.ID, actionsForkApprovalSet.ID, actionsRetentionSet.ID,
		actionsAccessSet.ID, actionsOIDCSet.ID}
)

// settingsArguments holds the arguments of every finer Actions settings tool; the input schema of each
// admits only its own. A boolean, a number, or a list is a pointer, so a value left out differs from one sent.
type settingsArguments struct {
	GithubOwnedAllowed  *bool     `json:"github_owned_allowed"`
	VerifiedAllowed     *bool     `json:"verified_allowed"`
	PatternsAllowed     *[]string `json:"patterns_allowed"`
	ApprovalPolicy      string    `json:"approval_policy"`
	Days                *int      `json:"days"`
	AccessLevel         string    `json:"access_level"`
	UseDefault          *bool     `json:"use_default"`
	IncludeClaimKeys    *[]string `json:"include_claim_keys"`
	UseImmutableSubject *bool     `json:"use_immutable_subject"`
}

// settingsHandler decodes the arguments, selects the repository or the organization, and applies every check
// before a credential is resolved, so a refused request never reaches a secret or GitHub.
func settingsHandler(id string, repoOnly bool, check func(*settingsArguments, target) error,
	call func(context.Context, *Client, *settingsArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments settingsArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		var bound target
		var err error
		if repoOnly {
			bound, err = selectTarget(resolved, kindRepository, raw)
		} else {
			bound, err = selectRulesetTarget(resolved, raw)
		}
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
		value, err := call(ctx, client, &arguments)
		if repoOnly {
			return bound.locate(value, err)
		}
		return bound.rulesetLocate(value, err)
	}
}

// actionsSettingsOperations binds every tool of the finer Actions settings to its handler.
func actionsSettingsOperations() []capability.Operation {
	none := func(*settingsArguments, target) error { return nil }
	bind := func(d capability.Descriptor, repoOnly bool, check func(*settingsArguments, target) error,
		call func(context.Context, *Client, *settingsArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: settingsHandler(d.ID, repoOnly, check, call)}
	}
	return []capability.Operation{
		bind(actionsSelectedGet, false, none, func(ctx context.Context, c *Client, _ *settingsArguments) (any, error) {
			return available(c.selectedActions(ctx, "get selected actions"))
		}),
		bind(actionsSelectedSet, false, checkSelectedActions, func(ctx context.Context, c *Client, a *settingsArguments) (any, error) {
			return c.changeSelectedActions(ctx, a)
		}),
		bind(actionsForkApprovalGet, false, none, func(ctx context.Context, c *Client, _ *settingsArguments) (any, error) {
			return available(c.forkApproval(ctx, "get fork approval policy", false))
		}),
		bind(actionsForkApprovalSet, false, checkForkApproval, func(ctx context.Context, c *Client, a *settingsArguments) (any, error) {
			return c.changeForkApproval(ctx, a)
		}),
		bind(actionsRetentionGet, false, none, func(ctx context.Context, c *Client, _ *settingsArguments) (any, error) {
			return available(c.retention(ctx, "get artifact and log retention", false))
		}),
		bind(actionsRetentionSet, false, checkRetention, func(ctx context.Context, c *Client, a *settingsArguments) (any, error) {
			return c.changeRetention(ctx, a)
		}),
		bind(actionsAccessGet, true, none, func(ctx context.Context, c *Client, _ *settingsArguments) (any, error) {
			return available(c.workflowAccess(ctx, "get workflow access", false))
		}),
		bind(actionsAccessSet, true, checkWorkflowAccess, func(ctx context.Context, c *Client, a *settingsArguments) (any, error) {
			return c.changeWorkflowAccess(ctx, a)
		}),
		bind(actionsOIDCGet, false, none, func(ctx context.Context, c *Client, _ *settingsArguments) (any, error) {
			return available(c.oidcSubject(ctx, "get OIDC subject claim", false))
		}),
		bind(actionsOIDCSet, false, checkOIDCSubject, func(ctx context.Context, c *Client, a *settingsArguments) (any, error) {
			return c.changeOIDCSubject(ctx, a)
		}),
	}
}

// ---- checks

// validActionPattern accepts the plain patterns of allowed actions: owner, repository, path, and ref
// characters plus the wildcard and the negation GitHub documents, and no whitespace or control character.
func validActionPattern(pattern string) bool {
	if pattern == "" || len(pattern) > maxPatternLength || !utf8.ValidString(pattern) {
		return false
	}
	for _, r := range pattern {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			strings.ContainsRune(actionPatternAlphabet, r)) {
			return false
		}
	}
	return true
}

func checkSelectedActions(a *settingsArguments, _ target) error {
	if a.GithubOwnedAllowed == nil && a.VerifiedAllowed == nil && a.PatternsAllowed == nil {
		return invalidRequest("give github_owned_allowed, verified_allowed, patterns_allowed, or any of them")
	}
	if a.PatternsAllowed == nil {
		return nil
	}
	patterns := *a.PatternsAllowed
	if len(patterns) > maxSelectedPatterns {
		return invalidRequest(fmt.Sprintf("patterns_allowed holds at most %d patterns", maxSelectedPatterns))
	}
	seen := map[string]bool{}
	for _, pattern := range patterns {
		if !validActionPattern(pattern) {
			return invalidRequest(fmt.Sprintf("each pattern must hold 1 to %d characters of letters, digits, and "+
				"%s", maxPatternLength, actionPatternAlphabet))
		}
		if seen[pattern] {
			return invalidRequest("patterns_allowed must not repeat a pattern")
		}
		seen[pattern] = true
	}
	return nil
}

func checkForkApproval(a *settingsArguments, _ target) error {
	if !contains(forkApprovalPolicies, a.ApprovalPolicy) {
		return invalidRequest("approval_policy must be one of " + strings.Join(forkApprovalPolicies, ", "))
	}
	return nil
}

func checkRetention(a *settingsArguments, _ target) error {
	if a.Days == nil || *a.Days < 1 || *a.Days > maxRetentionDays {
		return invalidRequest(fmt.Sprintf("days must be a whole number from 1 through %d", maxRetentionDays))
	}
	return nil
}

func checkWorkflowAccess(a *settingsArguments, _ target) error {
	if !contains(accessLevels, a.AccessLevel) {
		return invalidRequest("access_level must be one of " + strings.Join(accessLevels, ", "))
	}
	return nil
}

func checkOIDCSubject(a *settingsArguments, bound target) error {
	repository := bound.kind != kindOwner
	if a.IncludeClaimKeys != nil {
		keys := *a.IncludeClaimKeys
		if len(keys) == 0 || len(keys) > maxClaimKeys {
			return invalidRequest("include_claim_keys must name at least one documented claim key")
		}
		seen := map[string]bool{}
		for _, key := range keys {
			if !validClaimKey(key) {
				return invalidRequest("include_claim_keys may hold only the claim keys GitHub documents, " +
					"or repo_property_ and a property name: " + strings.Join(oidcClaimKeys, ", "))
			}
			if seen[key] {
				return invalidRequest("include_claim_keys must not repeat a claim key")
			}
			seen[key] = true
		}
	}
	switch {
	case repository && a.UseDefault == nil:
		return invalidRequest("use_default is required for a repository: true for the default subject claim, " +
			"false for one built from include_claim_keys")
	case !repository && a.UseDefault != nil:
		return invalidRequest("use_default applies only to a repository")
	case repository && *a.UseDefault && a.IncludeClaimKeys != nil:
		return invalidRequest("include_claim_keys applies only with use_default false")
	case repository && !*a.UseDefault && a.IncludeClaimKeys == nil:
		return invalidRequest("use_default false needs include_claim_keys")
	case !repository && a.IncludeClaimKeys == nil && a.UseImmutableSubject == nil:
		return invalidRequest("give include_claim_keys, use_immutable_subject, or both")
	}
	return nil
}

// ---- transport

// settingPath is the route of one Actions setting below the bound repository or organization.
func (c *Client) settingPath(rest string) string {
	if c.target.kind == kindOwner {
		return c.orgPath("actions/" + rest)
	}
	return c.actionsPath(rest)
}

// Permission messages of the finer Actions settings. GitHub decides on every request; a message names what
// such a request needs without claiming what the configured token holds. The fine-grained organization
// permissions are stated cautiously, since GitHub documents them unevenly across these routes.
func settingPermission(t target, change bool) string {
	access := "read"
	verb := "reading it needs"
	if change {
		access, verb = "read and write", "changing it needs"
	}
	if t.kind == kindOwner {
		return "GitHub refused this token the Actions setting of this organization; " + verb + " admin:org on a " +
			"classic token, or, as far as GitHub documents it, Administration or Actions policies of the " +
			"organization on a fine-grained token"
	}
	return "GitHub refused this token the Actions setting of this repository; " + verb + " repo on a classic " +
		"token of a repository administrator, or Administration: " + access + " on a fine-grained token"
}

// settingMessages are what a refusal of one setting means for its own request, in Qatlas' words.
type settingMessages struct {
	conflict, rejected string
}

const (
	settingConflict = "GitHub refused the setting in its current state; an organization or enterprise policy may " +
		"fix it or the plan may not offer it, so nothing was changed"
	settingRejected = "GitHub rejected the value as invalid for this setting; the plan or an organization or " +
		"enterprise policy may not allow it, so nothing was changed"
)

func (c *Client) readSetting(ctx context.Context, op, rest string, messages settingMessages, out any) error {
	return variableFailure(c.rest(ctx, op, c.settingPath(rest), out), settingPermission(c.target, false),
		messages.conflict, messages.rejected)
}

// sendSetting sends one change, never repeated. A refusal says nothing was changed; any other failure after the
// request may have reached GitHub says the outcome is open.
func (c *Client) sendSetting(ctx context.Context, op, method, rest string, messages settingMessages, body any) error {
	return variableFailure(c.restChange(ctx, op, method, c.settingPath(rest), body, nil),
		settingPermission(c.target, true), messages.conflict, messages.rejected)
}

var (
	selectedMessages = settingMessages{
		conflict: "GitHub refused the selection in the current state; it applies only while allowed_actions is " +
			"selected (github.actions-permissions.get and .update at the repository, the organization settings " +
			"for an organization), and an organization or enterprise policy may fix it, so nothing was changed",
		rejected: "GitHub rejected the selection; check the patterns, so nothing was changed",
	}
	forkApprovalMessages = settingMessages{conflict: settingConflict, rejected: "GitHub rejected the approval " +
		"policy; the plan or an organization or enterprise policy may not offer it, so nothing was changed"}
	retentionMessages = settingMessages{
		conflict: settingConflict,
		rejected: "GitHub rejected the number of days; it must lie between 1 and the maximum_allowed_days the " +
			"read reports, which the plan or a policy may lower, so nothing was changed",
	}
	accessMessages = settingMessages{
		conflict: settingConflict,
		rejected: "GitHub rejected the access level; the setting applies only to a private or internal repository " +
			"of an organization, so nothing was changed",
	}
	oidcMessages = settingMessages{
		conflict: settingConflict,
		rejected: "GitHub rejected the subject claim customization; check the claim keys and, for a repository, " +
			"that use_default is false when keys are given, so nothing was changed",
	}
)

// SettingChange names a change by the values the call gave before it and as GitHub reports them after it.
// When GitHub accepted the change but its state could not be read back, After is absent and AfterUnconfirmed
// says so; a value that was sent never stands in for GitHub's state.
type SettingChange[T any] struct {
	Before           T    `json:"before"`
	After            *T   `json:"after,omitempty"`
	AfterUnconfirmed bool `json:"after_unconfirmed,omitempty"`
}

var settingChangeFields = []capability.Field{
	{Name: "before", Description: "Values the call gave, as GitHub reported them before the change"},
	{Name: "after", Description: "Values the call gave, as GitHub reports them after the change; absent when " +
		"after_unconfirmed is true"},
	{Name: "after_unconfirmed", Description: "True when GitHub accepted the change but its state could not be " +
		"read back; read the setting before relying on it or repeating the change"},
}

func settingChangeOutput(properties, required string) string {
	setting := string(settingsOutput(properties, required))
	return `{"type":"object","properties":{"before":` + setting + `,"after":` + setting +
		`,"after_unconfirmed":{"type":"boolean"}},"required":["before"],"additionalProperties":false}`
}

// SettingUnavailable is what a read answers while the setting does not apply: GitHub refuses it with a
// conflict in the current state, because of the plan, a policy, or another setting. It is a state, not an
// error, and holds no value of the setting and no text of GitHub.
type SettingUnavailable struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

const (
	unavailableState  = "unavailable"
	unavailableReason = "GitHub reports that this setting does not apply in the current state: the plan, an " +
		"organization or enterprise policy, or another setting (for example allowed_actions not being selected, " +
		"or a public repository) does not offer it"
	unavailableProperties = `"state":{"type":"string"},"reason":{"type":"string"}`
)

var unavailableFields = []capability.Field{
	{Name: "state", Description: "unavailable when GitHub reports that the setting does not apply in the current " +
		"state; then no value of the setting is given; absent otherwise"},
	{Name: "reason", Description: "Fixed reason that accompanies state unavailable"},
}

// available turns a read's conflict into the unavailable state; every other failure stays an error. 403 and
// 404 cannot be told from a missing right or target, so they are no state.
func available[T any](value *T, err error) (any, error) {
	if err == nil {
		return value, nil
	}
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Original != nil && failure.Original.Status == http.StatusConflict {
		return &SettingUnavailable{State: unavailableState, Reason: unavailableReason}, nil
	}
	return nil, err
}

// ---- selected actions

// ActionsSelected is the selection of allowed actions and reusable workflows.
type ActionsSelected struct {
	GithubOwnedAllowed *bool    `json:"github_owned_allowed,omitempty"`
	VerifiedAllowed    *bool    `json:"verified_allowed,omitempty"`
	PatternsAllowed    []string `json:"patterns_allowed,omitempty"`
}

func (c *Client) selectedActions(ctx context.Context, op string) (*ActionsSelected, error) {
	var raw ActionsSelected
	if err := c.readSetting(ctx, op, "permissions/selected-actions", selectedMessages, &raw); err != nil {
		return nil, err
	}
	return &raw, nil
}

// given keeps only the settings the call gave, so the answer claims nothing about the others.
func (s ActionsSelected) given(a *settingsArguments) ActionsSelected {
	var out ActionsSelected
	if a.GithubOwnedAllowed != nil {
		out.GithubOwnedAllowed = s.GithubOwnedAllowed
	}
	if a.VerifiedAllowed != nil {
		out.VerifiedAllowed = s.VerifiedAllowed
	}
	if a.PatternsAllowed != nil {
		out.PatternsAllowed = s.PatternsAllowed
	}
	return out
}

// changeSelectedActions sends the current selection with the given values, because GitHub replaces it as a
// whole.
func (c *Client) changeSelectedActions(ctx context.Context, a *settingsArguments) (*SettingChange[ActionsSelected], error) {
	const op = "change selected actions"
	before, err := c.selectedActions(ctx, op)
	if err != nil {
		return nil, err
	}
	sent := *before
	if a.GithubOwnedAllowed != nil {
		sent.GithubOwnedAllowed = a.GithubOwnedAllowed
	}
	if a.VerifiedAllowed != nil {
		sent.VerifiedAllowed = a.VerifiedAllowed
	}
	if a.PatternsAllowed != nil {
		sent.PatternsAllowed = *a.PatternsAllowed
	}
	patterns := sent.PatternsAllowed
	if patterns == nil {
		patterns = []string{}
	}
	body := map[string]any{"patterns_allowed": patterns}
	if sent.GithubOwnedAllowed != nil {
		body["github_owned_allowed"] = *sent.GithubOwnedAllowed
	}
	if sent.VerifiedAllowed != nil {
		body["verified_allowed"] = *sent.VerifiedAllowed
	}
	if err := c.sendSetting(ctx, op, http.MethodPut, "permissions/selected-actions", selectedMessages, body); err != nil {
		return nil, err
	}
	// A failed read back must not turn the applied change into an error, and no sent value stands in for
	// GitHub's state.
	current, err := c.selectedActions(ctx, op)
	if err != nil {
		return &SettingChange[ActionsSelected]{Before: before.given(a), AfterUnconfirmed: true}, nil
	}
	after := current.given(a)
	return &SettingChange[ActionsSelected]{Before: before.given(a), After: &after}, nil
}

// ---- fork pull request approval

// ActionsForkApproval is the approval policy for fork pull requests.
type ActionsForkApproval struct {
	ApprovalPolicy string `json:"approval_policy"`
}

func (c *Client) forkApproval(ctx context.Context, op string, change bool) (*ActionsForkApproval, error) {
	var raw ActionsForkApproval
	if err := c.readSetting(ctx, op, "permissions/fork-pr-contributor-approval", forkApprovalMessages, &raw); err != nil {
		return nil, err
	}
	if !contains(forkApprovalPolicies, raw.ApprovalPolicy) {
		return nil, invalidResponse(op, change)
	}
	return &raw, nil
}

func (c *Client) changeForkApproval(ctx context.Context, a *settingsArguments) (*SettingChange[ActionsForkApproval], error) {
	const op = "change fork approval policy"
	before, err := c.forkApproval(ctx, op, true)
	if err != nil {
		return nil, err
	}
	if err := c.sendSetting(ctx, op, http.MethodPut, "permissions/fork-pr-contributor-approval", forkApprovalMessages,
		map[string]any{"approval_policy": a.ApprovalPolicy}); err != nil {
		return nil, err
	}
	current, err := c.forkApproval(ctx, op, true)
	if err != nil {
		return &SettingChange[ActionsForkApproval]{Before: *before, AfterUnconfirmed: true}, nil
	}
	return &SettingChange[ActionsForkApproval]{Before: *before, After: current}, nil
}

// ---- artifact and log retention

// ActionsRetention is how long checks, runs, statuses, artifacts, and logs are kept.
type ActionsRetention struct {
	Days               int `json:"days"`
	MaximumAllowedDays int `json:"maximum_allowed_days"`
}

func (c *Client) retention(ctx context.Context, op string, change bool) (*ActionsRetention, error) {
	var raw struct {
		Days    *int `json:"days"`
		Maximum *int `json:"maximum_allowed_days"`
	}
	if err := c.readSetting(ctx, op, "permissions/artifact-and-log-retention", retentionMessages, &raw); err != nil {
		return nil, err
	}
	if raw.Days == nil || raw.Maximum == nil || *raw.Days < 0 || *raw.Maximum < 0 {
		return nil, invalidResponse(op, change)
	}
	return &ActionsRetention{Days: *raw.Days, MaximumAllowedDays: *raw.Maximum}, nil
}

// RetentionDays is the retention a change reports: only the days it set, never the maximum it did not.
type RetentionDays struct {
	Days int `json:"days"`
}

func (c *Client) changeRetention(ctx context.Context, a *settingsArguments) (*SettingChange[RetentionDays], error) {
	const op = "change artifact and log retention"
	before, err := c.retention(ctx, op, true)
	if err != nil {
		return nil, err
	}
	if err := c.sendSetting(ctx, op, http.MethodPut, "permissions/artifact-and-log-retention", retentionMessages,
		map[string]any{"days": *a.Days}); err != nil {
		return nil, err
	}
	current, err := c.retention(ctx, op, true)
	if err != nil {
		return &SettingChange[RetentionDays]{Before: RetentionDays{Days: before.Days}, AfterUnconfirmed: true}, nil
	}
	return &SettingChange[RetentionDays]{Before: RetentionDays{Days: before.Days},
		After: &RetentionDays{Days: current.Days}}, nil
}

// ---- workflow access to a private repository

// ActionsAccess is which workflows outside a repository may use its actions.
type ActionsAccess struct {
	AccessLevel string `json:"access_level"`
}

func (c *Client) workflowAccess(ctx context.Context, op string, change bool) (*ActionsAccess, error) {
	var raw ActionsAccess
	if err := c.readSetting(ctx, op, "permissions/access", accessMessages, &raw); err != nil {
		return nil, err
	}
	if !contains(accessLevels, raw.AccessLevel) {
		return nil, invalidResponse(op, change)
	}
	return &raw, nil
}

func (c *Client) changeWorkflowAccess(ctx context.Context, a *settingsArguments) (*SettingChange[ActionsAccess], error) {
	const op = "change workflow access"
	before, err := c.workflowAccess(ctx, op, true)
	if err != nil {
		return nil, err
	}
	if err := c.sendSetting(ctx, op, http.MethodPut, "permissions/access", accessMessages,
		map[string]any{"access_level": a.AccessLevel}); err != nil {
		return nil, err
	}
	current, err := c.workflowAccess(ctx, op, true)
	if err != nil {
		return &SettingChange[ActionsAccess]{Before: *before, AfterUnconfirmed: true}, nil
	}
	return &SettingChange[ActionsAccess]{Before: *before, After: current}, nil
}

// ---- OIDC subject claim

// ActionsOIDC is the customization of the OIDC subject claim. A repository reports use_default and its
// prefix; an organization reports neither.
type ActionsOIDC struct {
	UseDefault          *bool    `json:"use_default,omitempty"`
	IncludeClaimKeys    []string `json:"include_claim_keys,omitempty"`
	UseImmutableSubject *bool    `json:"use_immutable_subject,omitempty"`
	SubClaimPrefix      string   `json:"sub_claim_prefix,omitempty"`
}

func (c *Client) oidcSubject(ctx context.Context, op string, change bool) (*ActionsOIDC, error) {
	var raw ActionsOIDC
	if err := c.readSetting(ctx, op, "oidc/customization/sub", oidcMessages, &raw); err != nil {
		return nil, err
	}
	if c.target.kind != kindOwner && raw.UseDefault == nil {
		return nil, invalidResponse(op, change)
	}
	return &raw, nil
}

// given keeps only the settings the call gave.
func (o ActionsOIDC) given(a *settingsArguments) ActionsOIDC {
	var out ActionsOIDC
	if a.UseDefault != nil {
		out.UseDefault = o.UseDefault
	}
	if a.IncludeClaimKeys != nil {
		out.IncludeClaimKeys = o.IncludeClaimKeys
	}
	if a.UseImmutableSubject != nil {
		out.UseImmutableSubject = o.UseImmutableSubject
	}
	return out
}

// changeOIDCSubject sends a repository's use_default with the keys it names, or an organization's keys, which
// are the current ones when the call gave only use_immutable_subject.
func (c *Client) changeOIDCSubject(ctx context.Context, a *settingsArguments) (*SettingChange[ActionsOIDC], error) {
	const op = "change OIDC subject claim"
	before, err := c.oidcSubject(ctx, op, true)
	if err != nil {
		return nil, err
	}
	body := map[string]any{}
	switch {
	case c.target.kind != kindOwner:
		body["use_default"] = *a.UseDefault
		if a.IncludeClaimKeys != nil {
			body["include_claim_keys"] = *a.IncludeClaimKeys
		}
	case a.IncludeClaimKeys != nil:
		body["include_claim_keys"] = *a.IncludeClaimKeys
	case len(before.IncludeClaimKeys) > 0:
		body["include_claim_keys"] = before.IncludeClaimKeys
	default:
		return nil, invalidRequest("the organization has no customized claim keys yet; give include_claim_keys")
	}
	if a.UseImmutableSubject != nil {
		body["use_immutable_subject"] = *a.UseImmutableSubject
	}
	if err := c.sendSetting(ctx, op, http.MethodPut, "oidc/customization/sub", oidcMessages, body); err != nil {
		return nil, err
	}
	current, err := c.oidcSubject(ctx, op, true)
	if err != nil {
		return &SettingChange[ActionsOIDC]{Before: before.given(a), AfterUnconfirmed: true}, nil
	}
	after := current.given(a)
	return &SettingChange[ActionsOIDC]{Before: before.given(a), After: &after}, nil
}

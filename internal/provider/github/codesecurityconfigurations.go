package github

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The code security configuration tools read the sets of security settings of one organization (code
// scanning, Dependabot, secret protection, and the dependency graph), the defaults for new repositories, and
// the repositories a configuration is attached to. Every tool names an organization as an owner target; the
// enterprise routes are not reachable. Names, descriptions, and runner labels come from the organization and
// are untrusted data.

const codeSecurityConfigurationsReadPermission = "GitHub refused this token the code security configurations of " +
	"this organization; reading them needs read:org on a classic token, and the account must be an " +
	"organization owner or security manager"

// codeSecurityIDSchema is a configuration id as GitHub numbers it.
const codeSecurityIDSchema = `{"type":"integer","minimum":1,"maximum":9007199254740991}`

var (
	codeSecurityTargetTypeValues = []string{"global", "all"}
	// codeSecurityStatusValues are the attachment statuses of the status filter, all excluded: all stands
	// for no filter and is accepted only alone.
	codeSecurityStatusValues = []string{"attached", "attaching", "detached", "removed", "enforced", "failed",
		"updating", "removed_by_enterprise", "enterprise_enforced"}
)

const codeSecurityConfigurationProperties = `"id":{"type":"integer"},"name":{"type":"string"},` +
	`"target_type":{"type":"string"},"description":{"type":"string"},"advanced_security":{"type":"string"},` +
	`"dependency_graph":{"type":"string"},"dependency_graph_autosubmit_action":{"type":"string"},` +
	`"dependency_graph_autosubmit_action_options":{"type":"object","properties":{"labeled_runners":{"type":"boolean"}},` +
	`"additionalProperties":false},` +
	`"dependabot_alerts":{"type":"string"},"dependabot_reachability":{"type":"string"},` +
	`"dependabot_security_updates":{"type":"string"},"dependabot_delegated_alert_dismissal":{"type":"string"},` +
	`"code_scanning_options":{"type":"object","properties":{"allow_advanced":{"type":"boolean"}},` +
	`"additionalProperties":false},` +
	`"code_scanning_default_setup":{"type":"string"},` +
	`"code_scanning_default_setup_options":{"type":"object","properties":{"runner_type":{"type":"string"},` +
	`"runner_label":{"type":"string"}},"additionalProperties":false},` +
	`"code_scanning_delegated_alert_dismissal":{"type":"string"},` +
	`"secret_scanning":{"type":"string"},"secret_scanning_push_protection":{"type":"string"},` +
	`"secret_scanning_delegated_bypass":{"type":"string"},` +
	`"secret_scanning_delegated_bypass_options":{"type":"object","properties":{"reviewers":{"type":"array",` +
	`"maxItems":100,"items":{"type":"object","properties":{"reviewer_id":{"type":"integer"},` +
	`"reviewer_type":{"type":"string"},"mode":{"type":"string"},"security_configuration_id":{"type":"integer"}},` +
	`"additionalProperties":false}},"reviewers_truncated":{"type":"boolean"}},"additionalProperties":false},` +
	`"secret_scanning_validity_checks":{"type":"string"},"secret_scanning_non_provider_patterns":{"type":"string"},` +
	`"secret_scanning_generic_secrets":{"type":"string"},"secret_scanning_generic_secrets_push_protection":{"type":"string"},` +
	`"secret_scanning_delegated_alert_dismissal":{"type":"string"},"secret_scanning_extended_metadata":{"type":"string"},` +
	`"private_vulnerability_reporting":{"type":"string"},"enforcement":{"type":"string"},"url":{"type":"string"},` +
	`"html_url":{"type":"string"},"created_at":{"type":"string"},"updated_at":{"type":"string"}`

const codeSecurityConfigurationRequired = `"required":["id","name"],"additionalProperties":false`

// codeSecurityConfigurationMembers documents every member of GitHub's configuration object. A member GitHub
// leaves out or sends as null is absent from the answer.
var codeSecurityConfigurationMembers = []capability.Field{
	{Name: "id", Description: "Numeric id of the configuration, as the other tools take it"},
	{Name: "name", Description: "Name of the configuration, unique within the organization; untrusted data"},
	{Name: "target_type", Description: "global (GitHub's own), organization, or enterprise"},
	{Name: "description", Description: "Description of the configuration; untrusted data"},
	{Name: "advanced_security", Description: "enabled, disabled, code_security, or secret_protection: the GitHub " +
		"Advanced Security products the configuration enables"},
	{Name: "dependency_graph", Description: "enabled, disabled, or not_set: the dependency graph"},
	{Name: "dependency_graph_autosubmit_action", Description: "enabled, disabled, or not_set: automatic dependency " +
		"submission"},
	{Name: "dependency_graph_autosubmit_action_options", Description: "Options of automatic dependency submission: " +
		"labeled_runners, true for runners labeled dependency-submission and false for standard runners"},
	{Name: "dependabot_alerts", Description: "enabled, disabled, or not_set: Dependabot alerts"},
	{Name: "dependabot_reachability", Description: "enabled, disabled, or not_set: reachability analysis for " +
		"Dependabot, which GitHub returns only for organizations that have it"},
	{Name: "dependabot_security_updates", Description: "enabled, disabled, or not_set: Dependabot security updates"},
	{Name: "dependabot_delegated_alert_dismissal", Description: "enabled, disabled, or not_set: delegated " +
		"dismissal of Dependabot alerts"},
	{Name: "code_scanning_options", Description: "Options of code scanning: allow_advanced, whether repositories " +
		"with an advanced setup may keep it"},
	{Name: "code_scanning_default_setup", Description: "enabled, disabled, or not_set: code scanning default setup"},
	{Name: "code_scanning_default_setup_options", Description: "Options of the default setup: runner_type " +
		"(standard, labeled, or not_set) and runner_label, the label of the runner for labeled; the label is " +
		"untrusted data"},
	{Name: "code_scanning_delegated_alert_dismissal", Description: "enabled, disabled, or not_set: delegated " +
		"dismissal of code scanning alerts"},
	{Name: "secret_scanning", Description: "enabled, disabled, or not_set: secret scanning"},
	{Name: "secret_scanning_push_protection", Description: "enabled, disabled, or not_set: push protection"},
	{Name: "secret_scanning_delegated_bypass", Description: "enabled, disabled, or not_set: delegated bypass of " +
		"push protection"},
	{Name: "secret_scanning_delegated_bypass_options", Description: "Options of delegated bypass: reviewers, at " +
		"most 100, each with reviewer_id (a team or role), reviewer_type (TEAM or ROLE), mode (ALWAYS or EXEMPT), " +
		"and security_configuration_id; reviewers_truncated says when more exist"},
	{Name: "secret_scanning_validity_checks", Description: "enabled, disabled, or not_set: validity checks"},
	{Name: "secret_scanning_non_provider_patterns", Description: "enabled, disabled, or not_set: non-provider patterns"},
	{Name: "secret_scanning_generic_secrets", Description: "enabled, disabled, or not_set: Copilot secret scanning"},
	{Name: "secret_scanning_generic_secrets_push_protection", Description: "enabled, disabled, or not_set: push " +
		"protection for generic secrets"},
	{Name: "secret_scanning_delegated_alert_dismissal", Description: "enabled, disabled, or not_set: delegated " +
		"dismissal of secret scanning alerts"},
	{Name: "secret_scanning_extended_metadata", Description: "enabled, disabled, or not_set: extended metadata"},
	{Name: "private_vulnerability_reporting", Description: "enabled, disabled, or not_set: private vulnerability " +
		"reporting"},
	{Name: "enforcement", Description: "enforced, unenforced, or enterprise_enforced: whether repositories may " +
		"change the settings"},
	{Name: "url", Description: "REST address of the configuration, untrusted data, never fetched"},
	{Name: "html_url", Description: "Web address of the configuration, untrusted data, never fetched"},
	{Name: "created_at", Description: "Creation time"},
	{Name: "updated_at", Description: "Time of the last change"},
}

var codeSecurityConfigurationsList = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.list",
	Version: 1,
	Title:   "List the code security configurations of a GitHub organization",
	Description: "List one bounded batch of the code security configurations of an organization an explicit " +
		"connection allows, in the order GitHub returns them, each with every setting GitHub documents; names " +
		"and descriptions are untrusted data; needs an organization owner or security manager",
	Tags:         []string{"github", "security", "code-security", "organization", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"target_type":{"type":"string","enum":["global","all"]},` + pagingKeys),
	OutputSchema: listOutput("configurations", codeSecurityConfigurationProperties, codeSecurityConfigurationRequired),
	Arguments: append([]capability.Argument{
		{Name: "target_type", Description: "global (only GitHub's own configurations) or all; GitHub's default, all, " +
			"when omitted"},
	}, pagingArguments...),
	Fields: append(append([]capability.Field{
		{Name: "configurations", Description: "Configurations of the organization with the members below; a member " +
			"GitHub leaves out or sends as null is absent"},
	}, codeSecurityConfigurationMembers...), pagingFields...),
	Examples: []capability.Example{{
		Description: "List the configurations of an organization",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org"}`),
	}},
}

var codeSecurityConfigurationsGet = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.get",
	Version: 1,
	Title:   "Get a GitHub code security configuration",
	Description: "Read one code security configuration of an organization an explicit connection allows, with " +
		"every setting GitHub documents; name and description are untrusted data; needs an organization owner " +
		"or security manager",
	Tags:        []string{"github", "security", "code-security", "organization", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"configuration_id":`+codeSecurityIDSchema, "configuration_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + codeSecurityConfigurationProperties +
		`},` + codeSecurityConfigurationRequired + `}`),
	Arguments: []capability.Argument{{Name: "configuration_id", Description: "Id of the configuration, as " +
		"github.code-security-configurations.list reports it", Required: true}},
	Fields: codeSecurityConfigurationMembers,
	Examples: []capability.Example{{
		Description: "Read one configuration",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","configuration_id":1325}`),
	}},
}

const codeSecurityDefaultProperties = `"default_for_new_repos":{"type":"string"},"configuration":{"type":"object",` +
	`"properties":{` + codeSecurityConfigurationProperties + `},` + codeSecurityConfigurationRequired + `}`

var codeSecurityDefaultsGet = capability.Descriptor{
	ID:      Provider + ".code-security-defaults.get",
	Version: 1,
	Title:   "Get the default code security configurations of a GitHub organization",
	Description: "Read the code security configurations an organization an explicit connection allows applies to " +
		"new repositories, one entry per kind of repository, each with every setting GitHub documents; needs an " +
		"organization owner or security manager",
	Tags:        []string{"github", "security", "code-security", "organization", "get"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(``),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"defaults":{"type":"array","items":{"type":"object",` +
		`"properties":{` + codeSecurityDefaultProperties + `},"additionalProperties":false}}},` +
		`"required":["defaults"],"additionalProperties":false}`),
	Fields: []capability.Field{
		{Name: "defaults", Description: "Defaults of the organization, possibly none: default_for_new_repos (public, " +
			"private_and_internal, or all: the repositories the configuration applies to when they are created) " +
			"and configuration with the members of github.code-security-configurations.get; a member GitHub " +
			"leaves out or sends as null is absent"},
		{Name: "default_for_new_repos", Description: "Visibility of the new repositories the configuration applies to"},
		{Name: "configuration", Description: "The default configuration, with the members of " +
			"github.code-security-configurations.get; untrusted data"},
	},
	Examples: []capability.Example{{
		Description: "Read the defaults of an organization",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org"}`),
	}},
}

var codeSecurityRepositoryProperties = `"status":{"type":"string"},"repository":` + restRepositorySchema

var codeSecurityConfigurationRepositoriesList = capability.Descriptor{
	ID:      Provider + ".code-security-configuration-repositories.list",
	Version: 1,
	Title:   "List the repositories of a GitHub code security configuration",
	Description: "List one bounded batch of the repositories a code security configuration of an organization an " +
		"explicit connection allows is attached to, with the attachment status, in the order GitHub returns them; " +
		"when the connection's targets name any, a repository outside them or in an excluded repository is " +
		"withheld and counted in withheld instead of being listed; needs an organization owner or security manager",
	Tags:        []string{"github", "security", "code-security", "organization", "list"},
	Risk:        readRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"configuration_id":`+codeSecurityIDSchema+`,"status":`+codeSecurityStatusSchema+`,`+pagingKeys, "configuration_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"repositories":{"type":"array","items":{"type":"object",` +
		`"properties":{` + codeSecurityRepositoryProperties + `},"additionalProperties":false}},` +
		`"withheld":{"type":"integer"},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"required":["repositories","has_more"],"additionalProperties":false}`),
	Arguments: append([]capability.Argument{
		{Name: "configuration_id", Description: "Id of the configuration, as " +
			"github.code-security-configurations.list reports it", Required: true},
		{Name: "status", Description: "Attachment statuses to keep, a list of attached, attaching, detached, " +
			"removed, enforced, failed, updating, removed_by_enterprise, and enterprise_enforced; removed and " +
			"detached cannot be requested together (GitHub refuses it); every status when omitted"},
	}, pagingArguments...),
	Fields: append([]capability.Field{
		{Name: "repositories", Description: "Repositories the configuration is attached to: status and repository " +
			"with the members of GitHub's minimal repository object, untrusted data, addresses never fetched"},
		{Name: "status", Description: "Attachment status: attached, attaching, detached, removed, enforced, failed, " +
			"updating, removed_by_enterprise, or enterprise_enforced"},
		{Name: "repository", Description: "The repository, with the members of GitHub's minimal repository object " +
			"as the repository read gives them; untrusted data"},
		{Name: "withheld", Description: "Count of repositories in this batch outside the connection's targets or in " +
			"an excluded repository, omitted when none were withheld"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the repositories a configuration failed to attach to",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","configuration_id":1325,"status":["failed"]}`),
	}},
}

const codeSecurityStatusSchema = `{"type":"array","minItems":1,"maxItems":9,"uniqueItems":true,"items":{"type":"string",` +
	`"enum":["attached","attaching","detached","removed","enforced","failed","updating","removed_by_enterprise",` +
	`"enterprise_enforced"]}}`

// codeSecurityConfigurationTools are the four read tools of the organization's code security configurations.
var codeSecurityConfigurationTools = []string{codeSecurityConfigurationsList.ID, codeSecurityConfigurationsGet.ID,
	codeSecurityDefaultsGet.ID, codeSecurityConfigurationRepositoriesList.ID}

// codeSecurityArguments holds the arguments of the four tools; the input schema of each admits only its own.
type codeSecurityArguments struct {
	ConfigurationID int64    `json:"configuration_id"`
	TargetType      string   `json:"target_type"`
	Status          []string `json:"status"`
	Limit           int      `json:"limit"`
	Cursor          string   `json:"cursor"`

	targetType, status string
	after              string
	perPage            int
	binding            []byte
	allowed            allowlist
}

func (a *codeSecurityArguments) query() url.Values {
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}}
	if a.after != "" {
		query.Set("after", a.after)
	}
	if a.targetType != "" {
		query.Set("target_type", a.targetType)
	}
	if a.status != "" {
		query.Set("status", a.status)
	}
	return query
}

// startPage reads the batch size and the cursor of a list the way the advisory lists do: GitHub pages these
// routes by the after cursor of its Link header, which the Qatlas cursor carries with the batch size of the
// first batch, bound to the tool, the organization, and the filters.
func (a *codeSecurityArguments) startPage(kind string, bound target, filters ...any) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint(append([]any{kind, strings.ToLower(bound.String())}, filters...)...)
	page := advisoryArguments{Cursor: a.Cursor, binding: a.binding}
	if err := page.advisoryPage(limit); err != nil {
		return err
	}
	a.after, a.perPage = page.after, page.perPage
	return nil
}

func (a *codeSecurityArguments) more(next string) (bool, string) {
	return (&advisoryArguments{binding: a.binding, perPage: a.perPage}).more(next)
}

func checkCodeSecurityConfigurationID(a *codeSecurityArguments) error {
	if a.ConfigurationID < 1 {
		return invalidRequest("configuration_id must be a positive configuration id")
	}
	return nil
}

func checkCodeSecurityConfigurationsList(a *codeSecurityArguments, bound target) error {
	var filters []any
	if a.TargetType != "" {
		if !containsFold(codeSecurityTargetTypeValues, a.TargetType) {
			return invalidRequest("target_type must be global or all")
		}
		a.targetType = strings.ToLower(a.TargetType)
		filters = append(filters, "target_type", a.targetType)
	}
	return a.startPage("codesecurityconfigurations", bound, filters...)
}

func checkCodeSecurityConfigurationsGet(a *codeSecurityArguments, _ target) error {
	return checkCodeSecurityConfigurationID(a)
}

func checkCodeSecurityDefaultsGet(*codeSecurityArguments, target) error { return nil }

func checkCodeSecurityConfigurationRepositoriesList(a *codeSecurityArguments, bound target) error {
	if err := checkCodeSecurityConfigurationID(a); err != nil {
		return err
	}
	filters := []any{a.ConfigurationID}
	if len(a.Status) > 0 {
		seen := map[string]bool{}
		statuses := make([]string, 0, len(a.Status))
		for _, status := range a.Status {
			status = strings.ToLower(status)
			if !containsFold(codeSecurityStatusValues, status) || seen[status] {
				return invalidRequest("status must list each of attached, attaching, detached, removed, enforced, " +
					"failed, updating, removed_by_enterprise, and enterprise_enforced at most once")
			}
			seen[status] = true
			statuses = append(statuses, status)
		}
		if seen["removed"] && seen["detached"] {
			return invalidRequest("status cannot list removed and detached together; GitHub refuses that combination")
		}
		a.status = strings.Join(statuses, ",")
		filters = append(filters, "status", a.status)
	}
	return a.startPage("codesecurityrepositories", bound, filters...)
}

func codeSecurityConfigurationOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*codeSecurityArguments, target) error,
		call func(context.Context, *Client, *codeSecurityArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: codeSecurityHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(codeSecurityConfigurationsList, checkCodeSecurityConfigurationsList,
			func(ctx context.Context, c *Client, a *codeSecurityArguments) (any, error) {
				return c.listCodeSecurityConfigurations(ctx, a)
			}),
		bind(codeSecurityConfigurationsGet, checkCodeSecurityConfigurationsGet,
			func(ctx context.Context, c *Client, a *codeSecurityArguments) (any, error) {
				return c.getCodeSecurityConfiguration(ctx, a)
			}),
		bind(codeSecurityDefaultsGet, checkCodeSecurityDefaultsGet,
			func(ctx context.Context, c *Client, _ *codeSecurityArguments) (any, error) {
				return c.getCodeSecurityDefaults(ctx)
			}),
		bind(codeSecurityConfigurationRepositoriesList, checkCodeSecurityConfigurationRepositoriesList,
			func(ctx context.Context, c *Client, a *codeSecurityArguments) (any, error) {
				return c.listCodeSecurityConfigurationRepositories(ctx, a)
			}),
	}
}

// codeSecurityHandler decodes and checks the arguments and the organization before a credential is
// resolved, so a refused request never becomes a provider call or a secret read. The owner must be an
// organization the targets name as an owner and no exclusion covers.
func codeSecurityHandler(id string, check func(*codeSecurityArguments, target) error,
	call func(context.Context, *Client, *codeSecurityArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments codeSecurityArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOwner(resolved, kindOwner, raw)
		if err != nil {
			return nil, err
		}
		if bound.scope != "orgs" {
			return nil, invalidRequest("owner must be an organization, as orgs/LOGIN; code security " +
				"configurations belong to an organization, not a user")
		}
		if arguments.allowed, err = allowlistOf(resolved); err != nil {
			return nil, providerError("open", err.Error())
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

// CodeSecurityAutosubmitOptions are the options of automatic dependency submission.
type CodeSecurityAutosubmitOptions struct {
	LabeledRunners *bool `json:"labeled_runners,omitempty"`
}

// CodeSecurityScanningOptions are the options of code scanning.
type CodeSecurityScanningOptions struct {
	AllowAdvanced *bool `json:"allow_advanced,omitempty"`
}

// CodeSecuritySetupOptions are the options of code scanning default setup. RunnerLabel is untrusted data.
type CodeSecuritySetupOptions struct {
	RunnerType  *string `json:"runner_type,omitempty"`
	RunnerLabel *string `json:"runner_label,omitempty"`
}

// CodeSecurityBypassReviewer is one reviewer of delegated bypass.
type CodeSecurityBypassReviewer struct {
	ReviewerID              *int64  `json:"reviewer_id,omitempty"`
	ReviewerType            *string `json:"reviewer_type,omitempty"`
	Mode                    *string `json:"mode,omitempty"`
	SecurityConfigurationID *int64  `json:"security_configuration_id,omitempty"`
}

// CodeSecurityBypassOptions are the options of delegated bypass, with at most 100 reviewers.
type CodeSecurityBypassOptions struct {
	Reviewers          []CodeSecurityBypassReviewer `json:"reviewers,omitempty"`
	ReviewersTruncated bool                         `json:"reviewers_truncated,omitempty"`
}

// CodeSecurityConfiguration is GitHub's code security configuration with every member it documents. The
// text members are untrusted data.
type CodeSecurityConfiguration struct {
	ID                                      int64                          `json:"id"`
	Name                                    string                         `json:"name"`
	TargetType                              *string                        `json:"target_type,omitempty"`
	Description                             *string                        `json:"description,omitempty"`
	AdvancedSecurity                        *string                        `json:"advanced_security,omitempty"`
	DependencyGraph                         *string                        `json:"dependency_graph,omitempty"`
	DependencyGraphAutosubmitAction         *string                        `json:"dependency_graph_autosubmit_action,omitempty"`
	DependencyGraphAutosubmitActionOptions  *CodeSecurityAutosubmitOptions `json:"dependency_graph_autosubmit_action_options,omitempty"`
	DependabotAlerts                        *string                        `json:"dependabot_alerts,omitempty"`
	DependabotReachability                  *string                        `json:"dependabot_reachability,omitempty"`
	DependabotSecurityUpdates               *string                        `json:"dependabot_security_updates,omitempty"`
	DependabotDelegatedAlertDismissal       *string                        `json:"dependabot_delegated_alert_dismissal,omitempty"`
	CodeScanningOptions                     *CodeSecurityScanningOptions   `json:"code_scanning_options,omitempty"`
	CodeScanningDefaultSetup                *string                        `json:"code_scanning_default_setup,omitempty"`
	CodeScanningDefaultSetupOptions         *CodeSecuritySetupOptions      `json:"code_scanning_default_setup_options,omitempty"`
	CodeScanningDelegatedAlertDismissal     *string                        `json:"code_scanning_delegated_alert_dismissal,omitempty"`
	SecretScanning                          *string                        `json:"secret_scanning,omitempty"`
	SecretScanningPushProtection            *string                        `json:"secret_scanning_push_protection,omitempty"`
	SecretScanningDelegatedBypass           *string                        `json:"secret_scanning_delegated_bypass,omitempty"`
	SecretScanningDelegatedBypassOptions    *CodeSecurityBypassOptions     `json:"secret_scanning_delegated_bypass_options,omitempty"`
	SecretScanningValidityChecks            *string                        `json:"secret_scanning_validity_checks,omitempty"`
	SecretScanningNonProviderPatterns       *string                        `json:"secret_scanning_non_provider_patterns,omitempty"`
	SecretScanningGenericSecrets            *string                        `json:"secret_scanning_generic_secrets,omitempty"`
	SecretScanningGenericSecretsPushProtect *string                        `json:"secret_scanning_generic_secrets_push_protection,omitempty"`
	SecretScanningDelegatedAlertDismissal   *string                        `json:"secret_scanning_delegated_alert_dismissal,omitempty"`
	SecretScanningExtendedMetadata          *string                        `json:"secret_scanning_extended_metadata,omitempty"`
	PrivateVulnerabilityReporting           *string                        `json:"private_vulnerability_reporting,omitempty"`
	Enforcement                             *string                        `json:"enforcement,omitempty"`
	URL                                     *string                        `json:"url,omitempty"`
	HTMLURL                                 *string                        `json:"html_url,omitempty"`
	CreatedAt                               *string                        `json:"created_at,omitempty"`
	UpdatedAt                               *string                        `json:"updated_at,omitempty"`
}

// codeSecurityConfigurationRaw is the configuration as GitHub sends it; every member is read as given and
// then cleaned by project.
type codeSecurityConfigurationRaw struct {
	ID                                     *int64  `json:"id"`
	Name                                   *string `json:"name"`
	TargetType                             *string `json:"target_type"`
	Description                            *string `json:"description"`
	AdvancedSecurity                       *string `json:"advanced_security"`
	DependencyGraph                        *string `json:"dependency_graph"`
	DependencyGraphAutosubmitAction        *string `json:"dependency_graph_autosubmit_action"`
	DependencyGraphAutosubmitActionOptions *struct {
		LabeledRunners *bool `json:"labeled_runners"`
	} `json:"dependency_graph_autosubmit_action_options"`
	DependabotAlerts                  *string `json:"dependabot_alerts"`
	DependabotReachability            *string `json:"dependabot_reachability"`
	DependabotSecurityUpdates         *string `json:"dependabot_security_updates"`
	DependabotDelegatedAlertDismissal *string `json:"dependabot_delegated_alert_dismissal"`
	CodeScanningOptions               *struct {
		AllowAdvanced *bool `json:"allow_advanced"`
	} `json:"code_scanning_options"`
	CodeScanningDefaultSetup        *string `json:"code_scanning_default_setup"`
	CodeScanningDefaultSetupOptions *struct {
		RunnerType  *string `json:"runner_type"`
		RunnerLabel *string `json:"runner_label"`
	} `json:"code_scanning_default_setup_options"`
	CodeScanningDelegatedAlertDismissal  *string `json:"code_scanning_delegated_alert_dismissal"`
	SecretScanning                       *string `json:"secret_scanning"`
	SecretScanningPushProtection         *string `json:"secret_scanning_push_protection"`
	SecretScanningDelegatedBypass        *string `json:"secret_scanning_delegated_bypass"`
	SecretScanningDelegatedBypassOptions *struct {
		Reviewers []struct {
			ReviewerID              *int64  `json:"reviewer_id"`
			ReviewerType            *string `json:"reviewer_type"`
			Mode                    *string `json:"mode"`
			SecurityConfigurationID *int64  `json:"security_configuration_id"`
		} `json:"reviewers"`
	} `json:"secret_scanning_delegated_bypass_options"`
	SecretScanningValidityChecks            *string `json:"secret_scanning_validity_checks"`
	SecretScanningNonProviderPatterns       *string `json:"secret_scanning_non_provider_patterns"`
	SecretScanningGenericSecrets            *string `json:"secret_scanning_generic_secrets"`
	SecretScanningGenericSecretsPushProtect *string `json:"secret_scanning_generic_secrets_push_protection"`
	SecretScanningDelegatedAlertDismissal   *string `json:"secret_scanning_delegated_alert_dismissal"`
	SecretScanningExtendedMetadata          *string `json:"secret_scanning_extended_metadata"`
	PrivateVulnerabilityReporting           *string `json:"private_vulnerability_reporting"`
	Enforcement                             *string `json:"enforcement"`
	URL                                     *string `json:"url"`
	HTMLURL                                 *string `json:"html_url"`
	CreatedAt                               *string `json:"created_at"`
	UpdatedAt                               *string `json:"updated_at"`
}

// project cleans one configuration. The state members are short enumerations GitHub documents; an unknown
// value is passed on as a short, cleaned text rather than refused.
func (r *codeSecurityConfigurationRaw) project(op string) (*CodeSecurityConfiguration, error) {
	if r.ID == nil || r.Name == nil || *r.Name == "" {
		return nil, invalidEntry(op, "a code security configuration")
	}
	out := &CodeSecurityConfiguration{ID: *r.ID, Name: *nameText(r.Name), TargetType: shortText(r.TargetType),
		Description: bulkText(r.Description), AdvancedSecurity: shortText(r.AdvancedSecurity),
		DependencyGraph: shortText(r.DependencyGraph), DependencyGraphAutosubmitAction: shortText(r.DependencyGraphAutosubmitAction),
		DependabotAlerts: shortText(r.DependabotAlerts), DependabotReachability: shortText(r.DependabotReachability),
		DependabotSecurityUpdates:               shortText(r.DependabotSecurityUpdates),
		DependabotDelegatedAlertDismissal:       shortText(r.DependabotDelegatedAlertDismissal),
		CodeScanningDefaultSetup:                shortText(r.CodeScanningDefaultSetup),
		CodeScanningDelegatedAlertDismissal:     shortText(r.CodeScanningDelegatedAlertDismissal),
		SecretScanning:                          shortText(r.SecretScanning),
		SecretScanningPushProtection:            shortText(r.SecretScanningPushProtection),
		SecretScanningDelegatedBypass:           shortText(r.SecretScanningDelegatedBypass),
		SecretScanningValidityChecks:            shortText(r.SecretScanningValidityChecks),
		SecretScanningNonProviderPatterns:       shortText(r.SecretScanningNonProviderPatterns),
		SecretScanningGenericSecrets:            shortText(r.SecretScanningGenericSecrets),
		SecretScanningGenericSecretsPushProtect: shortText(r.SecretScanningGenericSecretsPushProtect),
		SecretScanningDelegatedAlertDismissal:   shortText(r.SecretScanningDelegatedAlertDismissal),
		SecretScanningExtendedMetadata:          shortText(r.SecretScanningExtendedMetadata),
		PrivateVulnerabilityReporting:           shortText(r.PrivateVulnerabilityReporting),
		Enforcement:                             shortText(r.Enforcement), URL: address(r.URL), HTMLURL: address(r.HTMLURL),
		CreatedAt: shortText(r.CreatedAt), UpdatedAt: shortText(r.UpdatedAt)}
	if o := r.DependencyGraphAutosubmitActionOptions; o != nil {
		out.DependencyGraphAutosubmitActionOptions = &CodeSecurityAutosubmitOptions{LabeledRunners: o.LabeledRunners}
	}
	if o := r.CodeScanningOptions; o != nil {
		out.CodeScanningOptions = &CodeSecurityScanningOptions{AllowAdvanced: o.AllowAdvanced}
	}
	if o := r.CodeScanningDefaultSetupOptions; o != nil {
		out.CodeScanningDefaultSetupOptions = &CodeSecuritySetupOptions{RunnerType: shortText(o.RunnerType),
			RunnerLabel: shortText(o.RunnerLabel)}
	}
	if o := r.SecretScanningDelegatedBypassOptions; o != nil {
		options := &CodeSecurityBypassOptions{}
		reviewers := o.Reviewers
		if len(reviewers) > projectionListLimit {
			reviewers, options.ReviewersTruncated = reviewers[:projectionListLimit], true
		}
		for _, reviewer := range reviewers {
			options.Reviewers = append(options.Reviewers, CodeSecurityBypassReviewer{ReviewerID: reviewer.ReviewerID,
				ReviewerType: shortText(reviewer.ReviewerType), Mode: shortText(reviewer.Mode),
				SecurityConfigurationID: reviewer.SecurityConfigurationID})
		}
		out.SecretScanningDelegatedBypassOptions = options
	}
	return out, nil
}

// CodeSecurityConfigurationList is one batch of the code security configurations of an organization.
type CodeSecurityConfigurationList struct {
	Configurations []CodeSecurityConfiguration `json:"configurations"`
	NextCursor     string                      `json:"next_cursor,omitempty"`
	HasMore        bool                        `json:"has_more"`
}

// CodeSecurityDefault is the configuration an organization applies to one kind of new repository.
type CodeSecurityDefault struct {
	DefaultForNewRepos *string                    `json:"default_for_new_repos,omitempty"`
	Configuration      *CodeSecurityConfiguration `json:"configuration,omitempty"`
}

// CodeSecurityDefaultList holds the defaults of an organization.
type CodeSecurityDefaultList struct {
	Defaults []CodeSecurityDefault `json:"defaults"`
}

// CodeSecurityConfigurationRepository is one repository a configuration is attached to.
type CodeSecurityConfigurationRepository struct {
	Status     *string         `json:"status,omitempty"`
	Repository *RESTRepository `json:"repository,omitempty"`
}

// CodeSecurityConfigurationRepositoryList is one batch of the repositories of a configuration. Withheld
// counts the repositories of the batch outside the connection's targets, which are not listed.
type CodeSecurityConfigurationRepositoryList struct {
	Repositories []CodeSecurityConfigurationRepository `json:"repositories"`
	Withheld     int                                   `json:"withheld,omitempty"`
	NextCursor   string                                `json:"next_cursor,omitempty"`
	HasMore      bool                                  `json:"has_more"`
}

func (c *Client) codeSecurityPath(rest string) string {
	if rest == "" {
		return c.orgPath("code-security/configurations")
	}
	return c.orgPath("code-security/configurations/" + rest)
}

func (c *Client) listCodeSecurityConfigurations(ctx context.Context, a *codeSecurityArguments) (*CodeSecurityConfigurationList, error) {
	const op = "list code security configurations"
	var raw []codeSecurityConfigurationRaw
	next, err := c.restCursorPage(ctx, op, c.codeSecurityPath(""), a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, codeSecurityConfigurationsReadPermission)
	}
	result := &CodeSecurityConfigurationList{Configurations: make([]CodeSecurityConfiguration, 0, len(raw))}
	for i := range raw {
		configuration, err := raw[i].project(op)
		if err != nil {
			return nil, err
		}
		result.Configurations = append(result.Configurations, *configuration)
	}
	result.HasMore, result.NextCursor = a.more(next)
	return result, nil
}

func (c *Client) getCodeSecurityConfiguration(ctx context.Context, a *codeSecurityArguments) (*CodeSecurityConfiguration, error) {
	const op = "get code security configuration"
	var raw codeSecurityConfigurationRaw
	if err := c.rest(ctx, op, c.codeSecurityPath(strconv.FormatInt(a.ConfigurationID, 10)), &raw); err != nil {
		return nil, actionsFailure(err, codeSecurityConfigurationsReadPermission)
	}
	return raw.project(op)
}

func (c *Client) getCodeSecurityDefaults(ctx context.Context) (*CodeSecurityDefaultList, error) {
	const op = "get code security defaults"
	var raw []struct {
		DefaultForNewRepos *string                       `json:"default_for_new_repos"`
		Configuration      *codeSecurityConfigurationRaw `json:"configuration"`
	}
	if err := c.rest(ctx, op, c.codeSecurityPath("defaults"), &raw); err != nil {
		return nil, actionsFailure(err, codeSecurityConfigurationsReadPermission)
	}
	result := &CodeSecurityDefaultList{Defaults: make([]CodeSecurityDefault, 0, len(raw))}
	for _, entry := range raw {
		item := CodeSecurityDefault{DefaultForNewRepos: shortText(entry.DefaultForNewRepos)}
		if entry.Configuration != nil {
			configuration, err := entry.Configuration.project(op)
			if err != nil {
				return nil, err
			}
			item.Configuration = configuration
		}
		result.Defaults = append(result.Defaults, item)
	}
	return result, nil
}

func (c *Client) listCodeSecurityConfigurationRepositories(ctx context.Context, a *codeSecurityArguments) (*CodeSecurityConfigurationRepositoryList, error) {
	const op = "list code security configuration repositories"
	var raw []struct {
		Status     *string        `json:"status"`
		Repository *repositoryRaw `json:"repository"`
	}
	path := c.codeSecurityPath(strconv.FormatInt(a.ConfigurationID, 10) + "/repositories")
	next, err := c.restCursorPage(ctx, op, path, a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, codeSecurityConfigurationsReadPermission)
	}
	result := &CodeSecurityConfigurationRepositoryList{
		Repositories: make([]CodeSecurityConfigurationRepository, 0, len(raw))}
	for _, entry := range raw {
		if entry.Repository == nil || entry.Repository.ID == nil {
			return nil, invalidEntry(op, "a repository")
		}
		// A repository whose name cannot be checked counts as outside the targets.
		if !entry.Repository.admitted(a.allowed) {
			result.Withheld++
			continue
		}
		result.Repositories = append(result.Repositories, CodeSecurityConfigurationRepository{
			Status: shortText(entry.Status), Repository: entry.Repository.project(a.allowed)})
	}
	result.HasMore, result.NextCursor = a.more(next)
	return result, nil
}

// codeSecuritySubject names what a path below an organization's code-security/configurations addresses: the
// list, the defaults, one configuration, or the repositories of one. It names only a numeric id.
func codeSecuritySubject(rest []string) string {
	if len(rest) == 0 || rest[0] == "" {
		return "the code security configurations"
	}
	if rest[0] == "defaults" {
		return "the default code security configurations"
	}
	id, tail, _ := strings.Cut(rest[0], "/")
	if n, err := strconv.ParseInt(id, 10, 64); err != nil || n < 1 {
		return "the code security configurations"
	}
	if tail == "repositories" {
		return "the repositories of code security configuration " + id
	}
	return "code security configuration " + id
}

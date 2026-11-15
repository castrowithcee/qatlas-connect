package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The change tools create, update, and delete one code security configuration of an organization. They act
// on the configuration object of the organization, not on a repository: an update changes the settings of
// every repository the configuration is attached to, and a delete detaches it from all of them. Both are
// therefore allowed whatever repositories are attached, whether or not they lie inside the connection's
// targets. Each is exactly one request, never repeated, and offered only where a connection's tools list
// names it.

const codeSecurityConfigurationSensitivity = "github-code-security-configuration"

const codeSecurityChangePermission = "GitHub refused this token the change of the code security configurations " +
	"of this organization; changing them needs write:org on a classic token, and the account must be an " +
	"organization owner or security manager; a configuration owned by an enterprise or one of GitHub's own " +
	"cannot be changed"

const (
	codeSecurityDescriptionMax = 255
	codeSecurityReviewersMax   = 100
)

var (
	codeSecurityStateValues       = []string{"enabled", "disabled", "not_set"}
	codeSecurityAdvancedValues    = []string{"enabled", "disabled", "code_security", "secret_protection"}
	codeSecurityEnforcementValues = []string{"enforced", "unenforced"}
	codeSecurityRunnerTypeValues  = []string{"standard", "labeled", "not_set"}
	codeSecurityReviewerTypes     = []string{"TEAM", "ROLE"}
	codeSecurityReviewerModes     = []string{"ALWAYS", "EXEMPT"}
)

// codeSecurityStateSettings are the settings of the three states enabled, disabled, and not_set, in the
// order GitHub documents them, each with what it controls.
var codeSecurityStateSettings = []struct{ name, description string }{
	{"code_security", "GitHub Code Security as a product"},
	{"dependency_graph", "the dependency graph"},
	{"dependency_graph_autosubmit_action", "automatic dependency submission"},
	{"dependabot_alerts", "Dependabot alerts"},
	{"dependabot_reachability", "reachability analysis for Dependabot"},
	{"dependabot_security_updates", "Dependabot security updates"},
	{"dependabot_delegated_alert_dismissal", "delegated dismissal of Dependabot alerts"},
	{"code_scanning_default_setup", "code scanning default setup"},
	{"code_scanning_delegated_alert_dismissal", "delegated dismissal of code scanning alerts"},
	{"secret_protection", "GitHub Secret Protection as a product"},
	{"secret_scanning", "secret scanning"},
	{"secret_scanning_push_protection", "push protection"},
	{"secret_scanning_delegated_bypass", "delegated bypass of push protection"},
	{"secret_scanning_validity_checks", "validity checks"},
	{"secret_scanning_non_provider_patterns", "non-provider patterns"},
	{"secret_scanning_generic_secrets", "Copilot secret scanning"},
	{"secret_scanning_generic_secrets_push_protection", "push protection for generic secrets"},
	{"secret_scanning_delegated_alert_dismissal", "delegated dismissal of secret scanning alerts"},
	{"secret_scanning_extended_metadata", "extended metadata"},
	{"private_vulnerability_reporting", "private vulnerability reporting"},
}

func codeSecurityEnum(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return `{"type":"string","enum":[` + strings.Join(quoted, ",") + `]}`
}

// codeSecuritySettingsProperties is the schema of every setting of create and update; name is added by the
// caller, since only create requires it.
func codeSecuritySettingsProperties() string {
	var b strings.Builder
	b.WriteString(`"description":{"type":"string","maxLength":255},"advanced_security":` +
		codeSecurityEnum(codeSecurityAdvancedValues) + `,"enforcement":` + codeSecurityEnum(codeSecurityEnforcementValues))
	for _, setting := range codeSecurityStateSettings {
		b.WriteString(`,"` + setting.name + `":` + codeSecurityEnum(codeSecurityStateValues))
	}
	b.WriteString(`,"dependency_graph_autosubmit_action_options":{"type":"object","properties":{` +
		`"labeled_runners":{"type":"boolean"}},"additionalProperties":false}`)
	b.WriteString(`,"code_scanning_options":{"type":"object","properties":{"allow_advanced":{"type":"boolean"}},` +
		`"additionalProperties":false}`)
	b.WriteString(`,"code_scanning_default_setup_options":{"type":"object","properties":{"runner_type":` +
		codeSecurityEnum(codeSecurityRunnerTypeValues) + `,"runner_label":{"type":"string"}},"additionalProperties":false}`)
	b.WriteString(`,"secret_scanning_delegated_bypass_options":{"type":"object","properties":{"reviewers":{` +
		`"type":"array","maxItems":100,"items":{"type":"object","properties":{"reviewer_id":` + codeSecurityIDSchema +
		`,"reviewer_type":` + codeSecurityEnum(codeSecurityReviewerTypes) + `,"mode":` + codeSecurityEnum(codeSecurityReviewerModes) +
		`},"required":["reviewer_id","reviewer_type"],"additionalProperties":false}}},"additionalProperties":false}`)
	return b.String()
}

const codeSecurityNameSchema = `{"type":"string","minLength":1}`

func codeSecuritySettingsArguments(nameRequired bool) []capability.Argument {
	arguments := []capability.Argument{
		{Name: "name", Description: "Name of the configuration, unique within the organization", Required: nameRequired},
		{Name: "description", Description: "Description of the configuration, at most 255 characters; an empty text " +
			"clears it on an update"},
		{Name: "advanced_security", Description: "enabled, disabled, code_security, or secret_protection: the GitHub " +
			"Advanced Security products the configuration enables"},
		{Name: "enforcement", Description: "enforced or unenforced: whether repositories may change the settings"},
	}
	for _, setting := range codeSecurityStateSettings {
		arguments = append(arguments, capability.Argument{Name: setting.name,
			Description: "enabled, disabled, or not_set: " + setting.description})
	}
	return append(arguments,
		capability.Argument{Name: "dependency_graph_autosubmit_action_options", Description: "Options of automatic " +
			"dependency submission: labeled_runners, true for runners labeled dependency-submission and false for " +
			"standard runners"},
		capability.Argument{Name: "code_scanning_options", Description: "Options of code scanning: allow_advanced, " +
			"whether repositories with an advanced setup may keep it"},
		capability.Argument{Name: "code_scanning_default_setup_options", Description: "Options of the default setup: " +
			"runner_type (standard, labeled, or not_set) and runner_label, the label of the runner for labeled"},
		capability.Argument{Name: "secret_scanning_delegated_bypass_options", Description: "Options of delegated bypass: " +
			"reviewers, at most 100, each with reviewer_id (a team or role id), reviewer_type (TEAM or ROLE), and " +
			"mode (ALWAYS or EXEMPT, ALWAYS when omitted)"})
}

var codeSecurityConfigurationsCreate = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.create",
	Version: 1,
	Title:   "Create a GitHub code security configuration",
	Description: "Create one new code security configuration in an organization an explicit connection allows, " +
		"with any setting GitHub documents; a setting left out takes GitHub's default; the configuration is not " +
		"attached to any repository and not made a default by this tool; not idempotent, since a repeated call " +
		"is refused for the taken name or, under another name, creates a second configuration; offered only where " +
		"a connection's tools list names it",
	Tags:                  []string{"github", "security", "code-security", "organization", "create"},
	Risk:                  guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, codeSecurityConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"name":`+codeSecurityNameSchema+`,`+codeSecuritySettingsProperties(), "name"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + codeSecurityConfigurationProperties +
		`},` + codeSecurityConfigurationRequired + `}`),
	Arguments: codeSecuritySettingsArguments(true),
	Fields:    codeSecurityConfigurationMembers,
	Examples: []capability.Example{{
		Description: "Create a configuration that enables the dependency graph and Dependabot alerts",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","name":"Baseline","description":"Default set",` +
			`"dependency_graph":"enabled","dependabot_alerts":"enabled","enforcement":"unenforced"}`),
	}},
}

var codeSecurityConfigurationsUpdate = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.update",
	Version: 1,
	Title:   "Update a GitHub code security configuration",
	Description: "Change settings of one code security configuration of an organization an explicit connection " +
		"allows; only the given settings change; the change applies to every repository the configuration is " +
		"attached to, in the whole organization and also to those outside the connection's targets; GitHub " +
		"refuses a configuration of GitHub or of an enterprise; idempotent, since sending the same settings " +
		"again leaves GitHub in the same state; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "code-security", "organization", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeSecurityConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"configuration_id":`+codeSecurityIDSchema+`,"name":`+codeSecurityNameSchema+`,`+
		codeSecuritySettingsProperties(), "configuration_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"updated":{"type":"boolean"},"configuration":{` +
		`"type":"object","properties":{` + codeSecurityConfigurationProperties + `},` + codeSecurityConfigurationRequired +
		`}},"required":["updated"],"additionalProperties":false}`),
	Arguments: append([]capability.Argument{{Name: "configuration_id", Description: "Id of the configuration, as " +
		"github.code-security-configurations.list reports it", Required: true}}, codeSecuritySettingsArguments(false)...),
	Fields: []capability.Field{
		{Name: "updated", Description: "True when GitHub applied the change, false when it reported that there was " +
			"nothing new to apply"},
		{Name: "configuration", Description: "The configuration after the change, with the members of " +
			"github.code-security-configurations.get; absent when nothing changed; untrusted data"},
	},
	Examples: []capability.Example{{
		Description: "Turn on push protection of a configuration",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","configuration_id":1325,"secret_scanning_push_protection":"enabled"}`),
	}},
}

var codeSecurityConfigurationsDelete = capability.Descriptor{
	ID:      Provider + ".code-security-configurations.delete",
	Version: 1,
	Title:   "Delete a GitHub code security configuration",
	Description: "Delete one code security configuration of an organization an explicit connection allows; the " +
		"repositories attached to it, in the whole organization and also those outside the connection's targets, " +
		"keep their settings but are no longer attached to it; GitHub refuses a configuration of GitHub or of an " +
		"enterprise; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "code-security", "organization", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, codeSecurityConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"configuration_id":`+codeSecurityIDSchema, "configuration_id"),
	OutputSchema:          json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean"}},"required":["deleted"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "configuration_id", Description: "Id of the configuration to delete, as " +
		"github.code-security-configurations.list reports it", Required: true}},
	Fields: []capability.Field{{Name: "deleted", Description: "True once GitHub deleted the configuration"}},
	Examples: []capability.Example{{
		Description: "Delete a configuration",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","configuration_id":1325}`),
	}},
}

// codeSecurityChangeToolIDs are the three tools that change a code security configuration.
var codeSecurityChangeToolIDs = []string{codeSecurityConfigurationsCreate.ID, codeSecurityConfigurationsUpdate.ID,
	codeSecurityConfigurationsDelete.ID}

// CodeSecurityBypassReviewerInput is one reviewer of delegated bypass as a request carries it.
type CodeSecurityBypassReviewerInput struct {
	ReviewerID   *int64  `json:"reviewer_id,omitempty"`
	ReviewerType *string `json:"reviewer_type,omitempty"`
	Mode         *string `json:"mode,omitempty"`
}

// CodeSecurityBypassOptionsInput are the delegated bypass options of a request.
type CodeSecurityBypassOptionsInput struct {
	// A pointer keeps an absent list out of the body while an empty one still clears the reviewers.
	Reviewers *[]CodeSecurityBypassReviewerInput `json:"reviewers,omitempty"`
}

// codeSecuritySettings holds every setting create and update take. The pointers tell a setting left out
// from one sent; the struct is also the request body, so a setting reaches GitHub exactly as it was checked.
type codeSecuritySettings struct {
	Name                                    *string                         `json:"name,omitempty"`
	Description                             *string                         `json:"description,omitempty"`
	AdvancedSecurity                        *string                         `json:"advanced_security,omitempty"`
	CodeSecurity                            *string                         `json:"code_security,omitempty"`
	DependencyGraph                         *string                         `json:"dependency_graph,omitempty"`
	DependencyGraphAutosubmitAction         *string                         `json:"dependency_graph_autosubmit_action,omitempty"`
	DependencyGraphAutosubmitActionOptions  *CodeSecurityAutosubmitOptions  `json:"dependency_graph_autosubmit_action_options,omitempty"`
	DependabotAlerts                        *string                         `json:"dependabot_alerts,omitempty"`
	DependabotReachability                  *string                         `json:"dependabot_reachability,omitempty"`
	DependabotSecurityUpdates               *string                         `json:"dependabot_security_updates,omitempty"`
	DependabotDelegatedAlertDismissal       *string                         `json:"dependabot_delegated_alert_dismissal,omitempty"`
	CodeScanningOptions                     *CodeSecurityScanningOptions    `json:"code_scanning_options,omitempty"`
	CodeScanningDefaultSetup                *string                         `json:"code_scanning_default_setup,omitempty"`
	CodeScanningDefaultSetupOptions         *CodeSecuritySetupOptions       `json:"code_scanning_default_setup_options,omitempty"`
	CodeScanningDelegatedAlertDismissal     *string                         `json:"code_scanning_delegated_alert_dismissal,omitempty"`
	SecretProtection                        *string                         `json:"secret_protection,omitempty"`
	SecretScanning                          *string                         `json:"secret_scanning,omitempty"`
	SecretScanningPushProtection            *string                         `json:"secret_scanning_push_protection,omitempty"`
	SecretScanningDelegatedBypass           *string                         `json:"secret_scanning_delegated_bypass,omitempty"`
	SecretScanningDelegatedBypassOptions    *CodeSecurityBypassOptionsInput `json:"secret_scanning_delegated_bypass_options,omitempty"`
	SecretScanningValidityChecks            *string                         `json:"secret_scanning_validity_checks,omitempty"`
	SecretScanningNonProviderPatterns       *string                         `json:"secret_scanning_non_provider_patterns,omitempty"`
	SecretScanningGenericSecrets            *string                         `json:"secret_scanning_generic_secrets,omitempty"`
	SecretScanningGenericSecretsPushProtect *string                         `json:"secret_scanning_generic_secrets_push_protection,omitempty"`
	SecretScanningDelegatedAlertDismissal   *string                         `json:"secret_scanning_delegated_alert_dismissal,omitempty"`
	SecretScanningExtendedMetadata          *string                         `json:"secret_scanning_extended_metadata,omitempty"`
	PrivateVulnerabilityReporting           *string                         `json:"private_vulnerability_reporting,omitempty"`
	Enforcement                             *string                         `json:"enforcement,omitempty"`
}

// stateSettings lists the values of the three-state settings in the order of codeSecurityStateSettings.
func (s *codeSecuritySettings) stateSettings() []*string {
	return []*string{s.CodeSecurity, s.DependencyGraph, s.DependencyGraphAutosubmitAction, s.DependabotAlerts,
		s.DependabotReachability, s.DependabotSecurityUpdates, s.DependabotDelegatedAlertDismissal,
		s.CodeScanningDefaultSetup, s.CodeScanningDelegatedAlertDismissal, s.SecretProtection, s.SecretScanning,
		s.SecretScanningPushProtection, s.SecretScanningDelegatedBypass, s.SecretScanningValidityChecks,
		s.SecretScanningNonProviderPatterns, s.SecretScanningGenericSecrets,
		s.SecretScanningGenericSecretsPushProtect, s.SecretScanningDelegatedAlertDismissal,
		s.SecretScanningExtendedMetadata, s.PrivateVulnerabilityReporting}
}

func absentOrOneOf(value *string, allowed []string) bool {
	if value == nil {
		return true
	}
	for _, candidate := range allowed {
		if *value == candidate {
			return true
		}
	}
	return false
}

// check validates every enumeration and bound before any request. Values are matched exactly, as GitHub
// documents them.
func (s *codeSecuritySettings) check() error {
	if s.Name != nil && strings.TrimSpace(*s.Name) == "" {
		return invalidRequest("name must not be empty")
	}
	if s.Description != nil && utf8.RuneCountInString(*s.Description) > codeSecurityDescriptionMax {
		return invalidRequest("description must be at most 255 characters")
	}
	if !absentOrOneOf(s.AdvancedSecurity, codeSecurityAdvancedValues) {
		return invalidRequest("advanced_security must be enabled, disabled, code_security, or secret_protection")
	}
	if !absentOrOneOf(s.Enforcement, codeSecurityEnforcementValues) {
		return invalidRequest("enforcement must be enforced or unenforced")
	}
	for i, value := range s.stateSettings() {
		if !absentOrOneOf(value, codeSecurityStateValues) {
			return invalidRequest(codeSecurityStateSettings[i].name + " must be enabled, disabled, or not_set")
		}
	}
	if o := s.CodeScanningDefaultSetupOptions; o != nil && !absentOrOneOf(o.RunnerType, codeSecurityRunnerTypeValues) {
		return invalidRequest("runner_type must be standard, labeled, or not_set")
	}
	if o := s.SecretScanningDelegatedBypassOptions; o != nil && o.Reviewers != nil {
		if len(*o.Reviewers) > codeSecurityReviewersMax {
			return invalidRequest("reviewers must list at most 100 reviewers")
		}
		for _, reviewer := range *o.Reviewers {
			if reviewer.ReviewerID == nil || *reviewer.ReviewerID < 1 || reviewer.ReviewerType == nil {
				return invalidRequest("each reviewer needs a positive reviewer_id and a reviewer_type")
			}
			if !absentOrOneOf(reviewer.ReviewerType, codeSecurityReviewerTypes) {
				return invalidRequest("reviewer_type must be TEAM or ROLE")
			}
			if !absentOrOneOf(reviewer.Mode, codeSecurityReviewerModes) {
				return invalidRequest("mode must be ALWAYS or EXEMPT")
			}
		}
	}
	return nil
}

// empty reports whether no setting was given.
func (s *codeSecuritySettings) empty() bool {
	body, err := json.Marshal(s)
	return err != nil || string(body) == "{}"
}

// codeSecurityChange holds the arguments of the three change tools.
type codeSecurityChange struct {
	ConfigurationID int64 `json:"configuration_id"`
	codeSecuritySettings
}

// CodeSecurityConfigurationUpdated is the answer to an update. GitHub answers 204 without a body when
// nothing new was applied; Configuration is then absent.
type CodeSecurityConfigurationUpdated struct {
	Updated       bool                       `json:"updated"`
	Configuration *CodeSecurityConfiguration `json:"configuration,omitempty"`
}

// CodeSecurityConfigurationDeleted is the answer to a deleted configuration.
type CodeSecurityConfigurationDeleted struct {
	Deleted bool `json:"deleted"`
}

func codeSecurityChangeOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*codeSecurityChange) error,
		call func(context.Context, *Client, *codeSecurityChange) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: codeSecurityChangeHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(codeSecurityConfigurationsCreate, func(a *codeSecurityChange) error {
			if a.Name == nil {
				return invalidRequest("name is required")
			}
			return a.check()
		}, func(ctx context.Context, c *Client, a *codeSecurityChange) (any, error) {
			return c.createCodeSecurityConfiguration(ctx, a)
		}),
		bind(codeSecurityConfigurationsUpdate, func(a *codeSecurityChange) error {
			if err := checkCodeSecurityConfigurationID(&codeSecurityArguments{ConfigurationID: a.ConfigurationID}); err != nil {
				return err
			}
			if a.empty() {
				return invalidRequest("at least one setting must be given")
			}
			return a.check()
		}, func(ctx context.Context, c *Client, a *codeSecurityChange) (any, error) {
			return c.updateCodeSecurityConfiguration(ctx, a)
		}),
		bind(codeSecurityConfigurationsDelete, func(a *codeSecurityChange) error {
			return checkCodeSecurityConfigurationID(&codeSecurityArguments{ConfigurationID: a.ConfigurationID})
		}, func(ctx context.Context, c *Client, a *codeSecurityChange) (any, error) {
			return c.deleteCodeSecurityConfiguration(ctx, a)
		}),
	}
}

// codeSecurityChangeHandler decodes and checks the arguments and the organization before a credential is
// resolved. The configuration id is addressed only below the organization in the path, so an id of another
// organization is not found there.
func codeSecurityChangeHandler(id string, check func(*codeSecurityChange) error,
	call func(context.Context, *Client, *codeSecurityChange) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments codeSecurityChange
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

func (c *Client) createCodeSecurityConfiguration(ctx context.Context, a *codeSecurityChange) (*CodeSecurityConfiguration, error) {
	const op = "create code security configuration"
	var raw codeSecurityConfigurationRaw
	if err := c.restChange(ctx, op, http.MethodPost, c.codeSecurityPath(""), &a.codeSecuritySettings, &raw); err != nil {
		return nil, actionsFailure(err, codeSecurityChangePermission)
	}
	return raw.project(op)
}

func (c *Client) updateCodeSecurityConfiguration(ctx context.Context, a *codeSecurityChange) (*CodeSecurityConfigurationUpdated, error) {
	const op = "update code security configuration"
	var raw codeSecurityConfigurationRaw
	status, err := c.restChangeStatus(ctx, op, http.MethodPatch,
		c.codeSecurityPath(strconv.FormatInt(a.ConfigurationID, 10)), &a.codeSecuritySettings, &raw)
	if err != nil {
		return nil, actionsFailure(err, codeSecurityChangePermission)
	}
	if status == http.StatusNoContent {
		return &CodeSecurityConfigurationUpdated{}, nil
	}
	configuration, err := raw.project(op)
	if err != nil {
		return nil, err
	}
	return &CodeSecurityConfigurationUpdated{Updated: true, Configuration: configuration}, nil
}

func (c *Client) deleteCodeSecurityConfiguration(ctx context.Context, a *codeSecurityChange) (*CodeSecurityConfigurationDeleted, error) {
	const op = "delete code security configuration"
	if err := c.restChange(ctx, op, http.MethodDelete, c.codeSecurityPath(strconv.FormatInt(a.ConfigurationID, 10)),
		struct{}{}, nil); err != nil {
		return nil, actionsFailure(err, codeSecurityChangePermission)
	}
	return &CodeSecurityConfigurationDeleted{Deleted: true}, nil
}

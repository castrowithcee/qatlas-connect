package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The security switches of a repository: Dependabot alerts and security updates, private vulnerability
// reporting, immutable releases, and the secret scanning members of security_and_analysis. The read bundles
// the repository read and four route reads, the change sends exactly one request per switch. A switch GitHub
// does not offer for the repository or the plan is reported as unavailable, never as off.

// securitySettingSensitivity is the data sensitivity of the security settings tools.
const securitySettingSensitivity = "github-security-settings"

// The states of a switch.
const (
	stateEnabled     = "enabled"
	stateDisabled    = "disabled"
	stateUnavailable = "unavailable"
)

// analysisFeatures are the members of security_and_analysis the read reports. advanced_security and
// code_security are read-only here: changing them changes what the organization is billed for.
var analysisFeatures = []string{"advanced_security", "code_security", "dependabot_security_updates",
	"secret_scanning", "secret_scanning_push_protection", "secret_scanning_non_provider_patterns",
	"secret_scanning_ai_detection", "secret_scanning_delegated_alert_dismissal", "secret_scanning_delegated_bypass"}

// securitySwitch is one switch the change tool sets: a PUT or DELETE route below the repository, or a
// member of security_and_analysis sent through the repository PATCH.
type securitySwitch struct {
	name    string
	route   string
	feature string
	given   func(*securityChangeArguments) *bool
}

// securitySwitches are the switches in the order the change sends them.
var securitySwitches = []securitySwitch{
	{name: "dependabot_alerts", route: "vulnerability-alerts",
		given: func(a *securityChangeArguments) *bool { return a.DependabotAlerts }},
	{name: "automated_security_fixes", route: "automated-security-fixes",
		given: func(a *securityChangeArguments) *bool { return a.AutomatedSecurityFixes }},
	{name: "private_vulnerability_reporting", route: "private-vulnerability-reporting",
		given: func(a *securityChangeArguments) *bool { return a.PrivateVulnerabilityReporting }},
	{name: "immutable_releases", route: "immutable-releases",
		given: func(a *securityChangeArguments) *bool { return a.ImmutableReleases }},
	{name: "secret_scanning", feature: "secret_scanning",
		given: func(a *securityChangeArguments) *bool { return a.SecretScanning }},
	{name: "secret_scanning_push_protection", feature: "secret_scanning_push_protection",
		given: func(a *securityChangeArguments) *bool { return a.SecretScanningPushProtection }},
	{name: "secret_scanning_non_provider_patterns", feature: "secret_scanning_non_provider_patterns",
		given: func(a *securityChangeArguments) *bool { return a.SecretScanningNonProviderPatterns }},
	{name: "secret_scanning_ai_detection", feature: "secret_scanning_ai_detection",
		given: func(a *securityChangeArguments) *bool { return a.SecretScanningAIDetection }},
	{name: "secret_scanning_delegated_alert_dismissal", feature: "secret_scanning_delegated_alert_dismissal",
		given: func(a *securityChangeArguments) *bool { return a.SecretScanningDelegatedAlertDismissal }},
	{name: "secret_scanning_delegated_bypass", feature: "secret_scanning_delegated_bypass",
		given: func(a *securityChangeArguments) *bool { return a.SecretScanningDelegatedBypass }},
}

// securityChangeArguments are the arguments of github.security-settings.update. A boolean is a pointer, so a
// switch left out stays apart from false.
type securityChangeArguments struct {
	DependabotAlerts                      *bool `json:"dependabot_alerts"`
	AutomatedSecurityFixes                *bool `json:"automated_security_fixes"`
	PrivateVulnerabilityReporting         *bool `json:"private_vulnerability_reporting"`
	ImmutableReleases                     *bool `json:"immutable_releases"`
	SecretScanning                        *bool `json:"secret_scanning"`
	SecretScanningPushProtection          *bool `json:"secret_scanning_push_protection"`
	SecretScanningNonProviderPatterns     *bool `json:"secret_scanning_non_provider_patterns"`
	SecretScanningAIDetection             *bool `json:"secret_scanning_ai_detection"`
	SecretScanningDelegatedAlertDismissal *bool `json:"secret_scanning_delegated_alert_dismissal"`
	SecretScanningDelegatedBypass         *bool `json:"secret_scanning_delegated_bypass"`
}

func checkSecurityChange(a *securityChangeArguments, _ target) error {
	for _, one := range securitySwitches {
		if one.given(a) != nil {
			return nil
		}
	}
	return invalidRequest("give at least one security switch")
}

const switchStateSchema = `{"type":"string","enum":["enabled","disabled","unavailable"]}`

func switchSchema(extra string) string {
	return `{"type":"object","properties":{"state":` + switchStateSchema + extra +
		`},"required":["state"],"additionalProperties":false}`
}

func securitySettingsGetProperties() string {
	properties := []string{`"dependabot_alerts":` + switchSchema(""),
		`"automated_security_fixes":` + switchSchema(`,"paused":{"type":"boolean"}`),
		`"private_vulnerability_reporting":` + switchSchema(""),
		`"immutable_releases":` + switchSchema(`,"enforced_by_owner":{"type":"boolean"}`)}
	for _, name := range analysisFeatures {
		properties = append(properties, `"`+name+`":`+switchSchema(""))
	}
	return strings.Join(properties, ",") + `,"secret_scanning_delegated_bypass_reviewers":{"type":"array",` +
		`"items":{"type":"object","properties":{"reviewer_id":{"type":"integer"},"reviewer_type":{"type":"string"},` +
		`"mode":{"type":"string"}},"required":["reviewer_id","reviewer_type","mode"],"additionalProperties":false}}`
}

func securitySettingsGetRequired() string {
	required := []string{`"dependabot_alerts"`, `"automated_security_fixes"`, `"private_vulnerability_reporting"`,
		`"immutable_releases"`}
	for _, name := range analysisFeatures {
		required = append(required, `"`+name+`"`)
	}
	return strings.Join(required, ",")
}

func securitySettingsGetFields() []capability.Field {
	fields := []capability.Field{
		{Name: "dependabot_alerts", Description: "state is enabled, or disabled where GitHub answers 404 on the alerts route; " +
			"without administrator rights shown by the repository read, that 404 fails the read"},
		{Name: "automated_security_fixes", Description: "state, and paused while Dependabot security updates are enabled; " +
			"disabled where GitHub answers 404 and the repository read shows administrator rights"},
		{Name: "private_vulnerability_reporting", Description: "state; unavailable where GitHub refuses the route with 422"},
		{Name: "immutable_releases", Description: "state and enforced_by_owner; disabled where GitHub answers 404 and the repository read shows administrator rights"},
	}
	for _, name := range analysisFeatures {
		fields = append(fields, capability.Field{Name: name, Description: "state from security_and_analysis; " +
			"unavailable where GitHub leaves the member out, as it does for a plan without the feature and for a token " +
			"without administrator rights"})
	}
	return append(fields, capability.Field{Name: "secret_scanning_delegated_bypass_reviewers",
		Description: "Reviewers of delegated bypass as reviewer_id, reviewer_type TEAM or ROLE, and mode ALWAYS or " +
			"EXEMPT; absent when GitHub reports none"})
}

var securitySettingsGet = capability.Descriptor{
	ID:      Provider + ".security-settings.get",
	Version: 1,
	Title:   "Get the security settings of a GitHub repository",
	Description: "Read whether Dependabot alerts, Dependabot security updates, private vulnerability reporting, " +
		"immutable releases, and the security_and_analysis features such as secret scanning and push protection " +
		"are on for a repository a connection allows; a switch GitHub does not offer for the repository is " +
		"reported as unavailable, not as disabled; reads the repository and four more routes",
	Tags:         []string{"github", "security", "settings", "dependabot", "secret-scanning", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(""),
	OutputSchema: settingsOutput(securitySettingsGetProperties(), securitySettingsGetRequired()),
	Fields:       securitySettingsGetFields(),
	Examples:     []capability.Example{{Description: "Read the security switches", Arguments: json.RawMessage(`{}`)}},
}

var securitySettingsUpdate = capability.Descriptor{
	ID:      Provider + ".security-settings.update",
	Version: 1,
	Title:   "Change the security settings of a GitHub repository",
	Description: "Switch Dependabot alerts, Dependabot security updates, private vulnerability reporting, " +
		"immutable releases, secret scanning, push protection, non-provider patterns, AI detection, delegated " +
		"alert dismissal, and delegated bypass on or off for a repository a connection allows; each switch is " +
		"one request that is never repeated, the result names every switch as set, rejected, unknown, or " +
		"not-sent, and no switch follows an unclear outcome; switching secret scanning, push protection, or " +
		"Dependabot alerts off lowers the protection of the repository; Advanced Security, Code Security, and " +
		"the delegated bypass reviewers are never changed; offered only where the connection lists it",
	Tags:                  []string{"github", "security", "settings", "dependabot", "secret-scanning", "update", "admin"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, securitySettingSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(securitySwitchProperties()),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"results":{"type":"array","items":{"type":"object",` +
		`"properties":{"switch":{"type":"string"},"requested":{"type":"string","enum":["enabled","disabled"]},` +
		`"outcome":{"type":"string","enum":["set","rejected","unknown","not-sent"]},` +
		`"reason":{"type":"string"},"reported":{"type":"string"}},` +
		`"required":["switch","requested","outcome"],"additionalProperties":false}}},` +
		`"required":["results"],"additionalProperties":false}`),
	Arguments: securitySwitchArguments(),
	Fields: []capability.Field{{Name: "results", Description: "One entry per given switch in the order of the " +
		"arguments below: switch, requested, outcome (set when GitHub accepted the request; rejected when GitHub " +
		"refused it, with reason permission, not-found, conflict, invalid, auth, rate-limited, or refused; unknown " +
		"when the outcome is unclear, read the settings before repeating; not-sent after an unclear outcome), and " +
		"reported, the state GitHub's answer names for the switch where it does"}},
	Examples: []capability.Example{{
		Description: "Turn on secret scanning and push protection",
		Arguments:   json.RawMessage(`{"secret_scanning":true,"secret_scanning_push_protection":true}`),
	}},
}

func securitySwitchProperties() string {
	properties := make([]string, len(securitySwitches))
	for i, one := range securitySwitches {
		properties[i] = `"` + one.name + `":{"type":"boolean"}`
	}
	return strings.Join(properties, ",")
}

func securitySwitchArguments() []capability.Argument {
	arguments := make([]capability.Argument, len(securitySwitches))
	for i, one := range securitySwitches {
		arguments[i] = capability.Argument{Name: one.name, Description: "true to switch on, false to switch off"}
	}
	return arguments
}

// Messages of the security settings tools; GitHub decides on every request.
const (
	securityReadPermission = "GitHub refused this token the security settings of this repository or does not " +
		"show the repository to it; reading them needs repo on a classic token for a private repository, and " +
		"Administration: read on a fine-grained token"
	securityChangePermission = "GitHub refused this change of the security settings or does not show the " +
		"repository to this token; it needs the admin role on the repository with repo on a classic token, or " +
		"Administration: write on a fine-grained token, and an organization policy may forbid it as well"
)

// SwitchState is the state of one security switch.
type SwitchState struct {
	State string `json:"state"`
}

// AutomatedSecurityFixes is the state of Dependabot security updates.
type AutomatedSecurityFixes struct {
	State  string `json:"state"`
	Paused *bool  `json:"paused,omitempty"`
}

// ImmutableReleases is the state of immutable releases.
type ImmutableReleases struct {
	State           string `json:"state"`
	EnforcedByOwner *bool  `json:"enforced_by_owner,omitempty"`
}

// BypassReviewer is one reviewer of secret scanning delegated bypass.
type BypassReviewer struct {
	ReviewerID   int64  `json:"reviewer_id"`
	ReviewerType string `json:"reviewer_type"`
	Mode         string `json:"mode"`
}

// SecuritySettings are the security switches of the bound repository.
type SecuritySettings struct {
	DependabotAlerts                      SwitchState            `json:"dependabot_alerts"`
	AutomatedSecurityFixes                AutomatedSecurityFixes `json:"automated_security_fixes"`
	PrivateVulnerabilityReporting         SwitchState            `json:"private_vulnerability_reporting"`
	ImmutableReleases                     ImmutableReleases      `json:"immutable_releases"`
	AdvancedSecurity                      SwitchState            `json:"advanced_security"`
	CodeSecurity                          SwitchState            `json:"code_security"`
	DependabotSecurityUpdates             SwitchState            `json:"dependabot_security_updates"`
	SecretScanning                        SwitchState            `json:"secret_scanning"`
	SecretScanningPushProtection          SwitchState            `json:"secret_scanning_push_protection"`
	SecretScanningNonProviderPatterns     SwitchState            `json:"secret_scanning_non_provider_patterns"`
	SecretScanningAIDetection             SwitchState            `json:"secret_scanning_ai_detection"`
	SecretScanningDelegatedAlertDismissal SwitchState            `json:"secret_scanning_delegated_alert_dismissal"`
	SecretScanningDelegatedBypass         SwitchState            `json:"secret_scanning_delegated_bypass"`
	DelegatedBypassReviewers              []BypassReviewer       `json:"secret_scanning_delegated_bypass_reviewers,omitempty"`
}

// featureStatus is one member of security_and_analysis; nothing else of it is read.
type featureStatus struct {
	Status string `json:"status"`
}

// analysisAnswer is the part of a repository GitHub's answer is decoded into.
type analysisAnswer struct {
	Permissions *struct {
		Admin bool `json:"admin"`
	} `json:"permissions"`
	SecurityAndAnalysis *struct {
		AdvancedSecurity                      *featureStatus `json:"advanced_security"`
		CodeSecurity                          *featureStatus `json:"code_security"`
		DependabotSecurityUpdates             *featureStatus `json:"dependabot_security_updates"`
		SecretScanning                        *featureStatus `json:"secret_scanning"`
		SecretScanningPushProtection          *featureStatus `json:"secret_scanning_push_protection"`
		SecretScanningNonProviderPatterns     *featureStatus `json:"secret_scanning_non_provider_patterns"`
		SecretScanningAIDetection             *featureStatus `json:"secret_scanning_ai_detection"`
		SecretScanningDelegatedAlertDismissal *featureStatus `json:"secret_scanning_delegated_alert_dismissal"`
		SecretScanningDelegatedBypass         *featureStatus `json:"secret_scanning_delegated_bypass"`
		DelegatedBypassOptions                *struct {
			Reviewers []struct {
				ReviewerID   *int64 `json:"reviewer_id"`
				ReviewerType string `json:"reviewer_type"`
				Mode         string `json:"mode"`
			} `json:"reviewers"`
		} `json:"secret_scanning_delegated_bypass_options"`
	} `json:"security_and_analysis"`
}

// admin reports whether GitHub showed the token administrator rights on the repository.
func (a *analysisAnswer) admin() bool { return a.Permissions != nil && a.Permissions.Admin }

// adminNotProven fails a read whose 404 cannot be told from missing rights.
func adminNotProven() error {
	return &provider.Error{Class: provider.ClassPermission, Op: "get security settings", Message: securityReadPermission +
		"; GitHub did not show administrator rights on the repository, so an answer of 404 for a switch cannot be " +
		"told from a switch that is off"}
}

// feature returns the member of security_and_analysis the switch name stands for, nil when GitHub left it out.
func (a *analysisAnswer) feature(name string) *featureStatus {
	s := a.SecurityAndAnalysis
	if s == nil {
		return nil
	}
	return map[string]*featureStatus{"advanced_security": s.AdvancedSecurity, "code_security": s.CodeSecurity,
		"dependabot_security_updates": s.DependabotSecurityUpdates, "secret_scanning": s.SecretScanning,
		"secret_scanning_push_protection":           s.SecretScanningPushProtection,
		"secret_scanning_non_provider_patterns":     s.SecretScanningNonProviderPatterns,
		"secret_scanning_ai_detection":              s.SecretScanningAIDetection,
		"secret_scanning_delegated_alert_dismissal": s.SecretScanningDelegatedAlertDismissal,
		"secret_scanning_delegated_bypass":          s.SecretScanningDelegatedBypass}[name]
}

// state names a member's status; a status other than enabled or disabled is no state Qatlas knows.
func (f *featureStatus) state() (string, bool) {
	switch {
	case f == nil:
		return stateUnavailable, true
	case f.Status == stateEnabled || f.Status == stateDisabled:
		return f.Status, true
	}
	return "", false
}

// statusOf is the HTTP status of a failed GitHub request, 0 when the failure has none.
func statusOf(err error) int {
	var failure *provider.Error
	if errors.As(err, &failure) && failure.Original != nil {
		return failure.Original.Status
	}
	return 0
}

// securityFailure names the permission a refused or unseen repository needs, without any text of GitHub's.
func securityFailure(err error, message string) error {
	var failure *provider.Error
	if errors.As(err, &failure) && (failure.Class == provider.ClassPermission || failure.Class == provider.ClassNotFound) {
		refused := *failure
		refused.Message = message
		return &refused
	}
	return err
}

// routeRead reads one switch route. It reports false when GitHub answers one of the statuses that route uses
// to say the switch is off or not offered, and fails on every other refusal.
func (c *Client) routeRead(ctx context.Context, op, route string, out any, absent ...int) (bool, error) {
	err := c.do(ctx, op, http.MethodGet, c.endpoints.rest+c.repoPath(route), nil, out, false, nil, nil)
	if err == nil {
		return true, nil
	}
	status := statusOf(err)
	for _, want := range absent {
		if status == want {
			return false, nil
		}
	}
	return false, securityFailure(err, securityReadPermission)
}

func (c *Client) securitySettings(ctx context.Context) (*SecuritySettings, error) {
	const op = "get security settings"
	var repository analysisAnswer
	if err := c.rest(ctx, op, strings.TrimSuffix(c.repoPath(""), "/"), &repository); err != nil {
		return nil, securityFailure(err, securityReadPermission)
	}
	result := &SecuritySettings{}
	states := map[string]*SwitchState{"advanced_security": &result.AdvancedSecurity,
		"code_security": &result.CodeSecurity, "dependabot_security_updates": &result.DependabotSecurityUpdates,
		"secret_scanning":                           &result.SecretScanning,
		"secret_scanning_push_protection":           &result.SecretScanningPushProtection,
		"secret_scanning_non_provider_patterns":     &result.SecretScanningNonProviderPatterns,
		"secret_scanning_ai_detection":              &result.SecretScanningAIDetection,
		"secret_scanning_delegated_alert_dismissal": &result.SecretScanningDelegatedAlertDismissal,
		"secret_scanning_delegated_bypass":          &result.SecretScanningDelegatedBypass}
	for _, name := range analysisFeatures {
		state, ok := repository.feature(name).state()
		if !ok {
			return nil, invalidResponse(op, false)
		}
		states[name].State = state
	}
	if options := repository.SecurityAndAnalysis; options != nil && options.DelegatedBypassOptions != nil {
		for _, reviewer := range options.DelegatedBypassOptions.Reviewers {
			if reviewer.ReviewerID == nil || (reviewer.ReviewerType != "TEAM" && reviewer.ReviewerType != "ROLE") ||
				(reviewer.Mode != "ALWAYS" && reviewer.Mode != "EXEMPT") {
				return nil, invalidResponse(op, false)
			}
			result.DelegatedBypassReviewers = append(result.DelegatedBypassReviewers,
				BypassReviewer{ReviewerID: *reviewer.ReviewerID, ReviewerType: reviewer.ReviewerType, Mode: reviewer.Mode})
		}
	}

	// GitHub answers 204 for enabled alerts and 404 for disabled ones.
	enabled, err := c.routeRead(ctx, op, "vulnerability-alerts", nil, http.StatusNotFound)
	if err != nil {
		return nil, err
	}
	result.DependabotAlerts.State = stateDisabled
	if !enabled && !repository.admin() {
		return nil, adminNotProven()
	}
	if enabled {
		result.DependabotAlerts.State = stateEnabled
	}

	// GitHub documents 404 as not enabled; it is also the answer to a token without administrator rights, so
	// the repository read must have shown those rights first.
	var fixes struct {
		Enabled *bool `json:"enabled"`
		Paused  *bool `json:"paused"`
	}
	present, err := c.routeRead(ctx, op, "automated-security-fixes", &fixes, http.StatusNotFound)
	switch {
	case err != nil:
		return nil, err
	case !present && !repository.admin():
		return nil, adminNotProven()
	case !present:
		result.AutomatedSecurityFixes.State = stateDisabled
	case fixes.Enabled == nil:
		return nil, invalidResponse(op, false)
	default:
		result.AutomatedSecurityFixes = AutomatedSecurityFixes{State: stateDisabled, Paused: fixes.Paused}
		if *fixes.Enabled {
			result.AutomatedSecurityFixes.State = stateEnabled
		}
	}

	var reporting struct {
		Enabled *bool `json:"enabled"`
	}
	present, err = c.routeRead(ctx, op, "private-vulnerability-reporting", &reporting, http.StatusUnprocessableEntity)
	switch {
	case err != nil:
		return nil, err
	case !present:
		result.PrivateVulnerabilityReporting.State = stateUnavailable
	case reporting.Enabled == nil:
		return nil, invalidResponse(op, false)
	default:
		result.PrivateVulnerabilityReporting.State = stateDisabled
		if *reporting.Enabled {
			result.PrivateVulnerabilityReporting.State = stateEnabled
		}
	}

	var immutable struct {
		Enabled         *bool `json:"enabled"`
		EnforcedByOwner *bool `json:"enforced_by_owner"`
	}
	present, err = c.routeRead(ctx, op, "immutable-releases", &immutable, http.StatusNotFound)
	switch {
	case err != nil:
		return nil, err
	case !present && !repository.admin():
		return nil, adminNotProven()
	case !present:
		result.ImmutableReleases.State = stateDisabled
	case immutable.Enabled == nil:
		return nil, invalidResponse(op, false)
	default:
		result.ImmutableReleases = ImmutableReleases{State: stateDisabled, EnforcedByOwner: immutable.EnforcedByOwner}
		if *immutable.Enabled {
			result.ImmutableReleases.State = stateEnabled
		}
	}
	return result, nil
}

// SwitchResult is the outcome of one switch of a security settings change.
type SwitchResult struct {
	Switch    string `json:"switch"`
	Requested string `json:"requested"`
	Outcome   string `json:"outcome"`
	Reason    string `json:"reason,omitempty"`
	Reported  string `json:"reported,omitempty"`
}

// SecuritySettingsChange is what security-settings.update reports: every given switch with its outcome.
type SecuritySettingsChange struct {
	Results []SwitchResult `json:"results"`
}

// settingFailure sorts a failed switch request. A refusal that proves the request was not applied is
// rejected, and the next switch may follow unless the refusal says every further request fails alike. An
// outcome that may include an applied change is unknown and ends the call.
func settingFailure(err error) (outcome, reason string, stop bool) {
	var failure *provider.Error
	if !errors.As(err, &failure) {
		return "unknown", "", true
	}
	status := statusOf(err)
	switch {
	case failure.MayHaveArrived() || failure.Class == provider.ClassTimeout ||
		failure.Class == provider.ClassInvalidResponse || status >= 500:
		return "unknown", "", true
	case failure.Class == provider.ClassPermission:
		return "rejected", "permission", false
	case failure.Class == provider.ClassNotFound:
		return "rejected", "not-found", false
	case status == http.StatusConflict:
		return "rejected", "conflict", false
	case status == http.StatusBadRequest || status == http.StatusUnprocessableEntity:
		return "rejected", "invalid", false
	case failure.Class == provider.ClassAuth:
		return "rejected", "auth", true
	case failure.Class == provider.ClassRateLimited:
		return "rejected", "rate-limited", true
	}
	return "rejected", "refused", true
}

// sendSwitch sends the one request of a switch and reports its outcome, whether the call ends there, and the
// failure behind a rejected or unknown outcome.
func (c *Client) sendSwitch(ctx context.Context, op string, one securitySwitch, enable bool) (SwitchResult, bool, error) {
	result := SwitchResult{Switch: one.name, Requested: stateDisabled}
	if enable {
		result.Requested = stateEnabled
	}
	var err error
	if one.route != "" {
		method := http.MethodDelete
		if enable {
			method = http.MethodPut
		}
		err = c.do(ctx, op, method, c.endpoints.rest+c.repoPath(one.route), nil, nil, true, nil, nil)
	} else {
		body := map[string]any{"security_and_analysis": map[string]any{one.feature: map[string]string{
			"status": result.Requested}}}
		var answer analysisAnswer
		if _, err = c.restChangeStatus(ctx, op, http.MethodPatch, strings.TrimSuffix(c.repoPath(""), "/"), body,
			&answer); err == nil {
			// The answer names what GitHub holds now; a state other than the requested one is no success.
			if reported, ok := answer.feature(one.feature).state(); !ok {
				err = invalidResponse(op, true)
			} else if reported != stateUnavailable {
				result.Reported = reported
				if reported != result.Requested {
					result.Outcome = "unknown"
					return result, true, nil
				}
			}
		}
	}
	if err != nil {
		outcome, reason, stop := settingFailure(err)
		result.Outcome, result.Reason = outcome, reason
		return result, stop, err
	}
	result.Outcome = "set"
	return result, false, nil
}

// updateSecuritySettings sends each given switch in one request that is never repeated. A clear refusal
// leaves the next switch to be sent, an unclear outcome or a refusal that would hit every switch ends the
// call and the rest are reported as not sent. When no switch was set the call fails.
func (c *Client) updateSecuritySettings(ctx context.Context, a *securityChangeArguments) (*SecuritySettingsChange, error) {
	const op = "update security settings"
	change := &SecuritySettingsChange{}
	var first error
	stopped, set, unknown := false, false, false
	for _, one := range securitySwitches {
		want := one.given(a)
		if want == nil {
			continue
		}
		if stopped {
			requested := stateDisabled
			if *want {
				requested = stateEnabled
			}
			change.Results = append(change.Results, SwitchResult{Switch: one.name, Requested: requested,
				Outcome: "not-sent"})
			continue
		}
		result, stop, err := c.sendSwitch(ctx, op, one, *want)
		change.Results = append(change.Results, result)
		switch result.Outcome {
		case "set":
			set = true
		case "unknown":
			unknown = true
		}
		if err != nil && first == nil {
			first = err
		}
		if result.Outcome == "unknown" && first == nil {
			first = invalidResponse(op, true)
		}
		stopped = stop
	}
	if set {
		return change, nil
	}
	return nil, noSwitchSet(op, change, first, unknown)
}

// noSwitchSet is the failure of a change that set nothing. It names every switch and its outcome, and keeps
// the class of the first failure; no text of GitHub's enters it.
func noSwitchSet(op string, change *SecuritySettingsChange, first error, unknown bool) error {
	parts := make([]string, len(change.Results))
	permission := false
	for i, result := range change.Results {
		parts[i] = result.Switch + " " + result.Outcome
		if result.Reason != "" {
			parts[i] += " (" + result.Reason + ")"
		}
		permission = permission || result.Reason == "permission" || result.Reason == "not-found"
	}
	message := "no security switch was changed: " + strings.Join(parts, ", ")
	if permission {
		message += "; " + securityChangePermission
	}
	if unknown {
		message += uncertain
	}
	failure := &provider.Error{Class: provider.ClassProviderError, Op: op, Message: message}
	var original *provider.Error
	if errors.As(first, &original) {
		failure.Class, failure.Cause, failure.Original = original.Class, original.Cause, original.Original
	}
	return failure
}

// securityChangeHandler checks the arguments and the repository before a credential is resolved.
func securityChangeHandler(id string) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments securityChangeArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if err := checkSecurityChange(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(client.updateSecuritySettings(ctx, &arguments))
	}
}

// securitySettingsOperations binds the security settings and code scanning default setup tools.
func securitySettingsOperations() []capability.Operation {
	return []capability.Operation{
		{Descriptor: securitySettingsGet, Handler: repositoryReadHandler(func(ctx context.Context, c *Client) (any, error) {
			return c.securitySettings(ctx)
		})},
		{Descriptor: securitySettingsUpdate, Handler: securityChangeHandler(securitySettingsUpdate.ID)},
		{Descriptor: codeScanningSetupGet, Handler: repositoryReadHandler(func(ctx context.Context, c *Client) (any, error) {
			return c.codeScanningSetup(ctx)
		})},
		{Descriptor: codeScanningSetupUpdate, Handler: codeScanningSetupHandler(codeScanningSetupUpdate.ID)},
	}
}

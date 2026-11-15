package github

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// github.code-scanning-alerts.update and github.dependabot-alerts.update dismiss an alert or reopen it, and
// github.secret-scanning-alerts.update resolves one or reopens it. Each is offered only where a connection's
// tools list names it. The values of dismissed_reason and resolution are the ones GitHub documents, and the
// fields each state needs are checked before a credential is resolved. Every call first reads the alert below
// the bound repository, which binds the number to it, then sends exactly one PATCH that is never repeated; an
// unclear outcome is reported as uncertain. The answer is the alert as the get tool reads it, decoded into the
// same shapes, so a secret scanning answer has no place for the found secret even though GitHub's PATCH answer
// carries it. Assigning an alert is not offered.
//
// Verified against https://docs.github.com/en/rest/code-scanning/code-scanning#update-a-code-scanning-alert,
// https://docs.github.com/en/rest/dependabot/alerts#update-a-dependabot-alert, and
// https://docs.github.com/en/rest/secret-scanning/secret-scanning#update-a-secret-scanning-alert.

// alertCommentLimit is the length GitHub accepts for a dismissal comment. GitHub documents no limit for a
// resolution comment, which takes the length of a comment body instead.
const (
	alertCommentLimit      = 280
	resolutionCommentLimit = 65536
)

var (
	codeScanningUpdateStates   = []string{"open", "dismissed"}
	dependabotUpdateStates     = []string{"open", "dismissed"}
	secretUpdateStates         = []string{"open", "resolved"}
	codeScanningDismissReasons = []string{"false positive", "won't fix", "used in tests", "mitigated"}
	dependabotDismissReasons   = []string{"fix_started", "inaccurate", "no_bandwidth", "not_used", "tolerable_risk"}
	// GitHub lists pattern_edited and pattern_deleted as resolutions a list filters by, but a change may only
	// set these.
	secretResolveReasons = []string{"false_positive", "wont_fix", "revoked", "used_in_tests"}
)

const (
	alertCommentSchema      = `{"type":"string","maxLength":280}`
	resolutionCommentSchema = `{"type":"string","maxLength":65536}`

	codeScanningWritePermission = "GitHub refused this token to change the code scanning alerts of this repository; " +
		"it needs security_events or repo on a classic token (public_repo for a public repository), or Code " +
		"scanning alerts: read and write on a fine-grained token, and write access to the repository"
	dependabotWritePermission = "GitHub refused this token to change the Dependabot alerts of this repository; it " +
		"needs security_events or repo on a classic token (public_repo for a public repository), or Dependabot " +
		"alerts: read and write on a fine-grained token, and write access to the repository"
	secretScanningWritePermission = "GitHub refused this token to change the secret scanning alerts of this " +
		"repository; it needs repo or security_events on a classic token, or Secret scanning alerts: read and " +
		"write on a fine-grained token, and write access to the repository"
)

var alertUpdateNumberArgument = capability.Argument{Name: "alert_number", Description: "Alert number, as the " +
	"list tool reports it", Required: true}

var codeScanningAlertsUpdate = capability.Descriptor{
	ID:      Provider + ".code-scanning-alerts.update",
	Version: 1,
	Title:   "Dismiss or reopen a GitHub code scanning alert",
	Description: "Dismiss one code scanning alert of a repository a connection allows with a reason and an " +
		"optional comment, or reopen it; a dismissal can ask for approval with create_request, and repeating that " +
		"asks again; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "code-scanning", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyNonIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"alert_number":`+alertNumberSchema+`,"state":`+enumSchema(codeScanningUpdateStates)+
		`,"dismissed_reason":`+enumSchema(codeScanningDismissReasons)+`,"dismissed_comment":`+alertCommentSchema+
		`,"create_request":{"type":"boolean"}`, "alert_number", "state"),
	OutputSchema: alertGetOutput(codeScanningProperties),
	Arguments: []capability.Argument{
		alertUpdateNumberArgument,
		{Name: "state", Description: "open to reopen the alert or dismissed to dismiss it", Required: true},
		{Name: "dismissed_reason", Description: "Required with state dismissed and not allowed with open: " +
			strings.Join(codeScanningDismissReasons, ", ")},
		{Name: "dismissed_comment", Description: "Optional comment of a dismissal, at most 280 characters"},
		{Name: "create_request", Description: "With state dismissed: ask for the dismissal to be approved instead " +
			"of dismissing the alert at once, where the repository requires approval"},
	},
	DefaultFields: []string{"number", "state", "url", "dismissed_at", "dismissed_by", "dismissed_reason",
		"dismissed_comment"},
	Fields: codeScanningAlertsGet.Fields,
	Examples: []capability.Example{{
		Description: "Dismiss an alert as a false positive",
		Arguments:   json.RawMessage(`{"alert_number":42,"state":"dismissed","dismissed_reason":"false positive","dismissed_comment":"Input is validated upstream"}`),
	}},
}

var dependabotAlertsUpdate = capability.Descriptor{
	ID:      Provider + ".dependabot-alerts.update",
	Version: 1,
	Title:   "Dismiss or reopen a GitHub Dependabot alert",
	Description: "Dismiss one Dependabot alert of a repository a connection allows with a reason and an optional " +
		"comment, or reopen it; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "security", "dependabot", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"alert_number":`+alertNumberSchema+`,"state":`+enumSchema(dependabotUpdateStates)+
		`,"dismissed_reason":`+enumSchema(dependabotDismissReasons)+`,"dismissed_comment":`+alertCommentSchema,
		"alert_number", "state"),
	OutputSchema: alertGetOutput(dependabotProperties),
	Arguments: []capability.Argument{
		alertUpdateNumberArgument,
		{Name: "state", Description: "open to reopen the alert or dismissed to dismiss it", Required: true},
		{Name: "dismissed_reason", Description: "Required with state dismissed and not allowed with open: " +
			strings.Join(dependabotDismissReasons, ", ")},
		{Name: "dismissed_comment", Description: "Optional comment of a dismissal, at most 280 characters"},
	},
	Fields: dependabotAlertsGet.Fields,
	Examples: []capability.Example{{
		Description: "Dismiss an alert whose risk is tolerable",
		Arguments:   json.RawMessage(`{"alert_number":7,"state":"dismissed","dismissed_reason":"tolerable_risk"}`),
	}},
}

var secretScanningAlertsUpdate = capability.Descriptor{
	ID:      Provider + ".secret-scanning-alerts.update",
	Version: 1,
	Title:   "Resolve or reopen a GitHub secret scanning alert",
	Description: "Resolve one secret scanning alert of a repository a connection allows with a resolution and an " +
		"optional comment, or reopen it; the found secret is never shown; offered only where a connection's tools " +
		"list names it",
	Tags:                  []string{"github", "security", "secret-scanning", "update"},
	Risk:                  changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"alert_number":`+alertNumberSchema+`,"state":`+enumSchema(secretUpdateStates)+
		`,"resolution":`+enumSchema(secretResolveReasons)+`,"resolution_comment":`+resolutionCommentSchema,
		"alert_number", "state"),
	OutputSchema: alertGetOutput(secretScanningProperties),
	Arguments: []capability.Argument{
		alertUpdateNumberArgument,
		{Name: "state", Description: "open to reopen the alert or resolved to resolve it", Required: true},
		{Name: "resolution", Description: "Required with state resolved and not allowed with open: " +
			strings.Join(secretResolveReasons, ", ")},
		{Name: "resolution_comment", Description: "Optional comment, at most 65536 characters; the answer never " +
			"repeats it"},
	},
	DefaultFields: []string{"number", "state", "url", "resolution", "resolved_at", "resolved_by"},
	Fields:        secretScanningAlertsGet.Fields,
	Examples: []capability.Example{{
		Description: "Resolve an alert whose secret was revoked",
		Arguments:   json.RawMessage(`{"alert_number":3,"state":"resolved","resolution":"revoked"}`),
	}},
}

// alertUpdateArguments holds the arguments of every alert update; the input schema of each admits only its own.
type alertUpdateArguments struct {
	AlertNumber       int64  `json:"alert_number"`
	State             string `json:"state"`
	DismissedReason   string `json:"dismissed_reason"`
	DismissedComment  string `json:"dismissed_comment"`
	CreateRequest     *bool  `json:"create_request"`
	Resolution        string `json:"resolution"`
	ResolutionComment string `json:"resolution_comment"`

	allowed allowlist
}

func securityAlertUpdateOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*alertUpdateArguments) error,
		call func(context.Context, *Client, *alertUpdateArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: alertUpdateHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(codeScanningAlertsUpdate, checkCodeScanningUpdate,
			func(ctx context.Context, c *Client, a *alertUpdateArguments) (any, error) {
				return c.updateCodeScanningAlert(ctx, a)
			}),
		bind(dependabotAlertsUpdate, checkDependabotUpdate,
			func(ctx context.Context, c *Client, a *alertUpdateArguments) (any, error) {
				return c.updateDependabotAlert(ctx, a)
			}),
		bind(secretScanningAlertsUpdate, checkSecretUpdate,
			func(ctx context.Context, c *Client, a *alertUpdateArguments) (any, error) {
				return c.updateSecretScanningAlert(ctx, a)
			}),
	}
}

// alertUpdateHandler checks the arguments and the repository before a credential is resolved, so a refused
// request never becomes a provider call and never touches a secret.
func alertUpdateHandler(id string, check func(*alertUpdateArguments) error,
	call func(context.Context, *Client, *alertUpdateArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments alertUpdateArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if id == codeScanningAlertsUpdate.ID {
			if arguments.allowed, err = allowlistOf(resolved); err != nil {
				return nil, providerError("open", err.Error())
			}
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

// checkAlertComment bounds a comment at GitHub's limit and refuses control characters other than line breaks
// and tabs.
func checkAlertComment(name, value string, limit int) error {
	if utf8.RuneCountInString(value) > limit || !utf8.ValidString(value) ||
		strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
		return invalidRequest(name + " must be text of at most " + strconv.Itoa(limit) + " characters")
	}
	return nil
}

func checkAlertState(state string, states []string) error {
	for _, allowed := range states {
		if state == allowed {
			return nil
		}
	}
	return invalidRequest("state must be one of " + strings.Join(states, ", "))
}

func inList(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// checkDismissal checks the fields a dismissal needs and refuses them on a reopening.
func checkDismissal(state, reason, comment string, reasons []string) error {
	if state == "open" {
		if reason != "" || comment != "" {
			return invalidRequest("dismissed_reason and dismissed_comment are only allowed with state dismissed")
		}
		return nil
	}
	if reason == "" {
		return invalidRequest("dismissed_reason is required with state dismissed: " + strings.Join(reasons, ", "))
	}
	if !inList(reasons, reason) {
		return invalidRequest("dismissed_reason must be one of " + strings.Join(reasons, ", "))
	}
	return checkAlertComment("dismissed_comment", comment, alertCommentLimit)
}

func checkCodeScanningUpdate(a *alertUpdateArguments) error {
	if a.AlertNumber < 1 {
		return invalidRequest("alert_number must be a positive alert number")
	}
	if err := checkAlertState(a.State, codeScanningUpdateStates); err != nil {
		return err
	}
	if a.Resolution != "" || a.ResolutionComment != "" {
		return invalidRequest("resolution and resolution_comment belong to secret scanning alerts")
	}
	if a.CreateRequest != nil && a.State != "dismissed" {
		return invalidRequest("create_request is only allowed with state dismissed")
	}
	return checkDismissal(a.State, a.DismissedReason, a.DismissedComment, codeScanningDismissReasons)
}

func checkDependabotUpdate(a *alertUpdateArguments) error {
	if a.AlertNumber < 1 {
		return invalidRequest("alert_number must be a positive alert number")
	}
	if err := checkAlertState(a.State, dependabotUpdateStates); err != nil {
		return err
	}
	if a.Resolution != "" || a.ResolutionComment != "" || a.CreateRequest != nil {
		return invalidRequest("this tool takes only state, dismissed_reason, and dismissed_comment")
	}
	return checkDismissal(a.State, a.DismissedReason, a.DismissedComment, dependabotDismissReasons)
}

func checkSecretUpdate(a *alertUpdateArguments) error {
	if a.AlertNumber < 1 {
		return invalidRequest("alert_number must be a positive alert number")
	}
	if err := checkAlertState(a.State, secretUpdateStates); err != nil {
		return err
	}
	if a.DismissedReason != "" || a.DismissedComment != "" || a.CreateRequest != nil {
		return invalidRequest("this tool takes only state, resolution, and resolution_comment")
	}
	if a.State == "open" && a.Resolution != "" {
		return invalidRequest("resolution is only allowed with state resolved")
	}
	if a.State == "resolved" {
		if a.Resolution == "" {
			return invalidRequest("resolution is required with state resolved: " + strings.Join(secretResolveReasons, ", "))
		}
		if !inList(secretResolveReasons, a.Resolution) {
			return invalidRequest("resolution must be one of " + strings.Join(secretResolveReasons, ", "))
		}
	}
	return checkAlertComment("resolution_comment", a.ResolutionComment, resolutionCommentLimit)
}

// bindAlertToRepository reads the alert below the bound repository and confirms the number GitHub answers. The
// read is the proof that the alert belongs to the chosen repository, and it decodes nothing but the number.
func (c *Client) bindAlertToRepository(ctx context.Context, op, kind string, number int64, query, permission string) error {
	var proof struct {
		Number int64 `json:"number"`
	}
	if err := c.rest(ctx, op, c.alertPath(kind, number)+query, &proof); err != nil {
		return actionsFailure(err, permission)
	}
	if proof.Number != number {
		return invalidResponse(op, false)
	}
	return nil
}

func (c *Client) updateCodeScanningAlert(ctx context.Context, a *alertUpdateArguments) (*CodeScanningAlert, error) {
	const op = "update code scanning alert"
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	if err := c.bindAlertToRepository(ctx, op, "code-scanning", a.AlertNumber, "", codeScanningWritePermission); err != nil {
		return nil, err
	}
	body := map[string]any{"state": a.State}
	if a.State == "dismissed" {
		body["dismissed_reason"] = a.DismissedReason
		if a.DismissedComment != "" {
			body["dismissed_comment"] = a.DismissedComment
		}
		if a.CreateRequest != nil {
			body["create_request"] = *a.CreateRequest
		}
	}
	var raw codeScanningAlertJSON
	if err := c.restChange(ctx, op, http.MethodPatch, c.alertPath("code-scanning", a.AlertNumber), body, &raw); err != nil {
		return nil, actionsFailure(err, codeScanningWritePermission)
	}
	if int64(raw.Number) != a.AlertNumber {
		return nil, invalidResponse(op, true)
	}
	view := raw.view(true, a.allowed)
	return &view, nil
}

func (c *Client) updateDependabotAlert(ctx context.Context, a *alertUpdateArguments) (*DependabotAlert, error) {
	const op = "update Dependabot alert"
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	if err := c.bindAlertToRepository(ctx, op, "dependabot", a.AlertNumber, "", dependabotWritePermission); err != nil {
		return nil, err
	}
	body := map[string]any{"state": a.State}
	if a.State == "dismissed" {
		body["dismissed_reason"] = a.DismissedReason
		if a.DismissedComment != "" {
			body["dismissed_comment"] = a.DismissedComment
		}
	}
	var raw dependabotAlertJSON
	if err := c.restChange(ctx, op, http.MethodPatch, c.alertPath("dependabot", a.AlertNumber), body, &raw); err != nil {
		return nil, actionsFailure(err, dependabotWritePermission)
	}
	if int64(raw.Number) != a.AlertNumber {
		return nil, invalidResponse(op, true)
	}
	view := raw.view(true)
	return &view, nil
}

func (c *Client) updateSecretScanningAlert(ctx context.Context, a *alertUpdateArguments) (*SecretScanningAlert, error) {
	const op = "update secret scanning alert"
	if c.target.kind != kindRepository {
		return nil, providerError(op, "this connection is not bound to a repository")
	}
	// hide_secret is documented for the reads only, so the change sends none; the decoded shape keeps the secret out.
	if err := c.bindAlertToRepository(ctx, op, "secret-scanning", a.AlertNumber, "?hide_secret=true", secretScanningWritePermission); err != nil {
		return nil, err
	}
	body := map[string]any{"state": a.State}
	if a.State == "resolved" {
		body["resolution"] = a.Resolution
	}
	if a.ResolutionComment != "" {
		body["resolution_comment"] = a.ResolutionComment
	}
	var raw secretScanningAlertJSON
	path := c.alertPath("secret-scanning", a.AlertNumber)
	if err := c.restChange(ctx, op, http.MethodPatch, path, body, &raw); err != nil {
		return nil, actionsFailure(err, secretScanningWritePermission)
	}
	if int64(raw.Number) != a.AlertNumber {
		return nil, invalidResponse(op, true)
	}
	view := raw.view()
	return &view, nil
}

package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The code scanning default setup of a repository. GitHub has no route that lists the languages a repository
// could be analyzed with, so the read reports the languages of the configuration and nothing more.

// The values the default setup accepts.
var (
	setupStateValues       = []string{"configured", "not-configured"}
	setupRunnerTypeValues  = []string{"standard", "labeled"}
	setupQuerySuiteValues  = []string{"default", "extended"}
	setupThreatModelValues = []string{"remote", "remote_and_local"}
	setupLanguageValues    = []string{"actions", "c-cpp", "csharp", "go", "java-kotlin", "javascript-typescript",
		"python", "ruby", "swift"}
)

const (
	setupUnavailable = "unavailable"
	maxRunnerLabel   = 256
	maxSetupURL      = 2048
)

// setupLanguagePattern bounds a language GitHub reports, which may be one this provider does not know yet.
var setupLanguagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

const codeScanningSetupSensitivity = "github-code-scanning-setup"

const codeScanningSetupProperties = `"state":{"type":"string","enum":["configured","not-configured","unavailable"]},` +
	`"languages":{"type":"array","items":{"type":"string"}},` +
	`"runner_type":{"type":"string"},"runner_label":{"type":"string"},"query_suite":{"type":"string"},` +
	`"threat_model":{"type":"string"},"updated_at":{"type":"string"},"schedule":{"type":"string"}`

var codeScanningSetupGet = capability.Descriptor{
	ID:      Provider + ".code-scanning-setup.get",
	Version: 1,
	Title:   "Get the code scanning default setup of a GitHub repository",
	Description: "Read the state, languages, runner, query suite, threat model, schedule, and last update of the " +
		"code scanning default setup of a repository a connection allows; a repository for which GitHub offers " +
		"no code scanning, such as one without a license for it, is reported with state unavailable, not as " +
		"not configured; the languages are those of the configuration, GitHub offers no list of the others",
	Tags:         []string{"github", "security", "code-scanning", "codeql", "setup", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(""),
	OutputSchema: settingsOutput(codeScanningSetupProperties, `"state"`),
	Fields: []capability.Field{
		{Name: "state", Description: "configured, not-configured, or unavailable where GitHub refuses the read with " +
			"403 or 503, which it documents for a repository without the license; a token without the right shows the " +
			"same 403"},
		{Name: "languages", Description: "Languages the setup analyzes, as GitHub names them; absent when it reports none"},
		{Name: "runner_type", Description: "standard or labeled; absent when GitHub reports none"},
		{Name: "runner_label", Description: "Label of the runner for runner_type labeled" + untrustedText},
		{Name: "query_suite", Description: "default or extended"},
		{Name: "threat_model", Description: "remote or remote_and_local"},
		{Name: "updated_at", Description: "When the configuration last changed; absent when GitHub reports none"},
		{Name: "schedule", Description: "weekly while a periodic analysis runs; absent otherwise"},
	},
	Examples: []capability.Example{{Description: "Read the default setup", Arguments: json.RawMessage(`{}`)}},
}

var codeScanningSetupUpdate = capability.Descriptor{
	ID:      Provider + ".code-scanning-setup.update",
	Version: 1,
	Title:   "Change the code scanning default setup of a GitHub repository",
	Description: "Switch the code scanning default setup of a repository a connection allows on or off, or change " +
		"its languages, runner, query suite, or threat model, in one request that is never repeated; a value left " +
		"out stays as it is; GitHub may answer by starting a configuration run, which the result names but never " +
		"waits for; setting state not-configured turns code scanning off and lowers the protection of the " +
		"repository; offered only where the connection lists it",
	Tags:                  []string{"github", "security", "code-scanning", "codeql", "setup", "update", "admin"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, codeScanningSetupSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"state":` + enumSchema(setupStateValues) + `,"runner_type":` +
		enumSchema(setupRunnerTypeValues) + `,"runner_label":{"type":"string","minLength":1,"maxLength":256},` +
		`"query_suite":` + enumSchema(setupQuerySuiteValues) + `,"threat_model":` + enumSchema(setupThreatModelValues) +
		`,"languages":{"type":"array","minItems":1,"maxItems":9,"uniqueItems":true,"items":` +
		enumSchema(setupLanguageValues) + `}`),
	OutputSchema: settingsOutput(`"started":{"type":"boolean"},"run_id":{"type":"integer"},"run_url":{"type":"string"}`,
		`"started"`),
	Arguments: []capability.Argument{
		{Name: "state", Description: "configured to switch the default setup on, not-configured to switch it off"},
		{Name: "runner_type", Description: "standard, or labeled to run on a runner with runner_label"},
		{Name: "runner_label", Description: "Label of the runner; only together with runner_type labeled"},
		{Name: "query_suite", Description: "default or extended"},
		{Name: "threat_model", Description: "remote, or remote_and_local to include local sources"},
		{Name: "languages", Description: "Languages to analyze: " + strings.Join(setupLanguageValues, ", ")},
	},
	Fields: []capability.Field{
		{Name: "started", Description: "True when GitHub answered by starting a configuration run, false when it applied " +
			"the change without one"},
		{Name: "run_id", Description: "Identifier of the started run; absent when GitHub names none"},
		{Name: "run_url", Description: "Web address of the started run, never fetched" + untrustedText},
	},
	Examples: []capability.Example{{
		Description: "Switch the default setup on for Go and Python with the extended query suite",
		Arguments:   json.RawMessage(`{"state":"configured","languages":["go","python"],"query_suite":"extended"}`),
	}},
}

const (
	codeScanningSetupReadPermission = "GitHub refused this token the code scanning default setup of this repository or " +
		"does not show the repository to it; reading it needs repo on a classic token, and Administration: read on " +
		"a fine-grained token"
	codeScanningSetupChangePermission = "GitHub refused this change of the code scanning default setup or does not show " +
		"the repository to this token; it needs the admin role on the repository with repo on a classic token, or " +
		"Administration: write on a fine-grained token; an archived repository and one without a license for code " +
		"scanning are refused the same way"
	codeScanningConflict = "GitHub refused this change because a validation run with a different default setup " +
		"configuration is already in progress; wait for it to finish and read the setup again"
	codeScanningRejected = "GitHub rejected this change because the repository is not in the state it needs, for " +
		"example because a language is not in the repository or the runner label names no runner"
)

// CodeScanningSetup is the default setup of the bound repository. State unavailable stands alone.
type CodeScanningSetup struct {
	State       string   `json:"state"`
	Languages   []string `json:"languages,omitempty"`
	RunnerType  string   `json:"runner_type,omitempty"`
	RunnerLabel string   `json:"runner_label,omitempty"`
	QuerySuite  string   `json:"query_suite,omitempty"`
	ThreatModel string   `json:"threat_model,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
	Schedule    string   `json:"schedule,omitempty"`
}

// codeScanningSetupAnswer is the decoded default setup; a member GitHub reports as null is empty here.
type codeScanningSetupAnswer struct {
	State       string   `json:"state"`
	Languages   []string `json:"languages"`
	RunnerType  *string  `json:"runner_type"`
	RunnerLabel *string  `json:"runner_label"`
	QuerySuite  string   `json:"query_suite"`
	ThreatModel string   `json:"threat_model"`
	UpdatedAt   *string  `json:"updated_at"`
	Schedule    *string  `json:"schedule"`
}

func nullable(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func emptyOr(values []string, value string) bool { return value == "" || contains(values, value) }

func (a *codeScanningSetupAnswer) valid() bool {
	if !contains(setupStateValues, a.State) || !emptyOr(setupRunnerTypeValues, nullable(a.RunnerType)) ||
		!emptyOr(setupQuerySuiteValues, a.QuerySuite) || !emptyOr(setupThreatModelValues, a.ThreatModel) ||
		!emptyOr([]string{"weekly"}, nullable(a.Schedule)) || len(a.Languages) > 30 ||
		len(nullable(a.UpdatedAt)) > 64 || !validSetupText(nullable(a.RunnerLabel), maxRunnerLabel) {
		return false
	}
	for _, language := range a.Languages {
		if !setupLanguagePattern.MatchString(language) {
			return false
		}
	}
	return true
}

func validSetupText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsFunc(value, unicode.IsControl)
}

func (c *Client) codeScanningSetup(ctx context.Context) (*CodeScanningSetup, error) {
	const op = "get code scanning default setup"
	var answer codeScanningSetupAnswer
	err := c.rest(ctx, op, c.repoPath("code-scanning/default-setup"), &answer)
	if err != nil {
		var failure *provider.Error
		// GitHub documents 403 for a repository without the license and 503 for a service that is not there;
		// a token without the right shows the same 403, which is why the state names no cause.
		if errors.As(err, &failure) && (failure.Class == provider.ClassPermission ||
			statusOf(err) == http.StatusServiceUnavailable) {
			return &CodeScanningSetup{State: setupUnavailable}, nil
		}
		return nil, securityFailure(err, codeScanningSetupReadPermission)
	}
	if !answer.valid() {
		return nil, invalidResponse(op, false)
	}
	return &CodeScanningSetup{State: answer.State, Languages: answer.Languages, RunnerType: nullable(answer.RunnerType),
		RunnerLabel: nullable(answer.RunnerLabel), QuerySuite: answer.QuerySuite, ThreatModel: answer.ThreatModel,
		UpdatedAt: nullable(answer.UpdatedAt), Schedule: nullable(answer.Schedule)}, nil
}

// codeScanningChangeArguments are the arguments of github.code-scanning-setup.update. A text is a pointer or
// empty and the languages are nil when left out, so a value left out stays apart from one given.
type codeScanningChangeArguments struct {
	State       string   `json:"state"`
	RunnerType  string   `json:"runner_type"`
	RunnerLabel *string  `json:"runner_label"`
	QuerySuite  string   `json:"query_suite"`
	ThreatModel string   `json:"threat_model"`
	Languages   []string `json:"languages"`
}

func checkCodeScanningChange(a *codeScanningChangeArguments, _ target) error {
	switch {
	case a.State == "" && a.RunnerType == "" && a.RunnerLabel == nil && a.QuerySuite == "" && a.ThreatModel == "" &&
		a.Languages == nil:
		return invalidRequest("give at least one default setup value")
	case a.State != "" && !contains(setupStateValues, a.State):
		return invalidRequest("state must be one of " + strings.Join(setupStateValues, ", "))
	case a.RunnerType != "" && !contains(setupRunnerTypeValues, a.RunnerType):
		return invalidRequest("runner_type must be one of " + strings.Join(setupRunnerTypeValues, ", "))
	case a.QuerySuite != "" && !contains(setupQuerySuiteValues, a.QuerySuite):
		return invalidRequest("query_suite must be one of " + strings.Join(setupQuerySuiteValues, ", "))
	case a.ThreatModel != "" && !contains(setupThreatModelValues, a.ThreatModel):
		return invalidRequest("threat_model must be one of " + strings.Join(setupThreatModelValues, ", "))
	case a.RunnerLabel != nil && (*a.RunnerLabel == "" || !validSetupText(*a.RunnerLabel, maxRunnerLabel)):
		return invalidRequest("runner_label must be text of 1 to 256 bytes without control characters")
	case a.RunnerLabel != nil && a.RunnerType != "labeled":
		return invalidRequest("runner_label belongs to runner_type labeled and needs it in the same call")
	case a.RunnerType == "labeled" && a.RunnerLabel == nil:
		return invalidRequest("runner_type labeled needs a runner_label")
	case a.Languages != nil && (len(a.Languages) == 0 || len(a.Languages) > len(setupLanguageValues)):
		return invalidRequest("languages must name between 1 and 9 languages")
	}
	seen := map[string]bool{}
	for _, language := range a.Languages {
		if !contains(setupLanguageValues, language) || seen[language] {
			return invalidRequest("languages must be distinct values of " + strings.Join(setupLanguageValues, ", "))
		}
		seen[language] = true
	}
	return nil
}

// CodeScanningSetupChange is what code-scanning-setup.update reports: whether GitHub started a run.
type CodeScanningSetupChange struct {
	Started bool   `json:"started"`
	RunID   *int64 `json:"run_id,omitempty"`
	RunURL  string `json:"run_url,omitempty"`
}

// setupChangeAnswer is the 202 answer of the update; the 200 answer has no members, and may come without a body.
type setupChangeAnswer struct {
	RunID  *int64 `json:"run_id"`
	RunURL string `json:"run_url"`
}

func (*setupChangeAnswer) allowsEmptyBody() {}

// codeScanningChangeFailure names what a refused change most likely means, without any text of GitHub's.
func codeScanningChangeFailure(err error) error {
	var failure *provider.Error
	if !errors.As(err, &failure) {
		return err
	}
	refused := *failure
	switch {
	case failure.Class == provider.ClassPermission || failure.Class == provider.ClassNotFound:
		refused.Message = codeScanningSetupChangePermission
	case statusOf(err) == http.StatusConflict:
		refused.Message = codeScanningConflict
	case statusOf(err) == http.StatusUnprocessableEntity:
		refused.Message = codeScanningRejected
	default:
		return err
	}
	return &refused
}

// updateCodeScanningSetup sends the given values, and only those, in one request that is never repeated.
func (c *Client) updateCodeScanningSetup(ctx context.Context, a *codeScanningChangeArguments) (*CodeScanningSetupChange, error) {
	const op = "update code scanning default setup"
	body := map[string]any{}
	for name, value := range map[string]string{"state": a.State, "runner_type": a.RunnerType,
		"query_suite": a.QuerySuite, "threat_model": a.ThreatModel} {
		if value != "" {
			body[name] = value
		}
	}
	if a.RunnerLabel != nil {
		body["runner_label"] = *a.RunnerLabel
	}
	if a.Languages != nil {
		body["languages"] = a.Languages
	}
	var answer setupChangeAnswer
	status, err := c.restChangeStatus(ctx, op, http.MethodPatch, c.repoPath("code-scanning/default-setup"), body, &answer)
	if err != nil {
		return nil, codeScanningChangeFailure(err)
	}
	if status != http.StatusAccepted {
		return &CodeScanningSetupChange{}, nil
	}
	if (answer.RunID != nil && *answer.RunID < 1) || !validSetupText(answer.RunURL, maxSetupURL) {
		return nil, invalidResponse(op, true)
	}
	return &CodeScanningSetupChange{Started: true, RunID: answer.RunID, RunURL: answer.RunURL}, nil
}

// codeScanningSetupHandler checks the arguments and the repository before a credential is resolved.
func codeScanningSetupHandler(id string) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments codeScanningChangeArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectTarget(resolved, kindRepository, raw)
		if err != nil {
			return nil, err
		}
		if err := checkCodeScanningChange(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(client.updateCodeScanningSetup(ctx, &arguments))
	}
}

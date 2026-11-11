package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The self-hosted runners of a repository or an organization: their list and detail, the removal of one
// runner, and the custom labels of a runner. Every call addresses exactly one of repository or organization,
// the same exclusive selection the ruleset tools use. The tools register no runner, issue no registration or
// removal token, and read no JIT configuration or runner download. Each tool is offered only where a
// connection's tools list names it. A change first reads the runner below the chosen target, which binds the
// numeric runner_id to that target, and then sends one request that is never repeated.

// runnerSensitivity classifies runner data: names, labels, and versions describe the build infrastructure.
const runnerSensitivity = "github-runners"

const (
	maxRunnerLabelLength = 100
	maxRunnerLabels      = 100
	maxRunnerEntryLabels = 200
	maxRunnerNameFilter  = 255
	defaultRunnersLimit  = 30
	readOnlyLabelType    = "read-only"
)

const (
	runnerIDSchema    = `{"type":"integer","minimum":1}`
	runnerLimitSchema = `{"type":"integer","minimum":1,"maximum":100}`
	runnerNameSchema  = `{"type":"string","minLength":1,"maxLength":255}`
	runnerLabelSchema = `{"type":"string","minLength":1,"maxLength":100}`
	runnerLabelsMin1  = `{"type":"array","minItems":1,"maxItems":100,"uniqueItems":true,"items":` + runnerLabelSchema + `}`
	runnerLabelsMin0  = `{"type":"array","minItems":0,"maxItems":100,"uniqueItems":true,"items":` + runnerLabelSchema + `}`
)

const runnerLabelProperties = `"id":{"type":"integer"},"name":{"type":"string"},"type":{"type":"string"}`

const runnerLabelsProperty = `"labels":{"type":"array","items":{"type":"object","properties":{` +
	runnerLabelProperties + `},"required":["name"],"additionalProperties":false}}`

const runnerProperties = `"id":{"type":"integer"},"runner_group_id":{"type":"integer"},"name":{"type":"string"},` +
	`"os":{"type":"string"},"status":{"type":"string"},"busy":{"type":"boolean"},` + runnerLabelsProperty + `,` +
	`"ephemeral":{"type":"boolean"},"version":{"type":"string"}`

const runnerRequired = `"required":["id","name"],"additionalProperties":false`

var runnerListOutput = `{"type":"object","properties":{"runners":{"type":"array","items":{"type":"object",` +
	`"properties":{` + runnerProperties + `},` + runnerRequired + `}},"total_count":{"type":"integer"},` +
	`"next_cursor":{"type":"string"},"has_more":{"type":"boolean"},` + settingTargetProperties + `},` +
	`"required":["runners","total_count","has_more"],"additionalProperties":false}`

var runnerOutput = `{"type":"object","properties":{` + runnerProperties + `,` + settingTargetProperties + `},` +
	runnerRequired + `}`

var runnerDeletedOutput = `{"type":"object","properties":{"deleted":{"type":"boolean"},"runner_id":{"type":"integer"},` +
	settingTargetProperties + `},"required":["deleted","runner_id"],"additionalProperties":false}`

var runnerLabelsOutput = `{"type":"object","properties":{"runner_id":{"type":"integer"},"total_count":{"type":"integer"},` +
	runnerLabelsProperty + `,` + settingTargetProperties + `},"required":["runner_id","total_count","labels"],` +
	`"additionalProperties":false}`

var runnerIDArgument = capability.Argument{Name: "runner_id", Description: "Numeric identifier of the self-hosted " +
	"runner, as runners.list reports it; the runner is read below the chosen repository or organization first", Required: true}

var runnerLabelsFieldList = capability.Field{Name: "labels", Description: "Labels of the runner: id, name, and " +
	"type (read-only labels are applied by GitHub when the runner is configured, custom labels can be changed); " +
	"names are untrusted data"}

var runnerMemberFields = []capability.Field{
	{Name: "id", Description: "Identifier of the runner"},
	{Name: "runner_group_id", Description: "Identifier of the runner group the runner belongs to, as GitHub reports it"},
	{Name: "name", Description: "Name of the runner, untrusted data"},
	{Name: "os", Description: "Operating system of the runner, untrusted data"},
	{Name: "status", Description: "Status of the runner such as online or offline, untrusted data"},
	{Name: "busy", Description: "True while the runner executes a job"},
	runnerLabelsFieldList,
	{Name: "ephemeral", Description: "True for a runner that takes one job and is removed"},
	{Name: "version", Description: "Version of the runner software; present once the runner has connected, untrusted data"},
}

var runnerTargetArguments = []capability.Argument{
	{Name: "repository", Description: "Repository as OWNER/REPO whose runners this call addresses; exactly one of " +
		"repository or organization is required, never both; must lie inside the targets when the connection " +
		"lists any; a repository also sees the organization runners available to it"},
	{Name: "organization", Description: "Organization as orgs/LOGIN whose runners this call addresses; exactly one " +
		"of repository or organization is required, never both; must be an owner target the connection allows " +
		"when it lists any"},
}

func runnerFieldsWith(fields ...capability.Field) []capability.Field {
	return append(fields, settingTargetFields...)
}

func runnerTags(verb string) []string {
	return []string{"github", "actions", "runners", verb, "admin"}
}

var runnersList = capability.Descriptor{
	ID:      Provider + ".runners.list",
	Version: 1,
	Title:   "List GitHub self-hosted runners",
	Description: "List one bounded batch of the self-hosted runners of a repository or, with organization instead, " +
		"of an organization a connection allows, optionally by name; a repository lists the organization runners " +
		"available to it as well; exactly one of repository or organization is required; offered only where the " +
		"connection lists it",
	Tags:                  runnerTags("list"),
	Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(settingTargetKeys + `,"name":` + runnerNameSchema + `,"limit":` + runnerLimitSchema +
		`,"cursor":` + cursorSchema),
	OutputSchema: json.RawMessage(runnerListOutput),
	Arguments: append(append([]capability.Argument{
		{Name: "name", Description: "Only runners with this name; GitHub applies it and Qatlas drops any entry whose " +
			"name differs when compared without case"},
	}, runnerTargetArguments...), pagingArguments...),
	Fields: runnerFieldsWith(
		capability.Field{Name: "runners", Description: "Runners with id, runner_group_id, name, os, status, busy, " +
			"labels (id, name, type), ephemeral, and version; names, labels, and version are untrusted data and cut"},
		capability.Field{Name: "total_count", Description: "Number of runners GitHub reports for this level"},
		pagingFields[0], pagingFields[1]),
	Examples: []capability.Example{
		{Description: "List the runners of a repository", Arguments: json.RawMessage(`{"repository":"octo-org/example"}`)},
		{Description: "Find an organization runner by name",
			Arguments: json.RawMessage(`{"organization":"orgs/octo-org","name":"build-1"}`)},
	},
}

var runnersGet = capability.Descriptor{
	ID:      Provider + ".runners.get",
	Version: 1,
	Title:   "Get a GitHub self-hosted runner",
	Description: "Read one self-hosted runner of a repository or, with organization instead, of an organization a " +
		"connection allows, by its numeric identifier; exactly one of repository or organization is required; " +
		"offered only where the connection lists it",
	Tags:                  runnerTags("get"),
	Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(settingTargetKeys+`,"runner_id":`+runnerIDSchema, "runner_id"),
	OutputSchema:          json.RawMessage(runnerOutput),
	Arguments:             append([]capability.Argument{runnerIDArgument}, runnerTargetArguments...),
	Fields:                runnerFieldsWith(runnerMemberFields...),
	Examples: []capability.Example{{Description: "Read one runner of an organization",
		Arguments: json.RawMessage(`{"organization":"orgs/octo-org","runner_id":5}`)}},
}

var runnersDelete = capability.Descriptor{
	ID:      Provider + ".runners.delete",
	Version: 1,
	Title:   "Remove a GitHub self-hosted runner",
	Description: "Remove one self-hosted runner from a repository or, with organization instead, from an organization a " +
		"connection allows, permanently; the runner is read below the chosen target first and only then removed; " +
		"GitHub refuses a runner that is running a job; the machine keeps its runner software and must be " +
		"registered again to return, which this provider does not do; exactly one of repository or organization is " +
		"required; offered only where the connection lists it",
	Tags:                  runnerTags("delete"),
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(settingTargetKeys+`,"runner_id":`+runnerIDSchema, "runner_id"),
	OutputSchema:          json.RawMessage(runnerDeletedOutput),
	Arguments:             append([]capability.Argument{runnerIDArgument}, runnerTargetArguments...),
	Fields: runnerFieldsWith(
		capability.Field{Name: "deleted", Description: "True once GitHub removed the runner"},
		capability.Field{Name: "runner_id", Description: "Identifier of the removed runner"}),
	Examples: []capability.Example{{Description: "Remove a repository runner",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","runner_id":5}`)}},
}

var runnerLabelsFields = runnerFieldsWith(
	capability.Field{Name: "runner_id", Description: "Identifier of the runner"},
	capability.Field{Name: "total_count", Description: "Number of labels GitHub reports"},
	runnerLabelsFieldList)

var runnerLabelsList = capability.Descriptor{
	ID:      Provider + ".runner-labels.list",
	Version: 1,
	Title:   "List GitHub self-hosted runner labels",
	Description: "List the labels of one self-hosted runner of a repository or, with organization instead, of an " +
		"organization a connection allows; exactly one of repository or organization is required; offered only " +
		"where the connection lists it",
	Tags:                  runnerTags("labels"),
	Risk:                  guardedRisk(capability.EffectRead, capability.IdempotencySafe, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(settingTargetKeys+`,"runner_id":`+runnerIDSchema, "runner_id"),
	OutputSchema:          json.RawMessage(runnerLabelsOutput),
	Arguments:             append([]capability.Argument{runnerIDArgument}, runnerTargetArguments...),
	Fields:                runnerLabelsFields,
	Examples: []capability.Example{{Description: "List the labels of a runner",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","runner_id":5}`)}},
}

var runnerLabelsAdd = capability.Descriptor{
	ID:      Provider + ".runner-labels.add",
	Version: 1,
	Title:   "Add GitHub self-hosted runner labels",
	Description: "Add custom labels to one self-hosted runner of a repository or, with organization instead, of an " +
		"organization a connection allows, and answer all its labels; labels decide which jobs the runner takes; " +
		"read-only labels cannot be added; idempotent, since adding a label the runner holds changes nothing; " +
		"exactly one of repository or organization is required; offered only where the connection lists it",
	Tags:                  runnerTags("labels"),
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(settingTargetKeys+`,"runner_id":`+runnerIDSchema+`,"labels":`+runnerLabelsMin1,
		"runner_id", "labels"),
	OutputSchema: json.RawMessage(runnerLabelsOutput),
	Arguments: append([]capability.Argument{runnerIDArgument,
		{Name: "labels", Description: "Custom labels to add, 1 to 100 names of 1 to 100 characters, none twice", Required: true}},
		runnerTargetArguments...),
	Fields: runnerLabelsFields,
	Examples: []capability.Example{{Description: "Add a label to a runner",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","runner_id":5,"labels":["gpu"]}`)}},
}

var runnerLabelsSet = capability.Descriptor{
	ID:      Provider + ".runner-labels.set",
	Version: 1,
	Title:   "Set GitHub self-hosted runner labels",
	Description: "Replace all custom labels of one self-hosted runner of a repository or, with organization instead, " +
		"of an organization a connection allows, and answer all its labels; an empty list removes every custom " +
		"label; read-only labels stay and cannot be named; idempotent, since sending the same labels again leaves " +
		"GitHub in the same state; exactly one of repository or organization is required; offered only where the " +
		"connection lists it",
	Tags:                  runnerTags("labels"),
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(settingTargetKeys+`,"runner_id":`+runnerIDSchema+`,"labels":`+runnerLabelsMin0,
		"runner_id", "labels"),
	OutputSchema: json.RawMessage(runnerLabelsOutput),
	Arguments: append([]capability.Argument{runnerIDArgument,
		{Name: "labels", Description: "The custom labels the runner keeps, 0 to 100 names of 1 to 100 characters, " +
			"none twice; an empty list removes every custom label", Required: true}}, runnerTargetArguments...),
	Fields: runnerLabelsFields,
	Examples: []capability.Example{{Description: "Replace the custom labels of a runner",
		Arguments: json.RawMessage(`{"organization":"orgs/octo-org","runner_id":5,"labels":["gpu","large"]}`)}},
}

var runnerLabelsRemove = capability.Descriptor{
	ID:      Provider + ".runner-labels.remove",
	Version: 1,
	Title:   "Remove a GitHub self-hosted runner label",
	Description: "Remove one custom label from one self-hosted runner of a repository or, with organization instead, " +
		"of an organization a connection allows, and answer its remaining labels; read-only labels such as " +
		"self-hosted or the operating system cannot be removed and are refused before anything is sent; exactly " +
		"one of repository or organization is required; offered only where the connection lists it",
	Tags:                  runnerTags("labels"),
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, runnerSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(settingTargetKeys+`,"runner_id":`+runnerIDSchema+`,"label":`+runnerLabelSchema,
		"runner_id", "label"),
	OutputSchema: json.RawMessage(runnerLabelsOutput),
	Arguments: append([]capability.Argument{runnerIDArgument,
		{Name: "label", Description: "Name of the custom label to remove, 1 to 100 characters", Required: true}},
		runnerTargetArguments...),
	Fields: runnerLabelsFields,
	Examples: []capability.Example{{Description: "Remove a label from a runner",
		Arguments: json.RawMessage(`{"repository":"octo-org/example","runner_id":5,"label":"gpu"}`)}},
}

// runnersReads and runnersChanges are the tools of the runners. The reads join the Actions administrator
// profile; the changes stay unticked, since each is offered only where a connection's tools list names it.
var (
	runnersReads   = []string{runnersList.ID, runnersGet.ID, runnerLabelsList.ID}
	runnersChanges = []string{runnersDelete.ID, runnerLabelsAdd.ID, runnerLabelsSet.ID, runnerLabelsRemove.ID}
)

// runnerArguments holds the arguments of every runner tool; the input schema of each admits only its own.
type runnerArguments struct {
	RunnerID *int64    `json:"runner_id"`
	Name     string    `json:"name"`
	Labels   *[]string `json:"labels"`
	Label    string    `json:"label"`
	Limit    int       `json:"limit"`
	Cursor   string    `json:"cursor"`

	page, perPage int
	binding       []byte
}

// runnersHandler decodes the arguments, selects the repository or the organization, and applies every check
// before a credential is resolved, so a refused request never reaches a secret or GitHub.
func runnersHandler(id string, check func(*runnerArguments, target) error,
	call func(context.Context, *Client, *runnerArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments runnerArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectRulesetTarget(resolved, raw)
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
		return bound.rulesetLocate(value, err)
	}
}

// runnersOperations binds every runner tool to its handler.
func runnersOperations() []capability.Operation {
	bind := func(d capability.Descriptor, check func(*runnerArguments, target) error,
		call func(context.Context, *Client, *runnerArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: d, Handler: runnersHandler(d.ID, check, call)}
	}
	return []capability.Operation{
		bind(runnersList, checkRunnersList, func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.listRunners(ctx, a)
		}),
		bind(runnersGet, checkRunnerID, func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.getRunner(ctx, a)
		}),
		bind(runnersDelete, checkRunnerID, func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.deleteRunner(ctx, a)
		}),
		bind(runnerLabelsList, checkRunnerID, func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.listRunnerLabels(ctx, a)
		}),
		bind(runnerLabelsAdd, checkRunnerLabels(true), func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.changeRunnerLabels(ctx, a, http.MethodPost, "add runner labels")
		}),
		bind(runnerLabelsSet, checkRunnerLabels(false), func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.changeRunnerLabels(ctx, a, http.MethodPut, "set runner labels")
		}),
		bind(runnerLabelsRemove, checkRunnerLabelRemoval, func(ctx context.Context, c *Client, a *runnerArguments) (any, error) {
			return c.removeRunnerLabel(ctx, a)
		}),
	}
}

// ---- checks

func checkRunnerID(a *runnerArguments, _ target) error {
	if a.RunnerID == nil || *a.RunnerID < 1 {
		return invalidRequest("runner_id must be a positive whole number")
	}
	return nil
}

func checkRunnersList(a *runnerArguments, bound target) error {
	limit := a.Limit
	if limit == 0 {
		limit = defaultRunnersLimit
	}
	if limit < 1 || limit > maxLimit {
		return invalidRequest(fmt.Sprintf("limit must be between 1 and %d", maxLimit))
	}
	if a.Name != "" {
		if err := checkRunnerText("name", a.Name, maxRunnerNameFilter); err != nil {
			return err
		}
	}
	a.binding = fingerprint("runners", "list", bound.String(), strings.ToLower(a.Name))
	var err error
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

// checkRunnerText bounds one line of text that travels as a query value or an opaque path segment: valid
// UTF-8 without control characters, not padded with spaces, and not a dot segment that could address another
// route once a server resolves the path.
func checkRunnerText(field, value string, limit int) error {
	if !utf8.ValidString(value) || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return invalidRequest(field + " must be one line of text without control characters")
	}
	if count := utf8.RuneCountInString(value); count < 1 || count > limit {
		return invalidRequest(fmt.Sprintf("%s must have 1 to %d characters", field, limit))
	}
	if value != strings.TrimSpace(value) {
		return invalidRequest(field + " must not start or end with a space")
	}
	if value == "." || value == ".." {
		return invalidRequest(field + " must not be . or ..")
	}
	return nil
}

// checkRunnerLabels checks the labels of an add (at least one) or a set (none allowed).
func checkRunnerLabels(atLeastOne bool) func(*runnerArguments, target) error {
	return func(a *runnerArguments, bound target) error {
		if err := checkRunnerID(a, bound); err != nil {
			return err
		}
		if a.Labels == nil {
			return invalidRequest("labels is required")
		}
		labels := *a.Labels
		if len(labels) > maxRunnerLabels || atLeastOne && len(labels) == 0 {
			if atLeastOne {
				return invalidRequest(fmt.Sprintf("labels must hold 1 to %d labels", maxRunnerLabels))
			}
			return invalidRequest(fmt.Sprintf("labels must hold at most %d labels", maxRunnerLabels))
		}
		seen := map[string]bool{}
		for _, label := range labels {
			if err := checkRunnerText("each label", label, maxRunnerLabelLength); err != nil {
				return err
			}
			if seen[strings.ToLower(label)] {
				return invalidRequest("labels must not repeat a label")
			}
			seen[strings.ToLower(label)] = true
		}
		return nil
	}
}

func checkRunnerLabelRemoval(a *runnerArguments, bound target) error {
	if err := checkRunnerID(a, bound); err != nil {
		return err
	}
	return checkRunnerText("label", a.Label, maxRunnerLabelLength)
}

// ---- transport

// runnerPath is the route of the runners below the bound repository or organization, of one runner, or of
// its labels.
func (c *Client) runnerPath(id int64, rest string) string {
	path := c.settingPath("runners")
	if id > 0 {
		path += "/" + strconv.FormatInt(id, 10)
	}
	return path + rest
}

// runnerPermission names what a refused runner request needs without claiming what the configured token holds.
func runnerPermission(t target, change bool) string {
	access, verb := "read", "reading them needs"
	if change {
		access, verb = "read and write", "changing them needs"
	}
	if t.kind == kindOwner {
		return "GitHub refused this token the self-hosted runners of this organization; " + verb + " admin:org on " +
			"a classic token, or Self-hosted runners: " + access + " of the organization on a fine-grained token"
	}
	return "GitHub refused this token the self-hosted runners of this repository; " + verb + " repo on a classic " +
		"token of a repository administrator, or Administration: " + access + " on a fine-grained token"
}

const (
	runnerDeleteRejected = "GitHub refused to remove the runner; it may be running a job or may belong to another " +
		"level than the one addressed, so nothing was removed"
	runnerLabelsRejected = "GitHub rejected the labels; check their number and names and that only custom labels " +
		"are named, so nothing was changed"
)

// runnerFailure turns a refused runner request into Qatlas' own message.
func (c *Client) runnerFailure(err error, change bool, conflict, rejected string) error {
	return variableFailure(err, runnerPermission(c.target, change), conflict, rejected)
}

// ---- projections

type runnerLabelRaw struct {
	ID   *int64  `json:"id"`
	Name *string `json:"name"`
	Type *string `json:"type"`
}

type runnerRaw struct {
	ID            *int64           `json:"id"`
	RunnerGroupID *int64           `json:"runner_group_id"`
	Name          *string          `json:"name"`
	OS            *string          `json:"os"`
	Status        *string          `json:"status"`
	Busy          *bool            `json:"busy"`
	Labels        []runnerLabelRaw `json:"labels"`
	Ephemeral     *bool            `json:"ephemeral"`
	Version       *string          `json:"version"`
}

// RunnerLabel is one label of a runner. Name is untrusted data.
type RunnerLabel struct {
	ID   *int64 `json:"id,omitempty"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Runner is one self-hosted runner. Name, OS, Status, Version, and the labels are untrusted data and cut.
type Runner struct {
	ID            int64         `json:"id"`
	RunnerGroupID *int64        `json:"runner_group_id,omitempty"`
	Name          string        `json:"name"`
	OS            string        `json:"os,omitempty"`
	Status        string        `json:"status,omitempty"`
	Busy          bool          `json:"busy"`
	Labels        []RunnerLabel `json:"labels"`
	Ephemeral     *bool         `json:"ephemeral,omitempty"`
	Version       string        `json:"version,omitempty"`
}

func viewRunnerLabels(raw []runnerLabelRaw) ([]RunnerLabel, bool) {
	if len(raw) > maxRunnerEntryLabels {
		return nil, false
	}
	labels := make([]RunnerLabel, 0, len(raw))
	for _, entry := range raw {
		name, _ := cleanText(filled(entry.Name), projectionNameLimit, false)
		if name == nil {
			return nil, false
		}
		label := RunnerLabel{ID: entry.ID, Name: *name}
		if kind := nameText(entry.Type); kind != nil {
			label.Type = *kind
		}
		labels = append(labels, label)
	}
	return labels, true
}

// view projects a runner, or reports false for one without a usable identifier, name, or label list.
func (r runnerRaw) view() (Runner, bool) {
	name, _ := cleanText(filled(r.Name), projectionNameLimit, false)
	labels, ok := viewRunnerLabels(r.Labels)
	if r.ID == nil || *r.ID < 1 || name == nil || !ok {
		return Runner{}, false
	}
	runner := Runner{ID: *r.ID, RunnerGroupID: r.RunnerGroupID, Name: *name, Labels: labels, Ephemeral: r.Ephemeral}
	if r.Busy != nil {
		runner.Busy = *r.Busy
	}
	if text := nameText(r.OS); text != nil {
		runner.OS = *text
	}
	if text := nameText(r.Status); text != nil {
		runner.Status = *text
	}
	if text := nameText(r.Version); text != nil {
		runner.Version = *text
	}
	return runner, true
}

// RunnerList is one batch of runners.
type RunnerList struct {
	Runners    []Runner `json:"runners"`
	TotalCount int64    `json:"total_count"`
	NextCursor string   `json:"next_cursor,omitempty"`
	HasMore    bool     `json:"has_more"`
}

// RunnerDeleted is the answer to a removed runner.
type RunnerDeleted struct {
	Deleted  bool  `json:"deleted"`
	RunnerID int64 `json:"runner_id"`
}

// RunnerLabels is the label list of one runner as GitHub reports it.
type RunnerLabels struct {
	RunnerID   int64         `json:"runner_id"`
	TotalCount int64         `json:"total_count"`
	Labels     []RunnerLabel `json:"labels"`
}

type runnerLabelsRaw struct {
	TotalCount *int64           `json:"total_count"`
	Labels     []runnerLabelRaw `json:"labels"`
}

func (r runnerLabelsRaw) view(id int64) (*RunnerLabels, bool) {
	labels, ok := viewRunnerLabels(r.Labels)
	if !ok {
		return nil, false
	}
	count := int64(len(labels))
	if r.TotalCount != nil {
		count = *r.TotalCount
	}
	return &RunnerLabels{RunnerID: id, TotalCount: count, Labels: labels}, true
}

// ---- reads and changes

func (c *Client) listRunners(ctx context.Context, a *runnerArguments) (*RunnerList, error) {
	const op = "list runners"
	var raw struct {
		TotalCount int64       `json:"total_count"`
		Runners    []runnerRaw `json:"runners"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if a.Name != "" {
		query.Set("name", a.Name)
	}
	if err := c.rest(ctx, op, c.runnerPath(0, "")+"?"+query.Encode(), &raw); err != nil {
		return nil, c.runnerFailure(err, false, "", "")
	}
	result := &RunnerList{Runners: make([]Runner, 0, len(raw.Runners)), TotalCount: raw.TotalCount}
	for _, entry := range raw.Runners {
		runner, ok := entry.view()
		if !ok {
			return nil, invalidEntry(op, "a runner")
		}
		// The name filter is an optimization at GitHub; an entry of another name is never passed on.
		if a.Name != "" && !strings.EqualFold(runner.Name, a.Name) {
			continue
		}
		result.Runners = append(result.Runners, runner)
	}
	// A short page ends the list whatever the reported total says.
	if len(raw.Runners) == a.perPage && int64(a.page*a.perPage) < raw.TotalCount && a.page < maxPage {
		result.HasMore = true
		result.NextCursor = encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
	}
	return result, nil
}

// readRunner reads one runner below the bound target. It is the evidence that binds runner_id to the chosen
// repository or organization, and refuses an answer that names another runner.
func (c *Client) readRunner(ctx context.Context, op string, id int64) (*runnerRaw, error) {
	var raw runnerRaw
	if err := c.rest(ctx, op, c.runnerPath(id, ""), &raw); err != nil {
		return nil, c.runnerFailure(err, false, "", "")
	}
	if raw.ID == nil || *raw.ID != id {
		return nil, invalidEntry(op, "a runner")
	}
	return &raw, nil
}

func (c *Client) getRunner(ctx context.Context, a *runnerArguments) (*Runner, error) {
	const op = "get runner"
	raw, err := c.readRunner(ctx, op, *a.RunnerID)
	if err != nil {
		return nil, err
	}
	runner, ok := raw.view()
	if !ok {
		return nil, invalidEntry(op, "a runner")
	}
	return &runner, nil
}

func (c *Client) deleteRunner(ctx context.Context, a *runnerArguments) (*RunnerDeleted, error) {
	const op = "remove runner"
	if _, err := c.readRunner(ctx, op, *a.RunnerID); err != nil {
		return nil, err
	}
	if err := c.restChange(ctx, op, http.MethodDelete, c.runnerPath(*a.RunnerID, ""), nil, nil); err != nil {
		return nil, c.runnerFailure(err, true, "", runnerDeleteRejected)
	}
	return &RunnerDeleted{Deleted: true, RunnerID: *a.RunnerID}, nil
}

func (c *Client) listRunnerLabels(ctx context.Context, a *runnerArguments) (*RunnerLabels, error) {
	const op = "list runner labels"
	var raw runnerLabelsRaw
	if err := c.rest(ctx, op, c.runnerPath(*a.RunnerID, "/labels"), &raw); err != nil {
		return nil, c.runnerFailure(err, false, "", "")
	}
	labels, ok := raw.view(*a.RunnerID)
	if !ok {
		return nil, invalidEntry(op, "a runner label")
	}
	return labels, nil
}

// readOnlyNamed reports whether name is a read-only label of the runner, compared without case. GitHub applies
// such labels itself and cannot remove or set them.
func (r *runnerRaw) readOnlyNamed(name string) bool {
	for _, label := range r.Labels {
		if label.Name != nil && label.Type != nil && *label.Type == readOnlyLabelType && strings.EqualFold(*label.Name, name) {
			return true
		}
	}
	return false
}

const readOnlyLabelMessage = "is a read-only label of the runner, which GitHub applies itself and cannot " +
	"change; only custom labels can be named, so nothing was sent"

// changeRunnerLabels adds or replaces the custom labels with one request, after the runner was read below the
// chosen target, and answers the labels GitHub reports.
func (c *Client) changeRunnerLabels(ctx context.Context, a *runnerArguments, method, op string) (*RunnerLabels, error) {
	runner, err := c.readRunner(ctx, op, *a.RunnerID)
	if err != nil {
		return nil, err
	}
	for _, label := range *a.Labels {
		if runner.readOnlyNamed(label) {
			return nil, invalidRequest("a named label " + readOnlyLabelMessage)
		}
	}
	var raw runnerLabelsRaw
	err = c.restChange(ctx, op, method, c.runnerPath(*a.RunnerID, "/labels"), map[string]any{"labels": *a.Labels}, &raw)
	return c.labelsAnswer(op, *a.RunnerID, &raw, err)
}

// removeRunnerLabel removes one custom label with one request, after the runner was read below the chosen
// target; a read-only label is refused before anything is sent.
func (c *Client) removeRunnerLabel(ctx context.Context, a *runnerArguments) (*RunnerLabels, error) {
	const op = "remove runner label"
	runner, err := c.readRunner(ctx, op, *a.RunnerID)
	if err != nil {
		return nil, err
	}
	if runner.readOnlyNamed(a.Label) {
		return nil, invalidRequest("the label " + readOnlyLabelMessage)
	}
	var raw runnerLabelsRaw
	err = c.restChange(ctx, op, http.MethodDelete, c.runnerPath(*a.RunnerID, "/labels/"+url.PathEscape(a.Label)), nil, &raw)
	return c.labelsAnswer(op, *a.RunnerID, &raw, err)
}

// labelsAnswer maps the outcome of one label change. An unreadable answer after the request may have been
// applied says so instead of claiming either result.
func (c *Client) labelsAnswer(op string, id int64, raw *runnerLabelsRaw, err error) (*RunnerLabels, error) {
	if err != nil {
		return nil, c.runnerFailure(err, true, "", runnerLabelsRejected)
	}
	labels, ok := raw.view(id)
	if !ok {
		return nil, invalidResponse(op, true)
	}
	return labels, nil
}

// runnerSubject names the runners a path below a repository or an organization addresses: actions/runners,
// a runner by its numeric identifier, or its labels. A label is never named, since its name is untrusted.
func runnerSubject(path string) string {
	rest, ok := strings.CutPrefix(path, "actions/runners")
	if !ok {
		return ""
	}
	if rest == "" {
		return "the self-hosted runners"
	}
	rest, ok = strings.CutPrefix(rest, "/")
	if !ok {
		return ""
	}
	value, tail, _ := strings.Cut(rest, "/")
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id < 1 {
		return "a self-hosted runner"
	}
	switch {
	case tail == "":
		return "self-hosted runner " + value
	case tail == "labels":
		return "the labels of self-hosted runner " + value
	case strings.HasPrefix(tail, "labels/"):
		return "a label of self-hosted runner " + value
	}
	return "self-hosted runner " + value
}

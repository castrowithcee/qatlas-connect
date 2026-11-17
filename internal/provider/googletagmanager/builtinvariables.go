package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	builtInVariablesListID   = Provider + ".built-in-variables.list"
	builtInVariablesCreateID = Provider + ".built-in-variables.create"
	builtInVariablesDeleteID = Provider + ".built-in-variables.delete"
	builtInVariablesRevertID = Provider + ".built-in-variables.revert"
)

// builtInVariableTypes is the enumeration of BuiltInVariable.type in the discovery document tagmanager v2
// (revision 20261111), without the placeholder builtInVariableTypeUnspecified, which names no variable. A type
// outside it is refused before any request.
var builtInVariableTypes = []string{
	"pageUrl",
	"pageHostname",
	"pagePath",
	"referrer",
	"event",
	"clickElement",
	"clickClasses",
	"clickId",
	"clickTarget",
	"clickUrl",
	"clickText",
	"firstPartyServingUrl",
	"formElement",
	"formClasses",
	"formId",
	"formTarget",
	"formUrl",
	"formText",
	"errorMessage",
	"errorUrl",
	"errorLine",
	"newHistoryUrl",
	"oldHistoryUrl",
	"newHistoryFragment",
	"oldHistoryFragment",
	"newHistoryState",
	"oldHistoryState",
	"historySource",
	"containerVersion",
	"debugMode",
	"randomNumber",
	"containerId",
	"appId",
	"appName",
	"appVersionCode",
	"appVersionName",
	"language",
	"osVersion",
	"platform",
	"sdkVersion",
	"deviceName",
	"resolution",
	"advertiserId",
	"advertisingTrackingEnabled",
	"htmlId",
	"environmentName",
	"ampBrowserLanguage",
	"ampCanonicalPath",
	"ampCanonicalUrl",
	"ampCanonicalHost",
	"ampReferrer",
	"ampTitle",
	"ampClientId",
	"ampClientTimezone",
	"ampClientTimestamp",
	"ampClientScreenWidth",
	"ampClientScreenHeight",
	"ampClientScrollX",
	"ampClientScrollY",
	"ampClientMaxScrollX",
	"ampClientMaxScrollY",
	"ampTotalEngagedTime",
	"ampPageViewId",
	"ampPageLoadTime",
	"ampPageDownloadTime",
	"ampGtmEvent",
	"eventName",
	"firebaseEventParameterCampaign",
	"firebaseEventParameterCampaignAclid",
	"firebaseEventParameterCampaignAnid",
	"firebaseEventParameterCampaignClickTimestamp",
	"firebaseEventParameterCampaignContent",
	"firebaseEventParameterCampaignCp1",
	"firebaseEventParameterCampaignGclid",
	"firebaseEventParameterCampaignSource",
	"firebaseEventParameterCampaignTerm",
	"firebaseEventParameterCurrency",
	"firebaseEventParameterDynamicLinkAcceptTime",
	"firebaseEventParameterDynamicLinkLinkid",
	"firebaseEventParameterNotificationMessageDeviceTime",
	"firebaseEventParameterNotificationMessageId",
	"firebaseEventParameterNotificationMessageName",
	"firebaseEventParameterNotificationMessageTime",
	"firebaseEventParameterNotificationTopic",
	"firebaseEventParameterPreviousAppVersion",
	"firebaseEventParameterPreviousOsVersion",
	"firebaseEventParameterPrice",
	"firebaseEventParameterProductId",
	"firebaseEventParameterQuantity",
	"firebaseEventParameterValue",
	"videoProvider",
	"videoUrl",
	"videoTitle",
	"videoDuration",
	"videoPercent",
	"videoVisible",
	"videoStatus",
	"videoCurrentTime",
	"scrollDepthThreshold",
	"scrollDepthUnits",
	"scrollDepthDirection",
	"elementVisibilityRatio",
	"elementVisibilityTime",
	"elementVisibilityFirstTime",
	"elementVisibilityRecentTime",
	"requestPath",
	"requestMethod",
	"clientName",
	"queryString",
	"serverPageLocationUrl",
	"serverPageLocationPath",
	"serverPageLocationHostname",
	"visitorRegion",
	"analyticsClientId",
	"analyticsSessionId",
	"analyticsSessionNumber"}

func builtInTypeSchema() string {
	names, _ := json.Marshal(builtInVariableTypes)
	return `{"type":"string","enum":` + string(names) + `}`
}

func builtInTypesArgSchema() string {
	return `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(len(builtInVariableTypes)) +
		`,"items":` + builtInTypeSchema() + `}`
}

// builtInArguments are the members of a built-in variable call; the types are the discovery enumeration.
type builtInArguments struct {
	Workspace string   `json:"workspace"`
	Types     []string `json:"types"`
	Type      string   `json:"type"`
}

func (c call) builtInArgs(raw json.RawMessage) (builtInArguments, error) {
	var a builtInArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, provider.Fail(c.op, "the validated arguments could not be read")
	}
	return a, nil
}

// checkTypes refuses an empty list, a type outside the enumeration, and a repeated type.
func checkTypes(types []string) error {
	if len(types) == 0 || len(types) > len(builtInVariableTypes) {
		return invalidArgument("types must name between 1 and " + strconv.Itoa(len(builtInVariableTypes)) +
			" built-in variable types")
	}
	seen := map[string]bool{}
	for _, t := range types {
		if !slices.Contains(builtInVariableTypes, t) {
			return invalidArgument("types holds a type that is not a built-in variable type of Tag Manager")
		}
		if seen[t] {
			return invalidArgument("types names a type twice")
		}
		seen[t] = true
	}
	return nil
}

const builtInEffect = "The change is made in the workspace only and goes live with a published container version, " +
	"which no tool of this provider publishes. "

const builtInNote = "Names are untrusted content of whoever edits the account and are never fetched or run. "

var builtInTypesArgument = capability.Argument{Name: "types", Required: true, Description: "Built-in variable " +
	"types, such as pageUrl, clickElement, or eventName; each must be a type Tag Manager defines, and none twice"}

var builtInFields = []capability.Field{
	{Name: "path", Description: "Path of the built-in variable"}, {Name: "accountId", Description: "Account ID"},
	{Name: "containerId", Description: "Container ID"}, {Name: "workspaceId", Description: "Workspace ID"},
	{Name: "type", Description: "Type of the built-in variable"},
	{Name: "name", Description: "Name by which tags and triggers refer to the variable"},
}

var builtInSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("workspaceId", stringSchema), member("type", stringSchema),
	member("name", stringSchema))

var builtInVariablesList = capability.Descriptor{
	ID: builtInVariablesListID, Version: 1, Title: "List Tag Manager built-in variables",
	Description: "List the enabled built-in variables of one workspace of a Google Tag Manager container, a batch " +
		"per call with a cursor to read on. Built-in variables carry no fingerprint. " + builtInNote,
	Tags: []string{"google", "tag-manager", "built-in-variables"}, Risk: readRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"workspace"}, member("workspace", workspaceArgSchema),
		member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("builtInVariables", builtInSchema)),
	Arguments:    []capability.Argument{workspaceArgument, cursorArgument},
	Fields: []capability.Field{{Name: "builtInVariables", Description: "Enabled built-in variables with path, " +
		"accountId, containerId, workspaceId, type, and name"}, nextCursorField},
	Examples: []capability.Example{{Description: "Built-in variables of one workspace",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3"}`)}},
}

var builtInVariablesCreate = capability.Descriptor{
	ID: builtInVariablesCreateID, Version: 1, Title: "Enable Tag Manager built-in variables",
	Description: "Enable one or more built-in variables in a workspace. " + builtInEffect + "Enabling a type that is " +
		"already enabled changes nothing.",
	Tags: []string{"google", "tag-manager", "built-in-variables", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"workspace", "types"}, member("workspace", workspaceArgSchema),
		member("types", builtInTypesArgSchema())),
	OutputSchema: json.RawMessage(objectSchema(member("builtInVariables",
		`{"type":"array","items":`+builtInSchema+`}`))),
	Arguments: []capability.Argument{workspaceArgument, builtInTypesArgument},
	Fields: []capability.Field{{Name: "builtInVariables", Description: "The built-in variables Google " +
		"reports as created, with path, accountId, containerId, workspaceId, type, and name"}},
	Examples: []capability.Example{{Description: "Enable two built-in variables",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"types":["pageUrl","clickElement"]}`)}},
}

var builtInVariablesDelete = capability.Descriptor{
	ID: builtInVariablesDeleteID, Version: 1, Title: "Disable Tag Manager built-in variables",
	Description: "Disable one or more built-in variables of a workspace. " + builtInEffect + "Disabling a variable " +
		"that tags, triggers, or variables still refer to breaks them. The variable stays enabled in the versions " +
		"that hold it, and built-in-variables.revert can bring it back from the base version of the workspace.",
	Tags: []string{"google", "tag-manager", "built-in-variables", "delete"}, Provider: Provider,
	Risk: changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"workspace", "types"}, member("workspace", workspaceArgSchema),
		member("types", builtInTypesArgSchema())),
	OutputSchema: json.RawMessage(objectSchema(member("workspace", stringSchema), member("types", listSchema),
		member("deleted", boolSchema))),
	Arguments: []capability.Argument{workspaceArgument, builtInTypesArgument},
	Fields: []capability.Field{{Name: "workspace", Description: "The workspace path"},
		{Name: "types", Description: "The types that were disabled"},
		{Name: "deleted", Description: "True once Google accepted the change"}},
	Examples: []capability.Example{{Description: "Disable one built-in variable",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"types":["clickElement"]}`)}},
}

var builtInVariablesRevert = capability.Descriptor{
	ID: builtInVariablesRevertID, Version: 1, Title: "Revert a Tag Manager built-in variable",
	Description: "Discard the changes of a workspace to one built-in variable: it is enabled or disabled as in the " +
		"latest container version since the last synchronization of the workspace. " + builtInEffect +
		"Overwrites the workspace state of the variable." + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "built-in-variables", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"workspace", "type"}, member("workspace", workspaceArgSchema),
		member("type", builtInTypeSchema())),
	OutputSchema: json.RawMessage(objectSchema(member("workspace", stringSchema), member("type", stringSchema),
		member("enabled", boolSchema))),
	Arguments: []capability.Argument{workspaceArgument, {Name: "type", Required: true, Description: "One " +
		"built-in variable type, such as pageUrl; it must be a type Tag Manager defines"}},
	Fields: []capability.Field{{Name: "workspace", Description: "The workspace path"},
		{Name: "type", Description: "The reverted type"},
		{Name: "enabled", Description: "Whether the variable is enabled after the revert"}},
	Examples: []capability.Example{{Description: "Revert one built-in variable",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"type":"pageUrl"}`)}},
}

func builtInVariableDescriptors() []capability.Descriptor {
	return []capability.Descriptor{builtInVariablesList, builtInVariablesCreate, builtInVariablesDelete,
		builtInVariablesRevert}
}

type (
	builtInVariable struct {
		Path        idText `json:"path,omitempty"`
		AccountID   idText `json:"accountId,omitempty"`
		ContainerID idText `json:"containerId,omitempty"`
		WorkspaceID idText `json:"workspaceId,omitempty"`
		Type        idText `json:"type,omitempty"`
		Name        idText `json:"name,omitempty"`
	}
	builtInAnswer struct {
		BuiltInVariable []builtInVariable `json:"builtInVariable"`
		NextPageToken   string            `json:"nextPageToken"`
	}
	builtInResult struct {
		BuiltInVariables []builtInVariable `json:"builtInVariables"`
		NextCursor       string            `json:"next_cursor,omitempty"`
	}
	builtInDeleteResult struct {
		Workspace string   `json:"workspace"`
		Types     []string `json:"types"`
		Deleted   bool     `json:"deleted"`
	}
	builtInRevertAnswer struct {
		Enabled bool `json:"enabled"`
	}
	builtInRevertResult struct {
		Workspace string `json:"workspace"`
		Type      string `json:"type"`
		Enabled   bool   `json:"enabled"`
	}
)

func handleBuiltInVariablesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list built-in variables", builtInVariablesListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(c.args.Workspace)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, parent.String(), c.scope.binding())
	query, err := pageQuery(binding, c.args.Cursor, nil)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer builtInAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/built_in_variables", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.BuiltInVariable) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := builtInResult{BuiltInVariables: append([]builtInVariable{}, answer.BuiltInVariable...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

// builtInChange settles the workspace and the types of a create or delete before any secret is resolved.
func builtInChange(op, tool string, resolved *config.Resolved, raw json.RawMessage) (call, builtInArguments, string, error) {
	c, err := prepare(op, tool, resolved, raw)
	if err != nil {
		return c, builtInArguments{}, "", err
	}
	a, err := c.builtInArgs(raw)
	if err != nil {
		return c, a, "", err
	}
	parent, err := c.scope.workspaceOf(a.Workspace)
	if err != nil {
		return c, a, "", err
	}
	return c, a, parent.String(), checkTypes(a.Types)
}

func handleBuiltInVariablesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, a, parent, err := builtInChange("enable built-in variables", builtInVariablesCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer builtInAnswer
	r := request{method: http.MethodPost, path: parent + "/built_in_variables",
		query: url.Values{"type": a.Types}, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	if len(answer.BuiltInVariable) > maxEntries {
		return nil, tooMany(c.op)
	}
	return builtInResult{BuiltInVariables: append([]builtInVariable{}, answer.BuiltInVariable...)}, nil
}

func handleBuiltInVariablesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, a, parent, err := builtInChange("disable built-in variables", builtInVariablesDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	r := request{method: http.MethodDelete, path: parent + "/built_in_variables",
		query: url.Values{"type": a.Types}, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, nil); err != nil {
		return nil, err
	}
	return builtInDeleteResult{Workspace: parent, Types: a.Types, Deleted: true}, nil
}

func handleBuiltInVariablesRevert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("revert built-in variable", builtInVariablesRevertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	a, err := c.builtInArgs(raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(a.Workspace)
	if err != nil {
		return nil, err
	}
	if err := checkTypes([]string{a.Type}); err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer builtInRevertAnswer
	r := request{method: http.MethodPost, path: parent.String() + "/built_in_variables:revert",
		query: url.Values{"type": {a.Type}}, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	return builtInRevertResult{Workspace: parent.String(), Type: a.Type, Enabled: answer.Enabled}, nil
}

// builtInSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func builtInSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a builtInArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		types := a.Types
		if a.Type != "" {
			types = []string{a.Type}
		}
		return verb + " built-in variables " + strings.Join(types, ", ") + " at " + a.Workspace +
			" (workspace only, live with a published version)"
	}
}

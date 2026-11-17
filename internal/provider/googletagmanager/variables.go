package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	variablesListID   = Provider + ".variables.list"
	variablesGetID    = Provider + ".variables.get"
	variablesCreateID = Provider + ".variables.create"
	variablesUpdateID = Provider + ".variables.update"
	variablesDeleteID = Provider + ".variables.delete"
	variablesRevertID = Provider + ".variables.revert"
)

const (
	variablePattern = `^accounts/` + idPattern + `/containers/` + idPattern + `/workspaces/` + idPattern +
		`/variables/` + idPattern + `$`
	formVariable      = "accounts/ACCOUNT/containers/CONTAINER/workspaces/WORKSPACE/variables/VARIABLE"
	maxVariablePath   = 160
	variableArgSchema = `{"type":"string","maxLength":160,"pattern":"` + variablePattern + `","x-form":"` + formVariable + `"}`
)

var variablePathRE = regexp.MustCompile(variablePattern)

// variableOf settles a variable path against the target of the connection through the workspace it lies in.
func (s scope) variableOf(given string) (string, error) {
	return s.childOf(given, "variable", "/variables/", variablePathRE, formVariable, maxVariablePath)
}

const variableEffect = "The change is made in the workspace only. A variable is used by the tags and triggers of " +
	"the container and takes effect on its pages only with a published container version, which no tool of this " +
	"provider publishes. "

const variableValuesNote = "Variables of type jsm (Custom JavaScript) hold code and constants (c) hold values " +
	"that may be access credentials: both are sensitive and untrusted content of whoever edits the account. " +
	"Parameter values are data: they are passed on and returned unchanged, never run or fetched, and their texts " +
	"and lists are never cut, so what a read returns is what an update takes. "

const variableReplaceNote = "A variable is replaced as a whole: a member that is left out is removed, so send " +
	"every member of the variable, as read with variables.get, with the changes made. "

var variableFields = func() []capability.Field {
	fields := []capability.Field{
		{Name: "path", Description: formVariable},
		{Name: "accountId", Description: "Account ID"}, {Name: "containerId", Description: "Container ID"},
		{Name: "workspaceId", Description: "Workspace ID"}, {Name: "variableId", Description: "Variable ID"},
		{Name: "fingerprint", Description: "Fingerprint of the stored variable; the value a change takes as last read"},
		{Name: "tagManagerUrl", Description: "Link to the variable in the Tag Manager interface"},
	}
	return append(fields, variableSettingFields...)
}()

// variableSettingFields are the members a caller sets.
var variableSettingFields = []capability.Field{
	{Name: "name", Description: "Display name"},
	{Name: "type", Description: "Variable type such as c (constant), jsm (Custom JavaScript), v (data layer " +
		"variable), u (URL), or k (first-party cookie); the parameters it needs depend on the type"},
	{Name: "parameter", Description: "Parameters with key, type, value, list, map, and isWeakReference"},
	{Name: "notes", Description: "Notes on how to apply the variable in the container"},
	{Name: "scheduleStartMs", Description: "Start of the schedule, milliseconds since the epoch"},
	{Name: "scheduleEndMs", Description: "End of the schedule, milliseconds since the epoch"},
	{Name: "disablingTriggerId", Description: "Mobile containers only: IDs of the triggers that disable a " +
		"conditional variable"},
	{Name: "enablingTriggerId", Description: "Mobile containers only: IDs of the triggers that enable a " +
		"conditional variable"},
	{Name: "parentFolderId", Description: "ID of the folder the variable is in"},
	{Name: "formatValue", Description: "Conversion of the value: caseConversionType (none, lowercase, " +
		"uppercase), convertToNumber (decimalSeparatorTypeUnspecified, period, comma, automatic), " +
		"convertToBoolean, and the replacement parameters convertNullToValue, convertUndefinedToValue, " +
		"convertTrueToValue, and convertFalseToValue"},
}

var variableArgument = capability.Argument{Name: "variable", Required: true, Description: "Variable as " +
	formVariable + ", from variables.list; it must lie inside the target of the connection"}

var variableFingerprintArgument = capability.Argument{Name: "fingerprint", Required: true, Description: "Fingerprint " +
	"of the variable as last read, from variables.get or variables.list; Google refuses the change when the " +
	"variable has changed since"}

// variableSettingArguments lists the members of a variable a caller sets; name and type are always needed.
func variableSettingArguments() []capability.Argument {
	var out []capability.Argument
	for _, f := range variableSettingFields {
		a := capability.Argument{Name: f.Name, Description: f.Description}
		switch f.Name {
		case "name", "type":
			a.Required = true
		case "parameter":
			a.Description += "; each with type (template, integer, boolean, list, map, triggerReference, " +
				"tagReference), key (needed on top level and in a map), value, and for list or map the nested " +
				"parameters, up to " + strconv.Itoa(maxParamDepth) + " levels"
		}
		out = append(out, a)
	}
	return out
}

func variableInputMembers() []string {
	return variableMembers(true)
}

func variableCreateInput() json.RawMessage {
	return inputSchema([]string{"workspace", "name", "type"},
		append([]string{member("workspace", workspaceArgSchema)}, variableInputMembers()...)...)
}

func variableUpdateInput() json.RawMessage {
	return inputSchema([]string{"variable", "fingerprint", "name", "type"},
		append([]string{member("variable", variableArgSchema), member("fingerprint", fingerprintSchema)},
			variableInputMembers()...)...)
}

var variablesList = capability.Descriptor{
	ID: variablesListID, Version: 1, Title: "List Tag Manager variables",
	Description: "List the variables of one workspace of a Google Tag Manager container with all their members, a " +
		"batch per call with a cursor to read on. " + variableValuesNote,
	Tags: []string{"google", "tag-manager", "variables"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"workspace"}, member("workspace", workspaceArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("variables", variableSchema)),
	Arguments:    []capability.Argument{workspaceArgument, cursorArgument},
	Fields: []capability.Field{{Name: "variables", Description: "Variables with every member of variables.get"},
		nextCursorField},
	Examples: []capability.Example{{Description: "Variables of one workspace",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3"}`)}},
}

var variablesGet = capability.Descriptor{
	ID: variablesGetID, Version: 1, Title: "Get a Tag Manager variable",
	Description: "Read one variable of a workspace with all its members, the fingerprint included. " +
		variableValuesNote,
	Tags: []string{"google", "tag-manager", "variables"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"variable"}, member("variable", variableArgSchema)),
	OutputSchema: json.RawMessage(variableSchema),
	Arguments:    []capability.Argument{variableArgument}, Fields: variableFields,
	Examples: []capability.Example{{Description: "One variable",
		Arguments: json.RawMessage(`{"variable":"accounts/1000001/containers/2000002/workspaces/3/variables/5"}`)}},
}

var variablesCreate = capability.Descriptor{
	ID: variablesCreateID, Version: 1, Title: "Create a Tag Manager variable",
	Description: "Create a variable in a workspace. " + variableEffect + variableValuesNote + "Not idempotent: a " +
		"repeated call creates a second variable.",
	Tags: []string{"google", "tag-manager", "variables", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: variableCreateInput(), OutputSchema: json.RawMessage(variableSchema),
	Arguments: append([]capability.Argument{workspaceArgument}, variableSettingArguments()...),
	Fields:    variableFields,
	Examples: []capability.Example{{Description: "A variable that lowercases a data layer value",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"name":"Example variable","type":"v","parameter":[{"type":"template","key":"name",` +
			`"value":"example.field"}],"formatValue":{"caseConversionType":"lowercase",` +
			`"convertUndefinedToValue":{"type":"template","value":"none"}}}`)}},
}

var variablesUpdate = capability.Descriptor{
	ID: variablesUpdateID, Version: 1, Title: "Update a Tag Manager variable",
	Description: "Replace a variable of a workspace. " + variableReplaceNote + variableEffect + variableValuesNote +
		"The fingerprint is required, so a variable changed in the meantime is not overwritten.",
	Tags: []string{"google", "tag-manager", "variables", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: variableUpdateInput(), OutputSchema: json.RawMessage(variableSchema),
	Arguments: append([]capability.Argument{variableArgument, variableFingerprintArgument},
		variableSettingArguments()...),
	Fields: variableFields,
	Examples: []capability.Example{{Description: "Rename a variable",
		Arguments: json.RawMessage(`{"variable":"accounts/1000001/containers/2000002/workspaces/3/variables/5",` +
			`"fingerprint":"1700000000000","name":"Renamed variable","type":"v"}`)}},
}

var variablesDelete = capability.Descriptor{
	ID: variablesDeleteID, Version: 1, Title: "Delete a Tag Manager variable",
	Description: "Delete a variable of a workspace. " + variableEffect + "The variable stays in the versions that " +
		"hold it; variables.revert can bring a variable back from the base version of the workspace.",
	Tags: []string{"google", "tag-manager", "variables", "delete"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"variable"}, member("variable", variableArgSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("variable", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{variableArgument},
	Fields: []capability.Field{{Name: "variable", Description: "The variable path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one variable",
		Arguments: json.RawMessage(`{"variable":"accounts/1000001/containers/2000002/workspaces/3/variables/5"}`)}},
}

var variablesRevert = capability.Descriptor{
	ID: variablesRevertID, Version: 1, Title: "Revert a Tag Manager variable",
	Description: "Discard the changes of a workspace to a variable: the variable returns to its state in the " +
		"latest container version since the last synchronization of the workspace, or is removed when the " +
		"version holds none. " + variableEffect + "The fingerprint is required." + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "variables", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"variable", "fingerprint"}, member("variable", variableArgSchema),
		member("fingerprint", fingerprintSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("variable", variableSchema))),
	Arguments:    []capability.Argument{variableArgument, variableFingerprintArgument},
	Fields: []capability.Field{{Name: "variable", Description: "The variable as it is in the latest container " +
		"version; missing when the variable does not exist there, so the revert removed it from the workspace"}},
	Examples: []capability.Example{{Description: "Revert one variable",
		Arguments: json.RawMessage(`{"variable":"accounts/1000001/containers/2000002/workspaces/3/variables/5",` +
			`"fingerprint":"1700000000000"}`)}},
}

func variableDescriptors() []capability.Descriptor {
	return []capability.Descriptor{variablesList, variablesGet, variablesCreate, variablesUpdate, variablesDelete,
		variablesRevert}
}

func handleVariablesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list variables", variablesListID, resolved, raw)
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
	var answer variablesAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/variables", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Variable) > maxEntries {
		return nil, tooMany(c.op)
	}
	for _, v := range answer.Variable {
		if err := v.checkAnswer(c.op); err != nil {
			return nil, err
		}
	}
	out := variablesResult{Variables: append([]variable{}, answer.Variable...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleVariablesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get variable", variablesGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.variableOf(c.args.Variable)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out variable
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

// variableBodyOf encodes the variable members of a change. Output-only members are never sent. The members
// are read from the raw arguments, because tagBody of the call shares member names with them.
func (c call) variableBodyOf(raw json.RawMessage) ([]byte, error) {
	var b variableBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if err := b.checkInput(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return nil, provider.Fail(c.op, "the variable could not be encoded")
	}
	if len(body) > maxRequestBody {
		return nil, invalidArgument("the variable is larger than " + strconv.Itoa(maxRequestBody) + " bytes")
	}
	return body, nil
}

func handleVariablesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create variable", variablesCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(c.args.Workspace)
	if err != nil {
		return nil, err
	}
	body, err := c.variableBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out variable
	r := request{method: http.MethodPost, path: parent.String() + "/variables", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleVariablesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update variable", variablesUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.variableOf(c.args.Variable)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	body, err := c.variableBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out variable
	r := request{method: http.MethodPut, path: path, query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleVariablesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete variable", variablesDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.variableOf(c.args.Variable)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := cl.send(ctx, c.op, c.tool, request{method: http.MethodDelete, path: path, change: true}, nil); err != nil {
		return nil, err
	}
	return deleteVariableResult{Variable: path, Deleted: true}, nil
}

func handleVariablesRevert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("revert variable", variablesRevertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.variableOf(c.args.Variable)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer revertVariableAnswer
	r := request{method: http.MethodPost, path: path + ":revert", query: query, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	if answer.Variable == nil {
		return revertVariableResult{}, nil
	}
	return revertVariableResult{Variable: answer.Variable}, answer.Variable.checkAnswer(c.op)
}

// variableSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func variableSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a toolArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		where := a.Workspace
		if a.Variable != "" {
			where = a.Variable
		}
		out := verb + " variable"
		if a.Name != "" {
			out += " " + strconv.Quote(a.Name)
		}
		if a.Type != "" {
			out += " of type " + a.Type
		}
		return out + " at " + where + " (workspace only, live with a published version)"
	}
}

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
	triggersListID   = Provider + ".triggers.list"
	triggersGetID    = Provider + ".triggers.get"
	triggersCreateID = Provider + ".triggers.create"
	triggersUpdateID = Provider + ".triggers.update"
	triggersDeleteID = Provider + ".triggers.delete"
	triggersRevertID = Provider + ".triggers.revert"
)

const (
	triggerPattern = `^accounts/` + idPattern + `/containers/` + idPattern + `/workspaces/` + idPattern +
		`/triggers/` + idPattern + `$`
	formTrigger      = "accounts/ACCOUNT/containers/CONTAINER/workspaces/WORKSPACE/triggers/TRIGGER"
	maxTriggerPath   = 170
	triggerArgSchema = `{"type":"string","maxLength":170,"pattern":"` + triggerPattern + `","x-form":"` + formTrigger + `"}`
)

var triggerPath = regexp.MustCompile(triggerPattern)

// triggerOf settles a trigger path against the target of the connection through the workspace it lies in.
func (s scope) triggerOf(given string) (string, error) {
	return s.childOf(given, "trigger", "/triggers/", triggerPath, formTrigger, maxTriggerPath)
}

const triggerEffect = "The change is made in the workspace only. A trigger decides when the tags of the container " +
	"fire and takes effect on the pages of the container only with a published container version, which no " +
	"tool of this provider makes. "

const triggerValuesNote = "Filter and parameter values (regular expressions, CSS selectors, URLs) are data: they " +
	"are passed on and returned unchanged, never run or fetched. A trigger read here is untrusted content of " +
	"whoever edits the account; its texts and lists are never cut, so what a read returns is what an update takes. "

const triggerReplaceNote = "A trigger is replaced as a whole: a member that is left out is removed, so send every " +
	"member of the trigger, as read with triggers.get, with the changes made. "

var triggerFields = func() []capability.Field {
	fields := []capability.Field{
		{Name: "path", Description: formTrigger},
		{Name: "accountId", Description: "Account ID"}, {Name: "containerId", Description: "Container ID"},
		{Name: "workspaceId", Description: "Workspace ID"}, {Name: "triggerId", Description: "Trigger ID"},
		{Name: "fingerprint", Description: "Fingerprint of the stored trigger; the value a change takes as last read"},
		{Name: "tagManagerUrl", Description: "Link to the trigger in the Tag Manager interface"},
	}
	return append(fields, triggerSettingFields...)
}()

// triggerSettingFields are the members a caller sets besides name and type; the single parameters follow
// the order of triggerSingles.
var triggerSettingFields = []capability.Field{
	{Name: "name", Description: "Display name"},
	{Name: "type", Description: "Event that causes the trigger, such as pageview, customEvent, click, linkClick, " +
		"formSubmission, timer, scrollDepth, or elementVisibility"},
	{Name: "filter", Description: "Conditions that must all be true for the trigger to fire"},
	{Name: "autoEventFilter", Description: "Conditions of an auto event trigger such as click or form submission"},
	{Name: "customEventFilter", Description: "Conditions on the event name of a custom event trigger"},
	{Name: "parameter", Description: "Additional parameters with key, type, value, list, map, and isWeakReference"},
	{Name: "eventName", Description: "Event name that a timer trigger fires, as a parameter"},
	{Name: "waitForTags", Description: "Delay a form submission or link click until the tags have fired; a boolean parameter"},
	{Name: "checkValidation", Description: "Fire only when the form submission or link click is not cancelled; a boolean parameter"},
	{Name: "waitForTagsTimeout", Description: "Milliseconds to wait for tags when waitForTags is true; an integer parameter"},
	{Name: "uniqueTriggerId", Description: "Globally unique ID of the auto event listener of the trigger, as a parameter"},
	{Name: "intervalSeconds", Description: "AMP timer: seconds between timer events, as a parameter"},
	{Name: "maxTimerLengthSeconds", Description: "AMP timer: seconds to fire timer events for, as a parameter"},
	{Name: "limit", Description: "Timer: how many events to fire at most, as a parameter"},
	{Name: "interval", Description: "Timer: milliseconds between events, as a parameter"},
	{Name: "verticalScrollPercentageList", Description: "AMP scroll: vertical percentages, as a list parameter"},
	{Name: "horizontalScrollPercentageList", Description: "AMP scroll: horizontal percentages, as a list parameter"},
	{Name: "visibilitySelector", Description: "AMP visibility: CSS selector, as a parameter"},
	{Name: "visiblePercentageMin", Description: "AMP visibility: minimum visible percentage, as a parameter"},
	{Name: "visiblePercentageMax", Description: "AMP visibility: maximum visible percentage, as a parameter"},
	{Name: "continuousTimeMinMilliseconds", Description: "AMP visibility: minimum continuous visible time in milliseconds, as a parameter"},
	{Name: "totalTimeMinMilliseconds", Description: "AMP visibility: minimum total visible time in milliseconds, as a parameter"},
	{Name: "selector", Description: "AMP click: CSS selector, as a parameter"},
	{Name: "parentFolderId", Description: "ID of the folder the trigger is in"},
	{Name: "notes", Description: "Notes"},
}

var triggerArgument = capability.Argument{Name: "trigger", Required: true, Description: "Trigger as " + formTrigger +
	", from triggers.list; it must lie inside the target of the connection"}

var triggerFingerprintArgument = capability.Argument{Name: "fingerprint", Required: true, Description: "Fingerprint " +
	"of the trigger as last read, from triggers.get or triggers.list; Google refuses the change when the trigger has " +
	"changed since"}

// triggerSettingArguments lists the members of a trigger a caller sets; name and type are always needed.
func triggerSettingArguments() []capability.Argument {
	var out []capability.Argument
	for _, f := range triggerSettingFields {
		a := capability.Argument{Name: f.Name, Description: f.Description}
		switch f.Name {
		case "name", "type":
			a.Required = true
		case "filter", "autoEventFilter", "customEventFilter":
			a.Description += "; each condition has a type (equals, contains, startsWith, endsWith, matchRegex, " +
				"greater, greaterOrEquals, less, lessOrEquals, cssSelector, urlMatches) and parameters such as " +
				"arg0 (a variable reference), arg1, ignore_case, and negate"
		case "parameter":
			a.Description += "; each with type (template, integer, boolean, list, map, triggerReference, " +
				"tagReference), key, value, and for list or map the nested parameters, up to " +
				strconv.Itoa(maxParamDepth) + " levels"
		}
		out = append(out, a)
	}
	return out
}

func triggerInputMembers() []string {
	return triggerMembers(true)
}

func triggerCreateInput() json.RawMessage {
	return inputSchema([]string{"workspace", "name", "type"},
		append([]string{member("workspace", workspaceArgSchema)}, triggerInputMembers()...)...)
}

func triggerUpdateInput() json.RawMessage {
	return inputSchema([]string{"trigger", "fingerprint", "name", "type"},
		append([]string{member("trigger", triggerArgSchema), member("fingerprint", fingerprintSchema)},
			triggerInputMembers()...)...)
}

var triggersList = capability.Descriptor{
	ID: triggersListID, Version: 1, Title: "List Tag Manager triggers",
	Description: "List the triggers of one workspace of a Google Tag Manager container with all their members, a " +
		"batch per call with a cursor to read on. " + triggerValuesNote,
	Tags: []string{"google", "tag-manager", "triggers"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"workspace"}, member("workspace", workspaceArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("triggers", triggerSchema)),
	Arguments:    []capability.Argument{workspaceArgument, cursorArgument},
	Fields: []capability.Field{{Name: "triggers", Description: "Triggers with every member of triggers.get"},
		nextCursorField},
	Examples: []capability.Example{{Description: "Triggers of one workspace",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3"}`)}},
}

var triggersGet = capability.Descriptor{
	ID: triggersGetID, Version: 1, Title: "Get a Tag Manager trigger",
	Description: "Read one trigger of a workspace with all its members, the fingerprint included. " + triggerValuesNote,
	Tags:        []string{"google", "tag-manager", "triggers"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"trigger"}, member("trigger", triggerArgSchema)),
	OutputSchema: json.RawMessage(triggerSchema),
	Arguments:    []capability.Argument{triggerArgument}, Fields: triggerFields,
	Examples: []capability.Example{{Description: "One trigger",
		Arguments: json.RawMessage(`{"trigger":"accounts/1000001/containers/2000002/workspaces/3/triggers/5"}`)}},
}

var triggersCreate = capability.Descriptor{
	ID: triggersCreateID, Version: 1, Title: "Create a Tag Manager trigger",
	Description: "Create a trigger in a workspace. " + triggerEffect + triggerValuesNote + "Not idempotent: a " +
		"repeated call creates a second trigger.",
	Tags: []string{"google", "tag-manager", "triggers", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: triggerCreateInput(), OutputSchema: json.RawMessage(triggerSchema),
	Arguments: append([]capability.Argument{workspaceArgument}, triggerSettingArguments()...), Fields: triggerFields,
	Examples: []capability.Example{{Description: "A click trigger with one filter condition",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"name":"Example click","type":"linkClick","filter":[{"type":"contains","parameter":[` +
			`{"type":"template","key":"arg0","value":"{{Click URL}}"},` +
			`{"type":"template","key":"arg1","value":"example.invalid"}]}]}`)}},
}

var triggersUpdate = capability.Descriptor{
	ID: triggersUpdateID, Version: 1, Title: "Update a Tag Manager trigger",
	Description: "Replace a trigger of a workspace. " + triggerReplaceNote + triggerEffect + triggerValuesNote +
		"The fingerprint is required, so a trigger changed in the meantime is not overwritten.",
	Tags: []string{"google", "tag-manager", "triggers", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: triggerUpdateInput(), OutputSchema: json.RawMessage(triggerSchema),
	Arguments: append([]capability.Argument{triggerArgument, triggerFingerprintArgument},
		triggerSettingArguments()...),
	Fields: triggerFields,
	Examples: []capability.Example{{Description: "Rename a trigger",
		Arguments: json.RawMessage(`{"trigger":"accounts/1000001/containers/2000002/workspaces/3/triggers/5",` +
			`"fingerprint":"1700000000000","name":"Renamed trigger","type":"pageview"}`)}},
}

var triggersDelete = capability.Descriptor{
	ID: triggersDeleteID, Version: 1, Title: "Delete a Tag Manager trigger",
	Description: "Delete a trigger of a workspace. " + triggerEffect + "The trigger stays in the versions that " +
		"hold it; triggers.revert can bring a trigger back from the base version of the workspace.",
	Tags: []string{"google", "tag-manager", "triggers", "delete"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"trigger"}, member("trigger", triggerArgSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("trigger", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{triggerArgument},
	Fields: []capability.Field{{Name: "trigger", Description: "The trigger path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one trigger",
		Arguments: json.RawMessage(`{"trigger":"accounts/1000001/containers/2000002/workspaces/3/triggers/5"}`)}},
}

var triggersRevert = capability.Descriptor{
	ID: triggersRevertID, Version: 1, Title: "Revert a Tag Manager trigger",
	Description: "Discard the changes of a workspace to a trigger: the trigger returns to its state in the latest " +
		"container version since the last synchronization of the workspace, or is removed when the version " +
		"holds none. " + triggerEffect + "The fingerprint is required." + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "triggers", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"trigger", "fingerprint"}, member("trigger", triggerArgSchema),
		member("fingerprint", fingerprintSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("trigger", triggerSchema))),
	Arguments:    []capability.Argument{triggerArgument, triggerFingerprintArgument},
	Fields: []capability.Field{{Name: "trigger", Description: "The trigger as it is in the latest container " +
		"version; missing when the trigger does not exist there, so the revert removed it from the workspace"}},
	Examples: []capability.Example{{Description: "Revert one trigger",
		Arguments: json.RawMessage(`{"trigger":"accounts/1000001/containers/2000002/workspaces/3/triggers/5",` +
			`"fingerprint":"1700000000000"}`)}},
}

func triggerDescriptors() []capability.Descriptor {
	return []capability.Descriptor{triggersList, triggersGet, triggersCreate, triggersUpdate, triggersDelete,
		triggersRevert}
}

func handleTriggersList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list triggers", triggersListID, resolved, raw)
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
	var answer triggersAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/triggers", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Trigger) > maxEntries {
		return nil, tooMany(c.op)
	}
	for _, t := range answer.Trigger {
		if err := t.checkAnswer(c.op); err != nil {
			return nil, err
		}
	}
	out := triggersResult{Triggers: append([]trigger{}, answer.Trigger...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleTriggersGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get trigger", triggersGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.triggerOf(c.args.Trigger)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out trigger
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

// triggerBodyOf encodes the trigger members of a change. Output-only members are never sent. The members are
// read from the raw arguments, because tagBody of the call shares member names with them.
func (c call) triggerBodyOf(raw json.RawMessage) ([]byte, error) {
	var b triggerBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if err := b.checkInput(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return nil, provider.Fail(c.op, "the trigger could not be encoded")
	}
	if len(body) > maxRequestBody {
		return nil, invalidArgument("the trigger is larger than " + strconv.Itoa(maxRequestBody) + " bytes")
	}
	return body, nil
}

func handleTriggersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create trigger", triggersCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(c.args.Workspace)
	if err != nil {
		return nil, err
	}
	body, err := c.triggerBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out trigger
	r := request{method: http.MethodPost, path: parent.String() + "/triggers", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleTriggersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update trigger", triggersUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.triggerOf(c.args.Trigger)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	body, err := c.triggerBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out trigger
	r := request{method: http.MethodPut, path: path, query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleTriggersDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete trigger", triggersDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.triggerOf(c.args.Trigger)
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
	return deleteTriggerResult{Trigger: path, Deleted: true}, nil
}

func handleTriggersRevert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("revert trigger", triggersRevertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.triggerOf(c.args.Trigger)
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
	var answer revertTriggerAnswer
	r := request{method: http.MethodPost, path: path + ":revert", query: query, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	if answer.Trigger == nil {
		return revertTriggerResult{}, nil
	}
	return revertTriggerResult{Trigger: answer.Trigger}, answer.Trigger.checkAnswer(c.op)
}

// triggerSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func triggerSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a toolArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		where := a.Workspace
		if a.Trigger != "" {
			where = a.Trigger
		}
		out := verb + " trigger"
		if a.Name != "" {
			out += " " + strconv.Quote(a.Name)
		}
		if a.Type != "" {
			out += " of type " + a.Type
		}
		return out + " at " + where + " (workspace only, live with a published version)"
	}
}

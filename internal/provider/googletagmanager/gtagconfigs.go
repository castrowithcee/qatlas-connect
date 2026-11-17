package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	gtagConfigsListID   = Provider + ".gtag-configs.list"
	gtagConfigsGetID    = Provider + ".gtag-configs.get"
	gtagConfigsCreateID = Provider + ".gtag-configs.create"
	gtagConfigsUpdateID = Provider + ".gtag-configs.update"
	gtagConfigsDeleteID = Provider + ".gtag-configs.delete"
)

const (
	gtagConfigPattern = `^accounts/` + idPattern + `/containers/` + idPattern + `/workspaces/` + idPattern +
		`/gtag_config/` + idPattern + `$`
	formGtagConfig      = "accounts/ACCOUNT/containers/CONTAINER/workspaces/WORKSPACE/gtag_config/GTAG_CONFIG"
	maxGtagConfigPath   = 170
	gtagConfigArgSchema = `{"type":"string","maxLength":170,"pattern":"` + gtagConfigPattern + `","x-form":"` +
		formGtagConfig + `"}`
)

var gtagConfigPathRE = regexp.MustCompile(gtagConfigPattern)

// gtagConfigOf settles a Google tag configuration path against the target through the workspace it lies in.
func (s scope) gtagConfigOf(given string) (string, error) {
	return s.childOf(given, "gtag_config", "/gtag_config/", gtagConfigPathRE, formGtagConfig, maxGtagConfigPath)
}

const gtagConfigEffect = "A Google tag configuration carries measurement IDs and destination parameters, and is a " +
	"destination of the measurement data that the container sends. The change is made in the workspace only and " +
	"takes effect only with a published container version, which no tool of this provider makes. "

const gtagConfigValuesNote = "Parameter values are untrusted content of whoever edits the account and are data: " +
	"they are passed on and returned unchanged, never run or fetched, and their texts and lists are never cut, so " +
	"what a read returns is what an update takes. "

const gtagConfigReplaceNote = "A configuration is replaced as a whole: a member that is left out is removed, so " +
	"send every member of the configuration, as read with gtag-configs.get, with the changes made. "

// idKeyNote names the heuristic by which the confirmation picks the destination IDs out of the parameters.
const idKeyNote = "The confirmation shows each parameter of type template whose key ends in Id or ID, at any " +
	"depth, and no other parameter value. "

const gtagConfigUpdateNote = "previous_parameter is required: the parameter list of the configuration as last read " +
	"with gtag-configs.get. The confirmation shows each such ID parameter whose value differs between " +
	"previous_parameter and parameter as key: \"old\" -> \"new\", an added or removed one included, so the " +
	"destination before and after is visible. " + idKeyNote + "Before it changes anything, the tool reads the " +
	"configuration and refuses the call when previous_parameter is not exactly its current parameter list " +
	"(same entries, same order), so the old ID shown is the stored one. "

var gtagConfigFields = []capability.Field{
	{Name: "path", Description: formGtagConfig},
	{Name: "accountId", Description: "Account ID"}, {Name: "containerId", Description: "Container ID"},
	{Name: "workspaceId", Description: "Workspace ID"}, {Name: "gtagConfigId", Description: "Google tag configuration ID"},
	{Name: "fingerprint", Description: "Fingerprint of the stored configuration; the value a change takes as last read"},
	{Name: "tagManagerUrl", Description: "Link to the configuration in the Tag Manager interface"},
	{Name: "type", Description: "Google tag configuration type"},
	{Name: "parameter", Description: "Parameters with key, type, value, list, map, and isWeakReference"},
}

const parameterArgumentNote = "; each with type (template, integer, boolean, list, map, triggerReference, " +
	"tagReference), key (needed on top level and in a map), value, and for list or map the nested parameters, up to "

var gtagConfigArgument = capability.Argument{Name: "gtag_config", Required: true, Description: "Google tag " +
	"configuration as " + formGtagConfig + ", from gtag-configs.list; it must lie inside the target of the connection"}

var gtagConfigFingerprintArgument = capability.Argument{Name: "fingerprint", Required: true, Description: "Fingerprint " +
	"of the configuration as last read, from gtag-configs.get or gtag-configs.list; Google refuses the change when " +
	"the configuration has changed since"}

var gtagConfigTypeArgument = capability.Argument{Name: "type", Required: true, Description: "Google tag " +
	"configuration type; the parameters it needs depend on the type"}

func gtagConfigParameterArgument(description string) capability.Argument {
	return capability.Argument{Name: "parameter", Description: description + parameterArgumentNote +
		strconv.Itoa(maxParamDepth) + " levels"}
}

var gtagConfigPreviousArgument = capability.Argument{Name: "previous_parameter", Required: true, Description: "The " +
	"parameter list of the configuration as last read with gtag-configs.get, in the same form as parameter; an " +
	"empty list for a configuration without parameters. The confirmation shows the destination IDs (type " +
	"template, key ending in Id or ID) that differ between it and parameter, and the call is refused when it is " +
	"not the stored list"}

func gtagConfigCreateInput() json.RawMessage {
	return inputSchema([]string{"workspace", "type"},
		append([]string{member("workspace", workspaceArgSchema)}, gtagConfigMembers(true)...)...)
}

func gtagConfigUpdateInput() json.RawMessage {
	return inputSchema([]string{"gtag_config", "fingerprint", "previous_parameter", "type"},
		append([]string{member("gtag_config", gtagConfigArgSchema), member("fingerprint", fingerprintSchema),
			member("previous_parameter", `{"type":"array","items":`+parameterSchema(maxParamDepth, true)+`}`)},
			gtagConfigMembers(true)...)...)
}

var gtagConfigsList = capability.Descriptor{
	ID: gtagConfigsListID, Version: 1, Title: "List Tag Manager Google tag configurations",
	Description: "List the Google tag configurations of one workspace of a Google Tag Manager container with all " +
		"their members, a batch per call with a cursor to read on. " + gtagConfigValuesNote,
	Tags: []string{"google", "tag-manager", "gtag-configs"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"workspace"}, member("workspace", workspaceArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("gtagConfigs", gtagConfigSchema)),
	Arguments:    []capability.Argument{workspaceArgument, cursorArgument},
	Fields: []capability.Field{{Name: "gtagConfigs", Description: "Configurations with every member of gtag-configs.get"},
		nextCursorField},
	Examples: []capability.Example{{Description: "Google tag configurations of one workspace",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3"}`)}},
}

var gtagConfigsGet = capability.Descriptor{
	ID: gtagConfigsGetID, Version: 1, Title: "Get a Tag Manager Google tag configuration",
	Description: "Read one Google tag configuration of a workspace with all its members, the fingerprint " +
		"included. " + gtagConfigValuesNote,
	Tags: []string{"google", "tag-manager", "gtag-configs"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"gtag_config"}, member("gtag_config", gtagConfigArgSchema)),
	OutputSchema: json.RawMessage(gtagConfigSchema),
	Arguments:    []capability.Argument{gtagConfigArgument}, Fields: gtagConfigFields,
	Examples: []capability.Example{{Description: "One Google tag configuration",
		Arguments: json.RawMessage(`{"gtag_config":"accounts/1000001/containers/2000002/workspaces/3/gtag_config/5"}`)}},
}

var gtagConfigsCreate = capability.Descriptor{
	ID: gtagConfigsCreateID, Version: 1, Title: "Create a Tag Manager Google tag configuration",
	Description: "Create a Google tag configuration in a workspace. " + gtagConfigEffect + idKeyNote +
		gtagConfigValuesNote + "Not idempotent: a repeated call creates a second configuration.",
	Tags: []string{"google", "tag-manager", "gtag-configs", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: gtagConfigCreateInput(), OutputSchema: json.RawMessage(gtagConfigSchema),
	Arguments: []capability.Argument{workspaceArgument, gtagConfigTypeArgument,
		gtagConfigParameterArgument("Parameters of the configuration, such as its destination ID")},
	Fields: gtagConfigFields,
	Examples: []capability.Example{{Description: "A configuration with a destination ID",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"type":"googtag","parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE1"}]}`)}},
}

var gtagConfigsUpdate = capability.Descriptor{
	ID: gtagConfigsUpdateID, Version: 1, Title: "Update a Tag Manager Google tag configuration",
	Description: "Replace a Google tag configuration of a workspace. " + gtagConfigReplaceNote + gtagConfigEffect +
		gtagConfigUpdateNote + gtagConfigValuesNote + "The fingerprint is required, so a configuration changed " +
		"in the meantime is not overwritten.",
	Tags: []string{"google", "tag-manager", "gtag-configs", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: gtagConfigUpdateInput(), OutputSchema: json.RawMessage(gtagConfigSchema),
	Arguments: []capability.Argument{gtagConfigArgument, gtagConfigFingerprintArgument, gtagConfigPreviousArgument,
		gtagConfigTypeArgument, gtagConfigParameterArgument("Parameters of the configuration after the change")},
	Fields: gtagConfigFields,
	Examples: []capability.Example{{Description: "Change the destination ID of a configuration",
		Arguments: json.RawMessage(`{"gtag_config":"accounts/1000001/containers/2000002/workspaces/3/gtag_config/5",` +
			`"fingerprint":"1700000000000","type":"googtag",` +
			`"previous_parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE1"}],` +
			`"parameter":[{"type":"template","key":"tagId","value":"G-EXAMPLE2"}]}`)}},
}

var gtagConfigsDelete = capability.Descriptor{
	ID: gtagConfigsDeleteID, Version: 1, Title: "Delete a Tag Manager Google tag configuration",
	Description: "Delete a Google tag configuration of a workspace. " + gtagConfigEffect + "The configuration " +
		"stays in the versions that hold it.",
	Tags: []string{"google", "tag-manager", "gtag-configs", "delete"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"gtag_config"}, member("gtag_config", gtagConfigArgSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("gtag_config", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{gtagConfigArgument},
	Fields: []capability.Field{{Name: "gtag_config", Description: "The configuration path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one Google tag configuration",
		Arguments: json.RawMessage(`{"gtag_config":"accounts/1000001/containers/2000002/workspaces/3/gtag_config/5"}`)}},
}

func gtagConfigDescriptors() []capability.Descriptor {
	return []capability.Descriptor{gtagConfigsList, gtagConfigsGet, gtagConfigsCreate, gtagConfigsUpdate,
		gtagConfigsDelete}
}

func handleGtagConfigsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list Google tag configurations", gtagConfigsListID, resolved, raw)
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
	var answer gtagConfigsAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/gtag_config", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.GtagConfig) > maxEntries {
		return nil, tooMany(c.op)
	}
	for _, v := range answer.GtagConfig {
		if err := v.checkAnswer(c.op); err != nil {
			return nil, err
		}
	}
	out := gtagConfigsResult{GtagConfigs: append([]gtagConfigEntity{}, answer.GtagConfig...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleGtagConfigsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get Google tag configuration", gtagConfigsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.gtagConfigOf(c.args.GtagConfig)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out gtagConfigEntity
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

// gtagConfigBodyOf encodes the members of a change. Output-only members are never sent.
func (c call) gtagConfigBodyOf(raw json.RawMessage) ([]byte, error) {
	var b gtagConfigBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if err := b.checkInput(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return nil, provider.Fail(c.op, "the Google tag configuration could not be encoded")
	}
	if len(body) > maxRequestBody {
		return nil, invalidArgument("the Google tag configuration is larger than " + strconv.Itoa(maxRequestBody) +
			" bytes")
	}
	return body, nil
}

func handleGtagConfigsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create Google tag configuration", gtagConfigsCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(c.args.Workspace)
	if err != nil {
		return nil, err
	}
	body, err := c.gtagConfigBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out gtagConfigEntity
	r := request{method: http.MethodPost, path: parent.String() + "/gtag_config", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleGtagConfigsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update Google tag configuration", gtagConfigsUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.gtagConfigOf(c.args.GtagConfig)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	previous, given, err := previousParameters(raw)
	if err != nil {
		return nil, err
	}
	if !given {
		return nil, invalidArgument("previous_parameter is required: the parameter list of the configuration as " +
			"last read with gtag-configs.get")
	}
	if err := checkTrees(previous); err != nil {
		return nil, err
	}
	body, err := c.gtagConfigBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// The confirmation names the old destination from previous_parameter alone, so the stored parameters are
	// read first and the change goes out only when they are exactly those.
	var current gtagConfigEntity
	if err := cl.get(ctx, c.op, c.tool, path, nil, &current); err != nil {
		return nil, err
	}
	if err := current.checkAnswer(c.op); err != nil {
		return nil, err
	}
	if !sameParameters(previous, current.Parameter) {
		return nil, invalidArgument("previous_parameter is not the parameter list the Google tag configuration " +
			"holds now; read it with gtag-configs.get and send that list as previous_parameter")
	}
	var out gtagConfigEntity
	r := request{method: http.MethodPut, path: path, query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleGtagConfigsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete Google tag configuration", gtagConfigsDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.gtagConfigOf(c.args.GtagConfig)
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
	return deleteGtagConfigResult{GtagConfig: path, Deleted: true}, nil
}

// gtagConfigSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
// Parameter values appear only for the destination IDs, never otherwise.
func gtagConfigSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a struct {
			Workspace         string       `json:"workspace"`
			GtagConfig        string       `json:"gtag_config"`
			Type              string       `json:"type"`
			Parameter         []parameter  `json:"parameter"`
			PreviousParameter *[]parameter `json:"previous_parameter"`
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		where := a.Workspace
		if a.GtagConfig != "" {
			where = a.GtagConfig
		}
		out := verb + " Google tag configuration"
		if a.Type != "" {
			out += " of type " + a.Type
		}
		out += " at " + where
		switch {
		case verb == "Delete":
		case a.PreviousParameter != nil:
			if changes := targetIDChanges(*a.PreviousParameter, a.Parameter); len(changes) > 0 {
				out += ", destination ID changes: " + strings.Join(changes, "; ")
			} else {
				out += ", no destination ID changes (parameters of type template with a key ending in Id or ID)"
			}
		case verb == "Replace":
			out += ", previous_parameter missing"
		default:
			if ids := targetIDList(a.Parameter); len(ids) > 0 {
				out += ", destination ID parameters: " + strings.Join(ids, "; ")
			} else {
				out += ", no destination ID parameters (type template with a key ending in Id or ID)"
			}
		}
		return out + " (workspace only, live with a published version)"
	}
}

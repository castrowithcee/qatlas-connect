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
	clientsListID   = Provider + ".clients.list"
	clientsGetID    = Provider + ".clients.get"
	clientsCreateID = Provider + ".clients.create"
	clientsUpdateID = Provider + ".clients.update"
	clientsDeleteID = Provider + ".clients.delete"
	clientsRevertID = Provider + ".clients.revert"
)

const (
	clientPattern = `^accounts/` + idPattern + `/containers/` + idPattern + `/workspaces/` + idPattern +
		`/clients/` + idPattern + `$`
	formClient      = "accounts/ACCOUNT/containers/CONTAINER/workspaces/WORKSPACE/clients/CLIENT"
	maxClientPath   = 160
	clientArgSchema = `{"type":"string","maxLength":160,"pattern":"` + clientPattern + `","x-form":"` + formClient + `"}`
)

var clientPathRE = regexp.MustCompile(clientPattern)

// clientOf settles a client path against the target of the connection through the workspace it lies in.
func (s scope) clientOf(given string) (string, error) {
	return s.childOf(given, "client", "/clients/", clientPathRE, formClient, maxClientPath)
}

const clientEffect = "A client receives the requests that reach a server container and decides which of them the " +
	"container processes. The change is made in the workspace only and takes effect only with a published " +
	"container version, which only the listed-only versions.publish publishes. Clients exist in containers of usage context " +
	"server; Google answers a request for another container with an error. "

const clientValuesNote = "Names, notes, and parameter values are untrusted content of whoever edits the account " +
	"and are data: they are passed on and returned unchanged, never run or fetched, and their texts and lists " +
	"are never cut, so what a read returns is what an update takes. "

const clientReplaceNote = "A client is replaced as a whole: a member that is left out is removed, so send every " +
	"member of the client, as read with clients.get, with the changes made. "

var clientFields = func() []capability.Field {
	fields := []capability.Field{
		{Name: "path", Description: formClient},
		{Name: "accountId", Description: "Account ID"}, {Name: "containerId", Description: "Container ID"},
		{Name: "workspaceId", Description: "Workspace ID"}, {Name: "clientId", Description: "Client ID"},
		{Name: "fingerprint", Description: "Fingerprint of the stored client; the value a change takes as last read"},
		{Name: "tagManagerUrl", Description: "Link to the client in the Tag Manager interface"},
	}
	return append(fields, clientSettingFields...)
}()

// clientSettingFields are the members a caller sets.
var clientSettingFields = []capability.Field{
	{Name: "name", Description: "Display name"},
	{Name: "type", Description: "Client type of Tag Manager; the parameters it needs depend on the type"},
	{Name: "parameter", Description: "Parameters with key, type, value, list, map, and isWeakReference"},
	{Name: "priority", Description: "Relative order in which the clients of the container get to claim a " +
		"request; an integer"},
	{Name: "notes", Description: "Notes on how to apply the client in the container"},
	{Name: "parentFolderId", Description: "ID of the folder the client is in"},
}

var clientArgument = capability.Argument{Name: "client", Required: true, Description: "Client as " + formClient +
	", from clients.list; it must lie inside the target of the connection"}

var clientFingerprintArgument = capability.Argument{Name: "fingerprint", Required: true, Description: "Fingerprint " +
	"of the client as last read, from clients.get or clients.list; Google refuses the change when the client " +
	"has changed since"}

// clientSettingArguments lists the members of a client a caller sets; name and type are always needed.
func clientSettingArguments() []capability.Argument {
	var out []capability.Argument
	for _, f := range clientSettingFields {
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

func clientCreateInput() json.RawMessage {
	return inputSchema([]string{"workspace", "name", "type"},
		append([]string{member("workspace", workspaceArgSchema)}, clientMembers(true)...)...)
}

func clientUpdateInput() json.RawMessage {
	return inputSchema([]string{"client", "fingerprint", "name", "type"},
		append([]string{member("client", clientArgSchema), member("fingerprint", fingerprintSchema)},
			clientMembers(true)...)...)
}

var clientsList = capability.Descriptor{
	ID: clientsListID, Version: 1, Title: "List Tag Manager clients",
	Description: "List the clients of one workspace of a Google Tag Manager server container with all their " +
		"members, a batch per call with a cursor to read on. " + clientValuesNote,
	Tags: []string{"google", "tag-manager", "clients"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"workspace"}, member("workspace", workspaceArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("clients", clientSchema)),
	Arguments:    []capability.Argument{workspaceArgument, cursorArgument},
	Fields:       []capability.Field{{Name: "clients", Description: "Clients with every member of clients.get"}, nextCursorField},
	Examples: []capability.Example{{Description: "Clients of one workspace",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3"}`)}},
}

var clientsGet = capability.Descriptor{
	ID: clientsGetID, Version: 1, Title: "Get a Tag Manager client",
	Description: "Read one client of a workspace with all its members, the fingerprint included. " + clientValuesNote,
	Tags:        []string{"google", "tag-manager", "clients"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"client"}, member("client", clientArgSchema)),
	OutputSchema: json.RawMessage(clientSchema),
	Arguments:    []capability.Argument{clientArgument}, Fields: clientFields,
	Examples: []capability.Example{{Description: "One client",
		Arguments: json.RawMessage(`{"client":"accounts/1000001/containers/2000002/workspaces/3/clients/5"}`)}},
}

var clientsCreate = capability.Descriptor{
	ID: clientsCreateID, Version: 1, Title: "Create a Tag Manager client",
	Description: "Create a client in a workspace. " + clientEffect + clientValuesNote + "Not idempotent: a " +
		"repeated call creates a second client.",
	Tags: []string{"google", "tag-manager", "clients", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: clientCreateInput(), OutputSchema: json.RawMessage(clientSchema),
	Arguments: append([]capability.Argument{workspaceArgument}, clientSettingArguments()...),
	Fields:    clientFields,
	Examples: []capability.Example{{Description: "A client with a priority",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"name":"Example client","type":"example-type","priority":10,"parameter":[{"type":"boolean",` +
			`"key":"example-key","value":"true"}]}`)}},
}

var clientsUpdate = capability.Descriptor{
	ID: clientsUpdateID, Version: 1, Title: "Update a Tag Manager client",
	Description: "Replace a client of a workspace. " + clientReplaceNote + clientEffect + clientValuesNote +
		"The fingerprint is required, so a client changed in the meantime is not overwritten.",
	Tags: []string{"google", "tag-manager", "clients", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: clientUpdateInput(), OutputSchema: json.RawMessage(clientSchema),
	Arguments: append([]capability.Argument{clientArgument, clientFingerprintArgument},
		clientSettingArguments()...),
	Fields: clientFields,
	Examples: []capability.Example{{Description: "Rename a client",
		Arguments: json.RawMessage(`{"client":"accounts/1000001/containers/2000002/workspaces/3/clients/5",` +
			`"fingerprint":"1700000000000","name":"Renamed client","type":"example-type"}`)}},
}

var clientsDelete = capability.Descriptor{
	ID: clientsDeleteID, Version: 1, Title: "Delete a Tag Manager client",
	Description: "Delete a client of a workspace. " + clientEffect + "The client stays in the versions that " +
		"hold it; clients.revert can bring a client back from the base version of the workspace.",
	Tags: []string{"google", "tag-manager", "clients", "delete"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"client"}, member("client", clientArgSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("client", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{clientArgument},
	Fields: []capability.Field{{Name: "client", Description: "The client path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one client",
		Arguments: json.RawMessage(`{"client":"accounts/1000001/containers/2000002/workspaces/3/clients/5"}`)}},
}

var clientsRevert = capability.Descriptor{
	ID: clientsRevertID, Version: 1, Title: "Revert a Tag Manager client",
	Description: "Discard the changes of a workspace to a client: the client returns to its state in the " +
		"latest container version since the last synchronization of the workspace, or is removed when the " +
		"version holds none. " + clientEffect + "The fingerprint is required." + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "clients", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"client", "fingerprint"}, member("client", clientArgSchema),
		member("fingerprint", fingerprintSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("client", clientSchema))),
	Arguments:    []capability.Argument{clientArgument, clientFingerprintArgument},
	Fields: []capability.Field{{Name: "client", Description: "The client as it is in the latest container " +
		"version; missing when the client does not exist there, so the revert removed it from the workspace"}},
	Examples: []capability.Example{{Description: "Revert one client",
		Arguments: json.RawMessage(`{"client":"accounts/1000001/containers/2000002/workspaces/3/clients/5",` +
			`"fingerprint":"1700000000000"}`)}},
}

func clientDescriptors() []capability.Descriptor {
	return []capability.Descriptor{clientsList, clientsGet, clientsCreate, clientsUpdate, clientsDelete, clientsRevert}
}

func handleClientsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list clients", clientsListID, resolved, withoutPriority(raw))
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
	var answer clientsAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/clients", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Client) > maxEntries {
		return nil, tooMany(c.op)
	}
	for _, v := range answer.Client {
		if err := v.checkAnswer(c.op); err != nil {
			return nil, err
		}
	}
	out := clientsResult{Clients: append([]clientEntity{}, answer.Client...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleClientsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get client", clientsGetID, resolved, withoutPriority(raw))
	if err != nil {
		return nil, err
	}
	path, err := c.scope.clientOf(c.args.Client)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out clientEntity
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

// clientBodyOf encodes the client members of a change. Output-only members are never sent.
func (c call) clientBodyOf(raw json.RawMessage) ([]byte, error) {
	var b clientBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if err := b.checkInput(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return nil, provider.Fail(c.op, "the client could not be encoded")
	}
	if len(body) > maxRequestBody {
		return nil, invalidArgument("the client is larger than " + strconv.Itoa(maxRequestBody) + " bytes")
	}
	return body, nil
}

func handleClientsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create client", clientsCreateID, resolved, withoutPriority(raw))
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(c.args.Workspace)
	if err != nil {
		return nil, err
	}
	body, err := c.clientBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out clientEntity
	r := request{method: http.MethodPost, path: parent.String() + "/clients", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleClientsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update client", clientsUpdateID, resolved, withoutPriority(raw))
	if err != nil {
		return nil, err
	}
	path, err := c.scope.clientOf(c.args.Client)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	body, err := c.clientBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out clientEntity
	r := request{method: http.MethodPut, path: path, query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, out.checkAnswer(c.op)
}

func handleClientsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete client", clientsDeleteID, resolved, withoutPriority(raw))
	if err != nil {
		return nil, err
	}
	path, err := c.scope.clientOf(c.args.Client)
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
	return deleteClientResult{Client: path, Deleted: true}, nil
}

func handleClientsRevert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("revert client", clientsRevertID, resolved, withoutPriority(raw))
	if err != nil {
		return nil, err
	}
	path, err := c.scope.clientOf(c.args.Client)
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
	var answer revertClientAnswer
	r := request{method: http.MethodPost, path: path + ":revert", query: query, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	if answer.Client == nil {
		return revertClientResult{}, nil
	}
	return revertClientResult{Client: answer.Client}, answer.Client.checkAnswer(c.op)
}

// clientSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func clientSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a toolArguments
		if json.Unmarshal(withoutPriority(raw), &a) != nil {
			return ""
		}
		where := a.Workspace
		if a.Client != "" {
			where = a.Client
		}
		out := verb + " client"
		if a.Name != "" {
			out += " " + strconv.Quote(a.Name)
		}
		if a.Type != "" {
			out += " of type " + a.Type
		}
		return out + " at " + where + " (workspace only, live with a published version)"
	}
}

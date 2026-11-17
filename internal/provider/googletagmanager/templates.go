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
	templatesListID   = Provider + ".templates.list"
	templatesGetID    = Provider + ".templates.get"
	templatesCreateID = Provider + ".templates.create"
	templatesUpdateID = Provider + ".templates.update"
	templatesDeleteID = Provider + ".templates.delete"
	templatesRevertID = Provider + ".templates.revert"
)

const (
	templatePattern = `^accounts/` + idPattern + `/containers/` + idPattern + `/workspaces/` + idPattern +
		`/templates/` + idPattern + `$`
	formTemplate      = "accounts/ACCOUNT/containers/CONTAINER/workspaces/WORKSPACE/templates/TEMPLATE"
	maxTemplatePath   = 160
	templateArgSchema = `{"type":"string","maxLength":160,"pattern":"` + templatePattern + `","x-form":"` + formTemplate + `"}`
)

var templatePathRE = regexp.MustCompile(templatePattern)

// templateOf settles a template path against the target of the connection through the workspace it lies in.
func (s scope) templateOf(given string) (string, error) {
	return s.childOf(given, "template", "/templates/", templatePathRE, formTemplate, maxTemplatePath)
}

const templateEffect = "The change is made in the workspace only. A template becomes a tag or variable type of " +
	"the container, and once a container version that uses it is published, its code runs on the pages of the " +
	"container with the network access of the permissions it declares; only the listed-only versions.publish publishes. "

const templateCodeNote = "templateData is code (sandboxed JavaScript) with the declaration of the fields and " +
	"permissions: it is sensitive and untrusted content of whoever edits the account, passed on and returned " +
	"unchanged, never run, and never cut, so what a read returns is what an update takes. It is at most " +
	"1048576 bytes on input. "

const templateReplaceNote = "A template is replaced as a whole: send name and templateData, as read with " +
	"templates.get, with the changes made. galleryReference is set by Google and never sent. "

var templateFields = []capability.Field{
	{Name: "path", Description: formTemplate},
	{Name: "accountId", Description: "Account ID"}, {Name: "containerId", Description: "Container ID"},
	{Name: "workspaceId", Description: "Workspace ID"}, {Name: "templateId", Description: "Template ID"},
	{Name: "fingerprint", Description: "Fingerprint of the stored template; the value a change takes as last read"},
	{Name: "tagManagerUrl", Description: "Link to the template in the Tag Manager interface"},
	{Name: "galleryReference", Description: "Link to the Community Template Gallery entry, read only: host, " +
		"owner, repository, version, isModified, signature, templateDeveloperId, and galleryTemplateId"},
	{Name: "name", Description: "Display name"},
	{Name: "templateData", Description: "The template in text format: code, declared fields, and permissions"},
}

var templateArgument = capability.Argument{Name: "template", Required: true, Description: "Template as " +
	formTemplate + ", from templates.list; it must lie inside the target of the connection"}

var templateFingerprintArgument = capability.Argument{Name: "fingerprint", Required: true, Description: "Fingerprint " +
	"of the template as last read, from templates.get or templates.list; Google refuses the change when the " +
	"template has changed since"}

var templateSettingArguments = []capability.Argument{
	{Name: "name", Required: true, Description: "Display name"},
	{Name: "templateData", Required: true, Description: "The template in text format, as exported by the " +
		"template editor; at most 1048576 bytes"},
}

func templateCreateInput() json.RawMessage {
	return inputSchema([]string{"workspace", "name", "templateData"},
		append([]string{member("workspace", workspaceArgSchema)}, templateMembers(true)...)...)
}

func templateUpdateInput() json.RawMessage {
	return inputSchema([]string{"template", "fingerprint", "name", "templateData"},
		append([]string{member("template", templateArgSchema), member("fingerprint", fingerprintSchema)},
			templateMembers(true)...)...)
}

var templatesList = capability.Descriptor{
	ID: templatesListID, Version: 1, Title: "List Tag Manager templates",
	Description: "List the custom templates of one workspace of a Google Tag Manager container with all their " +
		"members, a batch per call with a cursor to read on. " + templateCodeNote,
	Tags: []string{"google", "tag-manager", "templates"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"workspace"}, member("workspace", workspaceArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("templates", templateSchema)),
	Arguments:    []capability.Argument{workspaceArgument, cursorArgument},
	Fields: []capability.Field{{Name: "templates", Description: "Templates with every member of templates.get"},
		nextCursorField},
	Examples: []capability.Example{{Description: "Templates of one workspace",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3"}`)}},
}

var templatesGet = capability.Descriptor{
	ID: templatesGetID, Version: 1, Title: "Get a Tag Manager template",
	Description: "Read one custom template of a workspace with all its members, the fingerprint included. " +
		templateCodeNote,
	Tags: []string{"google", "tag-manager", "templates"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"template"}, member("template", templateArgSchema)),
	OutputSchema: json.RawMessage(templateSchema),
	Arguments:    []capability.Argument{templateArgument}, Fields: templateFields,
	Examples: []capability.Example{{Description: "One template",
		Arguments: json.RawMessage(`{"template":"accounts/1000001/containers/2000002/workspaces/3/templates/5"}`)}},
}

var templatesCreate = capability.Descriptor{
	ID: templatesCreateID, Version: 1, Title: "Create a Tag Manager template",
	Description: "Create a custom template in a workspace. " + templateEffect + templateCodeNote +
		"Not idempotent: a repeated call creates a second template.",
	Tags: []string{"google", "tag-manager", "templates", "create"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: templateCreateInput(), OutputSchema: json.RawMessage(templateSchema),
	Arguments: append([]capability.Argument{workspaceArgument}, templateSettingArguments...),
	Fields:    templateFields,
	Examples: []capability.Example{{Description: "A template from its text",
		Arguments: json.RawMessage(`{"workspace":"accounts/1000001/containers/2000002/workspaces/3",` +
			`"name":"Example template","templateData":"___INFO___\n..."}`)}},
}

var templatesUpdate = capability.Descriptor{
	ID: templatesUpdateID, Version: 1, Title: "Update a Tag Manager template",
	Description: "Replace a custom template of a workspace. " + templateReplaceNote + templateEffect +
		templateCodeNote + "The fingerprint is required, so a template changed in the meantime is not overwritten.",
	Tags: []string{"google", "tag-manager", "templates", "update"}, Provider: Provider,
	Risk:        changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: templateUpdateInput(), OutputSchema: json.RawMessage(templateSchema),
	Arguments: append([]capability.Argument{templateArgument, templateFingerprintArgument},
		templateSettingArguments...),
	Fields: templateFields,
	Examples: []capability.Example{{Description: "Replace a template",
		Arguments: json.RawMessage(`{"template":"accounts/1000001/containers/2000002/workspaces/3/templates/5",` +
			`"fingerprint":"1700000000000","name":"Renamed template","templateData":"___INFO___\n..."}`)}},
}

var templatesDelete = capability.Descriptor{
	ID: templatesDeleteID, Version: 1, Title: "Delete a Tag Manager template",
	Description: "Delete a custom template of a workspace. " + templateEffect + "The template stays in the " +
		"versions that hold it; templates.revert can bring it back from the base version of the workspace.",
	Tags: []string{"google", "tag-manager", "templates", "delete"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"template"}, member("template", templateArgSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("template", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{templateArgument},
	Fields: []capability.Field{{Name: "template", Description: "The template path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one template",
		Arguments: json.RawMessage(`{"template":"accounts/1000001/containers/2000002/workspaces/3/templates/5"}`)}},
}

var templatesRevert = capability.Descriptor{
	ID: templatesRevertID, Version: 1, Title: "Revert a Tag Manager template",
	Description: "Discard the changes of a workspace to a template: the template returns to its state in the " +
		"latest container version since the last synchronization of the workspace, or is removed when the " +
		"version holds none. " + templateEffect + "The fingerprint is required." + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "templates", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"template", "fingerprint"}, member("template", templateArgSchema),
		member("fingerprint", fingerprintSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("template", templateSchema))),
	Arguments:    []capability.Argument{templateArgument, templateFingerprintArgument},
	Fields: []capability.Field{{Name: "template", Description: "The template as it is in the latest container " +
		"version; missing when the template does not exist there, so the revert removed it from the workspace"}},
	Examples: []capability.Example{{Description: "Revert one template",
		Arguments: json.RawMessage(`{"template":"accounts/1000001/containers/2000002/workspaces/3/templates/5",` +
			`"fingerprint":"1700000000000"}`)}},
}

func templateDescriptors() []capability.Descriptor {
	return []capability.Descriptor{templatesList, templatesGet, templatesCreate, templatesUpdate, templatesDelete,
		templatesRevert}
}

func handleTemplatesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list templates", templatesListID, resolved, raw)
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
	var answer templatesAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/templates", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Template) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := templatesResult{Templates: append([]template{}, answer.Template...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleTemplatesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get template", templatesGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.templateOf(c.args.Template)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out template
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// templateBodyOf encodes the template members of a change. Output-only members are never sent. The members
// are read from the raw arguments, because the argument struct of the call shares member names with them.
func (c call) templateBodyOf(raw json.RawMessage) ([]byte, error) {
	var b templateBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if err := b.checkInput(); err != nil {
		return nil, err
	}
	body, err := json.Marshal(b)
	if err != nil {
		return nil, provider.Fail(c.op, "the template could not be encoded")
	}
	if len(body) > maxRequestBody {
		return nil, invalidArgument("the template is larger than " + strconv.Itoa(maxRequestBody) + " bytes")
	}
	return body, nil
}

func handleTemplatesCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create template", templatesCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.workspaceOf(c.args.Workspace)
	if err != nil {
		return nil, err
	}
	body, err := c.templateBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out template
	r := request{method: http.MethodPost, path: parent.String() + "/templates", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleTemplatesUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update template", templatesUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.templateOf(c.args.Template)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	body, err := c.templateBodyOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out template
	r := request{method: http.MethodPut, path: path, query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleTemplatesDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete template", templatesDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.templateOf(c.args.Template)
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
	return deleteTemplateResult{Template: path, Deleted: true}, nil
}

func handleTemplatesRevert(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("revert template", templatesRevertID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.templateOf(c.args.Template)
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
	var answer revertTemplateAnswer
	r := request{method: http.MethodPost, path: path + ":revert", query: query, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &answer); err != nil {
		return nil, err
	}
	return revertTemplateResult{Template: answer.Template}, nil
}

// templateSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
// templateData is code and never part of it.
func templateSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a toolArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		where := a.Workspace
		if a.Template != "" {
			where = a.Template
		}
		out := verb + " template"
		if a.Name != "" {
			out += " " + strconv.Quote(a.Name)
		}
		return out + " at " + where + " (workspace only, its code runs on pages with a published version)"
	}
}

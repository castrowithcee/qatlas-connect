package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
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
	apiKeysListID     = Provider + ".api-keys.list"
	apiKeysGetID      = Provider + ".api-keys.get"
	apiKeysDeleteID   = Provider + ".api-keys.delete"
	apiKeysUndeleteID = Provider + ".api-keys.undelete"
)

// keyStringMember is the member of a Key that holds the secret value of an API key. No wire type of this file
// declares it, so decoding drops it, and stripKeyString removes it from the open objects of an operation.
const keyStringMember = "keyString"

var (
	// apiKeyIDPattern is the key ID of discovery apikeys v2: 4 to 63 characters in the dialect Google uses for
	// generated IDs. Qatlas accepts the wider 1 to 63 so that a key with an older ID can still be read.
	apiKeyIDPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	// apiKeyName is the resource name of a key row. Google names the project by number, so the project segment
	// is only compared where the argument and the row use the same form (see sameProject).
	apiKeyName = regexp.MustCompile(`^projects/([A-Za-z0-9-]{1,30})/locations/global/keys/([A-Za-z0-9_-]{1,63})$`)
	// apiKeyEtag is the printable ASCII an etag consists of; it travels as a query value, never in a path.
	apiKeyEtag = regexp.MustCompile(`^[ -~]{1,256}$`)
)

const apiKeyIDSchema = `{"type":"string","minLength":1,"maxLength":63,"pattern":"^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$"}`

// apiKeyPath is the request path of the keys of a project, or of one key. The project is a checked project ID or
// number, the key ID matches apiKeyIDPattern, and both are escaped as path segments anyway.
func apiKeyPath(project, key string) string {
	path := "/v2/projects/" + url.PathEscape(project) + "/locations/global/keys"
	if key != "" {
		path += "/" + url.PathEscape(key)
	}
	return path
}

// --- wire and result types ---

// apiKeyWire is the part of a V2Key that Qatlas reads. keyString is left out on purpose: the secret value of
// the key never reaches a result, even where Google sends it.
type apiKeyWire struct {
	Name                string            `json:"name"`
	UID                 string            `json:"uid"`
	DisplayName         string            `json:"displayName"`
	CreateTime          string            `json:"createTime"`
	UpdateTime          string            `json:"updateTime"`
	DeleteTime          string            `json:"deleteTime"`
	Annotations         map[string]string `json:"annotations"`
	Restrictions        *apiKeyLimits     `json:"restrictions"`
	ServiceAccountEmail string            `json:"serviceAccountEmail"`
	Etag                string            `json:"etag"`
}

// apiKeyLimits is V2Restrictions. The same type decodes the answer and carries the bounded result.
type apiKeyLimits struct {
	APITargets             []apiTarget  `json:"apiTargets,omitempty"`
	AndroidKeyRestrictions *androidKeys `json:"androidKeyRestrictions,omitempty"`
	BrowserKeyRestrictions *browserKeys `json:"browserKeyRestrictions,omitempty"`
	ServerKeyRestrictions  *serverKeys  `json:"serverKeyRestrictions,omitempty"`
	IosKeyRestrictions     *iosKeys     `json:"iosKeyRestrictions,omitempty"`
}

type apiTarget struct {
	Service string   `json:"service,omitempty"`
	Methods []string `json:"methods,omitempty"`
}

type androidApp struct {
	Sha1Fingerprint string `json:"sha1Fingerprint,omitempty"`
	PackageName     string `json:"packageName,omitempty"`
}

type androidKeys struct {
	AllowedApplications []androidApp `json:"allowedApplications,omitempty"`
}

type browserKeys struct {
	AllowedReferrers []string `json:"allowedReferrers,omitempty"`
}

type serverKeys struct {
	AllowedIps []string `json:"allowedIps,omitempty"`
}

type iosKeys struct {
	AllowedBundleIds []string `json:"allowedBundleIds,omitempty"`
}

type apiKeyResult struct {
	Name                string            `json:"name"`
	UID                 string            `json:"uid,omitempty"`
	DisplayName         string            `json:"displayName,omitempty"`
	CreateTime          string            `json:"createTime,omitempty"`
	UpdateTime          string            `json:"updateTime,omitempty"`
	DeleteTime          string            `json:"deleteTime,omitempty"`
	Annotations         map[string]string `json:"annotations,omitempty"`
	Restrictions        *apiKeyLimits     `json:"restrictions,omitempty"`
	ServiceAccountEmail string            `json:"serviceAccountEmail,omitempty"`
	Etag                string            `json:"etag,omitempty"`
}

// sameProject is false only when the key row provably names another project than the argument: two project IDs,
// or two project numbers, that differ. An ID against a number cannot be compared.
func sameProject(argument, row string) bool {
	if argument == row {
		return true
	}
	return numberPattern.MatchString(argument) != numberPattern.MatchString(row)
}

// check refuses a row that is not a key below project. Google never answers a request below one project with a
// key of another, so such a row is an invalid response and is not shown.
func (k apiKeyWire) check(op, project string) (key string, err error) {
	m := apiKeyName.FindStringSubmatch(k.Name)
	if m == nil || !sameProject(project, m[1]) {
		return "", invalidRow(op)
	}
	return m[2], nil
}

func boundedTexts(in []string, limit int) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, min(len(in), maxListItems))
	for _, item := range in[:min(len(in), maxListItems)] {
		out = append(out, boundedText(item, limit))
	}
	return out
}

func (l *apiKeyLimits) bounded() *apiKeyLimits {
	if l == nil {
		return nil
	}
	out := &apiKeyLimits{}
	for _, t := range l.APITargets[:min(len(l.APITargets), maxListItems)] {
		out.APITargets = append(out.APITargets, apiTarget{Service: boundedText(t.Service, maxNameText),
			Methods: boundedTexts(t.Methods, maxNameText)})
	}
	if l.AndroidKeyRestrictions != nil {
		out.AndroidKeyRestrictions = &androidKeys{}
		apps := l.AndroidKeyRestrictions.AllowedApplications
		for _, a := range apps[:min(len(apps), maxListItems)] {
			out.AndroidKeyRestrictions.AllowedApplications = append(out.AndroidKeyRestrictions.AllowedApplications,
				androidApp{Sha1Fingerprint: boundedText(a.Sha1Fingerprint, maxNameText),
					PackageName: boundedText(a.PackageName, maxNameText)})
		}
	}
	if l.BrowserKeyRestrictions != nil {
		out.BrowserKeyRestrictions = &browserKeys{AllowedReferrers: boundedTexts(l.BrowserKeyRestrictions.AllowedReferrers, maxText)}
	}
	if l.ServerKeyRestrictions != nil {
		out.ServerKeyRestrictions = &serverKeys{AllowedIps: boundedTexts(l.ServerKeyRestrictions.AllowedIps, maxNameText)}
	}
	if l.IosKeyRestrictions != nil {
		out.IosKeyRestrictions = &iosKeys{AllowedBundleIds: boundedTexts(l.IosKeyRestrictions.AllowedBundleIds, maxNameText)}
	}
	return out
}

func (k apiKeyWire) result() apiKeyResult {
	return apiKeyResult{Name: boundedText(k.Name, maxNameText), UID: boundedText(k.UID, maxNameText),
		DisplayName: boundedText(k.DisplayName, maxNameText), CreateTime: boundedText(k.CreateTime, maxNameText),
		UpdateTime: boundedText(k.UpdateTime, maxNameText), DeleteTime: boundedText(k.DeleteTime, maxNameText),
		Annotations: boundedMap(k.Annotations), Restrictions: k.Restrictions.bounded(),
		ServiceAccountEmail: boundedText(k.ServiceAccountEmail, maxNameText), Etag: boundedText(k.Etag, maxNameText)}
}

// stripKeyString removes every member named keyString, at any depth, from decoded provider data. The Operation
// of a key change carries the changed key as its response, and a Key holds its secret value in keyString.
func stripKeyString(value any) any {
	switch value := value.(type) {
	case map[string]any:
		for member := range value {
			if strings.EqualFold(member, keyStringMember) {
				delete(value, member)
				continue
			}
			value[member] = stripKeyString(value[member])
		}
		return value
	case []any:
		for i := range value {
			value[i] = stripKeyString(value[i])
		}
		return value
	}
	return value
}

// --- shared descriptor parts ---

const (
	apiKeyProjectProp = `"project_id":{"type":"string","minLength":1,"maxLength":30,` +
		`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}`

	stringList = `{"type":"array","items":{"type":"string"}}`

	apiKeyLimitsSchema = `{"type":"object","properties":{"apiTargets":{"type":"array","items":{"type":"object",` +
		`"properties":{"service":{"type":"string"},"methods":` + stringList + `},"additionalProperties":false}},` +
		`"androidKeyRestrictions":{"type":"object","properties":{"allowedApplications":{"type":"array","items":` +
		`{"type":"object","properties":{"sha1Fingerprint":{"type":"string"},"packageName":{"type":"string"}},` +
		`"additionalProperties":false}}},"additionalProperties":false},` +
		`"browserKeyRestrictions":{"type":"object","properties":{"allowedReferrers":` + stringList + `},"additionalProperties":false},` +
		`"serverKeyRestrictions":{"type":"object","properties":{"allowedIps":` + stringList + `},"additionalProperties":false},` +
		`"iosKeyRestrictions":{"type":"object","properties":{"allowedBundleIds":` + stringList + `},"additionalProperties":false}},` +
		`"additionalProperties":false}`

	apiKeyOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"uid":{"type":"string"},` +
		`"displayName":{"type":"string"},"createTime":{"type":"string"},"updateTime":{"type":"string"},` +
		`"deleteTime":{"type":"string"},"annotations":{"type":"object","additionalProperties":{"type":"string"}},` +
		`"restrictions":` + apiKeyLimitsSchema + `,"serviceAccountEmail":{"type":"string"},"etag":{"type":"string"}},` +
		`"required":["name"],"additionalProperties":false}`

	apiKeyFieldText = "{name, uid, displayName, createTime, updateTime, deleteTime, annotations, restrictions, " +
		"serviceAccountEmail, etag}"

	apiKeysNote = " The project must lie in the targets of the connection. The API Keys API " +
		"(apikeys.googleapis.com) must be enabled in the project of the credential. The value of a key " +
		"(keyString) is never returned and never requested, and no tool of this provider creates a key or reads its value."
)

// apiKeyRisk is the risk of the read tools: they read, are safe to repeat, and describe the access of a project.
var apiKeyRisk = capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: accessSensitivity}

var (
	apiKeyProjectArgument = capability.Argument{Name: "project_id", Required: true, Description: "Project that " +
		"owns the keys: its ID or number, never a projects/ name or a path. The project must lie in the targets " +
		"of the connection"}
	apiKeyIDArgument = capability.Argument{Name: "key_id", Required: true, Description: "ID of the key, the last " +
		"segment of its name: lowercase letters, digits, and hyphens; never a resource name or a path"}
	apiKeyFields = []capability.Field{
		{Name: "name", Description: "Resource name, projects/NUMBER/locations/global/keys/KEY_ID"},
		{Name: "uid", Description: "Unique ID of the key (a UUID)"},
		{Name: "displayName", Description: "Display name given to the key; untrusted text, cut"},
		{Name: "createTime", Description: "When the key was created (RFC 3339)"},
		{Name: "updateTime", Description: "When the key was last updated (RFC 3339)"},
		{Name: "deleteTime", Description: "When the key was deleted (RFC 3339); present only for a deleted key, " +
			"which Google keeps for 30 days"},
		{Name: "annotations", Description: "Annotations of the key, text to text; untrusted, cut"},
		{Name: "restrictions", Description: "Where the key may be used: apiTargets (service and methods), " +
			"androidKeyRestrictions, browserKeyRestrictions (allowed referrers), serverKeyRestrictions (allowed IP " +
			"addresses), and iosKeyRestrictions; without any, the key has no restriction; untrusted text, cut"},
		{Name: "serviceAccountEmail", Description: "Service account the key is bound to, if any"},
		{Name: "etag", Description: "Checksum of the key as Google holds it; delete takes it as a precondition"},
	}
)

// apiKeyClient opens a key tool: the connection, the project check against the targets, and the client. The
// project is checked before any secret is read or request is sent.
func apiKeyClient(ctx context.Context, op, tool, project string, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (*client, error) {
	return serviceClient(ctx, op, tool, project, resolved, secrets, red)
}

// --- api-keys.list ---

var apiKeysList = capability.Descriptor{
	ID: apiKeysListID, Version: 1, Title: "List the API keys of a Google Cloud project",
	Description: "List the metadata of the API keys of one project (projects.locations.keys.list of API Keys v2): " +
		"name, unique ID, display name, timestamps, annotations, restrictions (API targets, browser referrers, " +
		"server IP addresses, Android and iOS applications), bound service account, and etag. With show_deleted the " +
		"list also holds the keys deleted in the last 30 days." + apiKeysNote + " Names and restrictions are " +
		"untrusted third-party text and are cut.",
	Tags: []string{"google", "cloud", "api-keys"}, Risk: apiKeyRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + apiKeyProjectProp + `,` + showDeletedProp + `,` +
		pageSizeProp + `,` + cursorProp + `},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("keys", apiKeyOutputSchema),
	Arguments: []capability.Argument{apiKeyProjectArgument,
		{Name: "show_deleted", Description: "True to include the keys deleted in the last 30 days; default false"},
		pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{{Name: "keys", Description: "Keys of the page, each " + apiKeyFieldText}},
		pagingFields...),
	Examples: []capability.Example{{
		Description: "List the keys of a project, deleted ones included",
		Arguments:   json.RawMessage(`{"project_id":"example-project-123","show_deleted":true}`),
	}},
}

func handleAPIKeysList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list API keys"
	var arguments struct {
		ProjectID   string `json:"project_id"`
		ShowDeleted bool   `json:"show_deleted"`
		PageSize    *int   `json:"page_size"`
		Cursor      string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	// The cursor binds the tool, the targets, the project, and the filter.
	binding := provider.CursorBinding(apiKeysListID, provider.TargetsOf(resolved),
		arguments.ProjectID+"|"+strconv.FormatBool(arguments.ShowDeleted))
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := apiKeyClient(ctx, op, apiKeysListID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if arguments.ShowDeleted {
		query.Set("showDeleted", "true")
	}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var members map[string]json.RawMessage
	if err := c.get(ctx, op, apiKeysListID, serviceAPIKeys, apiKeyPath(arguments.ProjectID, ""), query, &members); err != nil {
		return nil, err
	}
	rows, next, err := decodePage[apiKeyWire](op, members, "keys", 2*maxPageSize)
	if err != nil {
		return nil, err
	}
	out := make([]apiKeyResult, 0, len(rows))
	for _, row := range rows {
		if _, err := row.check(op, arguments.ProjectID); err != nil {
			return nil, err
		}
		out = append(out, row.result())
	}
	page := map[string]any{"keys": out, "has_more": next != ""}
	if next != "" {
		page["next_cursor"] = provider.EncodeCursor(binding, next)
	}
	return page, nil
}

// --- api-keys.get ---

var apiKeysGet = capability.Descriptor{
	ID: apiKeysGetID, Version: 1, Title: "Get an API key of a Google Cloud project",
	Description: "Read the metadata of one API key (projects.locations.keys.get of API Keys v2): name, unique ID, " +
		"display name, timestamps, annotations, restrictions, bound service account, and etag." + apiKeysNote +
		" Names and restrictions are untrusted third-party text and are cut.",
	Tags: []string{"google", "cloud", "api-keys"}, Risk: apiKeyRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + apiKeyProjectProp + `,"key_id":` + apiKeyIDSchema +
		`},"required":["project_id","key_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(apiKeyOutputSchema),
	Arguments:    []capability.Argument{apiKeyProjectArgument, apiKeyIDArgument},
	Fields:       apiKeyFields,
	Examples: []capability.Example{{
		Description: "Read the metadata of one key",
		Arguments:   json.RawMessage(`{"project_id":"example-project-123","key_id":"0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"}`),
	}},
}

type apiKeyArguments struct {
	ProjectID string `json:"project_id"`
	KeyID     string `json:"key_id"`
}

func handleAPIKeysGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get API key"
	var a apiKeyArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !apiKeyIDPattern.MatchString(a.KeyID) {
		return nil, badRequest("key_id must be the ID of the key: lowercase letters, digits, and hyphens")
	}
	c, err := apiKeyClient(ctx, op, apiKeysGetID, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// The request never asks for the value of the key, and the wire type drops keyString even if it comes.
	var row apiKeyWire
	if err := c.get(ctx, op, apiKeysGetID, serviceAPIKeys, apiKeyPath(a.ProjectID, a.KeyID), nil, &row); err != nil {
		return nil, err
	}
	key, err := row.check(op, a.ProjectID)
	if err != nil || key != a.KeyID {
		return nil, invalidRow(op)
	}
	return row.result(), nil
}

// --- api-keys.delete and api-keys.undelete ---

const (
	operationSchema = `{"type":"object","properties":{"name":{"type":"string"},"done":{"type":"boolean"},` +
		`"metadata":{"type":"object"},"response":{"type":"object"}},"required":["name","done"],"additionalProperties":false}`

	apiKeyChangeNote = " Google answers with a long-running operation, which this tool returns as it is and " +
		"never waits for: google.cloud.operations.get with service api-keys reads its state by name. The value of " +
		"the key is removed from the operation, whatever Google sends. Needs the confirmation of a person for each " +
		"call. The credential needs permission on the project in IAM, which Google decides."
)

var operationFields = []capability.Field{
	{Name: "name", Description: "Name of the operation, operations/ID; read it with operations.get"},
	{Name: "done", Description: "True when the operation already ended; false while it runs"},
	{Name: "metadata", Description: "Metadata of the operation with its @type; untrusted data, cut"},
	{Name: "response", Description: "Response of a finished operation without any key value; untrusted data, cut"},
}

var (
	apiKeysDelete = capability.Descriptor{
		ID: apiKeysDeleteID, Version: 1, Title: "Delete an API key of a Google Cloud project",
		Description: "Delete one API key (projects.locations.keys.delete of API Keys v2). Applications that use the " +
			"key fail afterwards. Google keeps a deleted key for 30 days, in which api-keys.undelete restores it. " +
			"etag makes the delete conditional on the state of the key that was read; check_existing_usage CHECK " +
			"lets Google refuse the delete if the key was used lately, SKIP deletes regardless; without it Google " +
			"decides. The project must lie in the targets of the connection." + apiKeyChangeNote,
		Tags: []string{"google", "cloud", "api-keys", "delete"}, Provider: Provider,
		Risk: accountWriteRisk(capability.EffectDelete, capability.IdempotencyUnknown),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + apiKeyProjectProp + `,"key_id":` + apiKeyIDSchema +
			`,"etag":{"type":"string","minLength":1,"maxLength":256,"pattern":"^[ -~]{1,256}$"},` +
			`"check_existing_usage":{"type":"string","enum":["SKIP","CHECK"]}},` +
			`"required":["project_id","key_id"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(operationSchema),
		Arguments: []capability.Argument{apiKeyProjectArgument, apiKeyIDArgument,
			{Name: "etag", Description: "Etag of the key as api-keys.get returned it; Google refuses the delete " +
				"if the key changed since"},
			{Name: "check_existing_usage", Description: "CHECK: Google refuses the delete if the key was used " +
				"lately; SKIP: delete without that check; absent: Google's default"}},
		Fields: operationFields,
		Examples: []capability.Example{{Description: "Delete a key if it did not change",
			Arguments: json.RawMessage(`{"project_id":"example-project-123","key_id":"0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d",` +
				`"etag":"W/\"synthetic\"","check_existing_usage":"CHECK"}`)}},
	}

	apiKeysUndelete = capability.Descriptor{
		ID: apiKeysUndeleteID, Version: 1, Title: "Restore a deleted API key of a Google Cloud project",
		Description: "Restore an API key that was deleted not more than 30 days ago (projects.locations.keys." +
			"undelete of API Keys v2). Outside that period Google refuses, which shows as a provider error. The " +
			"key works again with its restrictions as they were. The project must lie in the targets of the " +
			"connection." + apiKeyChangeNote,
		Tags: []string{"google", "cloud", "api-keys", "update"}, Provider: Provider,
		Risk: accountWriteRisk(capability.EffectUpdate, capability.IdempotencyUnknown),
		InputSchema: json.RawMessage(`{"type":"object","properties":{` + apiKeyProjectProp + `,"key_id":` + apiKeyIDSchema +
			`},"required":["project_id","key_id"],"additionalProperties":false}`),
		OutputSchema: json.RawMessage(operationSchema),
		Arguments:    []capability.Argument{apiKeyProjectArgument, apiKeyIDArgument},
		Fields:       operationFields,
		Examples: []capability.Example{{Description: "Restore a deleted key",
			Arguments: json.RawMessage(`{"project_id":"example-project-123","key_id":"0a1b2c3d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"}`)}},
	}
)

func handleAPIKeysDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete API key"
	var a struct {
		apiKeyArguments
		Etag               string `json:"etag"`
		CheckExistingUsage string `json:"check_existing_usage"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !apiKeyIDPattern.MatchString(a.KeyID) {
		return nil, badRequest("key_id must be the ID of the key: lowercase letters, digits, and hyphens")
	}
	query := url.Values{}
	if a.Etag != "" {
		if !apiKeyEtag.MatchString(a.Etag) {
			return nil, badRequest("etag must be the etag of the key as api-keys.get returned it")
		}
		query.Set("etag", a.Etag)
	}
	switch a.CheckExistingUsage {
	case "":
	case "SKIP", "CHECK":
		query.Set("checkExistingUsage", a.CheckExistingUsage)
	default:
		return nil, badRequest("check_existing_usage must be SKIP or CHECK")
	}
	c, err := apiKeyClient(ctx, op, apiKeysDeleteID, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire operationWire
	if err := c.send(ctx, op, apiKeysDeleteID, serviceAPIKeys, http.MethodDelete, apiKeyPath(a.ProjectID, a.KeyID), query,
		nil, &wire, true); err != nil {
		return nil, err
	}
	return operationAnswer(op, wire)
}

func handleAPIKeysUndelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "undelete API key"
	var a apiKeyArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !apiKeyIDPattern.MatchString(a.KeyID) {
		return nil, badRequest("key_id must be the ID of the key: lowercase letters, digits, and hyphens")
	}
	c, err := apiKeyClient(ctx, op, apiKeysUndeleteID, a.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var wire operationWire
	if err := c.mutate(ctx, op, apiKeysUndeleteID, serviceAPIKeys, apiKeyPath(a.ProjectID, a.KeyID)+":undelete",
		[]byte("{}"), &wire); err != nil {
		return nil, err
	}
	return operationAnswer(op, wire)
}

// operationAnswer returns the operation of a key change as the result of the tool.
func operationAnswer(op string, wire operationWire) (any, error) {
	result, err := operationOf(op, wire)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// apiKeySummary phrases a key change from its validated arguments alone, so it cannot name the display name of
// the key: that would take a request.
func apiKeySummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a apiKeyArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		project := "project " + boundedText(a.ProjectID, maxNameText)
		key := "API key " + strconv.Quote(boundedText(a.KeyID, maxNameText))
		if tool == apiKeysUndeleteID {
			return "Restore " + key + " in " + project + ": it works again, with the restrictions it had"
		}
		return "Delete " + key + " in " + project + ": applications that use this key fail afterwards, and Google " +
			"allows a restore for 30 days only"
	}
}

package googlecloud

import (
	"context"
	"encoding/json"
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
	hmacKeysListID = Provider + ".hmac-keys.list"
	hmacKeysGetID  = Provider + ".hmac-keys.get"
)

// maxHMACResults is the page size Google documents for hmacKeys.list: it serves the given size or 250,
// whichever is smaller.
const maxHMACResults = 250

// hmacAccessIDPattern holds the access IDs this provider accepts. It holds no slash, so an ID can never be a
// path. The ID is not a secret: it names a key and is shown by Google with the metadata.
var hmacAccessIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

// hmacWire is the HmacKeyMetadata of storage v1 as Google sends it. It lists no member for the key value,
// and every member is a string, so the value that hmacKeys.create alone returns cannot pass through this
// type at any depth, whatever the answer holds.
type hmacWire struct {
	Kind                string `json:"kind"`
	ID                  string `json:"id"`
	SelfLink            string `json:"selfLink"`
	AccessID            string `json:"accessId"`
	ProjectID           string `json:"projectId"`
	ServiceAccountEmail string `json:"serviceAccountEmail"`
	State               string `json:"state"`
	TimeCreated         string `json:"timeCreated"`
	Updated             string `json:"updated"`
	Etag                string `json:"etag"`
}

type hmacResult struct {
	AccessID            string `json:"accessId"`
	State               string `json:"state,omitempty"`
	ServiceAccountEmail string `json:"serviceAccountEmail,omitempty"`
	ProjectID           string `json:"projectId,omitempty"`
	TimeCreated         string `json:"timeCreated,omitempty"`
	Updated             string `json:"updated,omitempty"`
	Etag                string `json:"etag,omitempty"`
	ID                  string `json:"id,omitempty"`
	Kind                string `json:"kind,omitempty"`
	SelfLink            string `json:"selfLink,omitempty"`
}

func (k hmacWire) result() hmacResult {
	return hmacResult{AccessID: boundedText(k.AccessID, maxNameText), State: boundedText(k.State, maxNameText),
		ServiceAccountEmail: boundedText(k.ServiceAccountEmail, maxNameText),
		ProjectID:           boundedText(k.ProjectID, maxNameText), TimeCreated: boundedText(k.TimeCreated, maxNameText),
		Updated: boundedText(k.Updated, maxNameText), Etag: boundedText(k.Etag, maxNameText),
		ID: boundedText(k.ID, maxText), Kind: boundedText(k.Kind, maxNameText), SelfLink: boundedText(k.SelfLink, maxText)}
}

var hmacMembers = []computeMember{
	{"accessId", "string"}, {"state", "string"}, {"serviceAccountEmail", "string"}, {"projectId", "string"},
	{"timeCreated", "string"}, {"updated", "string"}, {"etag", "string"}, {"id", "string"}, {"kind", "string"},
	{"selfLink", "string"},
}

var hmacSchema = storageMembersSchema(hmacMembers, "accessId")

const hmacFields = "{accessId, state, serviceAccountEmail, projectId, timeCreated, updated, etag, id, kind, " +
	"selfLink}; state is ACTIVE, INACTIVE, DELETING, or DELETED; never the key value (secret)"

const hmacTail = " The project must lie in the targets of the connection, and Qatlas proves the project and its " +
	"number by reading it (projects.get) before any key is read; every returned key is checked again against it. " +
	"The key value (secret) is never read out: only the fixed metadata fields are returned, and keys cannot be " +
	"created, changed, or deleted." + storageTail

const hmacProjectProp = serviceProjectProp

// hmacProject binds the project of a HMAC tool: it holds the ID and the number that projects.get proved, which
// a row names in either spelling.
type hmacProject struct{ id, number string }

// identify reads the project once and returns its ID and number. The answer must be the project that was asked
// for.
func (s *storageSession) identify(ctx context.Context, ref string) (hmacProject, error) {
	project, err := s.c.project(ctx, s.op, projectsGetID, ref)
	if err != nil {
		return hmacProject{}, err
	}
	id, number := project.ProjectID, strings.TrimPrefix(project.Name, prefixProject)
	if !projectIDShape.MatchString(id) || !numberPattern.MatchString(number) || ref != id && ref != number {
		return hmacProject{}, invalidRow(s.op)
	}
	return hmacProject{id: id, number: number}, nil
}

func (p hmacProject) owns(projectID string) bool {
	return projectID != "" && (projectID == p.id || projectID == p.number)
}

// accountNamesProject reports whether the address of a service account names the project: the project ID in
// NAME@PROJECT_ID.iam.gserviceaccount.com and in the App Engine default account, the project number in the
// Compute Engine default account. The address was checked for its form before.
func (p hmacProject) accountNamesProject(email string) bool {
	member := memberServiceAccountPfx + email
	if m := projectAccountMember.FindStringSubmatch(member); m != nil {
		return m[1] == p.id
	}
	if m := appspotAccountMember.FindStringSubmatch(member); m != nil {
		return m[1] == p.id
	}
	if m := computeAccountMember.FindStringSubmatch(member); m != nil {
		return m[1] == p.number
	}
	return false
}

func hmacAccountForm(email string) bool {
	member := memberServiceAccountPfx + email
	return projectAccountMember.MatchString(member) || computeAccountMember.MatchString(member) ||
		appspotAccountMember.MatchString(member)
}

func hmacKeysPath(projectID string) string {
	return storageBasePath + "/projects/" + url.PathEscape(projectID) + "/hmacKeys"
}

var hmacKeysList = capability.Descriptor{
	ID: hmacKeysListID, Version: 1, Title: "List the HMAC key metadata of a project's Cloud Storage",
	Description: "List the metadata of the HMAC keys of a project from Cloud Storage, optionally for one service " +
		"account and with deleted keys: access ID, state, service account, project, creation and update time, and " +
		"etag. The service account must belong to the project (NAME@PROJECT_ID.iam.gserviceaccount.com, the " +
		"Compute Engine default account PROJECT_NUMBER-compute of the domain developer.gserviceaccount.com, or the App Engine " +
		"default account); an account of another project is refused before the list is read, without naming it." +
		hmacTail,
	Tags: []string{"google", "cloud", "storage", "hmac-keys"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hmacProjectProp + `,` +
		`"service_account_email":{"type":"string","minLength":1,"maxLength":128},"show_deleted_keys":{"type":"boolean"},` +
		`"max_results":{"type":"integer","minimum":1,"maximum":250},` + cursorProp + `,` + storageUserProjectProp + `},` +
		`"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"hmac_keys":{"type":"array","items":` + hmacSchema +
		`},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"required":["hmac_keys","has_more"],` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "service_account_email", Description: "Return only the keys of this service account, which must " +
			"belong to the project; Qatlas checks the account against the project and every returned key again"},
		{Name: "show_deleted_keys", Description: "True to include keys in the DELETED state"},
		{Name: "max_results", Description: "Keys Google returns at most per page, 1 to 250; Google may return " +
			"fewer, and Qatlas drops rows of another project. Without it the tool asks for 100"},
		computeCursorArgument, storageUserProjectArgument},
	Fields: append([]capability.Field{
		{Name: "hmac_keys", Description: "HMAC key metadata of the page, each " + hmacFields},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the HMAC keys of a project including deleted ones",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","show_deleted_keys":true}`),
	}},
}

var hmacKeysGet = capability.Descriptor{
	ID: hmacKeysGetID, Version: 1, Title: "Get the metadata of one HMAC key",
	Description: "Read the metadata of one HMAC key of a project from Cloud Storage by its access ID: state, " +
		"service account, project, creation and update time, and etag. The access ID names the key and is not " +
		"secret." + hmacTail,
	Tags: []string{"google", "cloud", "storage", "hmac-keys"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hmacProjectProp + `,` +
		`"access_id":{"type":"string","minLength":8,"maxLength":128,"pattern":"^[A-Za-z0-9_-]{8,128}$"},` +
		storageUserProjectProp + `},"required":["project_id","access_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(hmacSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "access_id", Required: true, Description: "Access ID of the key, 8 to 128 letters, digits, " +
			"hyphens, and underscores; never a path"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "accessId", Description: "The key metadata, " + hmacFields}},
	Examples: []capability.Example{{
		Description: "Read the metadata of one key",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID1234"}`),
	}},
}

func handleHMACKeysList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list HMAC keys"
	var arguments struct {
		ProjectID   string `json:"project_id"`
		Email       string `json:"service_account_email"`
		ShowDeleted bool   `json:"show_deleted_keys"`
		MaxResults  *int   `json:"max_results"`
		Cursor      string `json:"cursor"`
		UserProject string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	size := defaultStorageResults
	if arguments.MaxResults != nil {
		if *arguments.MaxResults < 1 || *arguments.MaxResults > maxHMACResults {
			return nil, badRequest("max_results must be 1 to " + strconv.Itoa(maxHMACResults))
		}
		size = *arguments.MaxResults
	}
	if arguments.Email != "" && !hmacAccountForm(arguments.Email) {
		return nil, badRequest("service_account_email must be a service account address of the project, such as " +
			"NAME@PROJECT_ID.iam.gserviceaccount.com")
	}
	binding := provider.CursorBinding(hmacKeysListID, provider.TargetsOf(resolved), arguments.ProjectID, arguments.Email,
		arguments.ShowDeleted, arguments.UserProject)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	s, err := openStorage(ctx, op, hmacKeysListID, []namedProject{{"project_id", arguments.ProjectID},
		{"user_project", arguments.UserProject}}, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	project, err := s.identify(ctx, arguments.ProjectID)
	if err != nil {
		return nil, err
	}
	if arguments.Email != "" && !project.accountNamesProject(arguments.Email) {
		return nil, badRequest("service_account_email is not a service account of the project_id project")
	}
	query := storageQuery(arguments.UserProject)
	query.Set("maxResults", strconv.Itoa(size))
	if arguments.Email != "" {
		query.Set("serviceAccountEmail", arguments.Email)
	}
	if arguments.ShowDeleted {
		query.Set("showDeletedKeys", "true")
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var answer struct {
		Items         []json.RawMessage `json:"items"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := s.c.get(ctx, op, hmacKeysListID, serviceStorage, hmacKeysPath(arguments.ProjectID), query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxStorageResults {
		return nil, invalidRow(op)
	}
	rows := make([]hmacResult, 0, len(answer.Items))
	for _, item := range answer.Items {
		var wire hmacWire
		if err := json.Unmarshal(item, &wire); err != nil || wire.AccessID == "" {
			return nil, invalidRow(op)
		}
		// The server filter is an optimisation: a row of another project, or of another account than the
		// filter names, is dropped here.
		if !project.owns(wire.ProjectID) || arguments.Email != "" && wire.ServiceAccountEmail != arguments.Email {
			continue
		}
		rows = append(rows, wire.result())
	}
	return computePaged(op, map[string]any{"hmac_keys": rows}, binding, answer.NextPageToken)
}

func handleHMACKeysGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get HMAC key"
	var arguments struct {
		ProjectID   string `json:"project_id"`
		AccessID    string `json:"access_id"`
		UserProject string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !hmacAccessIDPattern.MatchString(arguments.AccessID) {
		return nil, badRequest("access_id must be 8 to 128 letters, digits, hyphens, and underscores, never a path")
	}
	s, err := openStorage(ctx, op, hmacKeysGetID, []namedProject{{"project_id", arguments.ProjectID},
		{"user_project", arguments.UserProject}}, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	project, err := s.identify(ctx, arguments.ProjectID)
	if err != nil {
		return nil, err
	}
	var wire hmacWire
	if err := s.c.get(ctx, op, hmacKeysGetID, serviceStorage,
		hmacKeysPath(arguments.ProjectID)+"/"+url.PathEscape(arguments.AccessID), storageQuery(arguments.UserProject),
		&wire); err != nil {
		return nil, err
	}
	if wire.AccessID != arguments.AccessID {
		return nil, invalidRow(op)
	}
	if !project.owns(wire.ProjectID) {
		return nil, refuseOutside("the key")
	}
	return wire.result(), nil
}

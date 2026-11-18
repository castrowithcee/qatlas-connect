package googlecloud

import (
	"context"
	"encoding/json"
	"strconv"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	notificationsListID  = Provider + ".notifications.list"
	managedFoldersListID = Provider + ".managed-folders.list"
)

// maxNotificationConfigs is the number of notification configurations Google documents a bucket may hold.
const maxNotificationConfigs = 100

// notificationWire is the Notification resource of storage v1 as Google sends it. A member of another type
// than the documented one makes the answer invalid instead of passing it on.
type notificationWire struct {
	Kind             string            `json:"kind"`
	ID               string            `json:"id"`
	SelfLink         string            `json:"selfLink"`
	Topic            string            `json:"topic"`
	EventTypes       []string          `json:"event_types"`
	CustomAttributes map[string]string `json:"custom_attributes"`
	ObjectPrefix     string            `json:"object_name_prefix"`
	PayloadFormat    string            `json:"payload_format"`
	Etag             string            `json:"etag"`
}

type notificationResult struct {
	ID               string            `json:"id"`
	Kind             string            `json:"kind,omitempty"`
	SelfLink         string            `json:"selfLink,omitempty"`
	Topic            string            `json:"topic,omitempty"`
	EventTypes       []string          `json:"event_types,omitempty"`
	CustomAttributes map[string]string `json:"custom_attributes,omitempty"`
	ObjectPrefix     string            `json:"object_name_prefix,omitempty"`
	PayloadFormat    string            `json:"payload_format,omitempty"`
	Etag             string            `json:"etag,omitempty"`
}

func (n notificationWire) result() notificationResult {
	return notificationResult{ID: boundedText(n.ID, maxNameText), Kind: boundedText(n.Kind, maxNameText),
		SelfLink: boundedText(n.SelfLink, maxText), Topic: boundedText(n.Topic, maxText),
		EventTypes: boundedList(n.EventTypes), CustomAttributes: boundedMap(n.CustomAttributes),
		ObjectPrefix: boundedText(n.ObjectPrefix, maxText), PayloadFormat: boundedText(n.PayloadFormat, maxNameText),
		Etag: boundedText(n.Etag, maxNameText)}
}

var notificationMembers = []computeMember{
	{"id", "string"}, {"kind", "string"}, {"selfLink", "string"}, {"topic", "string"}, {"event_types", "array"},
	{"custom_attributes", "object"}, {"object_name_prefix", "string"}, {"payload_format", "string"},
	{"etag", "string"},
}

const notificationFields = "{id, topic, event_types, custom_attributes, object_name_prefix, payload_format, etag, " +
	"kind, selfLink}; topic is the Pub/Sub topic that receives the events, untrusted text that Qatlas only reports " +
	"and never calls"

var notificationsList = capability.Descriptor{
	ID: notificationsListID, Version: 1, Title: "List the notification configurations of a Cloud Storage bucket",
	Description: "List the Pub/Sub notification configurations of a bucket from Cloud Storage, each with its ID, " +
		"Pub/Sub topic, event types, custom attributes, object name prefix, payload format, and etag. The topic is a " +
		"notification target: Qatlas reads it as untrusted text and never fetches or calls it. Configurations " +
		"cannot be created, changed, or deleted." + bucketBindingTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "notifications"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` +
		storageUserProjectProp + `},"required":["bucket"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":` +
		storageMembersSchema(notificationMembers, "id") + `}},"required":["items"],"additionalProperties":false}`),
	Arguments: []capability.Argument{storageBucketArgument, storageUserProjectArgument},
	Fields: []capability.Field{
		{Name: "items", Description: "Notification configurations of the bucket, each " + notificationFields},
	},
	Examples: []capability.Example{{
		Description: "List the notification configurations of a bucket",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket"}`),
	}},
}

func handleNotificationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list notification configurations"
	var arguments struct {
		Bucket      string `json:"bucket"`
		UserProject string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(arguments.Bucket); err != nil {
		return nil, err
	}
	s, err := openStorage(ctx, op, notificationsListID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, notificationsListID, arguments.Bucket, arguments.UserProject); err != nil {
		return nil, err
	}
	var answer struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := s.c.get(ctx, op, notificationsListID, serviceStorage, bucketPath(arguments.Bucket)+"/notificationConfigs",
		storageQuery(arguments.UserProject), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxNotificationConfigs {
		return nil, invalidRow(op)
	}
	items := make([]notificationResult, 0, len(answer.Items))
	for _, item := range answer.Items {
		var wire notificationWire
		if err := json.Unmarshal(item, &wire); err != nil || wire.ID == "" {
			return nil, invalidRow(op)
		}
		items = append(items, wire.result())
	}
	return map[string]any{"items": items}, nil
}

// managedFolderMembers are the documented members of the ManagedFolder resource of storage v1 (revision
// 20261111); none of them is marked deprecated.
var managedFolderMembers = []computeMember{
	{"name", "string"}, {"id", "string"}, {"kind", "string"}, {"bucket", "string"}, {"selfLink", "string"},
	{"metageneration", "string"}, {"createTime", "string"}, {"updateTime", "string"},
	{"rapidCacheConfig", "object"},
}

const managedFolderFields = "{name, metageneration, createTime, updateTime, id, kind, bucket, selfLink, " +
	"rapidCacheConfig}; names are untrusted text"

// maxManagedFolderPrefix is the length of the longest object name Google accepts.
const maxManagedFolderPrefix = 1024

var managedFoldersList = capability.Descriptor{
	ID: managedFoldersListID, Version: 1, Title: "List the managed folders of a Cloud Storage bucket",
	Description: "List the managed folders of a bucket from Cloud Storage, optionally below a name prefix: each " +
		"with its name, metageneration, and creation and update time. Only a bucket with uniform bucket-level " +
		"access has managed folders. Managed folders cannot be created, changed, or deleted, and their IAM " +
		"policies are not read." + bucketBindingTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "managed-folders"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,"prefix":{"type":"string",` +
		`"minLength":1,"maxLength":1024},"page_size":{"type":"integer","minimum":1,"maximum":1000},` + cursorProp + `},` +
		`"required":["bucket"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"managed_folders":{"type":"array","items":` +
		storageMembersSchema(managedFolderMembers, "name") + `},"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["managed_folders","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "prefix", Description: "Return only managed folders whose name starts with it, such as logs/2026/; " +
			"1 to 1024 characters without control characters"},
		{Name: "page_size", Description: "Managed folders Google returns at most per page, 1 to 1000; Google may " +
			"return fewer. Without it the tool asks for 100"},
		computeCursorArgument},
	Fields: append([]capability.Field{
		{Name: "managed_folders", Description: "Managed folders of the page, each " + managedFolderFields},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the managed folders of a bucket below logs/",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket","prefix":"logs/"}`),
	}},
}

func handleManagedFoldersList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list managed folders"
	var arguments struct {
		Bucket   string `json:"bucket"`
		Prefix   string `json:"prefix"`
		PageSize *int   `json:"page_size"`
		Cursor   string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(arguments.Bucket); err != nil {
		return nil, err
	}
	if len(arguments.Prefix) > maxManagedFolderPrefix {
		return nil, badRequest("prefix must be at most 1024 characters")
	}
	for _, r := range arguments.Prefix {
		if unicode.IsControl(r) {
			return nil, badRequest("prefix must not contain control characters")
		}
	}
	pageSize := defaultStorageResults
	if arguments.PageSize != nil {
		if *arguments.PageSize < 1 || *arguments.PageSize > maxStorageResults {
			return nil, badRequest("page_size must be 1 to 1000")
		}
		pageSize = *arguments.PageSize
	}
	binding := provider.CursorBinding(managedFoldersListID, provider.TargetsOf(resolved), arguments.Bucket,
		arguments.Prefix)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	s, err := openStorage(ctx, op, managedFoldersListID, nil, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, managedFoldersListID, arguments.Bucket, ""); err != nil {
		return nil, err
	}
	query := storageQuery("")
	query.Set("pageSize", strconv.Itoa(pageSize))
	if arguments.Prefix != "" {
		query.Set("prefix", arguments.Prefix)
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var answer struct {
		Items         []json.RawMessage `json:"items"`
		NextPageToken string            `json:"nextPageToken"`
	}
	if err := s.c.get(ctx, op, managedFoldersListID, serviceStorage, bucketPath(arguments.Bucket)+"/managedFolders",
		query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxStorageResults {
		return nil, invalidRow(op)
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := boundedMembers(op, item, managedFolderMembers)
		if err != nil {
			return nil, err
		}
		name, _ := row["name"].(string)
		if bucket, has := row["bucket"].(string); name == "" || has && bucket != arguments.Bucket {
			return nil, invalidRow(op)
		}
		rows = append(rows, row)
	}
	return computePaged(op, map[string]any{"managed_folders": rows}, binding, answer.NextPageToken)
}

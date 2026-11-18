package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// This file holds the three tools that revoke an existing Cloud Storage access: they deactivate and delete an
// HMAC key and delete a notification configuration of a bucket. None of them creates a key or a configuration,
// and none of them reactivates a key. Each reads what it is about to change first and sends the change only
// when what it read matches what the caller named.

const (
	hmacKeysDeactivateID   = Provider + ".hmac-keys.deactivate"
	hmacKeysDeleteID       = Provider + ".hmac-keys.delete"
	notificationsDeleteID  = Provider + ".notifications.delete"
	hmacStateInactive      = "INACTIVE"
	pubsubTopicPrefix      = "//pubsub.googleapis.com/"
	maxNotificationTopic   = 256
	storageRevokeIDMaxSize = 64
)

var (
	// hmacEtagPattern holds the ETags of an HMAC key: base64 text that Google sends, never a path or a header.
	hmacEtagPattern = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{1,64}$`)
	// notificationIDPattern holds the IDs of a notification configuration. It holds no slash, so an ID can never
	// be a path.
	notificationIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,` + strconv.Itoa(storageRevokeIDMaxSize) + `}$`)
)

const (
	hmacAccessIDProp = `"access_id":{"type":"string","minLength":8,"maxLength":128,"pattern":"^[A-Za-z0-9_-]{8,128}$"}`
	hmacEtagProp     = `"etag":{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z0-9+/=_-]{1,64}$"}`
	notificationProp = `"notification_id":{"type":"string","minLength":1,"maxLength":64,"pattern":"^[A-Za-z0-9_-]{1,64}$"}`
	topicProp        = `"topic":{"type":"string","minLength":1,"maxLength":256}`

	hmacDeletedSchema = `{"type":"object","properties":{"projectId":{"type":"string"},"accessId":{"type":"string"},` +
		`"deleted":{"type":"boolean"}},"required":["projectId","accessId","deleted"],"additionalProperties":false}`
	notificationDeletedSchema = `{"type":"object","properties":{"bucket":{"type":"string"},` +
		`"notificationId":{"type":"string"},"topic":{"type":"string"},"deleted":{"type":"boolean"}},` +
		`"required":["bucket","notificationId","topic","deleted"],"additionalProperties":false}`
)

const hmacRevokeTail = " The project must lie in the targets of the connection, and Qatlas proves the project and " +
	"its number with projects.get, so the connection needs that tool. Before it changes anything, Qatlas reads the " +
	"key with hmacKeys.get and refuses, without a change, a key that does not belong to the project. The key " +
	"value (secret) is never read out, and no tool creates or reactivates a key." + storageChangeTail

var (
	hmacProjectArgument = serviceProjectArgument
	hmacAccessArgument  = capability.Argument{Name: "access_id", Required: true, Description: "Access ID of the key, " +
		"8 to 128 letters, digits, hyphens, and underscores; never a path"}
)

var hmacKeysDeactivate = capability.Descriptor{
	ID: hmacKeysDeactivateID, Version: 1, Title: "Deactivate an HMAC key of Cloud Storage",
	Description: "Set the state of one HMAC key of a project to INACTIVE (projects.hmacKeys.update of storage v1), so " +
		"requests signed with it fail. The state is fixed: no argument sets ACTIVE, and no tool reactivates a key. " +
		"The request body holds the state and, if etag is given, the etag, nothing else. The result is the metadata " +
		"of the key as Google returns it, never the key value." + hmacRevokeTail,
	Tags: []string{"google", "cloud", "storage", "hmac-keys", "update"}, Risk: storageChangeRisk(capability.EffectUpdate),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hmacProjectProp + `,` + hmacAccessIDProp + `,` +
		hmacEtagProp + `,` + storageUserProjectProp + `},"required":["project_id","access_id"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(hmacSchema),
	Arguments: []capability.Argument{hmacProjectArgument, hmacAccessArgument,
		{Name: "etag", Description: "Etag of the key as hmac-keys.get shows it. Only a comparison: Qatlas refuses " +
			"the change if the key it reads has another etag, and Google refuses it if the key changed since. " +
			"Without it the key is deactivated whatever its etag"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "accessId", Description: "The key metadata after the change, " + hmacFields}},
	Examples: []capability.Example{{
		Description: "Deactivate a key",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID1234","etag":"CAE="}`),
	}},
}

var hmacKeysDelete = capability.Descriptor{
	ID: hmacKeysDeleteID, Version: 1, Title: "Delete an HMAC key of Cloud Storage",
	Description: "Delete one HMAC key of a project for good (projects.hmacKeys.delete of storage v1). Google deletes " +
		"only a key in the state INACTIVE and refuses an ACTIVE one; deactivate it first. A deleted key cannot be " +
		"restored. Google answers with an empty body, so the result is the project and access ID as given, and " +
		"deleted." + hmacRevokeTail,
	Tags: []string{"google", "cloud", "storage", "hmac-keys", "delete"}, Risk: storageChangeRisk(capability.EffectDelete),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + hmacProjectProp + `,` + hmacAccessIDProp + `,` +
		storageUserProjectProp + `},"required":["project_id","access_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(hmacDeletedSchema),
	Arguments:    []capability.Argument{hmacProjectArgument, hmacAccessArgument, storageUserProjectArgument},
	Fields: []capability.Field{{Name: "projectId", Description: "Project of the key, as given"},
		{Name: "accessId", Description: "Access ID of the key, as given"},
		{Name: "deleted", Description: "True once Google deleted the key"}},
	Examples: []capability.Example{{
		Description: "Delete an inactive key",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","access_id":"EXAMPLEACCESSID1234"}`),
	}},
}

var notificationsDelete = capability.Descriptor{
	ID: notificationsDeleteID, Version: 1, Title: "Delete a notification configuration of a Cloud Storage bucket",
	Description: "Delete one Pub/Sub notification configuration of a bucket (notifications.delete of storage v1), " +
		"which stops the events of the bucket to its topic. The caller names the topic as notifications.list " +
		"shows it, only as a comparison: Qatlas reads the configuration first and refuses, without a change, one " +
		"whose topic is another. The topic is never changed, fetched, or called. Deleting cannot be undone, and no " +
		"tool creates a configuration. Google answers with an empty body, so the result is the bucket, " +
		"configuration, and topic as given, and deleted." + bucketBindingTail + storageChangeTail,
	Tags: []string{"google", "cloud", "storage", "notifications", "delete"}, Risk: storageChangeRisk(capability.EffectDelete),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + notificationProp + `,` +
		topicProp + `,` + storageUserProjectProp + `},"required":["bucket","notification_id","topic"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(notificationDeletedSchema),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "notification_id", Required: true, Description: "ID of the configuration as notifications.list " +
			"shows it, 1 to 64 letters, digits, hyphens, and underscores; never a path"},
		{Name: "topic", Required: true, Description: "Pub/Sub topic of the configuration as notifications.list " +
			"shows it, up to 256 characters. Only a comparison, never written: the deletion is refused if the " +
			"configuration has another topic"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "bucket", Description: "Bucket as given"},
		{Name: "notificationId", Description: "Configuration as given"},
		{Name: "topic", Description: "Topic as given, untrusted text that is never called"},
		{Name: "deleted", Description: "True once Google deleted the configuration"}},
	Examples: []capability.Example{{
		Description: "Delete a notification configuration",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","notification_id":"7",` +
			`"topic":"//pubsub.googleapis.com/projects/my-project-123/topics/events"}`),
	}},
}

type hmacRevokeArguments struct {
	ProjectID   string `json:"project_id"`
	AccessID    string `json:"access_id"`
	Etag        string `json:"etag"`
	UserProject string `json:"user_project"`
}

// hmacTarget checks the arguments, opens the session, binds the project, reads the key, and refuses a key that is
// not the one asked for or does not belong to the project, all before any change.
func hmacTarget(ctx context.Context, op, tool string, a hmacRevokeArguments, resolved *config.Resolved,
	secrets *secret.Resolver, red *redact.Redactor) (*storageSession, hmacWire, error) {
	var wire hmacWire
	if !hmacAccessIDPattern.MatchString(a.AccessID) {
		return nil, wire, badRequest("access_id must be 8 to 128 letters, digits, hyphens, and underscores, never a path")
	}
	if a.Etag != "" && !hmacEtagPattern.MatchString(a.Etag) {
		return nil, wire, badRequest("etag must be the etag of the key as hmac-keys.get shows it")
	}
	s, err := openStorage(ctx, op, tool, []namedProject{{"project_id", a.ProjectID}, {"user_project", a.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, wire, err
	}
	project, err := s.identify(ctx, a.ProjectID)
	if err != nil {
		return nil, wire, err
	}
	if err := s.c.get(ctx, op, tool, serviceStorage, hmacKeysPath(a.ProjectID)+"/"+url.PathEscape(a.AccessID),
		storageQuery(a.UserProject), &wire); err != nil {
		return nil, wire, err
	}
	if wire.AccessID != a.AccessID {
		return nil, wire, invalidRow(op)
	}
	if !project.owns(wire.ProjectID) {
		return nil, wire, refuseOutside("the key")
	}
	return s, wire, nil
}

func handleHMACKeysDeactivate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "deactivate HMAC key"
	var a hmacRevokeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	s, current, err := hmacTarget(ctx, op, hmacKeysDeactivateID, a, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if a.Etag != "" && current.Etag != a.Etag {
		return nil, badRequest("etag is not the etag of the key; read the key again, nothing was changed")
	}
	// The body is built from the fixed state and the checked etag alone: nothing of an answer or argument besides
	// them goes out.
	payload := struct {
		State string `json:"state"`
		Etag  string `json:"etag,omitempty"`
	}{State: hmacStateInactive, Etag: a.Etag}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, failure(op, "the request could not be built")
	}
	var changed hmacWire
	if err := s.c.send(ctx, op, hmacKeysDeactivateID, serviceStorage, http.MethodPut,
		hmacKeysPath(a.ProjectID)+"/"+url.PathEscape(a.AccessID), storageQuery(a.UserProject), body, &changed,
		true); err != nil {
		return nil, err
	}
	if changed.AccessID != a.AccessID {
		return nil, invalidRow(op)
	}
	return changed.result(), nil
}

func handleHMACKeysDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete HMAC key"
	var a hmacRevokeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	a.Etag = ""
	s, _, err := hmacTarget(ctx, op, hmacKeysDeleteID, a, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.c.send(ctx, op, hmacKeysDeleteID, serviceStorage, http.MethodDelete,
		hmacKeysPath(a.ProjectID)+"/"+url.PathEscape(a.AccessID), storageQuery(a.UserProject), nil, nil, true); err != nil {
		return nil, err
	}
	return map[string]any{"projectId": boundedText(a.ProjectID, maxNameText), "accessId": a.AccessID, "deleted": true}, nil
}

// sameTopic compares two Pub/Sub topics, the one the caller named and the one Google reported, in the long form
// Google reports and in the short form projects/P/topics/T.
func sameTopic(given, read string) bool {
	return given != "" && strings.TrimPrefix(given, pubsubTopicPrefix) == strings.TrimPrefix(read, pubsubTopicPrefix)
}

func checkTopic(topic string) error {
	if topic == "" || len(topic) > maxNotificationTopic || strings.IndexFunc(topic, unicode.IsControl) >= 0 {
		return badRequest("topic must be the Pub/Sub topic of the notification as notifications.list shows it, " +
			"up to 256 characters without control characters")
	}
	return nil
}

func handleNotificationsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete notification configuration"
	var a struct {
		Bucket         string `json:"bucket"`
		NotificationID string `json:"notification_id"`
		Topic          string `json:"topic"`
		UserProject    string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(a.Bucket); err != nil {
		return nil, err
	}
	if !notificationIDPattern.MatchString(a.NotificationID) {
		return nil, badRequest("notification_id must be 1 to 64 letters, digits, hyphens, and underscores, never a path")
	}
	if err := checkTopic(a.Topic); err != nil {
		return nil, err
	}
	s, err := openStorage(ctx, op, notificationsDeleteID, []namedProject{{"user_project", a.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, notificationsDeleteID, a.Bucket, a.UserProject); err != nil {
		return nil, err
	}
	path := bucketPath(a.Bucket) + "/notificationConfigs/" + url.PathEscape(a.NotificationID)
	var current notificationWire
	if err := s.c.get(ctx, op, notificationsDeleteID, serviceStorage, path, storageQuery(a.UserProject),
		&current); err != nil {
		return nil, err
	}
	if current.ID != a.NotificationID {
		return nil, invalidRow(op)
	}
	if !sameTopic(a.Topic, current.Topic) {
		return nil, badRequest("topic is not the topic of the notification; read the configuration again, " +
			"nothing was deleted")
	}
	if err := s.change(ctx, storageChange{op: op, tool: notificationsDeleteID, method: http.MethodDelete, path: path,
		bucket: a.Bucket, userProject: a.UserProject}, nil); err != nil {
		return nil, err
	}
	return map[string]any{"bucket": a.Bucket, "notificationId": a.NotificationID,
		"topic": boundedText(a.Topic, maxText), "deleted": true}, nil
}

// storageRevokeSummary phrases a revoking Cloud Storage change from its validated arguments alone.
func storageRevokeSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a struct {
			hmacRevokeArguments
			Bucket         string `json:"bucket"`
			NotificationID string `json:"notification_id"`
			Topic          string `json:"topic"`
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		key := "HMAC key " + strconv.Quote(boundedText(a.AccessID, maxNameText)) + " of project " +
			boundedText(a.ProjectID, maxNameText)
		switch tool {
		case hmacKeysDeactivateID:
			text := "Deactivate " + key + ": requests signed with it fail afterwards, and no tool activates it again"
			if a.Etag != "" {
				text += "; only if its etag is " + strconv.Quote(boundedText(a.Etag, maxNameText))
			}
			return text
		case hmacKeysDeleteID:
			return "Delete " + key + " for good: this cannot be undone, and Google refuses a key that is not INACTIVE"
		}
		return "Delete notification " + strconv.Quote(boundedText(a.NotificationID, maxNameText)) + " of bucket " +
			strconv.Quote(boundedText(a.Bucket, maxNameText)) + " for good: events stop going to the Pub/Sub topic " +
			strconv.Quote(boundedText(a.Topic, maxNotificationTopic)) + ", and the deletion is refused if the " +
			"notification has another topic"
	}
}

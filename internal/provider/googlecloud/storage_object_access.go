package googlecloud

import (
	"context"
	"encoding/json"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const objectAccessControlsListID = Provider + ".object-access-controls.list"

// objectACLMembers are the documented members of the ObjectAccessControl resource of storage v1.
var objectACLMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"selfLink", "string"}, {"bucket", "string"}, {"object", "string"},
	{"generation", "string"}, {"entity", "string"}, {"role", "string"}, {"email", "string"},
	{"entityId", "string"}, {"domain", "string"}, {"projectTeam", "object"}, {"etag", "string"},
}

var objectAccessControlsList = capability.Descriptor{
	ID: objectAccessControlsListID, Version: 1, Title: "List the access control list of a Cloud Storage object",
	Description: "List the access control entries of one object from Cloud Storage, each with its entity, role, " +
		"email, domain, entity ID, and project team. Entities are returned as Google sends them and nothing is " +
		"flagged or removed. A bucket with uniform bucket-level access refuses the call. Entries cannot be " +
		"changed. The method accepts only the scopes cloud-platform and devstorage.full_control, so the token may " +
		"change what its account may although the tool changes nothing." + bucketBindingTail + storageCostTail +
		storageTail,
	Tags: []string{"google", "cloud", "storage", "objects", "acl"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageObjectProp +
		`,"generation":` + storageGenProp + `,` + storageUserProjectProp + `},` +
		`"required":["bucket","object"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":` +
		objectACLSchema + `}},"required":["items"],"additionalProperties":false}`),
	Arguments: []capability.Argument{storageBucketArgument, storageObjectArgument, storageGenerationArgument,
		storageUserProjectArgument},
	Fields: []capability.Field{
		{Name: "items", Description: "Access control entries of the object, each {entity, role, email, domain, " +
			"entityId, projectTeam (projectNumber and team), id, kind, bucket, object, generation, selfLink, " +
			"etag}; entities such as user-EMAIL, group-EMAIL, allUsers, and allAuthenticatedUsers are personal " +
			"data and untrusted text and go out as Google sends them"},
	},
	Examples: []capability.Example{{
		Description: "List the access control entries of an object",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket","object":"reports/summary.txt"}`),
	}},
}

var objectACLSchema = storageMembersSchema(objectACLMembers, "entity")

func handleObjectAccessControlsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list object access controls"
	var arguments struct {
		Bucket      string `json:"bucket"`
		Object      string `json:"object"`
		Generation  string `json:"generation"`
		UserProject string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(arguments.Bucket); err != nil {
		return nil, err
	}
	if err := checkObjectName(arguments.Object); err != nil {
		return nil, err
	}
	if err := checkNumberArgument("generation", arguments.Generation); err != nil {
		return nil, err
	}
	s, err := openStorage(ctx, op, objectAccessControlsListID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, objectAccessControlsListID, arguments.Bucket, arguments.UserProject); err != nil {
		return nil, err
	}
	query := storageQuery(arguments.UserProject)
	if arguments.Generation != "" {
		query.Set("generation", arguments.Generation)
	}
	var answer bucketACLAnswer
	if err := s.c.get(ctx, op, objectAccessControlsListID, serviceStorage,
		objectPath(arguments.Bucket, arguments.Object)+"/acl", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxStorageACLEntries {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	items := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := boundedMembers(op, item, objectACLMembers)
		if err != nil {
			return nil, err
		}
		entity, _ := row["entity"].(string)
		if entity == "" || !objectOfBucket(row, arguments.Bucket) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		if object, has := row["object"].(string); has && object != arguments.Object {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		items = append(items, row)
	}
	return map[string]any{"items": items}, nil
}

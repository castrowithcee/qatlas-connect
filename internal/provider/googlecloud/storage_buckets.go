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

const (
	bucketsListID = Provider + ".buckets.list"
	bucketsGetID  = Provider + ".buckets.get"
)

// bucketMembers are the documented members of the Bucket resource of storage v1 (revision 20261111); none of
// them is marked deprecated.
var bucketMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"name", "string"}, {"selfLink", "string"}, {"projectNumber", "string"},
	{"metageneration", "string"}, {"generation", "string"}, {"location", "string"}, {"locationType", "string"},
	{"storageClass", "string"}, {"rpo", "string"}, {"timeCreated", "string"}, {"updated", "string"},
	{"softDeleteTime", "string"}, {"hardDeleteTime", "string"}, {"etag", "string"},
	{"defaultEventBasedHold", "boolean"}, {"satisfiesPZS", "boolean"}, {"satisfiesPZI", "boolean"},
	{"labels", "object"}, {"versioning", "object"}, {"lifecycle", "object"}, {"encryption", "object"},
	{"iamConfiguration", "object"}, {"retentionPolicy", "object"}, {"softDeletePolicy", "object"},
	{"autoclass", "object"}, {"website", "object"}, {"logging", "object"}, {"cors", "array"},
	{"hierarchicalNamespace", "object"}, {"billing", "object"}, {"customPlacementConfig", "object"},
	{"ipFilter", "object"}, {"objectRetention", "object"}, {"owner", "object"},
	{"acl", "array"}, {"defaultObjectAcl", "array"},
}

const bucketFields = "{name, id, kind, selfLink, projectNumber, metageneration, generation, location, " +
	"locationType, storageClass, rpo, timeCreated, updated, softDeleteTime, hardDeleteTime, etag, " +
	"defaultEventBasedHold, satisfiesPZS, satisfiesPZI, labels, versioning, lifecycle, encryption, " +
	"iamConfiguration, retentionPolicy, softDeletePolicy, autoclass, website, logging, cors, " +
	"hierarchicalNamespace, billing, customPlacementConfig, ipFilter, objectRetention, owner, and, with " +
	"projection full, acl and defaultObjectAcl}; untrusted text; labels, ACL entries, and lists are cut"

var bucketSchema = storageMembersSchema(bucketMembers, "name")

var bucketsList = capability.Descriptor{
	ID: bucketsListID, Version: 1, Title: "List the Cloud Storage buckets of a project",
	Description: "List the buckets of a project from Cloud Storage, optionally filtered by name prefix, with or " +
		"without their access control lists, with soft-deleted buckets, and with partial results accepted. Each " +
		"row has the documented members of the bucket: location, storage class, versioning, labels, lifecycle, " +
		"encryption, IAM configuration, retention and soft delete policies, autoclass, website, logging, CORS, " +
		"project number, and etag. Buckets cannot be created or changed. The project must lie in the targets of " +
		"the connection, and every row is checked again against them by its project number; rows outside the " +
		"targets are dropped, so a page can be empty while more follow." + storageTail,
	Tags: []string{"google", "cloud", "storage", "buckets"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"prefix":{"type":"string",` +
		`"minLength":1,"maxLength":63,"pattern":"^[a-z0-9._-]{1,63}$"},"max_results":{"type":"integer","minimum":1,` +
		`"maximum":1000},` + cursorProp + `,` + storageProjectionProp + `,"soft_deleted":{"type":"boolean"},` +
		`"return_partial_success":{"type":"boolean"},` + storageUserProjectProp + `},` +
		`"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"buckets":{"type":"array","items":` + bucketSchema +
		`},"unreachable":{"type":"array","items":{"type":"string"}},"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["buckets","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "prefix", Description: "Return only buckets whose name starts with it, 1 to 63 lowercase letters, " +
			"digits, hyphens, underscores, and dots"},
		{Name: "max_results", Description: "Buckets Google returns at most per page, 1 to 1000; Google may return " +
			"fewer, and Qatlas drops rows outside the targets. Without it the tool asks for 100. A page with the " +
			"full projection can exceed the size Qatlas accepts for an answer, which ends the call with an error"},
		computeCursorArgument, storageProjectionArgument,
		{Name: "soft_deleted", Description: "True to list soft-deleted buckets instead of live ones"},
		{Name: "return_partial_success", Description: "True to accept a partial list where a location cannot be " +
			"read; the answer then names what failed in unreachable"},
		storageUserProjectArgument},
	Fields: append([]capability.Field{
		{Name: "buckets", Description: "Buckets of the page, each " + bucketFields},
		{Name: "unreachable", Description: "Locations Google could not read, as Google sends them, present only " +
			"with return_partial_success; untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the buckets of a project whose name starts with logs-",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","prefix":"logs-"}`),
	}},
}

var bucketsGet = capability.Descriptor{
	ID: bucketsGetID, Version: 1, Title: "Get one Cloud Storage bucket",
	Description: "Read one bucket from Cloud Storage with the documented members of the bucket, with or without " +
		"its access control lists, optionally a soft-deleted generation, and only if its metageneration matches." +
		bucketBindingTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "buckets"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageProjectionProp +
		`,"if_metageneration_match":{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20},` +
		`"soft_deleted":{"type":"boolean"},"generation":{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20},` +
		storageUserProjectProp + `},"required":["bucket"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bucketSchema),
	Arguments: []capability.Argument{storageBucketArgument, storageProjectionArgument,
		{Name: "if_metageneration_match", Description: "Return the bucket only if its metageneration is this " +
			"number; otherwise Google refuses the request"},
		{Name: "soft_deleted", Description: "True to read a soft-deleted bucket, which needs generation"},
		{Name: "generation", Description: "Generation of the soft-deleted bucket to read, a decimal number; only " +
			"with soft_deleted"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "name", Description: "The bucket as one object: " + bucketFields}},
	Examples: []capability.Example{{
		Description: "Read a bucket with its access control lists",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket","projection":"full"}`),
	}},
}

// bucketRows decodes the rows of a bucket list and keeps those of a project in the targets.
func bucketRows(ctx context.Context, s *storageSession, items []json.RawMessage) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, err := boundedMembers(s.op, item, bucketMembers)
		if err != nil {
			return nil, err
		}
		if name, _ := row["name"].(string); name == "" {
			return nil, provider.InvalidResponse(s.op, subject+" returned an invalid response")
		}
		number, _ := row["projectNumber"].(string)
		admitted, err := s.admitNumber(ctx, number)
		if err != nil {
			return nil, err
		}
		if admitted {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

type bucketsAnswer struct {
	Items         []json.RawMessage `json:"items"`
	NextPageToken string            `json:"nextPageToken"`
	Unreachable   []string          `json:"unreachable"`
}

func handleBucketsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list buckets"
	var arguments struct {
		ProjectID            string `json:"project_id"`
		Prefix               string `json:"prefix"`
		MaxResults           *int   `json:"max_results"`
		Cursor               string `json:"cursor"`
		Projection           string `json:"projection"`
		SoftDeleted          bool   `json:"soft_deleted"`
		ReturnPartialSuccess bool   `json:"return_partial_success"`
		UserProject          string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	maxResults, err := storageMaxResults(arguments.MaxResults)
	if err != nil {
		return nil, err
	}
	switch {
	case arguments.Prefix != "" && !storagePrefixPattern.MatchString(arguments.Prefix):
		return nil, badRequest("prefix must be 1 to 63 lowercase letters, digits, hyphens, underscores, and dots")
	case arguments.Projection != "" && arguments.Projection != "full" && arguments.Projection != "noAcl":
		return nil, badRequest("projection must be full or noAcl")
	}
	binding := provider.CursorBinding(bucketsListID, provider.TargetsOf(resolved), arguments.ProjectID, arguments.Prefix,
		arguments.Projection, arguments.SoftDeleted, arguments.ReturnPartialSuccess, arguments.UserProject)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	s, err := openStorage(ctx, op, bucketsListID, []namedProject{{"project_id", arguments.ProjectID},
		{"user_project", arguments.UserProject}}, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := storageQuery(arguments.UserProject)
	query.Set("project", arguments.ProjectID)
	query.Set("maxResults", maxResults)
	if arguments.Prefix != "" {
		query.Set("prefix", arguments.Prefix)
	}
	if arguments.Projection != "" {
		query.Set("projection", arguments.Projection)
	}
	if arguments.SoftDeleted {
		query.Set("softDeleted", "true")
	}
	if arguments.ReturnPartialSuccess {
		query.Set("returnPartialSuccess", "true")
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var answer bucketsAnswer
	if err := s.c.get(ctx, op, bucketsListID, serviceStorage, storageBasePath+"/b", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxStorageResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows, err := bucketRows(ctx, s, answer.Items)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"buckets": rows}
	if unreachable := boundedList(answer.Unreachable); unreachable != nil {
		out["unreachable"] = unreachable
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleBucketsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get bucket"
	var arguments struct {
		Bucket                string `json:"bucket"`
		Projection            string `json:"projection"`
		IfMetagenerationMatch string `json:"if_metageneration_match"`
		SoftDeleted           bool   `json:"soft_deleted"`
		Generation            string `json:"generation"`
		UserProject           string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(arguments.Bucket); err != nil {
		return nil, err
	}
	for _, check := range []error{
		checkNumberArgument("if_metageneration_match", arguments.IfMetagenerationMatch),
		checkNumberArgument("generation", arguments.Generation),
	} {
		if check != nil {
			return nil, check
		}
	}
	switch {
	case arguments.Projection != "" && arguments.Projection != "full" && arguments.Projection != "noAcl":
		return nil, badRequest("projection must be full or noAcl")
	case arguments.Generation != "" && !arguments.SoftDeleted:
		return nil, badRequest("generation is an argument only with soft_deleted")
	}
	s, err := openStorage(ctx, op, bucketsGetID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := storageQuery(arguments.UserProject)
	if arguments.Projection != "" {
		query.Set("projection", arguments.Projection)
	}
	if arguments.IfMetagenerationMatch != "" {
		query.Set("ifMetagenerationMatch", arguments.IfMetagenerationMatch)
	}
	if arguments.SoftDeleted {
		query.Set("softDeleted", "true")
	}
	if arguments.Generation != "" {
		query.Set("generation", arguments.Generation)
	}
	var wire json.RawMessage
	if err := s.c.get(ctx, op, bucketsGetID, serviceStorage, bucketPath(arguments.Bucket), query, &wire); err != nil {
		return nil, err
	}
	row, err := boundedMembers(op, wire, bucketMembers)
	if err != nil {
		return nil, err
	}
	if name, _ := row["name"].(string); name != arguments.Bucket {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	number, _ := row["projectNumber"].(string)
	admitted, err := s.admitNumber(ctx, number)
	if err != nil {
		return nil, err
	}
	if !admitted {
		return nil, refuseOutside("the bucket")
	}
	return row, nil
}

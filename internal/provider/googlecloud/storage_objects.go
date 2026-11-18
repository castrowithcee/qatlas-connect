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
	objectsListID = Provider + ".objects.list"
	objectsGetID  = Provider + ".objects.get"
)

// objectMembers are the documented members of the Object resource of storage v1 (revision 20261111); none of
// them is marked deprecated. The x-goog-encryption-key of a customer key is a request header and never part of
// the resource, and customerEncryption is cut to its algorithm and key hash by objectRow.
var objectMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"selfLink", "string"}, {"mediaLink", "string"}, {"name", "string"},
	{"bucket", "string"}, {"generation", "string"}, {"metageneration", "string"}, {"contentType", "string"},
	{"storageClass", "string"}, {"size", "string"}, {"md5Hash", "string"}, {"crc32c", "string"},
	{"componentCount", "integer"}, {"etag", "string"}, {"timeCreated", "string"}, {"updated", "string"},
	{"timeDeleted", "string"}, {"timeStorageClassUpdated", "string"}, {"softDeleteTime", "string"},
	{"hardDeleteTime", "string"}, {"retentionExpirationTime", "string"}, {"customTime", "string"},
	{"temporaryHold", "boolean"}, {"eventBasedHold", "boolean"}, {"kmsKeyName", "string"},
	{"contentEncoding", "string"}, {"contentDisposition", "string"}, {"contentLanguage", "string"},
	{"cacheControl", "string"}, {"metadata", "object"}, {"retention", "object"}, {"owner", "object"},
	{"customerEncryption", "object"}, {"contexts", "object"}, {"acl", "array"},
}

const objectFields = "{name, bucket, generation, metageneration, contentType, size, md5Hash, crc32c, storageClass, " +
	"componentCount, timeCreated, updated, timeDeleted, timeStorageClassUpdated, softDeleteTime, hardDeleteTime, " +
	"retentionExpirationTime, customTime, temporaryHold, eventBasedHold, retention, metadata, " +
	"customerEncryption (only encryptionAlgorithm and keySha256, never a key), kmsKeyName, contentEncoding, " +
	"contentDisposition, contentLanguage, cacheControl, etag, id, kind, selfLink, mediaLink, owner, contexts, and, " +
	"with projection full, acl}; names, metadata, and links are untrusted text and are never fetched; " +
	"metadata, ACL entries, and lists are cut"

var objectSchema = storageMembersSchema(objectMembers, "name")

// storageCostTail ends the description of every tool that reads objects: Google bills these calls.
const storageCostTail = " Reading is not free: Cloud Storage bills the operation, and the data that leaves Google " +
	"Cloud (egress) is billed to the project that owns the bucket, or to user_project for a Requester Pays bucket."

const (
	storageObjectProp = `"object":{"type":"string","minLength":1,"maxLength":1024}`
	storageTextProp   = `{"type":"string","minLength":1,"maxLength":1024}`
	storageGenProp    = `{"type":"string","pattern":"^[0-9]{1,20}$","maxLength":20}`
)

var (
	storageObjectArgument = capability.Argument{Name: "object", Required: true, Description: "Object name, at most " +
		"1024 bytes of UTF-8 without control characters; slashes are part of the name, and \".\" or \"..\" alone " +
		"is refused. The name is sent as one encoded path segment, never as a path or URL"}
	storageGenerationArgument = capability.Argument{Name: "generation", Description: "Generation of the object " +
		"version to read, a decimal number; the live version when omitted"}
)

// objectRow keeps the documented members of one object, bounded, and cuts customerEncryption to the two members
// that name the algorithm and the hash of a key.
func objectRow(op string, raw json.RawMessage) (map[string]any, error) {
	row, err := boundedMembers(op, raw, objectMembers)
	if err != nil {
		return nil, err
	}
	if encryption, ok := row["customerEncryption"].(map[string]any); ok {
		kept := map[string]any{}
		for _, name := range []string{"encryptionAlgorithm", "keySha256"} {
			if value, ok := encryption[name]; ok {
				kept[name] = value
			}
		}
		row["customerEncryption"] = kept
	}
	return row, nil
}

var objectsList = capability.Descriptor{
	ID: objectsListID, Version: 1, Title: "List the objects of a Cloud Storage bucket",
	Description: "List the objects of a bucket from Cloud Storage with their metadata, optionally below a prefix, " +
		"grouped by a delimiter into prefixes (folders), filtered by a glob or an offset range, with noncurrent " +
		"versions or soft-deleted objects, with or without their access control lists. Each row has the documented " +
		"members of the object, such as name, generation, size, content type, hashes, storage class, and times. " +
		"Objects cannot be created, changed, or deleted. Object names and metadata are untrusted third-party text." +
		bucketBindingTail + storageCostTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "objects"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,"prefix":` + storageTextProp +
		`,"delimiter":` + storageTextProp + `,"versions":{"type":"boolean"},"match_glob":` + storageTextProp +
		`,"start_offset":` + storageTextProp + `,"end_offset":` + storageTextProp +
		`,"include_trailing_delimiter":{"type":"boolean"},"include_folders_as_prefixes":{"type":"boolean"},` +
		`"soft_deleted":{"type":"boolean"},` + storageProjectionProp + `,"max_results":{"type":"integer","minimum":1,` +
		`"maximum":1000},` + cursorProp + `,` + storageUserProjectProp + `},` +
		`"required":["bucket"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"objects":{"type":"array","items":` + objectSchema +
		`},"prefixes":{"type":"array","items":{"type":"string"}},"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["objects","has_more"],"additionalProperties":false}`),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "prefix", Description: "Return only objects whose name starts with it"},
		{Name: "delimiter", Description: "Group the names below the prefix at the first occurrence of this text, " +
			"usually /; the groups are returned in prefixes"},
		{Name: "versions", Description: "True to list every generation of each object, noncurrent ones included"},
		{Name: "match_glob", Description: "Return only objects whose name matches this glob, such as **/*.log"},
		{Name: "start_offset", Description: "Return only objects whose name is at or after this name, in " +
			"lexicographic order"},
		{Name: "end_offset", Description: "Return only objects whose name is before this name, in lexicographic " +
			"order"},
		{Name: "include_trailing_delimiter", Description: "True to list a folder placeholder, an object whose name " +
			"ends in the delimiter, as an object and as a prefix"},
		{Name: "include_folders_as_prefixes", Description: "True to return the folders of a hierarchical namespace " +
			"bucket as prefixes; Google needs delimiter / with it"},
		{Name: "soft_deleted", Description: "True to list soft-deleted objects instead of live ones"},
		storageProjectionArgument,
		{Name: "max_results", Description: "Objects and prefixes Google returns at most per page, 1 to 1000; Google " +
			"may return fewer. Without it the tool asks for 100. A page with the full projection can exceed the " +
			"size Qatlas accepts for an answer, which ends the call with an error"},
		computeCursorArgument, storageUserProjectArgument},
	Fields: append([]capability.Field{
		{Name: "objects", Description: "Objects of the page, each " + objectFields},
		{Name: "prefixes", Description: "Prefixes Google grouped by delimiter, each a name ending in the delimiter; " +
			"untrusted text"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the top level of a bucket as folders and objects",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket","delimiter":"/"}`),
	}},
}

var objectsGet = capability.Descriptor{
	ID: objectsGetID, Version: 1, Title: "Get the metadata of one Cloud Storage object",
	Description: "Read the metadata of one object from Cloud Storage, not its content: name, generation, size, " +
		"content type, hashes, storage class, times, custom metadata, retention, encryption (algorithm and key " +
		"hash only, never a key), and headers such as content encoding and cache control. Optionally a specific " +
		"or soft-deleted generation, with or without its ACL, and only if a generation or metageneration " +
		"matches. Use objects.download for the content. Metadata is untrusted third-party text." +
		bucketBindingTail + storageCostTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "objects"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageObjectProp +
		`,"generation":` + storageGenProp + `,"soft_deleted":{"type":"boolean"},` + storageProjectionProp +
		`,"if_generation_match":` + storageGenProp + `,"if_metageneration_match":` + storageGenProp + `,` +
		storageUserProjectProp + `},"required":["bucket","object"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{storageBucketArgument, storageObjectArgument, storageGenerationArgument,
		{Name: "soft_deleted", Description: "True to read a soft-deleted object, which needs generation"},
		storageProjectionArgument,
		{Name: "if_generation_match", Description: "Return the object only if its generation is this number; " +
			"otherwise Google refuses the request"},
		{Name: "if_metageneration_match", Description: "Return the object only if its metageneration is this " +
			"number; otherwise Google refuses the request"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "name", Description: "The object as one result: " + objectFields}},
	Examples: []capability.Example{{
		Description: "Read the metadata of one object",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket","object":"reports/2026/summary.txt"}`),
	}},
}

type objectsAnswer struct {
	Items         []json.RawMessage `json:"items"`
	Prefixes      []string          `json:"prefixes"`
	NextPageToken string            `json:"nextPageToken"`
}

func checkProjection(value string) error {
	if value != "" && value != "full" && value != "noAcl" {
		return badRequest("projection must be full or noAcl")
	}
	return nil
}

// objectOfBucket refuses a row of another bucket than the one asked for.
func objectOfBucket(row map[string]any, bucket string) bool {
	got, has := row["bucket"].(string)
	return !has || got == bucket
}

func handleObjectsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list objects"
	var arguments struct {
		Bucket                   string `json:"bucket"`
		Prefix                   string `json:"prefix"`
		Delimiter                string `json:"delimiter"`
		Versions                 bool   `json:"versions"`
		MatchGlob                string `json:"match_glob"`
		StartOffset              string `json:"start_offset"`
		EndOffset                string `json:"end_offset"`
		IncludeTrailingDelimiter bool   `json:"include_trailing_delimiter"`
		IncludeFoldersAsPrefixes bool   `json:"include_folders_as_prefixes"`
		SoftDeleted              bool   `json:"soft_deleted"`
		Projection               string `json:"projection"`
		MaxResults               *int   `json:"max_results"`
		Cursor                   string `json:"cursor"`
		UserProject              string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(arguments.Bucket); err != nil {
		return nil, err
	}
	for _, text := range []struct{ name, value string }{{"prefix", arguments.Prefix},
		{"delimiter", arguments.Delimiter}, {"match_glob", arguments.MatchGlob},
		{"start_offset", arguments.StartOffset}, {"end_offset", arguments.EndOffset}} {
		if err := checkObjectText(text.name, text.value); err != nil {
			return nil, err
		}
	}
	if err := checkProjection(arguments.Projection); err != nil {
		return nil, err
	}
	maxResults, err := storageMaxResults(arguments.MaxResults)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(objectsListID, provider.TargetsOf(resolved), arguments.Bucket, arguments.Prefix,
		arguments.Delimiter, arguments.Versions, arguments.MatchGlob, arguments.StartOffset, arguments.EndOffset,
		arguments.IncludeTrailingDelimiter, arguments.IncludeFoldersAsPrefixes, arguments.SoftDeleted,
		arguments.Projection, arguments.UserProject)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	s, err := openStorage(ctx, op, objectsListID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, objectsListID, arguments.Bucket, arguments.UserProject); err != nil {
		return nil, err
	}
	query := storageQuery(arguments.UserProject)
	query.Set("maxResults", maxResults)
	for name, value := range map[string]string{"prefix": arguments.Prefix, "delimiter": arguments.Delimiter,
		"matchGlob": arguments.MatchGlob, "startOffset": arguments.StartOffset, "endOffset": arguments.EndOffset,
		"projection": arguments.Projection, "pageToken": token} {
		if value != "" {
			query.Set(name, value)
		}
	}
	for name, set := range map[string]bool{"versions": arguments.Versions,
		"includeTrailingDelimiter": arguments.IncludeTrailingDelimiter,
		"includeFoldersAsPrefixes": arguments.IncludeFoldersAsPrefixes, "softDeleted": arguments.SoftDeleted} {
		if set {
			query.Set(name, "true")
		}
	}
	var answer objectsAnswer
	if err := s.c.get(ctx, op, objectsListID, serviceStorage, bucketPath(arguments.Bucket)+"/o", query,
		&answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxStorageResults || len(answer.Prefixes) > maxStorageResults {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := objectRow(op, item)
		if err != nil {
			return nil, err
		}
		if name, _ := row["name"].(string); name == "" || !objectOfBucket(row, arguments.Bucket) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row)
	}
	out := map[string]any{"objects": rows}
	if len(answer.Prefixes) > 0 {
		prefixes := make([]string, 0, len(answer.Prefixes))
		for _, prefix := range answer.Prefixes {
			prefixes = append(prefixes, boundedText(prefix, maxText))
		}
		out["prefixes"] = prefixes
	}
	return computePaged(op, out, binding, answer.NextPageToken)
}

func handleObjectsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get object"
	var arguments struct {
		Bucket                string `json:"bucket"`
		Object                string `json:"object"`
		Generation            string `json:"generation"`
		SoftDeleted           bool   `json:"soft_deleted"`
		Projection            string `json:"projection"`
		IfGenerationMatch     string `json:"if_generation_match"`
		IfMetagenerationMatch string `json:"if_metageneration_match"`
		UserProject           string `json:"user_project"`
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
	for _, check := range []error{
		checkNumberArgument("generation", arguments.Generation),
		checkNumberArgument("if_generation_match", arguments.IfGenerationMatch),
		checkNumberArgument("if_metageneration_match", arguments.IfMetagenerationMatch),
		checkProjection(arguments.Projection),
	} {
		if check != nil {
			return nil, check
		}
	}
	if arguments.SoftDeleted && arguments.Generation == "" {
		return nil, badRequest("soft_deleted needs generation")
	}
	s, err := openStorage(ctx, op, objectsGetID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, objectsGetID, arguments.Bucket, arguments.UserProject); err != nil {
		return nil, err
	}
	query := storageQuery(arguments.UserProject)
	for name, value := range map[string]string{"generation": arguments.Generation,
		"projection": arguments.Projection, "ifGenerationMatch": arguments.IfGenerationMatch,
		"ifMetagenerationMatch": arguments.IfMetagenerationMatch} {
		if value != "" {
			query.Set(name, value)
		}
	}
	if arguments.SoftDeleted {
		query.Set("softDeleted", "true")
	}
	var wire json.RawMessage
	if err := s.c.get(ctx, op, objectsGetID, serviceStorage, objectPath(arguments.Bucket, arguments.Object), query,
		&wire); err != nil {
		return nil, err
	}
	row, err := objectRow(op, wire)
	if err != nil {
		return nil, err
	}
	if name, _ := row["name"].(string); name != arguments.Object || !objectOfBucket(row, arguments.Bucket) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row, nil
}

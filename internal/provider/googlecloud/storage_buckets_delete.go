package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const bucketsDeleteID = Provider + ".buckets.delete"

const storageProjectProp = `"project":{"type":"string","minLength":1,"maxLength":30,` +
	`"pattern":"^([0-9]{1,20}|[a-z][a-z0-9-]{4,28}[a-z0-9])$"}`

const bucketDeletedSchema = `{"type":"object","properties":{"bucket":{"type":"string"},` +
	`"deleted":{"type":"boolean"}},"required":["bucket","deleted"],"additionalProperties":false}`

var bucketsDelete = capability.Descriptor{
	ID: bucketsDeleteID, Version: 1, Title: "Delete a Cloud Storage bucket",
	Description: "Delete one empty bucket from Cloud Storage (buckets.delete of storage v1). project is required: " +
		"the project the person confirms the bucket belongs to. It is only compared, never written: Qatlas reads the " +
		"bucket and refuses the deletion, without a change, unless the bucket belongs to that project, and the " +
		"project must lie in the targets of the connection. A project ID is resolved by reading the project, which " +
		"needs projects.get. The name becomes free " +
		"for anyone to take, and settings, labels, and policies of the bucket are lost. The bucket must be empty, " +
		"or Google refuses the call and the tool never deletes objects. Offered only where a connection's tools " +
		"list names it. Google answers with an empty body, so the result is the bucket as given and deleted." +
		bucketBindingTail + storageChangeTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "buckets", "delete"}, Risk: storageChangeRisk(capability.EffectDelete),
	Provider: Provider, RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageProjectProp + `,` +
		storageIfMetagenerationMatchProp + `,` + storageIfMetagenerationNotMatchProp + `,` + storageUserProjectProp +
		`},"required":["bucket","project"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bucketDeletedSchema),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "project", Required: true, Description: "Project ID or number the bucket belongs to, as the person " +
			"confirms it. Only compared with the project of the bucket, never written; it must lie in the targets"},
		{Name: "if_metageneration_match", Description: "Delete only if the metageneration of the bucket is this " +
			"number; otherwise Google refuses the request"},
		{Name: "if_metageneration_not_match", Description: "Delete only if the metageneration of the bucket is " +
			"not this number"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "bucket", Description: "Bucket as given"},
		{Name: "deleted", Description: "True once Google deleted the bucket"}},
	Examples: []capability.Example{{Description: "Delete an empty bucket",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","project":"example-project-123"}`)}},
}

func handleBucketsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete bucket"
	var a struct {
		Bucket                   string `json:"bucket"`
		Project                  string `json:"project"`
		IfMetagenerationMatch    string `json:"if_metageneration_match"`
		IfMetagenerationNotMatch string `json:"if_metageneration_not_match"`
		UserProject              string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(a.Bucket); err != nil {
		return nil, err
	}
	if a.Project == "" {
		return nil, badRequest("project is required")
	}
	for _, check := range []error{checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch),
		checkNumberArgument("if_metageneration_not_match", a.IfMetagenerationNotMatch)} {
		if check != nil {
			return nil, check
		}
	}
	s, err := openStorage(ctx, op, bucketsDeleteID, []namedProject{{"project", a.Project}, {"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	setQueryValues(query, map[string]string{"ifMetagenerationMatch": a.IfMetagenerationMatch,
		"ifMetagenerationNotMatch": a.IfMetagenerationNotMatch})
	if err := s.change(ctx, storageChange{op: op, tool: bucketsDeleteID, method: http.MethodDelete,
		path: bucketPath(a.Bucket), bucket: a.Bucket, userProject: a.UserProject, project: a.Project, query: query}, nil); err != nil {
		return nil, err
	}
	return map[string]any{"bucket": a.Bucket, "deleted": true}, nil
}

// bucketDeleteSummary phrases the deletion of a bucket from its validated arguments. It names the project the
// person confirms; the handler refuses the call unless the bucket belongs to it.
func bucketDeleteSummary(raw json.RawMessage) string {
	var a struct {
		Bucket  string `json:"bucket"`
		Project string `json:"project"`
	}
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Delete bucket " + strconv.Quote(boundedText(a.Bucket, maxNameText)) + " of project " +
		boundedText(a.Project, maxNameText) + ", which must be empty: its name becomes free for others, and its " +
		"settings are lost. The deletion is refused if the bucket belongs to another project"
}

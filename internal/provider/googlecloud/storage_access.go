package googlecloud

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	bucketsGetIAMPolicyID      = Provider + ".buckets.get-iam-policy"
	bucketAccessControlsListID = Provider + ".bucket-access-controls.list"
)

// bucketPolicyWire is the Policy of storage v1 as Google sends it.
type bucketPolicyWire struct {
	Kind       string        `json:"kind"`
	ResourceID string        `json:"resourceId"`
	Version    *int          `json:"version"`
	Etag       string        `json:"etag"`
	Bindings   []bindingWire `json:"bindings"`
}

type bucketPolicyResult struct {
	Kind       string          `json:"kind,omitempty"`
	ResourceID string          `json:"resourceId,omitempty"`
	Version    *int            `json:"version,omitempty"`
	Etag       string          `json:"etag,omitempty"`
	Bindings   []bindingResult `json:"bindings"`
	Truncated  bool            `json:"truncated,omitempty"`
}

// result bounds the policy like the IAM policies of the other tools: members go out as Google sends them, only
// the number and the length of what Google returns are cut, and a cut sets truncated.
func (p bucketPolicyWire) result() bucketPolicyResult {
	var cut cutter
	out := bucketPolicyResult{Kind: cut.text(p.Kind, maxNameText), ResourceID: cut.text(p.ResourceID, maxNameText),
		Version: p.Version, Etag: cut.text(p.Etag, maxNameText), Bindings: []bindingResult{}}
	if len(p.Bindings) > maxIAMBindings {
		cut.cut = true
	}
	for _, b := range p.Bindings[:min(len(p.Bindings), maxIAMBindings)] {
		members := cut.list(b.Members, maxIAMList)
		if members == nil {
			members = []string{}
		}
		out.Bindings = append(out.Bindings, bindingResult{Role: cut.text(b.Role, maxNameText), Members: members,
			Condition: cut.expr(b.Condition)})
	}
	out.Truncated = cut.cut
	return out
}

const bucketPolicySchema = `{"type":"object","properties":{"kind":{"type":"string"},"resourceId":{"type":"string"},` +
	`"version":{"type":"integer"},"etag":{"type":"string"},"bindings":{"type":"array","items":{"type":"object",` +
	`"properties":{"role":{"type":"string"},"members":` + stringArraySchema + `,"condition":` + exprSchema + `},` +
	`"required":["role","members"],"additionalProperties":false}},"truncated":{"type":"boolean"}},` +
	`"required":["bindings"],"additionalProperties":false}`

var bucketsGetIAMPolicy = capability.Descriptor{
	ID: bucketsGetIAMPolicyID, Version: 1, Title: "Get the IAM policy of a Cloud Storage bucket",
	Description: "Read the IAM policy of a bucket from Cloud Storage: its version, etag, and bindings, each with a " +
		"role, its members, and an optional condition. Members such as user:, group:, serviceAccount:, domain:, " +
		"allUsers, and allAuthenticatedUsers are personal data and untrusted third-party text; they are returned " +
		"as Google sends them, and nothing is flagged or removed. Lists and strings are cut. The policy cannot be " +
		"changed. The method accepts only the scopes cloud-platform and devstorage.full_control, so the token " +
		"may change what its account may although the tool changes nothing." + bucketBindingTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "buckets", "iam"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` +
		`"options_requested_policy_version":{"type":"integer","enum":[1,3]},` + storageUserProjectProp + `},` +
		`"required":["bucket"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(bucketPolicySchema),
	Arguments: []capability.Argument{storageBucketArgument,
		{Name: "options_requested_policy_version", Description: "Highest policy version Google may format the " +
			"policy in: 1 or 3. The default is 3, which shows the conditions of bindings; a policy with " +
			"conditions cannot be read faithfully with version 1"},
		storageUserProjectArgument},
	Fields: []capability.Field{
		{Name: "kind", Description: "storage#policy"},
		{Name: "resourceId", Description: "Identifier of the bucket Google reports, projects/_/buckets/NAME"},
		{Name: "version", Description: "Format version of the policy; 3 when it has conditional bindings"},
		{Name: "etag", Description: "Entity tag of the policy, base64"},
		{Name: "bindings", Description: "Bindings of the policy, each {role, members, condition}; members are " +
			"identities such as user:, group:, serviceAccount:, domain:, projectOwner:, allUsers, and " +
			"allAuthenticatedUsers; condition is {expression, title, description, location} and absent for an " +
			"unconditional binding"},
		truncatedField,
	},
	Examples: []capability.Example{{
		Description: "Read the policy of a bucket with its conditions",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket"}`),
	}},
}

// bucketACLMembers are the documented members of the BucketAccessControl resource of storage v1.
var bucketACLMembers = []computeMember{
	{"kind", "string"}, {"id", "string"}, {"selfLink", "string"}, {"bucket", "string"}, {"entity", "string"},
	{"role", "string"}, {"email", "string"}, {"entityId", "string"}, {"domain", "string"},
	{"projectTeam", "object"}, {"etag", "string"},
}

const bucketACLFields = "{entity, role, email, domain, entityId, projectTeam (projectNumber and team), id, " +
	"kind, bucket, selfLink, etag}; entities such as user-EMAIL, group-EMAIL, allUsers, and allAuthenticatedUsers " +
	"are personal data and untrusted text and go out as Google sends them"

var bucketAccessControlsList = capability.Descriptor{
	ID: bucketAccessControlsListID, Version: 1, Title: "List the access control list of a Cloud Storage bucket",
	Description: "List the access control entries of a bucket from Cloud Storage, each with its entity, role, " +
		"email, domain, entity ID, and project team. Entities are returned as Google sends them and nothing is " +
		"flagged or removed. A bucket with uniform bucket-level access refuses the call. Entries cannot be changed. " +
		"The method accepts only the scopes cloud-platform and devstorage.full_control, so the token may change " +
		"what its account may although the tool changes nothing." + bucketBindingTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "buckets", "acl"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` +
		storageUserProjectProp + `},"required":["bucket"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"items":{"type":"array","items":` +
		bucketACLSchema + `}},"required":["items"],"additionalProperties":false}`),
	Arguments: []capability.Argument{storageBucketArgument, storageUserProjectArgument},
	Fields: []capability.Field{
		{Name: "items", Description: "Access control entries of the bucket, each " + bucketACLFields},
	},
	Examples: []capability.Example{{
		Description: "List the access control entries of a bucket",
		Arguments:   json.RawMessage(`{"bucket":"example-bucket"}`),
	}},
}

var bucketACLSchema = storageMembersSchema(bucketACLMembers, "entity")

func handleBucketsGetIAMPolicy(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get bucket iam policy"
	var arguments struct {
		Bucket      string `json:"bucket"`
		Version     *int   `json:"options_requested_policy_version"`
		UserProject string `json:"user_project"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := checkBucket(arguments.Bucket); err != nil {
		return nil, err
	}
	version := 3
	if arguments.Version != nil {
		version = *arguments.Version
	}
	if version != 1 && version != 3 {
		return nil, badRequest("options_requested_policy_version must be 1 or 3")
	}
	s, err := openStorage(ctx, op, bucketsGetIAMPolicyID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, bucketsGetIAMPolicyID, arguments.Bucket, arguments.UserProject); err != nil {
		return nil, err
	}
	query := storageQuery(arguments.UserProject)
	query.Set("optionsRequestedPolicyVersion", strconv.Itoa(version))
	var wire bucketPolicyWire
	if err := s.c.get(ctx, op, bucketsGetIAMPolicyID, serviceStorage, bucketPath(arguments.Bucket)+"/iam", query,
		&wire); err != nil {
		return nil, err
	}
	return wire.result(), nil
}

type bucketACLAnswer struct {
	Items []json.RawMessage `json:"items"`
}

func handleBucketAccessControlsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list bucket access controls"
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
	s, err := openStorage(ctx, op, bucketAccessControlsListID, []namedProject{{"user_project", arguments.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := s.bindBucket(ctx, bucketAccessControlsListID, arguments.Bucket, arguments.UserProject); err != nil {
		return nil, err
	}
	var answer bucketACLAnswer
	if err := s.c.get(ctx, op, bucketAccessControlsListID, serviceStorage, bucketPath(arguments.Bucket)+"/acl",
		storageQuery(arguments.UserProject), &answer); err != nil {
		return nil, err
	}
	if len(answer.Items) > maxStorageACLEntries {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	items := make([]map[string]any, 0, len(answer.Items))
	for _, item := range answer.Items {
		row, err := boundedMembers(op, item, bucketACLMembers)
		if err != nil {
			return nil, err
		}
		entity, _ := row["entity"].(string)
		bucket, hasBucket := row["bucket"].(string)
		if entity == "" || hasBucket && bucket != arguments.Bucket {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		items = append(items, row)
	}
	return map[string]any{"items": items}, nil
}

// maxStorageACLEntries is the number of entries Google documents a bucket ACL may hold.
const maxStorageACLEntries = 100

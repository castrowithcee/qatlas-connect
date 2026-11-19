package googlecloud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	objectsDeleteID  = Provider + ".objects.delete"
	objectsRestoreID = Provider + ".objects.restore"
)

// The parameters of objects.delete and objects.restore are those of discovery storage v1 (revision 20261111).
// objects.restore has one more, copySourceAcl, which is no argument: it is never sent, so the access control list
// of the source does not come back with the object.

var (
	objectDeletedSchema = `{"type":"object","properties":{"bucket":{"type":"string"},"object":{"type":"string"},` +
		`"generation":{"type":"string"},"deleted":{"type":"boolean"}},"required":["bucket","object","deleted"],` +
		`"additionalProperties":false}`

	storagePreconditionArguments = []capability.Argument{
		{Name: "if_generation_match", Description: "Proceed only if the generation of the live object is this " +
			"number; otherwise Google refuses the request with a precondition failure"},
		{Name: "if_generation_not_match", Description: "Proceed only if the generation of the live object is not " +
			"this number"},
		{Name: "if_metageneration_match", Description: "Proceed only if the metageneration of the live object is " +
			"this number"},
		{Name: "if_metageneration_not_match", Description: "Proceed only if the metageneration of the live object " +
			"is not this number"},
	}
)

const objectPreconditionProps = `"if_generation_match":` + storageGenProp + `,` + storageIfGenerationNotMatchProp +
	`,` + storageIfMetagenerationMatchProp + `,` + storageIfMetagenerationNotMatchProp

var objectsDelete = capability.Descriptor{
	ID: objectsDeleteID, Version: 1, Title: "Delete one Cloud Storage object",
	Description: "Delete one object from a bucket of Cloud Storage (objects.delete of storage v1). Without " +
		"generation the live version is deleted: without versioning or soft delete that cannot be undone, and with " +
		"versioning the live version stays as a noncurrent version; with a bucket's soft delete policy the object " +
		"can be restored with objects.restore until its hard delete time. With generation exactly that version is " +
		"deleted for good. One call deletes exactly one object; there is no bulk deletion. The object name is " +
		"untrusted and is sent as one encoded path segment. Google answers with an empty body, so the result is " +
		"the bucket, the object, and the generation as given, and deleted." + listedOnlyNote + bucketBindingTail +
		storageChangeTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "objects", "delete"}, Risk: storageChangeRisk(capability.EffectDelete),
	Provider: Provider, RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageObjectProp +
		`,"generation":` + storageGenProp + `,` + objectPreconditionProps + `,` + storageUserProjectProp + `},` +
		`"required":["bucket","object"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectDeletedSchema),
	Arguments: append(append([]capability.Argument{storageBucketArgument, storageObjectArgument,
		{Name: "generation", Description: "Generation of the one version to delete for good, a decimal number; " +
			"the live version when omitted"}}, storagePreconditionArguments...), storageUserProjectArgument),
	Fields: []capability.Field{{Name: "bucket", Description: "Bucket as given"},
		{Name: "object", Description: "Object name as given, cut; untrusted text"},
		{Name: "generation", Description: "Generation as given; absent when the live version was deleted"},
		{Name: "deleted", Description: "True once Google deleted the object or version"}},
	Examples: []capability.Example{{Description: "Delete the live version of one object",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","object":"reports/2026/summary.txt"}`)}},
}

var objectsRestore = capability.Descriptor{
	ID: objectsRestoreID, Version: 1, Title: "Restore a soft-deleted Cloud Storage object",
	Description: "Restore a soft-deleted object of a bucket of Cloud Storage (objects.restore of storage v1) " +
		"from its generation; the generation is required and comes from objects.list or objects.get with " +
		"soft_deleted. The restored object gets a new generation. The access control list of the soft-deleted " +
		"object is not copied back (copySourceAcl is never sent), so no old sharing returns. A live object of the " +
		"same name is never replaced: the request always carries ifGenerationMatch=0, so Google refuses it with a " +
		"precondition failure (412) while a live object of that name exists, and there is no overwrite switch " +
		"and no precondition argument. The result is the restored object as Google describes it, untrusted third-party text." +
		bucketBindingTail + storageChangeTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "objects", "restore"}, Risk: storageChangeRisk(capability.EffectUpdate),
	Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageObjectProp +
		`,"generation":` + storageGenProp + `,` + storageProjectionProp +
		`,"restore_token":` + storageTextProp + `,` + storageUserProjectProp + `},` +
		`"required":["bucket","object","generation"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{storageBucketArgument, storageObjectArgument,
		{Name: "generation", Required: true, Description: "Generation of the soft-deleted object to restore, a " +
			"decimal number"},
		storageProjectionArgument,
		{Name: "restore_token", Description: "Restore token of a soft-deleted object in a bucket with " +
			"hierarchical namespace, which Google needs to tell apart objects of the same name; read it from " +
			"objects.get with soft_deleted"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "name", Description: "The restored object as one result: " + objectFields}},
	Examples: []capability.Example{{Description: "Restore one soft-deleted object", Arguments: json.RawMessage(
		`{"bucket":"example-bucket","object":"reports/2026/summary.txt","generation":"1700000000000000"}`)}},
}

// objectChangeArguments are the arguments of both tools; each reads its own members.
type objectChangeArguments struct {
	Bucket                   string `json:"bucket"`
	Object                   string `json:"object"`
	Generation               string `json:"generation"`
	IfGenerationMatch        string `json:"if_generation_match"`
	IfGenerationNotMatch     string `json:"if_generation_not_match"`
	IfMetagenerationMatch    string `json:"if_metageneration_match"`
	IfMetagenerationNotMatch string `json:"if_metageneration_not_match"`
	Projection               string `json:"projection"`
	RestoreToken             string `json:"restore_token"`
	UserProject              string `json:"user_project"`
}

// check refuses what is not in its form, before any secret or request.
func (a objectChangeArguments) check() error {
	if err := checkBucket(a.Bucket); err != nil {
		return err
	}
	if err := checkObjectName(a.Object); err != nil {
		return err
	}
	for _, check := range []error{
		checkNumberArgument("generation", a.Generation),
		checkNumberArgument("if_generation_match", a.IfGenerationMatch),
		checkNumberArgument("if_generation_not_match", a.IfGenerationNotMatch),
		checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch),
		checkNumberArgument("if_metageneration_not_match", a.IfMetagenerationNotMatch),
		checkProjection(a.Projection),
	} {
		if check != nil {
			return check
		}
	}
	if a.RestoreToken != "" {
		return checkObjectText("restore_token", a.RestoreToken)
	}
	return nil
}

func nonEmptyValues(values map[string]string) []string {
	var out []string
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func (a objectChangeArguments) preconditions() map[string]string {
	return map[string]string{"ifGenerationMatch": a.IfGenerationMatch, "ifGenerationNotMatch": a.IfGenerationNotMatch,
		"ifMetagenerationMatch": a.IfMetagenerationMatch, "ifMetagenerationNotMatch": a.IfMetagenerationNotMatch}
}

func handleObjectsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete object"
	var a objectChangeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	s, err := openStorage(ctx, op, objectsDeleteID, []namedProject{{"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	setQueryValues(query, a.preconditions())
	setQueryValues(query, map[string]string{"generation": a.Generation})
	if err := s.change(ctx, storageChange{op: op, tool: objectsDeleteID, method: http.MethodDelete,
		path: objectPath(a.Bucket, a.Object), bucket: a.Bucket, userProject: a.UserProject, query: query}, nil); err != nil {
		return nil, err
	}
	out := map[string]any{"bucket": a.Bucket, "object": boundedText(a.Object, maxText), "deleted": true}
	if a.Generation != "" {
		out["generation"] = a.Generation
	}
	return out, nil
}

func handleObjectsRestore(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "restore object"
	var a objectChangeArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	if a.Generation == "" {
		return nil, badRequest("generation is required")
	}
	if len(nonEmptyValues(a.preconditions())) > 0 {
		return nil, badRequest("objects.restore takes no precondition: it never replaces a live object")
	}
	s, err := openStorage(ctx, op, objectsRestoreID, []namedProject{{"user_project", a.UserProject}}, resolved,
		secrets, red)
	if err != nil {
		return nil, err
	}
	// copySourceAcl is never sent, and ifGenerationMatch=0 lets the restore succeed only if no live object of the
	// name exists.
	query := url.Values{}
	setQueryValues(query, map[string]string{"generation": a.Generation, "projection": a.Projection,
		"restoreToken": a.RestoreToken, "ifGenerationMatch": "0"})
	var wire json.RawMessage
	if err := s.change(ctx, storageChange{op: op, tool: objectsRestoreID, method: http.MethodPost,
		path: objectPath(a.Bucket, a.Object) + "/restore", bucket: a.Bucket, userProject: a.UserProject,
		query: query}, &wire); err != nil {
		return nil, err
	}
	invalid := func() error {
		failed := provider.InvalidResponse(op, subject+" returned an invalid response")
		failed.Message += unsureNote
		return failed
	}
	row, err := objectRow(op, wire)
	if err != nil {
		return nil, invalid()
	}
	if name, _ := row["name"].(string); name != a.Object || !objectOfBucket(row, a.Bucket) {
		return nil, invalid()
	}
	return row, nil
}

// objectChangeSummary phrases a change of an object from its validated arguments.
func objectChangeSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a objectChangeArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		object := "object " + strconv.Quote(boundedText(a.Object, maxNameText)) + " in bucket " +
			strconv.Quote(boundedText(a.Bucket, maxNameText))
		if tool == objectsRestoreID {
			return "Restore the soft-deleted generation " + boundedText(a.Generation, maxNameText) + " of " + object +
				": it gets a new generation, its access control list is not copied back, and it is refused if a live " +
				"object of that name exists"
		}
		if a.Generation != "" {
			return "Delete generation " + boundedText(a.Generation, maxNameText) + " of " + object +
				" for good: this cannot be undone"
		}
		return "Delete the live version of " + object + ": without versioning or soft delete this cannot be undone"
	}
}

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

const objectsRestoreOverwriteID = Provider + ".objects.restore-overwrite"

// objects.restore-overwrite is objects.restore of discovery storage v1 with ifGenerationMatch set to the
// generation of the live object, so a live object is replaced only if it still has that generation.
// copySourceAcl is never sent.

var objectsRestoreOverwrite = capability.Descriptor{
	ID: objectsRestoreOverwriteID, Version: 1, Title: "Restore a soft-deleted Cloud Storage object over a live one",
	Description: "Restore a soft-deleted object of a bucket of Cloud Storage (objects.restore of storage v1) from " +
		"its generation and replace the live object of the same name. generation is the soft-deleted generation " +
		"and comes from objects.list or objects.get with soft_deleted; live_generation is the generation of the " +
		"live object to replace and is sent as ifGenerationMatch, so Google refuses the request with a " +
		"precondition failure (412) when the live object no longer has that generation. According to the " +
		"policies of the bucket the replaced object becomes a noncurrent version or is soft-deleted, and the " +
		"account also needs storage.objects.delete. The restored object gets a new generation. The access control " +
		"list of the soft-deleted object is not copied back (copySourceAcl is never sent). The result is the " +
		"restored object as Google describes it, untrusted third-party text." +
		bucketBindingTail + storageChangeTail + storageTail,
	Tags: []string{"google", "cloud", "storage", "objects", "restore", "overwrite"},
	Risk: storageChangeRisk(capability.EffectUpdate), Provider: Provider, RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + storageBucketProp + `,` + storageObjectProp +
		`,"generation":` + storageGenProp + `,"live_generation":` + storageGenProp + `,` +
		storageIfMetagenerationMatchProp + `,` + storageProjectionProp + `,"restore_token":` + storageTextProp +
		`,` + storageUserProjectProp + `},` +
		`"required":["bucket","object","generation","live_generation"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(objectSchema),
	Arguments: []capability.Argument{storageBucketArgument, storageObjectArgument,
		{Name: "generation", Required: true, Description: "Generation of the soft-deleted object to restore, a " +
			"decimal number"},
		{Name: "live_generation", Required: true, Description: "Generation of the live object to replace, a " +
			"decimal number; sent as ifGenerationMatch"},
		{Name: "if_metageneration_match", Description: "Proceed only if the metageneration of the live object is " +
			"this number"},
		storageProjectionArgument,
		{Name: "restore_token", Description: "Restore token of a soft-deleted object in a bucket with " +
			"hierarchical namespace, which Google needs to tell apart objects of the same name; read it from " +
			"objects.get with soft_deleted"},
		storageUserProjectArgument},
	Fields: []capability.Field{{Name: "name", Description: "The restored object as one result: " + objectFields}},
	Examples: []capability.Example{{Description: "Restore one soft-deleted object over the live one",
		Arguments: json.RawMessage(`{"bucket":"example-bucket","object":"reports/2026/summary.txt",` +
			`"generation":"1700000000000000","live_generation":"1700000100000000"}`)}},
}

type objectRestoreOverwriteArguments struct {
	Bucket                string `json:"bucket"`
	Object                string `json:"object"`
	Generation            string `json:"generation"`
	LiveGeneration        string `json:"live_generation"`
	IfMetagenerationMatch string `json:"if_metageneration_match"`
	Projection            string `json:"projection"`
	RestoreToken          string `json:"restore_token"`
	UserProject           string `json:"user_project"`
}

// check refuses what is not in its form, before any secret or request.
func (a objectRestoreOverwriteArguments) check() error {
	if err := checkBucket(a.Bucket); err != nil {
		return err
	}
	if err := checkObjectName(a.Object); err != nil {
		return err
	}
	if a.Generation == "" {
		return badRequest("generation is required")
	}
	if a.LiveGeneration == "" {
		return badRequest("live_generation is required")
	}
	for _, check := range []error{
		checkNumberArgument("generation", a.Generation),
		checkNumberArgument("live_generation", a.LiveGeneration),
		checkNumberArgument("if_metageneration_match", a.IfMetagenerationMatch),
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

func handleObjectsRestoreOverwrite(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "restore object over a live object"
	var a objectRestoreOverwriteArguments
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if err := a.check(); err != nil {
		return nil, err
	}
	s, err := openStorage(ctx, op, objectsRestoreOverwriteID, []namedProject{{"user_project", a.UserProject}},
		resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	setQueryValues(query, map[string]string{"generation": a.Generation, "ifGenerationMatch": a.LiveGeneration,
		"ifMetagenerationMatch": a.IfMetagenerationMatch, "projection": a.Projection,
		"restoreToken": a.RestoreToken})
	var wire json.RawMessage
	if err := s.change(ctx, storageChange{op: op, tool: objectsRestoreOverwriteID, method: http.MethodPost,
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
	// The answer must describe the requested object: bucket and name as asked, and a generation, which is a new
	// one and not that of the replaced live object.
	name, _ := row["name"].(string)
	bucket, _ := row["bucket"].(string)
	generation, _ := row["generation"].(string)
	if name != a.Object || bucket != a.Bucket || generation == "" || generation == a.LiveGeneration ||
		!storageNumberPattern.MatchString(generation) {
		return nil, invalid()
	}
	return row, nil
}

func objectsRestoreOverwriteSummary(raw json.RawMessage) string {
	var a objectRestoreOverwriteArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Restore the soft-deleted generation " + boundedText(a.Generation, maxNameText) + " of object " +
		strconv.Quote(boundedText(a.Object, maxNameText)) + " in bucket " +
		strconv.Quote(boundedText(a.Bucket, maxNameText)) + " and replace the live object of that name, generation " +
		boundedText(a.LiveGeneration, maxNameText) + ": the replaced object becomes noncurrent or soft-deleted " +
		"according to the policies of the bucket, the restored object gets a new generation, and its access " +
		"control list is not copied back"
}

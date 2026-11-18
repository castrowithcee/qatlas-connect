package googlecloud

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	storageProjectNumber = "415104041262"
	foreignNumber        = "999000111222"
	bucketsPath          = "/storage/v1/b"
)

var storageTools = []string{bucketsListID, bucketsGetID, bucketsGetIAMPolicyID, bucketAccessControlsListID,
	objectsListID, objectsGetID, objectsDownloadID, objectAccessControlsListID}

func bucketBody(name, number string) string {
	return `{"kind":"storage#bucket","id":"` + name + `","name":"` + name + `","projectNumber":"` + number + `",` +
		`"location":"EU","locationType":"multi-region","storageClass":"STANDARD","timeCreated":"2026-01-02T03:04:05.000Z",` +
		`"updated":"2026-02-03T04:05:06.000Z","metageneration":"3","etag":"CAM=","versioning":{"enabled":true},` +
		`"labels":{"env":"test"},"lifecycle":{"rule":[{"action":{"type":"Delete"},"condition":{"age":30}}]},` +
		`"encryption":{"defaultKmsKeyName":"projects/p/locations/eu/keyRings/r/cryptoKeys/k"},` +
		`"iamConfiguration":{"uniformBucketLevelAccess":{"enabled":true},"publicAccessPrevention":"enforced"},` +
		`"retentionPolicy":{"retentionPeriod":"86400"},"softDeletePolicy":{"retentionDurationSeconds":"604800"},` +
		`"autoclass":{"enabled":false},"website":{"mainPageSuffix":"index.html"},"logging":{"logBucket":"log-bucket"},` +
		`"cors":[{"origin":["*"],"method":["GET"]}],"hierarchicalNamespace":{"enabled":false},"unknownMember":"x"}`
}

func bucketPage(next string, buckets ...string) string {
	body := `{"kind":"storage#buckets","items":[` + strings.Join(buckets, ",") + `]`
	if next != "" {
		body += `,"nextPageToken":"` + next + `"`
	}
	return body + `}`
}

func TestBucketsListFollowsPagesAndBuildsTheQuery(t *testing.T) {
	for _, projection := range []string{"full", "noAcl"} {
		e := newEnvironment(t, nil)
		e.fake.hook = func(_ int, req *http.Request) *reply {
			if req.URL.Path != bucketsPath {
				return nil
			}
			if req.URL.Query().Get("pageToken") == "" {
				return &reply{status: 200, body: bucketPage("p2", bucketBody("logs-a", storageProjectNumber))}
			}
			return &reply{status: 200, body: `{"items":[` + bucketBody("logs-b", storageProjectNumber) +
				`],"unreachable":["projects/_/buckets/x"]}`}
		}
		base := `{"project_id":"my-project-123","prefix":"logs-","projection":"` + projection + `","soft_deleted":true,` +
			`"return_partial_success":true,"max_results":1`
		out, err := e.invoke(bucketsListID, base+`}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, got := listOf(t, out, "buckets")
		if len(rows) != 1 || rows[0]["name"] != "logs-a" || rows[0]["unknownMember"] != nil || got["has_more"] != true {
			t.Fatalf("page 1 = %s", out)
		}
		row := rows[0]
		if row["location"] != "EU" || row["locationType"] != "multi-region" || row["storageClass"] != "STANDARD" ||
			row["timeCreated"] == nil || row["updated"] == nil || row["etag"] != "CAM=" ||
			row["projectNumber"] != storageProjectNumber || row["versioning"].(map[string]any)["enabled"] != true ||
			row["labels"].(map[string]any)["env"] != "test" || row["lifecycle"] == nil || row["encryption"] == nil ||
			row["iamConfiguration"] == nil || row["retentionPolicy"] == nil || row["softDeletePolicy"] == nil ||
			row["autoclass"] == nil || row["website"] == nil || row["logging"] == nil || row["cors"] == nil ||
			row["hierarchicalNamespace"] == nil {
			t.Errorf("row = %v", row)
		}
		out, err = e.invoke(bucketsListID, base+`,"cursor":"`+got["next_cursor"].(string)+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, got = listOf(t, out, "buckets")
		if len(rows) != 1 || rows[0]["name"] != "logs-b" || got["has_more"] != false || got["next_cursor"] != nil ||
			len(got["unreachable"].([]any)) != 1 {
			t.Errorf("page 2 = %s", out)
		}
		for i, q := range queriesOf(e) {
			if q.Get("project") != "my-project-123" || q.Get("prefix") != "logs-" || q.Get("projection") != projection ||
				q.Get("softDeleted") != "true" || q.Get("returnPartialSuccess") != "true" || q.Get("maxResults") != "1" ||
				(i == 1) != (q.Get("pageToken") == "p2") {
				t.Errorf("request %d: %v", i, q)
			}
		}
		if paths := apiPaths(e); paths[0] != "storage.googleapis.com/storage/v1/b" {
			t.Errorf("paths = %v", paths)
		}
	}
}

func TestBucketsListDefaultsAndEmptyList(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[bucketsPath] = okReply(`{"kind":"storage#buckets"}`)
	out, err := e.invoke(bucketsListID, `{"project_id":"my-project-123"}`)
	if err != nil || out != `{"has_more":false}` {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	if q := queriesOf(e)[0]; q.Get("maxResults") != "100" || q.Has("projection") || q.Has("softDeleted") || q.Has("pageToken") {
		t.Errorf("query = %v", q)
	}
}

func TestBucketsListRefusesACursorOfAnotherList(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[bucketsPath] = okReply(bucketPage("p2", bucketBody("logs-a", storageProjectNumber)))
	out, err := e.invoke(bucketsListID, `{"project_id":"my-project-123","prefix":"a-"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, got := listOf(t, out, "buckets")
	cursor := got["next_cursor"].(string)
	before := len(e.fake.api())
	for _, arguments := range []string{
		`{"project_id":"my-project-123","prefix":"b-","cursor":"` + cursor + `"}`,
		`{"project_id":"other-project-1","prefix":"a-","cursor":"` + cursor + `"}`,
		`{"project_id":"my-project-123","prefix":"a-","projection":"full","cursor":"` + cursor + `"}`,
		`{"project_id":"my-project-123","cursor":"bogus"}`,
	} {
		if _, err := e.invoke(bucketsListID, arguments); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", arguments, err)
		}
	}
	if len(e.fake.api()) != before {
		t.Error("a refused cursor reached Google")
	}
}

func TestBucketsListChecksEveryRowAgainstTheTargets(t *testing.T) {
	rows := bucketPage("", bucketBody("mine-a", storageProjectNumber), bucketBody("foreign-a", foreignNumber),
		`{"name":"no-number"}`, bucketBody("mine-b", storageProjectNumber))
	e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[bucketsPath] = okReply(rows)
	out, err := e.invoke(bucketsListID, `{"project_id":"`+storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := listOf(t, out, "buckets")
	var names []string
	for _, row := range got {
		names = append(names, row["name"].(string))
	}
	if !slices.Equal(names, []string{"mine-a", "mine-b"}) || strings.Contains(out, "foreign") || strings.Contains(out, foreignNumber) {
		t.Errorf("names = %v, out = %s", names, out)
	}
	if len(e.fake.api()) != 1 {
		t.Errorf("requests = %v", apiPaths(e))
	}

	// A folder target proves each project number once by reading the project and the folders above it.
	e = newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
	e.fake.routes["/v3/projects/"+storageProjectNumber] = okReply(projectBody)
	e.fake.routes["/v3/projects/"+foreignNumber] = okReply(`{"name":"projects/` + foreignNumber + `","parent":"folders/9999"}`)
	e.fake.routes["/v3/folders/9999"] = okReply(`{"name":"folders/9999","parent":"organizations/1"}`)
	e.fake.routes[bucketsPath] = okReply(rows)
	out, err = e.invoke(bucketsListID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ = listOf(t, out, "buckets"); len(got) != 2 || strings.Contains(out, "foreign") {
		t.Errorf("out = %s", out)
	}
	reads := 0
	for _, p := range apiPaths(e) {
		if p == "cloudresourcemanager.googleapis.com/v3/projects/"+storageProjectNumber {
			reads++
		}
	}
	if reads != 1 {
		t.Errorf("the project of the rows was read %d times: %v", reads, apiPaths(e))
	}
}

func TestBucketsListRefusesProjectsOutsideTheTargetsBeforeIO(t *testing.T) {
	for _, arguments := range []string{`{"project_id":"other-project-1"}`,
		`{"project_id":"my-project-123","user_project":"other-project-1"}`} {
		e := newEnvironment(t, []string{"projects/my-project-123"})
		_, err := e.invoke(bucketsListID, arguments)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), "my-project-123") || strings.Contains(err.Error(), "other-project-1") {
			t.Errorf("%s: err = %v", arguments, err)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", arguments, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
}

func TestBucketsGetReadsOneBucketAndBindsItByProjectNumber(t *testing.T) {
	e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
	e.fake.routes[bucketsPath+"/example.bucket_1-a"] = okReply(bucketBody("example.bucket_1-a", storageProjectNumber))
	out, err := e.invoke(bucketsGetID, `{"bucket":"example.bucket_1-a","projection":"full","if_metageneration_match":"3",`+
		`"soft_deleted":true,"generation":"1700000000000000","user_project":"`+storageProjectNumber+`"}`)
	if err != nil || !strings.Contains(out, `"name":"example.bucket_1-a"`) || strings.Contains(out, "unknownMember") {
		t.Fatalf("out = %s, err = %v", out, err)
	}
	if got := e.fake.api(); len(got) != 1 {
		t.Fatalf("requests = %d, want exactly one reading", len(got))
	}
	u, _ := url.Parse(e.fake.api()[0].url)
	q := u.Query()
	if u.Host != "storage.googleapis.com" || u.EscapedPath() != "/storage/v1/b/example.bucket_1-a" ||
		q.Get("projection") != "full" || q.Get("ifMetagenerationMatch") != "3" || q.Get("softDeleted") != "true" ||
		q.Get("generation") != "1700000000000000" || q.Get("userProject") != storageProjectNumber {
		t.Errorf("url = %s", u)
	}
	for _, c := range []struct{ name, arguments string }{
		{"foreign project number", `{"bucket":"foreign-bucket"}`},
	} {
		e.fake.routes[bucketsPath+"/foreign-bucket"] = okReply(bucketBody("foreign-bucket", foreignNumber))
		out, err := e.invoke(bucketsGetID, c.arguments)
		if !isInvalidRequest(err) || out != "" || strings.Contains(err.Error(), foreignNumber) ||
			strings.Contains(err.Error(), "foreign-bucket") {
			t.Errorf("%s: out = %q, err = %v", c.name, out, err)
		}
	}
	// An answer for another bucket than the one asked for is invalid.
	e.fake.routes[bucketsPath+"/asked-bucket"] = okReply(bucketBody("other-bucket", storageProjectNumber))
	if _, err := e.invoke(bucketsGetID, `{"bucket":"asked-bucket"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("err = %v", err)
	}
}

func TestBucketsGetProvesAProjectTheTargetsDoNotDecideByReadingIt(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes[bucketsPath+"/example-bucket"] = okReply(bucketBody("example-bucket", storageProjectNumber))
	e.fake.routes["/v3/projects/"+storageProjectNumber] = okReply(projectBody)
	if _, err := e.invoke(bucketsGetID, `{"bucket":"example-bucket"}`); err != nil {
		t.Fatal(err)
	}
	e = newEnvironment(t, []string{"projects/other-project-1"})
	e.fake.routes[bucketsPath+"/example-bucket"] = okReply(bucketBody("example-bucket", storageProjectNumber))
	e.fake.routes["/v3/projects/"+storageProjectNumber] = okReply(projectBody)
	if _, err := e.invoke(bucketsGetID, `{"bucket":"example-bucket"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
}

func TestBucketToolsRefuseInvalidInputBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 64)
	for name, c := range map[string]struct{ tool, arguments string }{
		"path":              {bucketsGetID, `{"bucket":"a/b"}`},
		"nested path":       {bucketsGetID, `{"bucket":"projects/_/buckets/example"}`},
		"traversal":         {bucketsGetID, `{"bucket":"../x"}`},
		"traversal in":      {bucketsGetIAMPolicyID, `{"bucket":"a/../b"}`},
		"query":             {bucketAccessControlsListID, `{"bucket":"a?b=1"}`},
		"encoded slash":     {bucketsGetID, `{"bucket":"a%2Fb"}`},
		"url":               {bucketsGetID, `{"bucket":"gs://example"}`},
		"upper case":        {bucketsGetIAMPolicyID, `{"bucket":"Example"}`},
		"too short":         {bucketsGetID, `{"bucket":"ab"}`},
		"too long":          {bucketsGetID, `{"bucket":"` + long + `"}`},
		"leading dash":      {bucketsGetID, `{"bucket":"-abc"}`},
		"trailing dot":      {bucketAccessControlsListID, `{"bucket":"abc."}`},
		"space":             {bucketsGetID, `{"bucket":"a b c"}`},
		"missing":           {bucketsGetID, `{}`},
		"unknown argument":  {bucketsGetID, `{"bucket":"abc","path":"/x"}`},
		"projection":        {bucketsGetID, `{"bucket":"abc","projection":"all"}`},
		"projection list":   {bucketsListID, `{"project_id":"my-project-123","projection":"all"}`},
		"generation alone":  {bucketsGetID, `{"bucket":"abc","generation":"12"}`},
		"generation text":   {bucketsGetID, `{"bucket":"abc","soft_deleted":true,"generation":"1x"}`},
		"metageneration":    {bucketsGetID, `{"bucket":"abc","if_metageneration_match":"x"}`},
		"policy version 2":  {bucketsGetIAMPolicyID, `{"bucket":"abc","options_requested_policy_version":2}`},
		"user project path": {bucketsGetID, `{"bucket":"abc","user_project":"projects/x"}`},
		"project missing":   {bucketsListID, `{}`},
		"project path":      {bucketsListID, `{"project_id":"projects/x"}`},
		"prefix path":       {bucketsListID, `{"project_id":"my-project-123","prefix":"a/b"}`},
		"prefix upper":      {bucketsListID, `{"project_id":"my-project-123","prefix":"A"}`},
		"max results 1001":  {bucketsListID, `{"project_id":"my-project-123","max_results":1001}`},
		"max results 0":     {bucketsListID, `{"project_id":"my-project-123","max_results":0}`},
	} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(c.tool, c.arguments); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handlers refuse what the schema would not have let through.
	e := newEnvironment(t, nil)
	for name, call := range map[string]func() error{
		"get": func() error {
			_, err := handleBucketsGet(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{"bucket":"a/b"}`))
			return err
		},
		"policy": func() error {
			_, err := handleBucketsGetIAMPolicy(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{"bucket":"../b"}`))
			return err
		},
		"acl": func() error {
			_, err := handleBucketAccessControlsList(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{"bucket":"A"}`))
			return err
		},
		"list": func() error {
			_, err := handleBucketsList(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{"project_id":"a/b"}`))
			return err
		},
	} {
		if err := call(); !isInvalidRequest(err) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("api=%d lookups=%d", len(e.fake.api()), e.lookups)
	}
}

const policyWithConditionAndPublic = `{"kind":"storage#policy","resourceId":"projects/_/buckets/example-bucket",` +
	`"version":3,"etag":"CAM=","bindings":[` +
	`{"role":"roles/storage.objectViewer","members":["allUsers","allAuthenticatedUsers","user:person@example.com",` +
	`"group:team@example.com","serviceAccount:svc@example-project.iam.gserviceaccount.com"]},` +
	`{"role":"roles/storage.objectAdmin","members":["user:admin@example.com"],"condition":{"title":"expires",` +
	`"description":"d","expression":"request.time < timestamp(\"2027-01-01T00:00:00Z\")","location":"l"}}]}`

func TestBucketsGetIAMPolicyReturnsMembersAndConditionsUnchanged(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[bucketsPath+"/example-bucket/iam"] = okReply(policyWithConditionAndPublic)
	out, err := e.invoke(bucketsGetIAMPolicyID, `{"bucket":"example-bucket","user_project":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kind, ResourceID, Etag string
		Version                int
		Bindings               []struct {
			Role      string
			Members   []string
			Condition map[string]string
		}
		Truncated bool
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || got.Etag != "CAM=" || got.Kind != "storage#policy" || got.Truncated || len(got.Bindings) != 2 ||
		!slices.Equal(got.Bindings[0].Members, []string{"allUsers", "allAuthenticatedUsers", "user:person@example.com",
			"group:team@example.com", "serviceAccount:svc@example-project.iam.gserviceaccount.com"}) ||
		got.Bindings[1].Condition["title"] != "expires" || !strings.Contains(got.Bindings[1].Condition["expression"], "2027") ||
		got.Bindings[0].Condition != nil {
		t.Errorf("policy = %s", out)
	}
	// An unbound connection needs no proof, so the policy is the only reading.
	if got := e.fake.api(); len(got) != 1 {
		t.Fatalf("requests = %d", len(got))
	}
	q := queriesOf(e)[0]
	if q.Get("optionsRequestedPolicyVersion") != "3" || q.Get("userProject") != "my-project-123" {
		t.Errorf("query = %v", q)
	}
	if _, err := e.invoke(bucketsGetIAMPolicyID, `{"bucket":"example-bucket","options_requested_policy_version":1}`); err != nil {
		t.Fatal(err)
	}
	if q := queriesOf(e)[1]; q.Get("optionsRequestedPolicyVersion") != "1" {
		t.Errorf("query = %v", q)
	}
}

func TestBucketsGetIAMPolicyAndACLBindTheBucketWithOneReadingBeforeTheRest(t *testing.T) {
	for _, tool := range []string{bucketsGetIAMPolicyID, bucketAccessControlsListID} {
		suffix := "/iam"
		if tool == bucketAccessControlsListID {
			suffix = "/acl"
		}
		e := newEnvironment(t, []string{"projects/" + storageProjectNumber})
		e.fake.routes[bucketsPath+"/example-bucket"] = okReply(`{"name":"example-bucket","projectNumber":"` + storageProjectNumber + `"}`)
		e.fake.routes[bucketsPath+"/example-bucket"+suffix] = okReply(policyWithConditionAndPublic)
		if tool == bucketAccessControlsListID {
			e.fake.routes[bucketsPath+"/example-bucket"+suffix] = okReply(`{"items":[]}`)
		}
		if _, err := e.invoke(tool, `{"bucket":"example-bucket"}`); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if paths := apiPaths(e); len(paths) != 2 || paths[0] != "storage.googleapis.com/storage/v1/b/example-bucket" ||
			paths[1] != "storage.googleapis.com/storage/v1/b/example-bucket"+suffix {
			t.Errorf("%s: paths = %v", tool, paths)
		}
		if q := queriesOf(e)[0]; q.Get("fields") != "name,projectNumber" {
			t.Errorf("%s: proof query = %v", tool, q)
		}

		// A bucket of another project is refused after the proof, and nothing further is read.
		e = newEnvironment(t, []string{"projects/" + storageProjectNumber})
		e.fake.routes[bucketsPath+"/foreign-bucket"] = okReply(`{"name":"foreign-bucket","projectNumber":"` + foreignNumber + `"}`)
		e.fake.routes[bucketsPath+"/foreign-bucket"+suffix] = okReply(policyWithConditionAndPublic)
		out, err := e.invoke(tool, `{"bucket":"foreign-bucket"}`)
		if !isInvalidRequest(err) || out != "" || strings.Contains(err.Error(), foreignNumber) ||
			strings.Contains(err.Error(), "foreign-bucket") {
			t.Errorf("%s: out = %q, err = %v", tool, out, err)
		}
		if paths := apiPaths(e); len(paths) != 1 {
			t.Errorf("%s: paths = %v", tool, paths)
		}
	}
}

func TestBucketAccessControlsListReturnsEveryEntryUnchanged(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[bucketsPath+"/example-bucket/acl"] = okReply(`{"kind":"storage#bucketAccessControls","items":[` +
		`{"kind":"storage#bucketAccessControl","id":"example-bucket/allUsers","bucket":"example-bucket","entity":"allUsers",` +
		`"role":"READER","etag":"e","selfLink":"s"},` +
		`{"bucket":"example-bucket","entity":"user-person@example.com","role":"OWNER","email":"person@example.com","entityId":"abc"},` +
		`{"bucket":"example-bucket","entity":"domain-example.com","role":"READER","domain":"example.com"},` +
		`{"bucket":"example-bucket","entity":"project-owners-415104041262","role":"OWNER",` +
		`"projectTeam":{"projectNumber":"415104041262","team":"owners"},"unknownMember":"x"}]}`)
	out, err := e.invoke(bucketAccessControlsListID, `{"bucket":"example-bucket"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := listOf(t, out, "items")
	if len(rows) != 4 || rows[0]["entity"] != "allUsers" || rows[0]["role"] != "READER" ||
		rows[1]["email"] != "person@example.com" || rows[1]["entityId"] != "abc" || rows[2]["domain"] != "example.com" ||
		rows[3]["projectTeam"].(map[string]any)["team"] != "owners" || rows[3]["unknownMember"] != nil {
		t.Errorf("rows = %v", rows)
	}
	e.fake.routes[bucketsPath+"/example-bucket/acl"] = okReply(`{"kind":"storage#bucketAccessControls"}`)
	if out, err = e.invoke(bucketAccessControlsListID, `{"bucket":"example-bucket"}`); err != nil || out != `{}` {
		t.Errorf("empty = %s, %v", out, err)
	}
	e.fake.routes[bucketsPath+"/example-bucket/acl"] = okReply(`{"items":[{"bucket":"another-bucket","entity":"allUsers"}]}`)
	if _, err = e.invoke(bucketAccessControlsListID, `{"bucket":"example-bucket"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("entry of another bucket: %v", err)
	}
}

func TestStorageToolsMapBothErrorForms(t *testing.T) {
	cases := []struct {
		name  string
		reply reply
		want  provider.Class
	}{
		{"403 status form", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission},
		{"403 errors form", reply{status: 403, body: `{"error":{"code":403,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"forbidden"}]}}`}, provider.ClassPermission},
		{"404 status form", reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}, provider.ClassNotFound},
		{"404 errors form", reply{status: 404, body: `{"error":{"code":404,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"notFound"}]}}`}, provider.ClassNotFound},
		{"401 errors form", reply{status: 401, body: `{"error":{"code":401,"message":"` + detailCanary +
			`","errors":[{"message":"m","domain":"global","reason":"authError"}]}}`}, provider.ClassAuth},
		{"429", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")}, provider.ClassRateLimited},
		{"not an object", okReply(`[1]`), provider.ClassInvalidResponse},
	}
	for _, c := range cases {
		for _, id := range storageTools {
			arguments := `{"bucket":"example-bucket"}`
			switch id {
			case bucketsListID:
				arguments = `{"project_id":"my-project-123"}`
			case objectsGetID, objectsDownloadID, objectAccessControlsListID:
				arguments = `{"bucket":"example-bucket","object":"a.txt"}`
			}
			if id == objectsDownloadID && c.name == "not an object" {
				// The content of an object may be any JSON text, which is returned as data.
				continue
			}
			e := newEnvironment(t, nil)
			e.fake.hook = func(_ int, req *http.Request) *reply { return &c.reply }
			_, err := e.invoke(id, arguments)
			if classOf(err) != c.want {
				t.Errorf("%s %s: err = %v (%s), want %s", c.name, id, err, classOf(err), c.want)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %s: error echoes provider text", c.name, id)
			}
		}
	}
}

func TestStorageToolsAreReadToolsWithTheSmallestScopes(t *testing.T) {
	for _, id := range []string{bucketsListID, bucketsGetID} {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeStorageReadOnly {
			t.Errorf("%s scopes = %v", id, got)
		}
		if !slices.Contains(readTools, id) {
			t.Errorf("%s is not in the profile read", id)
		}
	}
	// Neither method lists a read-only scope, so they stay out of the profile read.
	for _, id := range []string{bucketsGetIAMPolicyID, bucketAccessControlsListID} {
		if got := tokenMetadata.Scopes[id]; len(got) != 1 || got[0] != scopeStorageFullControl {
			t.Errorf("%s scopes = %v", id, got)
		}
		if slices.Contains(readTools, id) {
			t.Errorf("%s is in the profile read", id)
		}
	}
	if scopeStorageReadOnly != "https://www.googleapis.com/auth/devstorage.read_only" ||
		scopeStorageFullControl != "https://www.googleapis.com/auth/devstorage.full_control" {
		t.Error("storage scope names changed")
	}
	e := newEnvironment(t, nil)
	for _, id := range storageTools {
		d, _, found := e.reg.Lookup(id)
		if !found {
			t.Fatalf("%s is not registered", id)
		}
		want := readRisk
		if id == objectsDownloadID {
			want.DataSensitivity = contentSensitivity
		}
		if d.Risk != want || !strings.Contains(d.Description, "untrusted") {
			t.Errorf("%s risk = %+v", id, d.Risk)
		}
	}
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, p := range metadata.Profiles {
		if p.ID == "read-storage-access" && (!slices.Contains(p.Tools, bucketsGetIAMPolicyID) ||
			!slices.Contains(p.Tools, bucketAccessControlsListID) || !slices.Contains(p.Tools, bucketsGetID)) {
			t.Errorf("profile = %v", p.Tools)
		}
	}
}

func TestStorageToolsNeedAProjectInTheTargets(t *testing.T) {
	for _, id := range storageTools {
		if ok, _ := targetReaches(id, []string{"billingAccounts/AAAAAA-BBBBBB-CCCCCC"}); ok {
			t.Errorf("%s is offered to a billing-only connection", id)
		}
		if ok, _ := targetReaches(id, []string{"projects/my-project-123"}); !ok {
			t.Errorf("%s is not offered to a project connection", id)
		}
	}
}

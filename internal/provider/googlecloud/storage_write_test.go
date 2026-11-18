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

const deleteObjectArgs = `{"bucket":"example-bucket","object":"reports/2026/a b.txt"`

// storageChangeEnvironment is an environment bound to the project of the test bucket whose connection may read,
// update, and delete; the bucket proof of that project is answered.
func storageChangeEnvironment(t *testing.T, tools []string) *environment {
	t.Helper()
	e := revokeEnvironment(t, []string{"projects/" + storageProjectNumber}, tools)
	e.fake.routes[bucketRoute] = okReply(bucketBody("example-bucket", storageProjectNumber))
	return e
}

func noContent() reply { return reply{status: 204} }

func TestObjectsDeleteSendsOneEncodedDeleteAfterTheBucketProof(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.Method == "DELETE" {
			r := noContent()
			return &r
		}
		return nil
	}
	out, err := e.invokeConfirmed(objectsDeleteID, deleteObjectArgs+`,"if_generation_match":"7",`+
		`"if_generation_not_match":"8","if_metageneration_match":"9","if_metageneration_not_match":"10",`+
		`"user_project":"`+storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["deleted"] != true || got["bucket"] != "example-bucket" ||
		got["object"] != "reports/2026/a b.txt" || got["generation"] != nil {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != "GET" || api[1].method != "DELETE" || api[1].body != "" {
		t.Fatalf("requests = %+v, want the bucket proof and one DELETE", api)
	}
	u, _ := url.Parse(api[1].url)
	q := u.Query()
	if u.Host != "storage.googleapis.com" || u.EscapedPath() != "/storage/v1/b/example-bucket/o/reports%2F2026%2Fa%20b.txt" ||
		q.Get("ifGenerationMatch") != "7" || q.Get("ifGenerationNotMatch") != "8" ||
		q.Get("ifMetagenerationMatch") != "9" || q.Get("ifMetagenerationNotMatch") != "10" ||
		q.Get("userProject") != storageProjectNumber || q.Has("generation") {
		t.Errorf("url = %s", u)
	}
}

func TestObjectsDeleteWithAGenerationNamesIt(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/a.txt"] = noContent()
	out, err := e.invokeConfirmed(objectsDeleteID, `{"bucket":"example-bucket","object":"a.txt","generation":"1700000000000000"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["generation"] != "1700000000000000" || got["deleted"] != true {
		t.Errorf("result = %s", out)
	}
	u, _ := url.Parse(e.fake.api()[1].url)
	if u.Query().Get("generation") != "1700000000000000" {
		t.Errorf("url = %s", u)
	}
	// An unbound connection needs no proof, so the DELETE is the only request.
	e = revokeEnvironment(t, nil, nil)
	e.fake.routes[bucketRoute+"/o/a.txt"] = noContent()
	if _, err := e.invokeConfirmed(objectsDeleteID, `{"bucket":"example-bucket","object":"a.txt"}`); err != nil {
		t.Fatal(err)
	}
	if api := e.fake.api(); len(api) != 1 || api[0].method != "DELETE" {
		t.Errorf("requests = %+v", api)
	}
}

func TestObjectsDeleteMapsAPreconditionConflictWithoutRepeating(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/a.txt"] = reply{status: 412, body: errorObject(412, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(objectsDeleteID, `{"bucket":"example-bucket","object":"a.txt","if_generation_match":"5"}`)
	if err == nil || classOf(err) == "" || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[1].method != "DELETE" {
		t.Errorf("requests = %+v, want the proof and one DELETE", api)
	}
}

func TestStorageChangeOfAnUnclearOutcomeIsNotRepeated(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/a.txt"] = reply{status: 503, body: errorObject(503, "UNAVAILABLE", "")}
	_, err := e.invokeConfirmed(objectsDeleteID, `{"bucket":"example-bucket","object":"a.txt"}`)
	if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(e.fake.api()) != 2 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
}

func TestStorageChangesRefuseABucketOutsideTheTargetsBeforeTheChange(t *testing.T) {
	for id, args := range map[string]string{
		objectsDeleteID:  `{"bucket":"foreign-bucket","object":"a.txt"}`,
		objectsRestoreID: `{"bucket":"foreign-bucket","object":"a.txt","generation":"5"}`,
		bucketsDeleteID:  `{"bucket":"foreign-bucket","project":"` + storageProjectNumber + `"}`,
	} {
		e := storageChangeEnvironment(t, nil)
		if id == bucketsDeleteID {
			e = storageChangeEnvironment(t, []string{bucketsDeleteID})
		}
		e.fake.routes[bucketsPath+"/foreign-bucket"] = okReply(bucketBody("foreign-bucket", foreignNumber))
		out, err := e.invokeConfirmed(id, args)
		if !isInvalidRequest(err) || out != "" || strings.Contains(err.Error(), foreignNumber) ||
			strings.Contains(err.Error(), "foreign-bucket") {
			t.Errorf("%s: out = %q, err = %v", id, out, err)
		}
		for _, r := range e.fake.api() {
			if r.method != "GET" {
				t.Errorf("%s sent %s %s before refusing the bucket", id, r.method, r.url)
			}
		}
		if len(e.fake.api()) != 1 {
			t.Errorf("%s requests = %d, want only the proof", id, len(e.fake.api()))
		}
	}
	// A user_project outside the targets ends before any request.
	e := storageChangeEnvironment(t, nil)
	if _, err := e.invokeConfirmed(objectsDeleteID, `{"bucket":"example-bucket","object":"a.txt","user_project":"`+foreignNumber+`"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	noIO(t, e, "foreign user_project")
}

func TestStorageChangesRefuseBadInputBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 1025)
	for id, list := range map[string]map[string]string{
		objectsDeleteID: {
			"no object":        `{"bucket":"example-bucket"}`,
			"dot object":       `{"bucket":"example-bucket","object":"."}`,
			"dotdot object":    `{"bucket":"example-bucket","object":".."}`,
			"long object":      `{"bucket":"example-bucket","object":"` + long + `"}`,
			"control object":   `{"bucket":"example-bucket","object":"a\u0000b"}`,
			"path bucket":      `{"bucket":"a/b","object":"x"}`,
			"url bucket":       `{"bucket":"https://x.example/b","object":"x"}`,
			"bad generation":   `{"bucket":"example-bucket","object":"x","generation":"1/2"}`,
			"bad precondition": `{"bucket":"example-bucket","object":"x","if_generation_match":"abc"}`,
			"extra":            `{"bucket":"example-bucket","object":"x","force":true}`,
			"bulk":             `{"bucket":"example-bucket","prefix":"logs/"}`,
		},
		objectsRestoreID: {
			"no generation":               `{"bucket":"example-bucket","object":"x"}`,
			"empty generation":            `{"bucket":"example-bucket","object":"x","generation":""}`,
			"copySourceAcl":               `{"bucket":"example-bucket","object":"x","generation":"5","copySourceAcl":true}`,
			"copy_source_acl":             `{"bucket":"example-bucket","object":"x","generation":"5","copy_source_acl":true}`,
			"bad projection":              `{"bucket":"example-bucket","object":"x","generation":"5","projection":"other"}`,
			"dotdot object":               `{"bucket":"example-bucket","object":"..","generation":"5"}`,
			"control token":               `{"bucket":"example-bucket","object":"x","generation":"5","restore_token":"a\u0001"}`,
			"if_generation_match":         `{"bucket":"example-bucket","object":"x","generation":"5","if_generation_match":"0"}`,
			"if_generation_not_match":     `{"bucket":"example-bucket","object":"x","generation":"5","if_generation_not_match":"1"}`,
			"if_metageneration_match":     `{"bucket":"example-bucket","object":"x","generation":"5","if_metageneration_match":"1"}`,
			"if_metageneration_not_match": `{"bucket":"example-bucket","object":"x","generation":"5","if_metageneration_not_match":"1"}`,
		},
		bucketsDeleteID: {
			"no bucket":    `{"project":"` + storageProjectNumber + `"}`,
			"no project":   `{"bucket":"example-bucket"}`,
			"path bucket":  `{"bucket":"a/b","project":"` + storageProjectNumber + `"}`,
			"path project": `{"bucket":"example-bucket","project":"projects/1"}`,
			"bad meta":     `{"bucket":"example-bucket","project":"` + storageProjectNumber + `","if_metageneration_match":"x"}`,
			"extra":        `{"bucket":"example-bucket","project":"` + storageProjectNumber + `","force":true}`,
			"object given": `{"bucket":"example-bucket","project":"` + storageProjectNumber + `","object":"x"}`,
		},
	} {
		for name, args := range list {
			e := storageChangeEnvironment(t, []string{bucketsDeleteID})
			if _, err := e.invokeConfirmed(id, args); err == nil {
				t.Errorf("%s %s: accepted", id, name)
			}
			noIO(t, e, id+" "+name)
		}
	}
}

func TestObjectsRestoreSendsOnePostWithoutCopySourceAcl(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/dir/a.txt/restore"] = okReply(objectBody("dir/a.txt"))
	out, err := e.invokeConfirmed(objectsRestoreID, `{"bucket":"example-bucket","object":"dir/a.txt",`+
		`"generation":"1700000000000000",`+
		`"projection":"noAcl","restore_token":"tok en","user_project":"`+storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["name"] != "dir/a.txt" || got["unknownMember"] != nil ||
		strings.Contains(out, "secret-material") {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[1].method != "POST" || api[1].body != "" {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[1].url)
	q := u.Query()
	if u.EscapedPath() != "/storage/v1/b/example-bucket/o/dir%2Fa.txt/restore" ||
		q.Get("generation") != "1700000000000000" || q.Get("ifGenerationMatch") != "0" ||
		q.Has("ifMetagenerationNotMatch") || q.Has("ifGenerationNotMatch") || q.Has("ifMetagenerationMatch") ||
		q.Get("projection") != "noAcl" || q.Get("restoreToken") != "tok en" ||
		q.Get("userProject") != storageProjectNumber {
		t.Errorf("url = %s", u)
	}
	for _, r := range api {
		if strings.Contains(strings.ToLower(r.url), "copysourceacl") {
			t.Errorf("copySourceAcl was sent: %s", r.url)
		}
	}
}

func TestObjectsRestoreChecksTheAnswer(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/a.txt/restore"] = okReply(objectBody("other.txt"))
	_, err := e.invokeConfirmed(objectsRestoreID, `{"bucket":"example-bucket","object":"a.txt","generation":"5"}`)
	if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
		t.Errorf("err = %v", err)
	}
	e = storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/a.txt/restore"] = reply{status: 404, body: errorObject(404, "NOT_FOUND", "")}
	if _, err := e.invokeConfirmed(objectsRestoreID, `{"bucket":"example-bucket","object":"a.txt","generation":"5"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("err = %v", err)
	}
}

func TestBucketsDeleteIsOnlyReachableByNameAndNeedsAnEmptyBucket(t *testing.T) {
	args := `{"bucket":"example-bucket","project":"` + storageProjectNumber + `","if_metageneration_match":"3"}`
	for name, tools := range map[string][]string{
		"no tools list":    nil,
		"other tool named": {objectsDeleteID},
		"namespace prefix": {"google.cloud.buckets.*"},
	} {
		e := storageChangeEnvironment(t, tools)
		if _, err := e.invokeConfirmed(bucketsDeleteID, args); err == nil {
			t.Errorf("%s: buckets.delete was reachable", name)
		}
		noIO(t, e, name)
	}
	e := storageChangeEnvironment(t, []string{bucketsDeleteID})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.Method == "DELETE" {
			r := noContent()
			return &r
		}
		return nil
	}
	out, err := e.invokeConfirmed(bucketsDeleteID, args)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["deleted"] != true || got["bucket"] != "example-bucket" {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[1].method != "DELETE" || api[1].body != "" {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[1].url)
	if u.EscapedPath() != "/storage/v1/b/example-bucket" || u.Query().Get("ifMetagenerationMatch") != "3" {
		t.Errorf("url = %s", u)
	}
	// A bucket that still holds objects is refused by Google; the tool deletes no object and does not repeat.
	e = storageChangeEnvironment(t, []string{bucketsDeleteID})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.Method == "DELETE" {
			return &reply{status: 409, body: errorObject(409, "ABORTED", "")}
		}
		return nil
	}
	if _, err := e.invokeConfirmed(bucketsDeleteID, `{"bucket":"example-bucket","project":"`+storageProjectNumber+`"}`); err == nil ||
		strings.Contains(err.Error(), detailCanary) {
		t.Errorf("err = %v", err)
	}
	api = e.fake.api()
	if len(api) != 2 || api[0].method != "GET" || api[1].method != "DELETE" {
		t.Errorf("requests = %+v", api)
	}
}

func TestBucketsDeleteIsInNoProfile(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	found := false
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, bucketsDeleteID) {
			t.Errorf("profile %s holds buckets.delete", profile.ID)
		}
		if profile.ID == "storage-delete" {
			found = true
		}
	}
	if !found {
		t.Error("profile storage-delete is missing")
	}
}

func TestStorageChangesNeedConfirmationAndNameWhatTheyChange(t *testing.T) {
	for id, args := range map[string]string{
		objectsDeleteID:  `{"bucket":"example-bucket","object":"a.txt"}`,
		objectsRestoreID: `{"bucket":"example-bucket","object":"a.txt","generation":"5"}`,
		bucketsDeleteID:  `{"bucket":"example-bucket","project":"example-project-123"}`,
	} {
		e := storageChangeEnvironment(t, []string{id})
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
		noIO(t, e, id)
		d, _, _ := e.reg.Lookup(id)
		wantEffect := "delete"
		if id == objectsRestoreID {
			wantEffect = "update"
		}
		if d.Risk.Confirmation != "required" || !d.Risk.OpenWorld || string(d.Risk.Effect) != wantEffect ||
			d.RequiresToolAllowList != (id == bucketsDeleteID) {
			t.Errorf("%s risk = %+v, listed-only = %v", id, d.Risk, d.RequiresToolAllowList)
		}
		summary := e.reg.ConfirmationSummary(id)
		if summary == nil {
			t.Fatalf("%s has no confirmation summary", id)
		}
		if s := summary(json.RawMessage(args)); !strings.Contains(s, "example-bucket") {
			t.Errorf("%s summary %q lacks the bucket", id, s)
		}
	}
	e := newEnvironment(t, nil)
	del := e.reg.ConfirmationSummary(objectsDeleteID)
	if s := del(json.RawMessage(`{"bucket":"example-bucket","object":"a.txt"}`)); !strings.Contains(s, `"a.txt"`) ||
		!strings.Contains(s, "live version") {
		t.Errorf("summary = %q", s)
	}
	if s := del(json.RawMessage(`{"bucket":"example-bucket","object":"a.txt","generation":"42"}`)); !strings.Contains(s, "generation 42") ||
		strings.Contains(s, "live version") {
		t.Errorf("summary = %q", s)
	}
	if s := e.reg.ConfirmationSummary(objectsRestoreID)(json.RawMessage(
		`{"bucket":"example-bucket","object":"a.txt","generation":"5"}`)); !strings.Contains(s, "generation 5") {
		t.Errorf("summary = %q", s)
	}
}

func TestObjectsRestoreAlwaysSendsIfGenerationMatchZeroAndGoogleRefusesALiveObject(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	e.fake.routes[bucketRoute+"/o/a.txt/restore"] = reply{status: 412, body: errorObject(412, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(objectsRestoreID, `{"bucket":"example-bucket","object":"a.txt","generation":"5"}`)
	if err == nil || classOf(err) == "" || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[1].method != "POST" {
		t.Fatalf("requests = %+v, want the proof and one POST", api)
	}
	if u, _ := url.Parse(api[1].url); u.Query().Get("ifGenerationMatch") != "0" {
		t.Errorf("url = %s", u)
	}
	d, _, _ := e.reg.Lookup(objectsRestoreID)
	if !strings.Contains(d.Description, "never replaced") || strings.Contains(d.Description, "noncurrent") {
		t.Errorf("description = %q", d.Description)
	}
	if s := e.reg.ConfirmationSummary(objectsRestoreID)(json.RawMessage(
		`{"bucket":"example-bucket","object":"a.txt","generation":"5"}`)); !strings.Contains(s, "live") {
		t.Errorf("summary = %q", s)
	}
}

func TestBucketsDeleteNeedsTheProjectOfTheBucket(t *testing.T) {
	deleteOnly := func(e *environment) {
		e.fake.hook = func(_ int, req *http.Request) *reply {
			if req.Method == "DELETE" {
				r := noContent()
				return &r
			}
			return nil
		}
	}
	// A project number, a project ID resolved by reading the project, and an unbound connection.
	for name, c := range map[string]struct {
		targets []string
		project string
	}{
		"number":         {[]string{"projects/" + storageProjectNumber}, storageProjectNumber},
		"id":             {[]string{"projects/my-project-123"}, "my-project-123"},
		"unbound number": {nil, storageProjectNumber},
		"unbound id":     {nil, "my-project-123"},
	} {
		e := revokeEnvironment(t, c.targets, []string{bucketsDeleteID})
		e.fake.routes[bucketRoute] = okReply(bucketBody("example-bucket", storageProjectNumber))
		e.fake.routes["/v3/projects/my-project-123"] = okReply(projectBody)
		e.fake.routes["/v3/projects/"+storageProjectNumber] = okReply(projectBody)
		deleteOnly(e)
		out, err := e.invokeConfirmed(bucketsDeleteID, `{"bucket":"example-bucket","project":"`+c.project+`"}`)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		api := e.fake.api()
		if api[len(api)-1].method != "DELETE" || !strings.Contains(out, `"deleted":true`) {
			t.Errorf("%s: requests = %+v, out = %s", name, api, out)
		}
	}
	// Another project than the one of the bucket: no DELETE, and the refusal names neither project.
	for name, project := range map[string]string{"number": foreignNumber, "id": "other-project-1"} {
		e := revokeEnvironment(t, nil, []string{bucketsDeleteID})
		e.fake.routes[bucketRoute] = okReply(bucketBody("example-bucket", storageProjectNumber))
		e.fake.routes["/v3/projects/other-project-1"] = okReply(
			`{"name":"projects/` + foreignNumber + `","projectId":"other-project-1","state":"ACTIVE"}`)
		deleteOnly(e)
		_, err := e.invokeConfirmed(bucketsDeleteID, `{"bucket":"example-bucket","project":"`+project+`"}`)
		if !isInvalidRequest(err) || strings.Contains(err.Error(), storageProjectNumber) ||
			strings.Contains(err.Error(), project) {
			t.Errorf("%s: err = %v", name, err)
		}
		for _, r := range e.fake.api() {
			if r.method == "DELETE" {
				t.Errorf("%s: DELETE sent for another project", name)
			}
		}
	}
	// A project outside the targets ends before any request.
	e := storageChangeEnvironment(t, []string{bucketsDeleteID})
	if _, err := e.invokeConfirmed(bucketsDeleteID, `{"bucket":"example-bucket","project":"`+foreignNumber+`"}`); !isInvalidRequest(err) {
		t.Errorf("err = %v", err)
	}
	noIO(t, e, "project outside the targets")
	// The confirmation names the bucket and the project.
	e = newEnvironment(t, nil)
	s := e.reg.ConfirmationSummary(bucketsDeleteID)(json.RawMessage(
		`{"bucket":"example-bucket","project":"example-project-123"}`))
	if !strings.Contains(s, "example-bucket") || !strings.Contains(s, "example-project-123") {
		t.Errorf("summary = %q", s)
	}
}

package googlecloud

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const restoreOverwriteArgs = `{"bucket":"example-bucket","object":"dir/a.txt","generation":"5","live_generation":"9"`

func restoreOverwriteEnvironment(t *testing.T) *environment {
	t.Helper()
	e := storageChangeEnvironment(t, []string{objectsRestoreOverwriteID})
	e.fake.routes[bucketRoute+"/o/dir/a.txt/restore"] = okReply(objectBody("dir/a.txt"))
	return e
}

func TestObjectsRestoreOverwriteSendsOnePostBoundToTheLiveGeneration(t *testing.T) {
	e := restoreOverwriteEnvironment(t)
	out, err := e.invokeConfirmed(objectsRestoreOverwriteID, restoreOverwriteArgs+
		`,"if_metageneration_match":"3","projection":"noAcl","restore_token":"tok","user_project":"`+
		storageProjectNumber+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := decode(t, out); got["name"] != "dir/a.txt" || strings.Contains(out, "secret-material") {
		t.Errorf("result = %s", out)
	}
	api := e.fake.api()
	if len(api) != 2 || api[1].method != "POST" || api[1].body != "" {
		t.Fatalf("requests = %+v", api)
	}
	u, _ := url.Parse(api[1].url)
	q := u.Query()
	if u.EscapedPath() != "/storage/v1/b/example-bucket/o/dir%2Fa.txt/restore" || q.Get("generation") != "5" ||
		q.Get("ifGenerationMatch") != "9" || q.Get("ifMetagenerationMatch") != "3" || q.Get("projection") != "noAcl" ||
		q.Get("restoreToken") != "tok" || q.Get("userProject") != storageProjectNumber {
		t.Errorf("url = %s", u)
	}
	for _, r := range api {
		if strings.Contains(strings.ToLower(r.url), "copysourceacl") {
			t.Errorf("copySourceAcl was sent: %s", r.url)
		}
	}
}

func TestObjectsRestoreOverwriteRefusesBadArgumentsBeforeIO(t *testing.T) {
	for name, args := range map[string]string{
		"no live_generation":    `{"bucket":"example-bucket","object":"x","generation":"5"}`,
		"empty live_generation": `{"bucket":"example-bucket","object":"x","generation":"5","live_generation":""}`,
		"no generation":         `{"bucket":"example-bucket","object":"x","live_generation":"9"}`,
		"bad live_generation":   `{"bucket":"example-bucket","object":"x","generation":"5","live_generation":"a"}`,
		"if_generation_match":   restoreOverwriteArgs + `,"if_generation_match":"1"}`,
		"copySourceAcl":         restoreOverwriteArgs + `,"copySourceAcl":true}`,
		"bad projection":        restoreOverwriteArgs + `,"projection":"other"}`,
	} {
		e := restoreOverwriteEnvironment(t)
		if _, err := e.invokeConfirmed(objectsRestoreOverwriteID, args); err == nil {
			t.Errorf("%s: accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestObjectsRestoreOverwriteRefusesABucketOutsideTheTargetsBeforeTheChange(t *testing.T) {
	e := restoreOverwriteEnvironment(t)
	_, err := e.invokeConfirmed(objectsRestoreOverwriteID,
		`{"bucket":"foreign-bucket","object":"a.txt","generation":"5","live_generation":"9"}`)
	if err == nil {
		t.Fatal("a foreign bucket was accepted")
	}
	for _, r := range e.fake.api() {
		if r.method == "POST" {
			t.Errorf("a change was sent: %+v", r)
		}
	}
}

func TestObjectsRestoreOverwriteChecksTheAnswer(t *testing.T) {
	for name, body := range map[string]string{
		"other name":       objectBody("other.txt"),
		"other bucket":     strings.Replace(objectBody("dir/a.txt"), `"bucket":"example-bucket"`, `"bucket":"other"`, 1),
		"no bucket":        strings.Replace(objectBody("dir/a.txt"), `"bucket":"example-bucket",`, "", 1),
		"no generation":    strings.Replace(objectBody("dir/a.txt"), `"generation":"1700000000000000",`, "", 1),
		"replaced version": strings.Replace(objectBody("dir/a.txt"), `"generation":"1700000000000000"`, `"generation":"9"`, 1),
	} {
		e := restoreOverwriteEnvironment(t)
		e.fake.routes[bucketRoute+"/o/dir/a.txt/restore"] = okReply(body)
		_, err := e.invokeConfirmed(objectsRestoreOverwriteID, restoreOverwriteArgs+`}`)
		if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestObjectsRestoreOverwriteReports412And5xxWithoutRetry(t *testing.T) {
	for _, status := range []int{412, 503} {
		e := restoreOverwriteEnvironment(t)
		e.fake.routes[bucketRoute+"/o/dir/a.txt/restore"] = reply{status: status, body: errorObject(status, "FAILED_PRECONDITION", "")}
		_, err := e.invokeConfirmed(objectsRestoreOverwriteID, restoreOverwriteArgs+`}`)
		if err == nil || strings.Contains(err.Error(), detailCanary) || len(e.fake.api()) != 2 {
			t.Errorf("status %d: err = %v, requests = %d", status, err, len(e.fake.api()))
		}
		if status == 503 && !strings.Contains(err.Error(), "outcome of the change is unknown") {
			t.Errorf("err = %v", err)
		}
	}
}

func TestObjectsRestoreOverwriteIsListedOnlyAndNeedsConfirmation(t *testing.T) {
	e := storageChangeEnvironment(t, nil)
	if offers(t, e, objectsRestoreOverwriteID) {
		t.Error("offered without a tools list")
	}
	if _, err := e.invokeConfirmed(objectsRestoreOverwriteID, restoreOverwriteArgs+`}`); err == nil {
		t.Error("ran without a tools list")
	}
	e = restoreOverwriteEnvironment(t)
	if _, err := e.invoke(objectsRestoreOverwriteID, restoreOverwriteArgs+`}`); err == nil {
		t.Error("ran without confirmation")
	}
	noIO(t, e, "unconfirmed")
	d, _, _ := e.reg.Lookup(objectsRestoreOverwriteID)
	if !d.RequiresToolAllowList || string(d.Risk.Effect) != "update" || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld {
		t.Errorf("descriptor = %+v", d.Risk)
	}
	if got := tokenMetadata.Scopes[objectsRestoreOverwriteID]; !slices.Equal(got, []string{scopeStorageFullControl}) {
		t.Errorf("scopes = %v", got)
	}
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, objectsRestoreOverwriteID) {
			t.Errorf("profile %s holds the tool", profile.ID)
		}
	}
	s := e.reg.ConfirmationSummary(objectsRestoreOverwriteID)(json.RawMessage(restoreOverwriteArgs + `}`))
	for _, part := range []string{"example-bucket", "dir/a.txt", "generation 5", "generation 9", "noncurrent", "soft-deleted"} {
		if !strings.Contains(s, part) {
			t.Errorf("summary %q lacks %q", s, part)
		}
	}
}

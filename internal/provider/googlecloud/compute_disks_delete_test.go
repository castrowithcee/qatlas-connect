package googlecloud

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	diskDeleteArgs     = `{"project_id":"my-project-123","zone":"us-central1-a","disk":"data-disk-a"`
	diskDeleteExpected = `,"expected_size_gb":100,"expected_users":["vm-a"]`
)

// diskDeleteRoutes answers the read of the disk with body and the DELETE with deleted.
func diskDeleteRoutes(e *environment, body string, deleted reply) {
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != diskWritePath {
			return nil
		}
		if req.Method == http.MethodGet {
			return &reply{status: 200, body: body}
		}
		return &deleted
	}
}

func offers(t *testing.T, e *environment, id string) bool {
	t.Helper()
	listed, err := e.core.Tools(application.SearchRequest{Connection: "work", Provider: Provider, Query: id})
	if err != nil {
		t.Fatalf("Tools() = %v", err)
	}
	return slices.ContainsFunc(listed.Tools, func(hit application.SearchHit) bool { return hit.ID == id })
}

func TestDisksDeleteIsOnlyOfferedWhereTheToolsListNamesIt(t *testing.T) {
	for name, tools := range map[string][]string{
		"effects only":       nil,
		"profile alone":      {projectsGetID, imagesInsertID, snapshotsDeleteID, imagesDeleteID},
		"disk writes listed": {projectsGetID, disksInsertID, disksResizeID, disksCreateSnapshotID},
		"namespace prefix":   {"google.cloud.disks.*"},
	} {
		e := createEnvironment(t, insertTargets, tools)
		if offers(t, e, disksDeleteID) {
			t.Errorf("%s: disks.delete is offered", name)
		}
		if _, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`}`); err == nil {
			t.Errorf("%s: disks.delete was reachable", name)
		}
		noIO(t, e, name)
	}
	e := createEnvironment(t, insertTargets, []string{disksDeleteID})
	if !offers(t, e, disksDeleteID) {
		t.Fatal("disks.delete is not offered where the tools list names it")
	}
	d, _, _ := e.reg.Lookup(disksDeleteID)
	if !d.RequiresToolAllowList || string(d.Risk.Effect) != "delete" || d.Risk.Confirmation != "required" ||
		!d.Risk.OpenWorld || string(d.Risk.Idempotency) != "idempotent" {
		t.Errorf("descriptor = %+v", d.Risk)
	}
	if got := tokenMetadata.Scopes[disksDeleteID]; !slices.Equal(got, []string{scopeCompute}) {
		t.Errorf("scopes = %v", got)
	}
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		if slices.Contains(profile.Tools, disksDeleteID) {
			t.Errorf("profile %s holds %s", profile.ID, disksDeleteID)
		}
	}
}

func TestDisksDeleteReadsTheDiskAndDeletesItWhenSizeAndUsersMatch(t *testing.T) {
	e := createEnvironment(t, insertTargets, []string{disksDeleteID})
	diskDeleteRoutes(e, diskBody("data-disk-a"), okReply(zonalOperationBody("delete", "RUNNING", "")))
	out, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`,"request_id":"`+deleteRequestID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	api := e.fake.api()
	if len(api) != 2 || api[0].method != http.MethodGet || api[1].method != http.MethodDelete || api[1].body != "" ||
		api[1].url != "https://compute.googleapis.com"+diskWritePath+"?requestId="+deleteRequestID {
		t.Fatalf("requests = %+v", api)
	}
	if got := decode(t, out); got["scope"] != "zone" || got["zone"] != "us-central1-a" || got["request_id"] != deleteRequestID {
		t.Errorf("result = %s", out)
	}
	// A disk that no instance uses is confirmed with an empty list; the order of users does not matter.
	e = createEnvironment(t, insertTargets, []string{disksDeleteID})
	unused := strings.Replace(diskBody("data-disk-a"), `"users":["`+diskZoneURL+`/instances/vm-a"],`, "", 1)
	diskDeleteRoutes(e, unused, okReply(zonalOperationBody("delete", "RUNNING", "")))
	if _, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+`,"expected_size_gb":100,"expected_users":[]}`); err != nil {
		t.Fatal(err)
	}
	e = createEnvironment(t, insertTargets, []string{disksDeleteID})
	shared := strings.Replace(diskBody("data-disk-a"), `"users":["`+diskZoneURL+`/instances/vm-a"]`,
		`"users":["`+diskZoneURL+`/instances/vm-a","`+diskZoneURL+`/instances/vm-b"]`, 1)
	diskDeleteRoutes(e, shared, okReply(zonalOperationBody("delete", "RUNNING", "")))
	if _, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+`,"expected_size_gb":100,"expected_users":["vm-b","vm-a"]}`); err != nil {
		t.Fatal(err)
	}
}

func TestDisksDeleteSendsNoDeleteWhenTheDiskNoLongerMatches(t *testing.T) {
	unused := strings.Replace(diskBody("data-disk-a"), `"users":["`+diskZoneURL+`/instances/vm-a"],`, "", 1)
	for name, c := range map[string]struct{ body, expected string }{
		"other size":         {diskBody("data-disk-a"), `,"expected_size_gb":200,"expected_users":["vm-a"]`},
		"attached meanwhile": {diskBody("data-disk-a"), `,"expected_size_gb":100,"expected_users":[]`},
		"other instance":     {diskBody("data-disk-a"), `,"expected_size_gb":100,"expected_users":["vm-b"]`},
		"detached meanwhile": {unused, `,"expected_size_gb":100,"expected_users":["vm-a"]`},
	} {
		e := createEnvironment(t, insertTargets, []string{disksDeleteID})
		diskDeleteRoutes(e, c.body, okReply(zonalOperationBody("delete", "RUNNING", "")))
		_, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+c.expected+`}`)
		var invalid *provider.InvalidRequestError
		if err == nil || !asInvalid(err, &invalid) || !strings.Contains(err.Error(), "nothing was deleted") {
			t.Errorf("%s: err = %v", name, err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{http.MethodGet}) {
			t.Errorf("%s: methods = %v, want only the read", name, got)
		}
	}
	// An answer for another disk, without a usable size, or with a user outside the zone leaves nothing to compare.
	for name, body := range map[string]string{
		"other disk": diskBody("disk-other"),
		"no size":    strings.Replace(diskBody("data-disk-a"), `"sizeGb":"100",`, "", 1),
		"other zone": strings.Replace(diskBody("data-disk-a"), diskZoneURL+`/instances/vm-a`,
			"https://www.googleapis.com/compute/v1/projects/my-project-123/zones/us-east1-b/instances/vm-a", 1),
	} {
		e := createEnvironment(t, insertTargets, []string{disksDeleteID})
		diskDeleteRoutes(e, body, okReply(zonalOperationBody("delete", "RUNNING", "")))
		if _, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("%s: class = %q, err = %v", name, classOf(err), err)
		}
		if got := methodsOf(e); !slices.Equal(got, []string{http.MethodGet}) {
			t.Errorf("%s: methods = %v", name, got)
		}
	}
}

func TestDisksDeleteReportsADiskInUseAsGoogleSendsIt(t *testing.T) {
	e := createEnvironment(t, insertTargets, []string{disksDeleteID})
	diskDeleteRoutes(e, diskBody("data-disk-a"), reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION",
		"")})
	_, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`}`)
	if err == nil || strings.Contains(err.Error(), detailCanary) {
		t.Fatalf("err = %v", err)
	}
	if got := methodsOf(e); !slices.Equal(got, []string{http.MethodGet, http.MethodDelete}) {
		t.Errorf("methods = %v", got)
	}
}

func TestDisksDeleteRepeatsTheSameRequestID(t *testing.T) {
	e := createEnvironment(t, insertTargets, []string{disksDeleteID})
	diskDeleteRoutes(e, diskBody("data-disk-a"), okReply(zonalOperationBody("delete", "DONE", "")))
	for i := 0; i < 2; i++ {
		if _, err := e.invokeConfirmed(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`,"request_id":"`+
			deleteRequestID+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	api := e.fake.api()
	if len(api) != 4 || api[1].url != api[3].url || api[1].method != http.MethodDelete {
		t.Errorf("requests = %+v", api)
	}
}

func TestDisksDeleteRefusesBeforeAnyIOAndWithoutConfirmation(t *testing.T) {
	for _, args := range []string{
		diskDeleteArgs + `}`,
		diskDeleteArgs + `,"expected_size_gb":100}`,
		diskDeleteArgs + `,"expected_users":[]}`,
		diskDeleteArgs + `,"expected_size_gb":0,"expected_users":[]}`,
		diskDeleteArgs + `,"expected_size_gb":100,"expected_users":["vm-a","vm-a"]}`,
		diskDeleteArgs + `,"expected_size_gb":100,"expected_users":["` + diskZoneURL + `/instances/vm-a"]}`,
		diskDeleteArgs + diskDeleteExpected + `,"request_id":"nope"}`,
		`{"project_id":"my-project-123","zone":"us-central1-a","disk":"../x"` + diskDeleteExpected + `}`,
		`{"project_id":"my-project-123","zone":"us-central1/a","disk":"data-disk-a"` + diskDeleteExpected + `}`,
		`{"project_id":"example-other-9","zone":"us-central1-a","disk":"data-disk-a"` + diskDeleteExpected + `}`,
	} {
		e := createEnvironment(t, insertTargets, []string{disksDeleteID})
		if _, err := e.invokeConfirmed(disksDeleteID, args); err == nil {
			t.Errorf("accepted %s", args)
		}
		noIO(t, e, args)
	}
	e := createEnvironment(t, insertTargets, []string{disksDeleteID})
	if _, err := e.invoke(disksDeleteID, diskDeleteArgs+diskDeleteExpected+`}`); err == nil {
		t.Error("ran without confirmation")
	}
	noIO(t, e, "no confirmation")
	nilUsers := diskDeleteArguments{ProjectID: "my-project-123", Zone: "us-central1-a", Disk: "d",
		ExpectedSizeGb: new(int64)}
	*nilUsers.ExpectedSizeGb = 10
	if nilUsers.check() == nil {
		t.Error("check accepted arguments without expected_users")
	}
}

func TestDisksDeleteSummaryNamesSizeAndUsers(t *testing.T) {
	e := newEnvironment(t, nil)
	summary := e.reg.ConfirmationSummary(disksDeleteID)
	s := summary(json.RawMessage(diskDeleteArgs + `,"expected_size_gb":250,"expected_users":["vm-a","vm-b"]}`))
	for _, part := range []string{"disk data-disk-a", "us-central1-a", "my-project-123", "250 GB", "vm-a, vm-b",
		"cannot be restored"} {
		if !strings.Contains(s, part) {
			t.Errorf("summary %q lacks %q", s, part)
		}
	}
	if s := summary(json.RawMessage(diskDeleteArgs + `,"expected_size_gb":10,"expected_users":[]}`)); !strings.Contains(s, "no instance") {
		t.Errorf("summary = %q", s)
	}
	many := make([]string, shownDiskUsers+3)
	for i := range many {
		many[i] = `"vm-` + string(rune('a'+i)) + `"`
	}
	if s := summary(json.RawMessage(diskDeleteArgs + `,"expected_size_gb":10,"expected_users":[` +
		strings.Join(many, ",") + `]}`)); !strings.Contains(s, "and 3 more") {
		t.Errorf("summary = %q", s)
	}
}

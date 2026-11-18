package googlecloud

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	delProjectArgs = `{"project_id":"my-project-123","expected_display_name":"My Project",` +
		`"expected_parent":"folders/2001"}`
	delFolderArgs = `{"folder_id":"2001","expected_display_name":"Team","expected_parent":"organizations/9"}`
	delFolderBody = `{"name":"folders/2001","parent":"organizations/9","displayName":"Team","state":"ACTIVE"}`
	delOperation  = `{"name":"operations/cp.7","metadata":{"@type":"type.googleapis.com/x.Metadata"}}`
)

// delServe answers every read with body and every other request with write.
func delServe(e *environment, body string, write reply) {
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.Method == http.MethodGet {
			r := okReply(body)
			return &r
		}
		return &write
	}
}

func delWrites(e *environment) []seen {
	var out []seen
	for _, r := range e.fake.api() {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func TestResourceManagerDeletesAreMissingWithoutTheToolsList(t *testing.T) {
	ids := []string{projectsDeleteID, foldersDeleteID}
	for name, tools := range map[string][]string{"effects only": nil, "other tool named": {projectsGetID}} {
		e := revokeEnvironment(t, nil, tools)
		searched, err := e.core.Search(application.SearchRequest{Provider: Provider, Connection: "work", Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		listed, err := e.core.Tools(application.SearchRequest{Provider: Provider, Connection: "work", Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range ids {
			for _, hit := range searched.Operations {
				if hit.ID == id {
					t.Errorf("%s: search offers %s", name, id)
				}
			}
			for _, tool := range listed.Tools {
				if tool.ID == id {
					t.Errorf("%s: tool index offers %s", name, id)
				}
			}
			var unsupported *capability.UnsupportedError
			if _, err := e.core.Describe(application.DescribeRequest{Operation: id, Connection: "work"}); err == nil ||
				!asUnsupported(err, &unsupported) {
				t.Errorf("%s: describe %s = %v", name, id, err)
			}
		}
		if _, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs); err == nil {
			t.Errorf("%s: projects.delete ran", name)
		}
		if _, err := e.invokeConfirmed(foldersDeleteID, delFolderArgs); err == nil {
			t.Errorf("%s: folders.delete ran", name)
		}
		noIO(t, e, name)
	}
	e := revokeEnvironment(t, nil, ids)
	for _, id := range ids {
		if _, err := e.core.Describe(application.DescribeRequest{Operation: id, Connection: "work"}); err != nil {
			t.Errorf("describe %s with the tool listed = %v", id, err)
		}
	}
}

func asUnsupported(err error, target **capability.UnsupportedError) bool {
	u, ok := err.(*capability.UnsupportedError)
	*target = u
	return ok
}

func TestResourceManagerDeletesAreListedOnlyDeletesInNoProfile(t *testing.T) {
	e := newEnvironment(t, nil)
	metadata, _ := e.reg.ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range []string{projectsDeleteID, foldersDeleteID} {
			if slices.Contains(profile.Tools, id) {
				t.Errorf("profile %s holds %s", profile.ID, id)
			}
		}
	}
	for _, id := range []string{projectsDeleteID, foldersDeleteID} {
		d, _, ok := e.reg.Lookup(id)
		if !ok || !d.RequiresToolAllowList || string(d.Risk.Effect) != "delete" || d.Risk.Confirmation != "required" ||
			!d.Risk.OpenWorld || e.reg.ConfirmationSummary(id) == nil {
			t.Errorf("descriptor of %s = %+v", id, d)
		}
		if scopes := tokenMetadata.Scopes[id]; len(scopes) != 1 || scopes[0] != scopeCloudPlatform {
			t.Errorf("%s scopes = %v", id, scopes)
		}
	}
}

func TestProjectsDeleteReturnsTheOperationWithoutWaiting(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{projectsDeleteID, operationsGetID})
	delServe(e, projectBody, okReply(delOperation))
	out, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	writes := delWrites(e)
	if got["service"] != "resource-manager" || got["name"] != "operations/cp.7" || got["done"] != false {
		t.Errorf("result = %s", out)
	}
	if len(writes) != 1 || writes[0].method != http.MethodDelete || writes[0].body != "" ||
		writes[0].url != "https://cloudresourcemanager.googleapis.com/v3/projects/415104041262" {
		t.Fatalf("writes = %+v", writes)
	}
	e.fake.hook = nil
	e.fake.routes["/v3/operations/cp.7"] = okReply(`{"name":"operations/cp.7","done":true,` +
		`"response":{"@type":"type.googleapis.com/google.cloud.resourcemanager.v3.Project","state":"DELETE_REQUESTED"}}`)
	out, err = e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/cp.7"}`)
	if err != nil || decode(t, out)["done"] != true {
		t.Errorf("operation = %s, err = %v", out, err)
	}
}

func TestFoldersDeleteReturnsTheOperationWithoutWaiting(t *testing.T) {
	e := revokeEnvironment(t, []string{"folders/2001"}, []string{foldersDeleteID})
	delServe(e, delFolderBody, okReply(delOperation))
	out, err := e.invokeConfirmed(foldersDeleteID, delFolderArgs)
	if err != nil {
		t.Fatal(err)
	}
	writes := delWrites(e)
	if decode(t, out)["done"] != false || len(writes) != 1 || writes[0].method != http.MethodDelete ||
		writes[0].body != "" || writes[0].url != "https://cloudresourcemanager.googleapis.com/v3/folders/2001" {
		t.Errorf("out = %s, writes = %+v", out, writes)
	}
}

func TestResourceManagerDeletesPassGoogleRefusalsThroughWithoutRetry(t *testing.T) {
	refusal := reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	for name, c := range map[string]struct {
		id, args, body string
		targets        []string
	}{
		"project with a lien": {projectsDeleteID, delProjectArgs, projectBody, nil},
		"folder not empty":    {foldersDeleteID, delFolderArgs, delFolderBody, nil},
	} {
		e := revokeEnvironment(t, c.targets, []string{c.id})
		delServe(e, c.body, refusal)
		_, err := e.invokeConfirmed(c.id, c.args)
		if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) ||
			len(delWrites(e)) != 1 {
			t.Errorf("%s: err = %v, writes = %d", name, err, len(delWrites(e)))
		}
	}
}

func TestResourceManagerDeletesRefuseAnObjectThatIsNotActiveWithoutWriting(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{projectsDeleteID})
	delServe(e, strings.Replace(projectBody, `"ACTIVE"`, `"DELETE_REQUESTED"`, 1), okReply(delOperation))
	if _, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs); err == nil || len(delWrites(e)) != 0 {
		t.Errorf("project: err = %v, writes = %d", err, len(delWrites(e)))
	}
	e = revokeEnvironment(t, nil, []string{foldersDeleteID})
	delServe(e, strings.Replace(delFolderBody, `"ACTIVE"`, `"DELETE_REQUESTED"`, 1), okReply(delOperation))
	if _, err := e.invokeConfirmed(foldersDeleteID, delFolderArgs); err == nil || len(delWrites(e)) != 0 {
		t.Errorf("folder: err = %v, writes = %d", err, len(delWrites(e)))
	}
}

func TestResourceManagerDeletesRefuseAnotherNameOrParentWithoutWriting(t *testing.T) {
	for name, c := range map[string]struct{ id, args, body string }{
		"project name":          {projectsDeleteID, strings.Replace(delProjectArgs, "My Project", "Other Name", 1), projectBody},
		"project parent":        {projectsDeleteID, strings.Replace(delProjectArgs, "folders/2001", "folders/2002", 1), projectBody},
		"project parent absent": {projectsDeleteID, `{"project_id":"my-project-123","expected_display_name":"My Project"}`, projectBody},
		"project has no parent": {projectsDeleteID, delProjectArgs,
			strings.Replace(projectBody, `"parent":"folders/2001",`, "", 1)},
		"folder name":   {foldersDeleteID, strings.Replace(delFolderArgs, "Team", "Other", 1), delFolderBody},
		"folder parent": {foldersDeleteID, strings.Replace(delFolderArgs, "organizations/9", "folders/7", 1), delFolderBody},
	} {
		e := revokeEnvironment(t, nil, []string{c.id})
		delServe(e, c.body, okReply(delOperation))
		_, err := e.invokeConfirmed(c.id, c.args)
		if err == nil || len(delWrites(e)) != 0 || strings.Contains(err.Error(), "Other") {
			t.Errorf("%s: err = %v, writes = %d", name, err, len(delWrites(e)))
		}
	}
	// A project without a parent is deleted with expected_parent omitted.
	e := revokeEnvironment(t, nil, []string{projectsDeleteID})
	delServe(e, strings.Replace(projectBody, `"parent":"folders/2001",`, "", 1), okReply(delOperation))
	if _, err := e.invokeConfirmed(projectsDeleteID,
		`{"project_id":"my-project-123","expected_display_name":"My Project"}`); err != nil || len(delWrites(e)) != 1 {
		t.Errorf("parentless project: err = %v, writes = %d", err, len(delWrites(e)))
	}
}

func TestProjectsDeleteNeedsTheProjectItselfAsTarget(t *testing.T) {
	// Project in a folder target, but not a target itself: decided before any request.
	e := revokeEnvironment(t, []string{"folders/2001", "projects/other-project-1"}, []string{projectsDeleteID})
	if _, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs); err == nil {
		t.Error("project below a folder target accepted")
	}
	noIO(t, e, "project below folder target")
	// A target in the other spelling needs a read, which then refuses without a write.
	e = revokeEnvironment(t, []string{"folders/2001", "projects/999"}, []string{projectsDeleteID})
	delServe(e, projectBody, okReply(delOperation))
	if _, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs); err == nil || len(delWrites(e)) != 0 ||
		len(e.fake.api()) != 1 {
		t.Errorf("err = %v, requests = %d", err, len(e.fake.api()))
	}
	// A project target by number admits an argument by ID after the read.
	e = revokeEnvironment(t, []string{"projects/415104041262"}, []string{projectsDeleteID})
	delServe(e, projectBody, okReply(delOperation))
	if _, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs); err != nil || len(delWrites(e)) != 1 ||
		len(e.fake.api()) != 2 {
		t.Errorf("by number: err = %v, requests = %d", err, len(e.fake.api()))
	}
	// An argument by number is admitted by a target by ID after the read.
	e = revokeEnvironment(t, []string{"projects/my-project-123"}, []string{projectsDeleteID})
	delServe(e, projectBody, okReply(delOperation))
	if _, err := e.invokeConfirmed(projectsDeleteID,
		strings.Replace(delProjectArgs, "my-project-123", "415104041262", 1)); err != nil || len(delWrites(e)) != 1 {
		t.Errorf("by ID: err = %v, writes = %d", err, len(delWrites(e)))
	}
}

func TestFoldersDeleteNeedsTheFolderItselfAsTarget(t *testing.T) {
	for name, targets := range map[string][]string{
		"folder below a folder target": {"folders/1999", "organizations/9"},
		"folder below an organization": {"organizations/9", "folders/3003"},
		"project target only":          {"projects/my-project-123", "folders/3003"},
	} {
		e := revokeEnvironment(t, targets, []string{foldersDeleteID})
		if _, err := e.invokeConfirmed(foldersDeleteID, delFolderArgs); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestResourceManagerDeletesWorkWithoutTargets(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{projectsDeleteID, foldersDeleteID})
	delServe(e, projectBody, okReply(delOperation))
	if _, err := e.invokeConfirmed(projectsDeleteID, delProjectArgs); err != nil {
		t.Error(err)
	}
	delServe(e, delFolderBody, okReply(delOperation))
	if _, err := e.invokeConfirmed(foldersDeleteID, delFolderArgs); err != nil {
		t.Error(err)
	}
}

func TestResourceManagerDeletesNeedConfirmationAndNameWhatTheyDo(t *testing.T) {
	e := revokeEnvironment(t, nil, []string{projectsDeleteID, foldersDeleteID})
	for id, args := range map[string]string{projectsDeleteID: delProjectArgs, foldersDeleteID: delFolderArgs} {
		if _, err := e.invoke(id, args); err == nil {
			t.Errorf("%s ran without confirmation", id)
		}
	}
	noIO(t, e, "unconfirmed")
	project := e.reg.ConfirmationSummary(projectsDeleteID)(json.RawMessage(delProjectArgs))
	for _, want := range []string{"my-project-123", "My Project", "folders/2001", "ACTIVE", "stop working", "billing",
		"projects.undelete"} {
		if !strings.Contains(project, want) {
			t.Errorf("project confirmation lacks %q: %s", want, project)
		}
	}
	folder := e.reg.ConfirmationSummary(foldersDeleteID)(json.RawMessage(delFolderArgs))
	for _, want := range []string{"folders/2001", "Team", "organizations/9", "ACTIVE", "empty", "folders.undelete"} {
		if !strings.Contains(folder, want) {
			t.Errorf("folder confirmation lacks %q: %s", want, folder)
		}
	}
	long := strings.Repeat("x", 100)
	worst := `{"project_id":"` + strings.Repeat("a", 30) + `","folder_id":"` + strings.Repeat("9", 20) +
		`","expected_display_name":"` + long + `","expected_parent":"folders/` + strings.Repeat("9", 20) + `"}`
	for _, id := range []string{projectsDeleteID, foldersDeleteID} {
		if s := e.reg.ConfirmationSummary(id)(json.RawMessage(worst)); !summaryFits(s) || strings.Contains(s, "refused") {
			t.Errorf("%s worst case does not fit: %d", id, len(s))
		}
	}
}

func TestResourceManagerDeletesRefuseBadArgumentsBeforeIO(t *testing.T) {
	for name, c := range map[string]struct{ id, args string }{
		"project path":        {projectsDeleteID, `{"project_id":"projects/x","expected_display_name":"A","expected_parent":"folders/1"}`},
		"project no name":     {projectsDeleteID, `{"project_id":"my-project-123","expected_parent":"folders/1"}`},
		"project empty name":  {projectsDeleteID, `{"project_id":"my-project-123","expected_display_name":""}`},
		"project bad parent":  {projectsDeleteID, `{"project_id":"my-project-123","expected_display_name":"A","expected_parent":"projects/1"}`},
		"project extra":       {projectsDeleteID, `{"project_id":"my-project-123","expected_display_name":"A","force":true}`},
		"folder path":         {foldersDeleteID, `{"folder_id":"1/x","expected_display_name":"A","expected_parent":"organizations/9"}`},
		"folder no parent":    {foldersDeleteID, `{"folder_id":"2001","expected_display_name":"A"}`},
		"folder no name":      {foldersDeleteID, `{"folder_id":"2001","expected_parent":"organizations/9"}`},
		"folder long name":    {foldersDeleteID, `{"folder_id":"2001","expected_display_name":"` + strings.Repeat("x", 101) + `","expected_parent":"organizations/9"}`},
		"folder extra member": {foldersDeleteID, `{"folder_id":"2001","expected_display_name":"A","expected_parent":"organizations/9","x":1}`},
	} {
		e := revokeEnvironment(t, nil, []string{c.id})
		if _, err := e.invokeConfirmed(c.id, c.args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestResourceManagerDeletesAreOfferedOnlyWhereTheTargetsServeThem(t *testing.T) {
	cases := []struct {
		targets []string
		tool    string
		offered bool
	}{
		{nil, projectsDeleteID, true},
		{nil, foldersDeleteID, true},
		{[]string{"projects/my-project-123"}, projectsDeleteID, true},
		{[]string{"projects/my-project-123"}, foldersDeleteID, false},
		{[]string{"folders/2001"}, foldersDeleteID, true},
		{[]string{"folders/2001"}, projectsDeleteID, false},
		{[]string{"organizations/9"}, projectsDeleteID, false},
		{[]string{"organizations/9"}, foldersDeleteID, false},
		{[]string{"organizations/9", "projects/p-12345"}, projectsDeleteID, true},
		{[]string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"}, projectsDeleteID, false},
	}
	for _, c := range cases {
		if offered, _ := targetReaches(c.tool, c.targets); offered != c.offered {
			t.Errorf("targets %v, %s: offered = %v", c.targets, c.tool, offered)
		}
	}
}

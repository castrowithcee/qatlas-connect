package googlecloud

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	rmProjectPath = "/v3/projects/my-project-123"
	rmNumberPath  = "/v3/projects/415104041262"
	rmFolderBody  = `{"name":"folders/2001","parent":"organizations/9","displayName":"Team","state":"ACTIVE"}`
	rmOperation   = `{"name":"operations/cp.1","metadata":{"@type":"type.googleapis.com/x.ProjectCreationStatus"}}`
	createArgs    = `{"project_id":"my-new-project-123","display_name":"My New Project","parent":"folders/2001",` +
		`"labels":{"env":"test"}}`
)

func rmWrites(e *environment) []seen {
	var out []seen
	for _, r := range e.fake.api() {
		if r.method != "GET" {
			out = append(out, r)
		}
	}
	return out
}

func TestProjectsCreateReturnsTheOperationAndOperationsGetReadsIt(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes["/v3/projects"] = okReply(rmOperation)
	e.fake.routes["/v3/operations/cp.1"] = okReply(`{"name":"operations/cp.1","done":true,` +
		`"response":{"@type":"type.googleapis.com/google.cloud.resourcemanager.v3.Project","projectId":"my-new-project-123"}}`)
	out, err := e.invokeConfirmed(projectsCreateID, createArgs)
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, out)
	api := e.fake.api()
	if got["service"] != "resource-manager" || got["name"] != "operations/cp.1" || got["done"] != false {
		t.Errorf("result = %s", out)
	}
	if len(api) != 1 || api[0].method != "POST" || api[0].url != "https://cloudresourcemanager.googleapis.com/v3/projects" ||
		api[0].body != `{"projectId":"my-new-project-123","displayName":"My New Project","parent":"folders/2001",`+
			`"labels":{"env":"test"}}` {
		t.Fatalf("requests = %+v", api)
	}
	out, err = e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/cp.1"}`)
	if err != nil {
		t.Fatal(err)
	}
	got = decode(t, out)
	if got["done"] != true || got["response"] == nil || len(e.fake.api()) != 2 {
		t.Errorf("result = %s", out)
	}
}

func TestProjectsCreateOfADoneOperationWithAnErrorIsAnError(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes["/v3/projects"] = okReply(`{"name":"operations/cp.2","done":true,"error":{"code":9,"message":"` +
		detailCanary + `"}}`)
	_, err := e.invokeConfirmed(projectsCreateID, createArgs)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestProjectsCreateWithATakenIDIsAConflictWithoutRetry(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes["/v3/projects"] = reply{status: 409, body: errorObject(409, "ALREADY_EXISTS", "")}
	_, err := e.invokeConfirmed(projectsCreateID, createArgs)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conflict") ||
		strings.Contains(err.Error(), detailCanary) || len(rmWrites(e)) != 1 {
		t.Fatalf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestProjectsCreateNeedsConfirmationAndSaysWhatItDoes(t *testing.T) {
	e := writeEnvironment(t, nil)
	if _, err := e.invoke(projectsCreateID, createArgs); err == nil {
		t.Fatal("create ran without confirmation")
	}
	summary := e.reg.ConfirmationSummary(projectsCreateID)(json.RawMessage(createArgs))
	for _, want := range []string{"my-new-project-123", "My New Project", "folders/2001", "env=test", "no billing account"} {
		if !strings.Contains(summary, want) {
			t.Errorf("confirmation lacks %q: %s", want, summary)
		}
	}
	noIO(t, e, "unconfirmed create")
}

func TestProjectsCreateRefusesBadArgumentsBeforeIO(t *testing.T) {
	long := strings.Repeat("a", 63)
	for name, args := range map[string]string{
		"no parent":        `{"project_id":"my-new-project-123"}`,
		"project parent":   `{"project_id":"my-new-project-123","parent":"projects/1"}`,
		"short ID":         `{"project_id":"abc","parent":"folders/2001"}`,
		"uppercase ID":     `{"project_id":"My-New-Project","parent":"folders/2001"}`,
		"trailing hyphen":  `{"project_id":"my-new-project-","parent":"folders/2001"}`,
		"short name":       `{"project_id":"my-new-project-123","display_name":"abc","parent":"folders/2001"}`,
		"bad name chars":   `{"project_id":"my-new-project-123","display_name":"My <b>","parent":"folders/2001"}`,
		"uppercase label":  `{"project_id":"my-new-project-123","parent":"folders/2001","labels":{"Env":"x"}}`,
		"label with dot":   `{"project_id":"my-new-project-123","parent":"folders/2001","labels":{"a.b":"x"}}`,
		"long label value": `{"project_id":"my-new-project-123","parent":"folders/2001","labels":{"a":"` + long + `a"}}`,
		"extra member":     `{"project_id":"my-new-project-123","parent":"folders/2001","tags":{"a":"b"}}`,
	} {
		e := writeEnvironment(t, nil)
		if _, err := e.invokeConfirmed(projectsCreateID, args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestProjectsCreateRefusesAConfirmationThatWouldBeCutBeforeIO(t *testing.T) {
	labels := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		labels = append(labels, `"label-number-`+strings.Repeat("x", 5)+string(rune('a'+i%26))+string(rune('a'+i/26))+`":"`+
			strings.Repeat("v", 40)+`"`)
	}
	args := `{"project_id":"my-new-project-123","parent":"folders/2001","labels":{` + strings.Join(labels, ",") + `}}`
	e := writeEnvironment(t, nil)
	_, err := e.invokeConfirmed(projectsCreateID, args)
	if err == nil || !strings.Contains(err.Error(), "500 characters") {
		t.Fatalf("err = %v", err)
	}
	noIO(t, e, "long confirmation")
	// The unconfirmed call already says that it will be refused.
	e = writeEnvironment(t, nil)
	if summary := e.reg.ConfirmationSummary(projectsCreateID)(json.RawMessage(args)); !strings.Contains(summary, "will be refused") ||
		!summaryFits(summary) {
		t.Errorf("summary = %s", summary)
	}
	noIO(t, e, "long confirmation, unconfirmed")
}

func TestProjectsCreateBindsTheParentToTheTargets(t *testing.T) {
	create := func(parent string) string {
		return `{"project_id":"my-new-project-123","parent":"` + parent + `"}`
	}
	// Refused before any request, also on a connection without targets for an organization.
	for name, c := range map[string]struct {
		targets []string
		parent  string
	}{
		"organization without targets":      {nil, "organizations/9"},
		"other organization":                {[]string{"organizations/8"}, "organizations/9"},
		"organization with a folder target": {[]string{"folders/2001"}, "organizations/9"},
		"project target only":               {[]string{"projects/my-project-123"}, "folders/2001"},
		"billing target only":               {[]string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"}, "folders/2001"},
	} {
		e := writeEnvironment(t, c.targets)
		if _, err := e.invokeConfirmed(projectsCreateID, create(c.parent)); err == nil {
			t.Errorf("%s: accepted", name)
		}
		noIO(t, e, name)
	}
	// Admitted without a read.
	for name, c := range map[string]struct {
		targets []string
		parent  string
	}{
		"folder target":       {[]string{"folders/2001"}, "folders/2001"},
		"organization target": {[]string{"organizations/9"}, "organizations/9"},
		"no targets, folder":  {nil, "folders/2001"},
	} {
		e := writeEnvironment(t, c.targets)
		e.fake.routes["/v3/projects"] = okReply(rmOperation)
		if _, err := e.invokeConfirmed(projectsCreateID, create(c.parent)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if api := e.fake.api(); len(api) != 1 || api[0].method != "POST" {
			t.Errorf("%s: requests = %+v", name, api)
		}
	}
	// A folder below the organization target is proved by reading it.
	e := writeEnvironment(t, []string{"organizations/9"})
	e.fake.routes["/v3/folders/2001"] = okReply(rmFolderBody)
	e.fake.routes["/v3/projects"] = okReply(rmOperation)
	if _, err := e.invokeConfirmed(projectsCreateID, create("folders/2001")); err != nil {
		t.Fatal(err)
	}
	if api := e.fake.api(); len(api) != 2 || api[0].method != "GET" || api[1].method != "POST" {
		t.Errorf("requests = %+v", api)
	}
	// A folder below another organization is refused after the read, and nothing is written.
	e = writeEnvironment(t, []string{"organizations/8"})
	e.fake.routes["/v3/folders/2001"] = okReply(rmFolderBody)
	if _, err := e.invokeConfirmed(projectsCreateID, create("folders/2001")); err == nil || len(rmWrites(e)) != 0 {
		t.Errorf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestProjectsPatchSendsOnlyTheSetMembersAsTheMask(t *testing.T) {
	for name, c := range map[string]struct{ args, query, body string }{
		"name and labels": {`{"project_id":"my-project-123","display_name":"New Name","labels":{"env":"prod"}}`,
			"updateMask=displayName%2Clabels", `{"displayName":"New Name","labels":{"env":"prod"}}`},
		"name only": {`{"project_id":"my-project-123","display_name":"New Name"}`, "updateMask=displayName",
			`{"displayName":"New Name"}`},
		"clear labels": {`{"project_id":"my-project-123","labels":{}}`, "updateMask=labels", `{}`},
		"with etag": {`{"project_id":"my-project-123","display_name":"New Name","etag":"abc"}`,
			"updateMask=displayName", `{"displayName":"New Name","etag":"abc"}`},
	} {
		e := writeEnvironment(t, nil)
		e.fake.routes[rmProjectPath] = okReply(projectBody)
		e.fake.routes[rmNumberPath] = okReply(rmOperation)
		out, err := e.invokeConfirmed(projectsPatchID, c.args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		writes := rmWrites(e)
		if len(writes) != 1 || writes[0].method != "PATCH" ||
			writes[0].url != "https://cloudresourcemanager.googleapis.com"+rmNumberPath+"?"+c.query ||
			writes[0].body != c.body {
			t.Errorf("%s: writes = %+v", name, writes)
		}
		if got := decode(t, out); got["service"] != "resource-manager" || got["done"] != false {
			t.Errorf("%s: result = %s", name, out)
		}
	}
}

func TestProjectsPatchRefusesWithoutAFieldAndOutsideTheTargetsBeforeIO(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"no field":       {nil, `{"project_id":"my-project-123"}`},
		"other project":  {[]string{"projects/other-project-1"}, `{"project_id":"my-project-123","display_name":"New Name"}`},
		"billing only":   {[]string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"}, `{"project_id":"my-project-123","display_name":"New Name"}`},
		"projects/ name": {nil, `{"project_id":"projects/415104041262","display_name":"New Name"}`},
	} {
		e := writeEnvironment(t, c.targets)
		if _, err := e.invokeConfirmed(projectsPatchID, c.args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestProjectsPatchOfAProjectBelowNoTargetIsRefusedAfterTheReadAndWritesNothing(t *testing.T) {
	e := writeEnvironment(t, []string{"folders/3000"})
	e.fake.routes[rmProjectPath] = okReply(projectBody)
	e.fake.routes["/v3/folders/2001"] = okReply(rmFolderBody)
	if _, err := e.invokeConfirmed(projectsPatchID, `{"project_id":"my-project-123","display_name":"New Name"}`); err == nil ||
		len(rmWrites(e)) != 0 {
		t.Errorf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestProjectsPatchEtagConflictIsAConflictWithoutRetry(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[rmProjectPath] = okReply(projectBody)
	e.fake.routes[rmNumberPath] = reply{status: 409, body: errorObject(409, "ABORTED", "")}
	_, err := e.invokeConfirmed(projectsPatchID, `{"project_id":"my-project-123","display_name":"New Name","etag":"old"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conflict") ||
		strings.Contains(err.Error(), detailCanary) || len(rmWrites(e)) != 1 {
		t.Fatalf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestProjectsPatchConfirmationNamesTheNewValues(t *testing.T) {
	e := writeEnvironment(t, nil)
	args := `{"project_id":"my-project-123","display_name":"New Name","labels":{"env":"prod"}}`
	if _, err := e.invoke(projectsPatchID, args); err == nil {
		t.Fatal("patch ran without confirmation")
	}
	summary := e.reg.ConfirmationSummary(projectsPatchID)(json.RawMessage(args))
	for _, want := range []string{"my-project-123", "New Name", "env=prod", "every other label is removed"} {
		if !strings.Contains(summary, want) {
			t.Errorf("confirmation lacks %q: %s", want, summary)
		}
	}
	noIO(t, e, "unconfirmed patch")
}

const moveArgs = `{"project_id":"my-project-123","current_parent":"folders/2001","destination_parent":"folders/2002"}`

func TestProjectsMoveReadsThenMovesAndTheConfirmationNamesBothParents(t *testing.T) {
	e := writeEnvironment(t, nil)
	if _, err := e.invoke(projectsMoveID, moveArgs); err == nil {
		t.Error("move ran without confirmation")
	}
	summary := e.reg.ConfirmationSummary(projectsMoveID)(json.RawMessage(moveArgs))
	for _, want := range []string{"my-project-123", "from folders/2001", "to folders/2002", "inherits"} {
		if !strings.Contains(summary, want) {
			t.Errorf("confirmation lacks %q: %s", want, summary)
		}
	}
	noIO(t, e, "unconfirmed move")
	e.fake.routes[rmProjectPath] = okReply(projectBody)
	e.fake.routes[rmNumberPath+":move"] = okReply(rmOperation)
	out, err := e.invokeConfirmed(projectsMoveID, moveArgs)
	if err != nil {
		t.Fatal(err)
	}
	writes := rmWrites(e)
	if len(writes) != 1 || writes[0].method != "POST" || writes[0].body != `{"destinationParent":"folders/2002"}` ||
		writes[0].url != "https://cloudresourcemanager.googleapis.com"+rmNumberPath+":move" {
		t.Fatalf("writes = %+v", writes)
	}
	if got := decode(t, out); got["name"] != "operations/cp.1" || got["service"] != "resource-manager" {
		t.Errorf("result = %s", out)
	}
}

func TestProjectsMoveWithAnotherCurrentParentWritesNothing(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[rmProjectPath] = okReply(projectBody)
	e.fake.routes[rmNumberPath+":move"] = okReply(rmOperation)
	_, err := e.invokeConfirmed(projectsMoveID,
		`{"project_id":"my-project-123","current_parent":"folders/9999","destination_parent":"folders/2002"}`)
	if err == nil || len(rmWrites(e)) != 0 || len(e.fake.api()) != 1 || strings.Contains(err.Error(), "folders/2001") {
		t.Errorf("err = %v, requests = %+v", err, e.fake.api())
	}
}

func TestProjectsMoveRefusesUndecidedTargetsBeforeIO(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"destination outside the folder target": {[]string{"folders/2001"},
			`{"project_id":"my-project-123","current_parent":"folders/2001","destination_parent":"organizations/9"}`},
		"organization without a target": {nil,
			`{"project_id":"my-project-123","current_parent":"folders/2001","destination_parent":"organizations/9"}`},
		"current organization without a target": {nil,
			`{"project_id":"my-project-123","current_parent":"organizations/9","destination_parent":"folders/2001"}`},
		"project target only": {[]string{"projects/my-project-123"}, moveArgs},
		"other project":       {[]string{"projects/other-project-1"}, moveArgs},
		"no destination":      {nil, `{"project_id":"my-project-123","current_parent":"folders/2001"}`},
		"no current parent":   {nil, `{"project_id":"my-project-123","destination_parent":"folders/2001"}`},
	} {
		e := writeEnvironment(t, c.targets)
		if _, err := e.invokeConfirmed(projectsMoveID, c.args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestProjectsMoveRefusesADestinationThatReadingProvesOutsideAndWritesNothing(t *testing.T) {
	e := writeEnvironment(t, []string{"folders/2001", "organizations/8"})
	e.fake.routes[rmProjectPath] = okReply(projectBody)
	e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"organizations/9"}`)
	if _, err := e.invokeConfirmed(projectsMoveID, moveArgs); err == nil || len(rmWrites(e)) != 0 {
		t.Errorf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestProjectsUndeleteSendsAnEmptyBodyAndLeavesTheStateToGoogle(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[rmProjectPath] = okReply(projectBody) // an active project: Qatlas does not check the state
	e.fake.routes[rmNumberPath+":undelete"] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(projectsUndeleteID, `{"project_id":"my-project-123"}`)
	writes := rmWrites(e)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) ||
		len(writes) != 1 || writes[0].body != "{}" {
		t.Fatalf("err = %v, writes = %+v", err, writes)
	}
	e = writeEnvironment(t, nil)
	e.fake.routes[rmProjectPath] = okReply(strings.Replace(projectBody, `"ACTIVE"`, `"DELETE_REQUESTED"`, 1))
	e.fake.routes[rmNumberPath+":undelete"] = okReply(rmOperation)
	if out, err := e.invokeConfirmed(projectsUndeleteID, `{"project_id":"my-project-123"}`); err != nil ||
		decode(t, out)["name"] != "operations/cp.1" {
		t.Errorf("out = %s, err = %v", out, err)
	}
}

func TestProjectsUndeleteIsBoundToTheTargetsAndNeedsConfirmation(t *testing.T) {
	e := writeEnvironment(t, []string{"projects/other-project-1"})
	if _, err := e.invokeConfirmed(projectsUndeleteID, `{"project_id":"my-project-123"}`); err == nil {
		t.Error("undelete outside the targets accepted")
	}
	noIO(t, e, "other project")
	e = writeEnvironment(t, nil)
	if _, err := e.invoke(projectsUndeleteID, `{"project_id":"my-project-123"}`); err == nil {
		t.Error("undelete ran without confirmation")
	}
	if summary := e.reg.ConfirmationSummary(projectsUndeleteID)(json.RawMessage(`{"project_id":"my-project-123"}`)); !strings.Contains(summary, "my-project-123") {
		t.Errorf("summary = %s", summary)
	}
	noIO(t, e, "unconfirmed undelete")
}

func TestProjectWriteToolsAreNotOfferedWhereTheTargetsCannotServeThem(t *testing.T) {
	cases := []struct {
		targets []string
		tool    string
		offered bool
	}{
		{[]string{"projects/my-project-123"}, projectsCreateID, false},
		{[]string{"projects/my-project-123"}, projectsMoveID, false},
		{[]string{"projects/my-project-123"}, projectsPatchID, true},
		{[]string{"projects/my-project-123"}, projectsUndeleteID, true},
		{[]string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"}, projectsPatchID, false},
		{[]string{"folders/2001"}, projectsCreateID, true},
		{[]string{"organizations/9"}, projectsMoveID, true},
		{nil, projectsCreateID, true},
	}
	for _, c := range cases {
		if offered, _ := targetReaches(c.tool, c.targets); offered != c.offered {
			t.Errorf("targets %v, %s: offered = %v", c.targets, c.tool, offered)
		}
	}
	for _, id := range []string{projectsCreateID, projectsPatchID, projectsMoveID, projectsUndeleteID} {
		d, _, ok := newEnvironment(t, nil).reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
			!slices.Contains(d.Tags, "projects") {
			t.Errorf("descriptor of %s = %+v", id, d)
		}
	}
}

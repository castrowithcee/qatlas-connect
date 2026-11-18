package googlecloud

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	fwFolderPath = "/v3/folders/2001"
	fwOperation  = `{"name":"operations/fo.1","metadata":{"@type":"type.googleapis.com/x.CreateFolderMetadata"}}`
	fwCreateArgs = `{"parent":"organizations/9","display_name":"Team Alpha"}`
	fwMoveArgs   = `{"folder_id":"2001","current_parent":"organizations/9","destination_parent":"folders/2002"}`
)

// answerPatch answers a PATCH of the folder, which shares its path with the read.
func answerPatch(e *environment, r reply) {
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.Method == http.MethodPatch && req.URL.Path == fwFolderPath {
			return &r
		}
		return nil
	}
}

func TestFoldersCreateUnderAnOrganizationAndAFolderReturnsTheOperation(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
		reads   int
	}{
		"organization":          {[]string{"organizations/9"}, fwCreateArgs, 0},
		"folder, no targets":    {nil, `{"parent":"folders/2001","display_name":"Team_Alpha-1"}`, 0},
		"folder target":         {[]string{"folders/2001"}, `{"parent":"folders/2001","display_name":"Team Alpha"}`, 0},
		"folder below the org":  {[]string{"organizations/9"}, `{"parent":"folders/2001","display_name":"Team Alpha"}`, 1},
		"organization, no list": {[]string{"organizations/9", "projects/my-project-123"}, fwCreateArgs, 0},
	} {
		e := writeEnvironment(t, c.targets)
		e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
		e.fake.routes["/v3/folders"] = okReply(fwOperation)
		out, err := e.invokeConfirmed(foldersCreateID, c.args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got := decode(t, out)
		api := e.fake.api()
		if got["service"] != "resource-manager" || got["name"] != "operations/fo.1" || got["done"] != false ||
			len(api) != c.reads+1 {
			t.Errorf("%s: result = %s, requests = %d", name, out, len(api))
		}
		last := api[len(api)-1]
		if last.method != "POST" || last.url != "https://cloudresourcemanager.googleapis.com/v3/folders" ||
			!strings.HasPrefix(last.body, `{"parent":"`) || !strings.Contains(last.body, `"displayName":"`) {
			t.Errorf("%s: write = %+v", name, last)
		}
	}
}

func TestFoldersCreateOperationRunsAndEndsWithGoogleSAnswer(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = okReply(fwOperation)
	e.fake.routes["/v3/operations/fo.1"] = okReply(`{"name":"operations/fo.1","done":true,` +
		`"response":{"@type":"type.googleapis.com/google.cloud.resourcemanager.v3.Folder","name":"folders/3"}}`)
	if _, err := e.invokeConfirmed(foldersCreateID, `{"parent":"folders/2001","display_name":"Team Alpha"}`); err != nil {
		t.Fatal(err)
	}
	out, err := e.invoke(operationsGetID, `{"service":"resource-manager","name":"operations/fo.1"}`)
	if err != nil || decode(t, out)["done"] != true {
		t.Errorf("out = %s, err = %v", out, err)
	}
	// An operation that is done with an error is a provider error.
	e = writeEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = okReply(`{"name":"operations/fo.2","done":true,"error":{"code":9,"message":"` +
		detailCanary + `"}}`)
	_, err = e.invokeConfirmed(foldersCreateID, `{"parent":"folders/2001","display_name":"Team Alpha"}`)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) {
		t.Errorf("class = %q, err = %v", classOf(err), err)
	}
}

func TestFoldersCreateWithATakenNameIsAConflictWithoutRetry(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes["/v3/folders"] = reply{status: 409, body: errorObject(409, "ALREADY_EXISTS", "")}
	_, err := e.invokeConfirmed(foldersCreateID, `{"parent":"folders/2001","display_name":"Team Alpha"}`)
	if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conflict") ||
		strings.Contains(err.Error(), detailCanary) || len(rmWrites(e)) != 1 {
		t.Fatalf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestFoldersCreateRefusesBadArgumentsAndParentsOutsideTheTargetsBeforeIO(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"two characters":               {nil, `{"parent":"folders/2001","display_name":"ab"}`},
		"31 characters":                {nil, `{"parent":"folders/2001","display_name":"` + strings.Repeat("a", 31) + `"}`},
		"ends with a hyphen":           {nil, `{"parent":"folders/2001","display_name":"Team-"}`},
		"starts with a space":          {nil, `{"parent":"folders/2001","display_name":" Team"}`},
		"bad characters":               {nil, `{"parent":"folders/2001","display_name":"Team <b>"}`},
		"no name":                      {nil, `{"parent":"folders/2001"}`},
		"no parent":                    {nil, `{"display_name":"Team Alpha"}`},
		"project parent":               {nil, `{"parent":"projects/1","display_name":"Team Alpha"}`},
		"extra member":                 {nil, `{"parent":"folders/2001","display_name":"Team Alpha","tags":{}}`},
		"organization without targets": {nil, fwCreateArgs},
		"other organization":           {[]string{"organizations/8"}, fwCreateArgs},
		"organization, only a folder":  {[]string{"folders/2001"}, fwCreateArgs},
		"project target only":          {[]string{"projects/my-project-123"}, `{"parent":"folders/2001","display_name":"Team Alpha"}`},
		"billing target only":          {[]string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"}, `{"parent":"folders/2001","display_name":"Team Alpha"}`},
	} {
		e := writeEnvironment(t, c.targets)
		if _, err := e.invokeConfirmed(foldersCreateID, c.args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
	// A folder that reading proves below another organization is refused and nothing is written.
	e := writeEnvironment(t, []string{"organizations/8"})
	e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
	if _, err := e.invokeConfirmed(foldersCreateID, `{"parent":"folders/2001","display_name":"Team Alpha"}`); err == nil ||
		len(rmWrites(e)) != 0 {
		t.Errorf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestFolderConfirmationsNameWhatTheyChange(t *testing.T) {
	for tool, c := range map[string]struct {
		args  string
		wants []string
	}{
		foldersCreateID: {fwCreateArgs, []string{"Team Alpha", "organizations/9"}},
		foldersPatchID: {`{"folder_id":"2001","display_name":"Team Beta","etag":"x"}`,
			[]string{"folders/2001", "Team Beta", "etag"}},
		foldersMoveID: {fwMoveArgs, []string{"folders/2001", "from organizations/9", "to folders/2002",
			"every folder and project"}},
		foldersUndeleteID: {`{"folder_id":"2001"}`, []string{"folders/2001", "marked for deletion"}},
	} {
		e := writeEnvironment(t, nil)
		if _, err := e.invoke(tool, c.args); err == nil {
			t.Errorf("%s ran without confirmation", tool)
		}
		noIO(t, e, tool)
		summary := e.reg.ConfirmationSummary(tool)(json.RawMessage(c.args))
		for _, want := range c.wants {
			if !strings.Contains(summary, want) {
				t.Errorf("%s: confirmation lacks %q: %s", tool, want, summary)
			}
		}
		if strings.Contains(summary, "projects are affected") {
			t.Errorf("%s: names a project count: %s", tool, summary)
		}
	}
}

func TestFolderConfirmationThatWouldBeCutIsRefused(t *testing.T) {
	// The schema bounds every value, so the guard is reached by the confirmation function and the summary text
	// directly, with values the schema would not let through.
	long := strings.Repeat("x", 400)
	a := folderWriteArguments{FolderID: "2001", CurrentParent: long, DestinationParent: long}
	if summaryFits(folderSummaryText(foldersMoveID, a)) {
		t.Fatal("test value fits")
	}
	e := writeEnvironment(t, nil)
	raw, _ := json.Marshal(a)
	if summary := e.reg.ConfirmationSummary(foldersMoveID)(raw); !strings.Contains(summary, "will be refused") ||
		!summaryFits(summary) {
		t.Errorf("summary = %s", summary)
	}
	// A call at the bounds of every argument fits.
	max := folderWriteArguments{FolderID: strings.Repeat("9", 20), CurrentParent: "organizations/" + strings.Repeat("9", 20),
		DestinationParent: "organizations/" + strings.Repeat("9", 20)}
	if !summaryFits(folderSummaryText(foldersMoveID, max)) {
		t.Error("a move at the bounds does not fit")
	}
}

func TestFoldersPatchSendsOnlyTheNameAsTheMask(t *testing.T) {
	for name, c := range map[string]struct{ args, body string }{
		"name":      {`{"folder_id":"2001","display_name":"Team Beta"}`, `{"displayName":"Team Beta"}`},
		"with etag": {`{"folder_id":"2001","display_name":"Team Beta","etag":"abc"}`, `{"displayName":"Team Beta","etag":"abc"}`},
	} {
		e := writeEnvironment(t, nil)
		e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
		answerPatch(e, okReply(fwOperation))
		out, err := e.invokeConfirmed(foldersPatchID, c.args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		writes := rmWrites(e)
		if len(writes) != 1 || writes[0].method != "PATCH" ||
			writes[0].url != "https://cloudresourcemanager.googleapis.com"+fwFolderPath+"?updateMask=displayName" ||
			writes[0].body != c.body {
			t.Errorf("%s: writes = %+v", name, writes)
		}
		if got := decode(t, out); got["service"] != "resource-manager" || got["done"] != false {
			t.Errorf("%s: result = %s", name, out)
		}
	}
}

func TestFoldersPatchDuplicateNameAndEtagAreConflicts(t *testing.T) {
	for _, status := range []int{409} {
		e := writeEnvironment(t, nil)
		e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
		answerPatch(e, reply{status: status, body: errorObject(status, "ABORTED", "")})
		_, err := e.invokeConfirmed(foldersPatchID, `{"folder_id":"2001","display_name":"Team Beta","etag":"old"}`)
		if classOf(err) != provider.ClassProviderError || !strings.Contains(err.Error(), "conflict") ||
			len(rmWrites(e)) != 1 {
			t.Fatalf("err = %v, writes = %d", err, len(rmWrites(e)))
		}
	}
}

func TestFolderChangesRefuseOutsideTheTargetsBeforeIOWhereTheTargetsDecide(t *testing.T) {
	for tool, args := range map[string]string{
		foldersPatchID:    `{"folder_id":"2001","display_name":"Team Beta"}`,
		foldersMoveID:     fwMoveArgs,
		foldersUndeleteID: `{"folder_id":"2001"}`,
	} {
		for name, targets := range map[string][]string{
			"project target only": {"projects/my-project-123"},
			"billing only":        {"billingAccounts/ABCDEF-ABCDEF-ABCDEF"},
		} {
			e := writeEnvironment(t, targets)
			if _, err := e.invokeConfirmed(tool, args); err == nil {
				t.Errorf("%s, %s: accepted", tool, name)
			}
			noIO(t, e, tool+" "+name)
		}
	}
	for name, c := range map[string]struct{ tool, args string }{
		"patch, no field":      {foldersPatchID, `{"folder_id":"2001"}`},
		"patch, short name":    {foldersPatchID, `{"folder_id":"2001","display_name":"ab"}`},
		"patch, folders/ name": {foldersPatchID, `{"folder_id":"folders/2001","display_name":"Team Beta"}`},
		"undelete, path":       {foldersUndeleteID, `{"folder_id":"2001/x"}`},
		"move, no destination": {foldersMoveID, `{"folder_id":"2001","current_parent":"organizations/9"}`},
		"move, no current":     {foldersMoveID, `{"folder_id":"2001","destination_parent":"folders/2002"}`},
		"move, project parent": {foldersMoveID, `{"folder_id":"2001","current_parent":"organizations/9","destination_parent":"projects/1"}`},
	} {
		e := writeEnvironment(t, nil)
		if _, err := e.invokeConfirmed(c.tool, c.args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
}

func TestFolderChangesOfAFolderOutsideTheTargetsAreRefusedAfterTheReadAndWriteNothing(t *testing.T) {
	for tool, args := range map[string]string{
		foldersPatchID:    `{"folder_id":"2001","display_name":"Team Beta"}`,
		foldersMoveID:     fwMoveArgs,
		foldersUndeleteID: `{"folder_id":"2001"}`,
	} {
		// The folder lies below organization 9, the targets name organization 8 and a folder.
		e := writeEnvironment(t, []string{"organizations/8", "folders/3000"})
		e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
		if _, err := e.invokeConfirmed(tool, args); err == nil || len(rmWrites(e)) != 0 {
			t.Errorf("%s: err = %v, writes = %d", tool, err, len(rmWrites(e)))
		}
	}
}

func TestFoldersMoveReadsThenMovesToABoundDestination(t *testing.T) {
	e := writeEnvironment(t, []string{"organizations/9"})
	e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
	e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"organizations/9"}`)
	e.fake.routes[fwFolderPath+":move"] = okReply(fwOperation)
	out, err := e.invokeConfirmed(foldersMoveID, fwMoveArgs)
	if err != nil {
		t.Fatal(err)
	}
	writes := rmWrites(e)
	if len(writes) != 1 || writes[0].method != "POST" || writes[0].body != `{"destinationParent":"folders/2002"}` ||
		writes[0].url != "https://cloudresourcemanager.googleapis.com"+fwFolderPath+":move" {
		t.Fatalf("writes = %+v", writes)
	}
	if got := decode(t, out); got["name"] != "operations/fo.1" || got["service"] != "resource-manager" {
		t.Errorf("result = %s", out)
	}
}

func TestFoldersMoveWithAnotherCurrentParentWritesNothing(t *testing.T) {
	e := writeEnvironment(t, []string{"organizations/9", "organizations/8"})
	e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
	e.fake.routes[fwFolderPath+":move"] = okReply(fwOperation)
	_, err := e.invokeConfirmed(foldersMoveID,
		`{"folder_id":"2001","current_parent":"organizations/8","destination_parent":"folders/2002"}`)
	if err == nil || len(rmWrites(e)) != 0 || len(e.fake.api()) != 1 || strings.Contains(err.Error(), "organizations/9") {
		t.Errorf("err = %v, requests = %+v", err, e.fake.api())
	}
}

func TestFoldersMoveRefusesParentsOutsideTheTargets(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		args    string
	}{
		"destination organization without a target": {nil,
			`{"folder_id":"2001","current_parent":"folders/5","destination_parent":"organizations/9"}`},
		"current organization without a target": {nil, fwMoveArgs},
		"destination outside a folder target": {[]string{"folders/2001"},
			`{"folder_id":"2001","current_parent":"folders/2001","destination_parent":"organizations/9"}`},
	} {
		e := writeEnvironment(t, c.targets)
		if _, err := e.invokeConfirmed(foldersMoveID, c.args); err == nil {
			t.Errorf("%s accepted", name)
		}
		noIO(t, e, name)
	}
	// A destination that reading proves outside is refused after the read and nothing is written.
	e := writeEnvironment(t, []string{"folders/2001", "organizations/8"})
	e.fake.routes[fwFolderPath] = okReply(`{"name":"folders/2001","parent":"organizations/8"}`)
	e.fake.routes["/v3/folders/2002"] = okReply(`{"name":"folders/2002","parent":"organizations/9"}`)
	if _, err := e.invokeConfirmed(foldersMoveID,
		`{"folder_id":"2001","current_parent":"organizations/8","destination_parent":"folders/2002"}`); err == nil ||
		len(rmWrites(e)) != 0 {
		t.Errorf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestFoldersMoveIntoItsOwnSubfolderIsLeftToGoogle(t *testing.T) {
	e := writeEnvironment(t, []string{"organizations/9"})
	e.fake.routes["/v3/folders/2003"] = okReply(`{"name":"folders/2003","parent":"folders/2001"}`)
	e.fake.routes[fwFolderPath] = okReply(rmFolderBody)
	e.fake.routes[fwFolderPath+":move"] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(foldersMoveID,
		`{"folder_id":"2001","current_parent":"organizations/9","destination_parent":"folders/2003"}`)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) ||
		len(rmWrites(e)) != 1 {
		t.Fatalf("err = %v, writes = %d", err, len(rmWrites(e)))
	}
}

func TestFoldersUndeleteSendsAnEmptyBodyAndLeavesTheStateToGoogle(t *testing.T) {
	e := writeEnvironment(t, nil)
	e.fake.routes[fwFolderPath] = okReply(rmFolderBody) // an active folder: Qatlas does not check the state
	e.fake.routes[fwFolderPath+":undelete"] = reply{status: 400, body: errorObject(400, "FAILED_PRECONDITION", "")}
	_, err := e.invokeConfirmed(foldersUndeleteID, `{"folder_id":"2001"}`)
	writes := rmWrites(e)
	if classOf(err) != provider.ClassProviderError || strings.Contains(err.Error(), detailCanary) ||
		len(writes) != 1 || writes[0].body != "{}" {
		t.Fatalf("err = %v, writes = %+v", err, writes)
	}
	e = writeEnvironment(t, nil)
	e.fake.routes[fwFolderPath] = okReply(strings.Replace(rmFolderBody, `"ACTIVE"`, `"DELETE_REQUESTED"`, 1))
	e.fake.routes[fwFolderPath+":undelete"] = okReply(fwOperation)
	if out, err := e.invokeConfirmed(foldersUndeleteID, `{"folder_id":"2001"}`); err != nil ||
		decode(t, out)["name"] != "operations/fo.1" {
		t.Errorf("out = %s, err = %v", out, err)
	}
}

func TestFolderWriteToolsAreOfferedByTheTargetsAndCarryTheirRisk(t *testing.T) {
	cases := []struct {
		targets []string
		offered bool
	}{
		{[]string{"projects/my-project-123"}, false},
		{[]string{"billingAccounts/ABCDEF-ABCDEF-ABCDEF"}, false},
		{[]string{"folders/2001"}, true},
		{[]string{"organizations/9"}, true},
		{nil, true},
	}
	ids := []string{foldersCreateID, foldersPatchID, foldersMoveID, foldersUndeleteID}
	for _, c := range cases {
		for _, id := range ids {
			if offered, _ := targetReaches(id, c.targets); offered != c.offered {
				t.Errorf("targets %v, %s: offered = %v", c.targets, id, offered)
			}
		}
	}
	for _, id := range ids {
		d, _, ok := newEnvironment(t, nil).reg.Lookup(id)
		if !ok || d.RequiresToolAllowList || d.Risk.Confirmation != "required" || !d.Risk.OpenWorld ||
			!slices.Contains(d.Tags, "folders") {
			t.Errorf("descriptor of %s = %+v", id, d)
		}
	}
}

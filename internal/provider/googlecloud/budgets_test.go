package googlecloud

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const budgetPrefix = billingA + "/budgets/"

func budgetRow(id, display string, projects ...string) string {
	row := `{"name":"` + budgetPrefix + id + `","displayName":"` + display + `","etag":"e1","ownershipScope":"ALL_USERS"`
	if len(projects) > 0 {
		row += `,"budgetFilter":{"projects":["` + strings.Join(projects, `","`) + `"],"calendarPeriod":"MONTH"}`
	}
	return row + `}`
}

func budgetIDs(rows []map[string]any) []string {
	var ids []string
	for _, row := range rows {
		ids = append(ids, strings.TrimPrefix(row["name"].(string), budgetPrefix))
	}
	return ids
}

func TestBudgetsListFollowsTwoPagesAndScope(t *testing.T) {
	e := newEnvironment(t, []string{billingA})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/"+billingA+"/budgets" {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"budgets":[` + budgetRow("b1", "One", "projects/415104041262") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"budgets":[` + budgetRow("b2", "Two") + `]}`}
	}
	out, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`","scope":"projects/my-project-123","page_size":1}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "budgets")
	if !slices.Equal(budgetIDs(rows), []string{"b1"}) || got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	// A cursor of one scope does not continue a list of another.
	before := len(e.fake.api())
	if _, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`","cursor":"`+got["next_cursor"].(string)+`"}`); err == nil ||
		len(e.fake.api()) != before {
		t.Errorf("a cursor of another scope was accepted: %v", err)
	}
	out, err = e.invoke(budgetsListID, `{"billing_account":"`+billingA+`","scope":"projects/my-project-123","cursor":"`+
		got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "budgets")
	if !slices.Equal(budgetIDs(rows), []string{"b2"}) || got["has_more"] != false {
		t.Fatalf("page 2 = %s", out)
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Get("scope") != "projects/my-project-123" || queries[0].Get("pageSize") != "1" ||
		queries[1].Get("pageToken") != "p2" {
		t.Errorf("queries = %v", queries)
	}
	if paths := apiPaths(e); paths[0] != "billingbudgets.googleapis.com/v1/"+billingA+"/budgets" {
		t.Errorf("path = %v", paths)
	}
}

func TestBudgetsGetReturnsEveryDocumentedField(t *testing.T) {
	e := newEnvironment(t, []string{billingA})
	e.fake.routes["/v1/"+budgetPrefix+"abc-123"] = okReply(`{"name":"` + budgetPrefix + `abc-123","displayName":"Monthly",` +
		`"budgetFilter":{"projects":["projects/415104041262"],"resourceAncestors":["folders/2001"],"creditTypes":["PROMOTION"],` +
		`"creditTypesTreatment":"INCLUDE_SPECIFIED_CREDITS","services":["services/6F81-5844-456A"],` +
		`"subaccounts":["` + billingB + `"],"labels":{"env":["prod"]},"customPeriod":{"startDate":{"year":2026,"month":11,"day":1},` +
		`"endDate":{"year":2026,"month":11,"day":30}}},"amount":{"specifiedAmount":{"currencyCode":"EUR","units":"500"}},` +
		`"thresholdRules":[{"thresholdPercent":0.5},{"thresholdPercent":1,"spendBasis":"FORECASTED_SPEND"}],` +
		`"notificationsRule":{"pubsubTopic":"projects/my-project-123/topics/budget","schemaVersion":"1.0",` +
		`"monitoringNotificationChannels":["projects/my-project-123/notificationChannels/9"],` +
		`"disableDefaultIamRecipients":true,"enableProjectLevelRecipients":true},` +
		`"spendCap":{"inputState":"CONFIGURED","outputState":"ENFORCED","reconciling":true},"etag":"e1","ownershipScope":"BILLING_ACCOUNT"}`)
	out, err := e.invoke(budgetsGetID, `{"billing_account":"`+billingA+`","budget_id":"abc-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"displayName":"Monthly"`, `"projects":["projects/415104041262"]`,
		`"resourceAncestors":["folders/2001"]`, `"creditTypes":["PROMOTION"]`, `"creditTypesTreatment":"INCLUDE_SPECIFIED_CREDITS"`,
		`"services":["services/6F81-5844-456A"]`, `"subaccounts":["` + billingB + `"]`, `"labels":{"env":["prod"]}`,
		`"startDate":{"day":1,"month":11,"year":2026}`, `"specifiedAmount":{"currencyCode":"EUR","units":"500"}`,
		`{"thresholdPercent":0.5}`, `{"spendBasis":"FORECASTED_SPEND","thresholdPercent":1}`,
		`"pubsubTopic":"projects/my-project-123/topics/budget"`, `"schemaVersion":"1.0"`,
		`"monitoringNotificationChannels":["projects/my-project-123/notificationChannels/9"]`,
		`"disableDefaultIamRecipients":true`, `"enableProjectLevelRecipients":true`,
		`"spendCap":{"inputState":"CONFIGURED","outputState":"ENFORCED","reconciling":true}`, `"etag":"e1"`,
		`"ownershipScope":"BILLING_ACCOUNT"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
	if paths := apiPaths(e); paths[0] != "billingbudgets.googleapis.com/v1/"+budgetPrefix+"abc-123" {
		t.Errorf("path = %v", paths)
	}
	// Qatlas leaves an empty object out of a result, so a last period amount shows as no amount.
	e.fake.routes["/v1/"+budgetPrefix+"abc-123"] = okReply(`{"name":"` + budgetPrefix + `abc-123","amount":{"lastPeriodAmount":{}}}`)
	if out, err := e.invoke(budgetsGetID, `{"billing_account":"`+billingA+`","budget_id":"abc-123"}`); err != nil ||
		strings.Contains(out, "specifiedAmount") {
		t.Errorf("last period = %s, %v", out, err)
	}
}

func TestBudgetsRefuseBeforeIO(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		tool    string
		args    string
	}{
		"list unbound":        {nil, budgetsListID, `{"billing_account":"` + billingA + `"}`},
		"list project target": {[]string{"projects/my-project-123"}, budgetsListID, `{"billing_account":"` + billingA + `"}`},
		"list other account":  {[]string{billingB}, budgetsListID, `{"billing_account":"` + billingA + `"}`},
		"list invalid name":   {[]string{billingA}, budgetsListID, `{"billing_account":"x"}`},
		"list bad scope":      {[]string{billingA}, budgetsListID, `{"billing_account":"` + billingA + `","scope":"folders/1"}`},
		"list scope outside": {[]string{billingA, "projects/other-project-1"}, budgetsListID,
			`{"billing_account":"` + billingA + `","scope":"projects/my-project-123"}`},
		"list page size": {[]string{billingA}, budgetsListID, `{"billing_account":"` + billingA + `","page_size":101}`},
		"list cursor":    {[]string{billingA}, budgetsListID, `{"billing_account":"` + billingA + `","cursor":"bogus"}`},
		"get unbound":    {nil, budgetsGetID, `{"billing_account":"` + billingA + `","budget_id":"b1"}`},
		"get no target":  {[]string{billingB}, budgetsGetID, `{"billing_account":"` + billingA + `","budget_id":"b1"}`},
		"get path":       {[]string{billingA}, budgetsGetID, `{"billing_account":"` + billingA + `","budget_id":"b1/../x"}`},
		"get empty id":   {[]string{billingA}, budgetsGetID, `{"billing_account":"` + billingA + `","budget_id":""}`},
		"get no id":      {[]string{billingA}, budgetsGetID, `{"billing_account":"` + billingA + `"}`},
	} {
		e := newEnvironment(t, c.targets)
		if _, err := e.invoke(c.tool, c.args); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	e := newEnvironment(t, []string{billingA})
	if _, err := handleBudgetsGet(t.Context(), e.resolved(), e.secrets(), e.red,
		[]byte(`{"billing_account":"`+billingA+`","budget_id":"../x"}`)); !isInvalidRequest(err) || len(e.fake.api()) != 0 {
		t.Errorf("err = %v", err)
	}
}

func TestBudgetsAreOfferedOnlyToABindingThatNamesTheAccount(t *testing.T) {
	for _, tool := range []string{budgetsListID, budgetsGetID} {
		for targets, want := range map[string]bool{"": false, "projects/my-project-123": false, billingA: true} {
			var list []string
			if targets != "" {
				list = []string{targets}
			}
			if got, _ := targetReaches(tool, list); got != want {
				t.Errorf("%s with %v: reaches = %v", tool, list, got)
			}
		}
	}
	for _, tool := range []string{catalogServicesListID, catalogSkusListID} {
		for _, list := range [][]string{nil, {"projects/my-project-123"}, {billingA}} {
			if got, _ := targetReaches(tool, list); !got {
				t.Errorf("%s with %v is not offered", tool, list)
			}
		}
	}
}

func TestBudgetsListFiltersRowsAgainstProjectTargets(t *testing.T) {
	rows := strings.Join([]string{
		budgetRow("own-id", "Own by ID", "projects/my-project-123"),
		budgetRow("own-number", "Own by number", "projects/415104041262"),
		budgetRow("foreign", "Foreign", "projects/999999999999"),
		budgetRow("mixed", "Mixed", "projects/999999999999", "projects/my-project-123"),
		budgetRow("account", "Whole account"),
		budgetRow("hidden", "Hidden", "projects/888888888888"),
	}, ",")
	routes := func(e *environment) {
		e.fake.routes["/v1/"+billingA+"/budgets"] = okReply(`{"budgets":[` + rows + `]}`)
		e.fake.routes["/v3/projects/415104041262"] = okReply(projectBody)
		e.fake.routes["/v3/projects/999999999999"] = okReply(projectRow("999999999999", "foreign-project-1", "folders/7001"))
		e.fake.routes["/v3/projects/888888888888"] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}
	}
	t.Run("project target", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA, "projects/my-project-123"})
		routes(e)
		out, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		list, _ := listOf(t, out, "budgets")
		if want := []string{"own-id", "own-number", "mixed", "account"}; !slices.Equal(budgetIDs(list), want) {
			t.Errorf("ids = %v, want %v", budgetIDs(list), want)
		}
		for _, leaked := range []string{"Foreign", "Hidden", "foreign-project", "888888888888"} {
			if strings.Contains(out, leaked) {
				t.Errorf("the result names %q", leaked)
			}
		}
		for _, r := range e.fake.api() {
			if r.method != http.MethodGet {
				t.Errorf("method = %s", r.method)
			}
		}
	})
	t.Run("billing account alone shows every budget of the account", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA})
		routes(e)
		out, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		list, _ := listOf(t, out, "budgets")
		if len(list) != 6 || len(e.fake.api()) != 1 {
			t.Errorf("rows = %d, requests = %v", len(list), apiPaths(e))
		}
	})
	t.Run("a budget filter never adds a target", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA, "projects/other-project-1"})
		routes(e)
		e.fake.routes["/v3/projects/415104041262"] = okReply(projectBody)
		out, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		list, _ := listOf(t, out, "budgets")
		if want := []string{"account"}; !slices.Equal(budgetIDs(list), want) {
			t.Errorf("ids = %v, want %v", budgetIDs(list), want)
		}
	})
	t.Run("a row of another account is invalid", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA})
		e.fake.routes["/v1/"+billingA+"/budgets"] = okReply(`{"budgets":[{"name":"` + billingB + `/budgets/x"}]}`)
		if _, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("class = %q", classOf(err))
		}
	})
}

func TestBudgetsListBoundsTheProofReads(t *testing.T) {
	e := newEnvironment(t, []string{billingA, "organizations/9001"})
	var rows []string
	for i := range maxProofReads + 2 {
		number := "1000000000" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		number = strings.Map(func(r rune) rune { return '0' + r%10 }, number)
		rows = append(rows, budgetRow("b"+number+string(rune('a'+i%26)), "x", "projects/2"+number))
		e.fake.routes["/v3/projects/2"+number] = okReply(projectRow("2"+number, "bulk-project-"+string(rune('a'+i%26)), "organizations/9001"))
	}
	e.fake.routes["/v1/"+billingA+"/budgets"] = okReply(`{"budgets":[` + strings.Join(rows, ",") + `]}`)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if strings.HasPrefix(req.URL.Path, "/v3/projects/") {
			return &reply{status: 200, body: projectRow(strings.TrimPrefix(req.URL.Path, "/v3/projects/"), "bulk-project-x", "folders/7001")}
		}
		if req.URL.Path == "/v3/folders/7001" {
			return &reply{status: 200, body: `{"name":"folders/7001","parent":"organizations/9999"}`}
		}
		return nil
	}
	if _, err := e.invoke(budgetsListID, `{"billing_account":"`+billingA+`"}`); !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestBudgetsGetRefusesABudgetOutsideTheProjectTargets(t *testing.T) {
	e := newEnvironment(t, []string{billingA, "projects/my-project-123"})
	e.fake.routes["/v1/"+budgetPrefix+"foreign"] = okReply(budgetRow("foreign", "Foreign", "projects/other-project-1"))
	e.fake.routes["/v1/"+budgetPrefix+"own"] = okReply(budgetRow("own", "Own", "projects/my-project-123"))
	e.fake.routes["/v1/"+budgetPrefix+"account"] = okReply(budgetRow("account", "Whole account"))
	_, err := e.invoke(budgetsGetID, `{"billing_account":"`+billingA+`","budget_id":"foreign"}`)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "other-project") || strings.Contains(err.Error(), "Foreign") {
		t.Errorf("err = %v", err)
	}
	for _, id := range []string{"own", "account"} {
		if _, err := e.invoke(budgetsGetID, `{"billing_account":"`+billingA+`","budget_id":"`+id+`"}`); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	// An answer for another budget is invalid.
	e.fake.routes["/v1/"+budgetPrefix+"own"] = okReply(budgetRow("other", "Other"))
	if _, err := e.invoke(budgetsGetID, `{"billing_account":"`+billingA+`","budget_id":"own"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q", classOf(err))
	}
}

func TestBudgetsBoundTextAndMapForbiddenAndNotFound(t *testing.T) {
	e := newEnvironment(t, []string{billingA})
	long := strings.Repeat("x", 600) + `\u0007`
	e.fake.routes["/v1/"+budgetPrefix+"b1"] = okReply(budgetRow("b1", long))
	out, err := e.invoke(budgetsGetID, `{"billing_account":"`+billingA+`","budget_id":"b1"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "\u0007") || strings.Count(out, "x") > maxNameText {
		t.Errorf("display name is not bounded: %d", len(out))
	}
	for status, want := range map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound} {
		state := map[int]string{403: "PERMISSION_DENIED", 404: "NOT_FOUND"}[status]
		for tool, args := range map[string]string{
			budgetsListID: `{"billing_account":"` + billingA + `"}`,
			budgetsGetID:  `{"billing_account":"` + billingA + `","budget_id":"b1"}`,
		} {
			e := newEnvironment(t, []string{billingA})
			e.fake.hook = func(_ int, req *http.Request) *reply {
				return &reply{status: status, body: errorObject(status, state, "")}
			}
			_, err := e.invoke(tool, args)
			if classOf(err) != want {
				t.Errorf("%s %d: class = %q, want %q (%v)", tool, status, classOf(err), want, err)
			}
			if err != nil && strings.Contains(err.Error(), detailCanary) {
				t.Errorf("%s %d: the error echoes the provider text", tool, status)
			}
		}
	}
}

// TestNoToolChangesBudgets proves that the provider registers exactly two budget tools, both reads, and that
// no tool names a changing method of the Budget API.
func TestNoToolChangesBudgets(t *testing.T) {
	e := newEnvironment(t, []string{billingA})
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var budgets []string
	for _, tool := range metadata.Tools {
		if strings.Contains(tool.ID, "budget") {
			budgets = append(budgets, tool.ID)
			if tool.Effect != config.EffectRead {
				t.Errorf("%s has the effect %v", tool.ID, tool.Effect)
			}
		}
		text := strings.ToLower(tool.ID + " " + tool.Title + " " + tool.Description)
		if strings.Contains(tool.ID, "budget") {
			for _, banned := range []string{"create", "patch", "delete", "update"} {
				if strings.Contains(tool.ID, banned) {
					t.Errorf("%s names %s", tool.ID, banned)
				}
			}
		}
		if strings.Contains(text, "budgets.create") || strings.Contains(text, "budgets.patch") || strings.Contains(text, "budgets.delete") {
			t.Errorf("%s mentions a changing budget method", tool.ID)
		}
	}
	slices.Sort(budgets)
	if want := []string{budgetsGetID, budgetsListID}; !slices.Equal(budgets, want) {
		t.Errorf("budget tools = %v, want %v", budgets, want)
	}
	e.fake.routes["/v1/"+billingA+"/budgets"] = okReply(`{}`)
	e.fake.routes["/v1/"+budgetPrefix+"b1"] = okReply(budgetRow("b1", "x"))
	for tool, args := range map[string]string{budgetsListID: `{"billing_account":"` + billingA + `"}`,
		budgetsGetID: `{"billing_account":"` + billingA + `","budget_id":"b1"}`} {
		if _, err := e.invoke(tool, args); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	for _, r := range e.fake.api() {
		if r.method != http.MethodGet || r.body != "" || !strings.HasPrefix(r.url, "https://billingbudgets.googleapis.com/v1/") {
			t.Errorf("request %s %s", r.method, r.url)
		}
	}
}

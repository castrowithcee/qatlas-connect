package googlecloud

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	billingA = "billingAccounts/012345-ABCDEF-678901"
	billingB = "billingAccounts/AAAAAA-BBBBBB-CCCCCC"
	billingC = "billingAccounts/ZZZZZZ-YYYYYY-XXXXXX"
)

func accountRow(name string, open bool, display string) string {
	row := `{"name":"` + name + `","displayName":"` + display + `"`
	if open {
		row += `,"open":true`
	}
	return row + `,"parent":"organizations/9001","currencyCode":"EUR"}`
}

func billedRow(project, account string, enabled bool) string {
	row := `{"name":"projects/` + project + `/billingInfo","projectId":"` + project + `"`
	if account != "" {
		row += `,"billingAccountName":"` + account + `"`
	}
	if enabled {
		row += `,"billingEnabled":true`
	}
	return row + `}`
}

func TestBillingAccountsListFollowsTwoPagesAndKeepsOnlyTargets(t *testing.T) {
	e := newEnvironment(t, []string{billingA, billingB})
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if req.URL.Path != "/v1/billingAccounts" {
			return nil
		}
		if req.URL.Query().Get("pageToken") == "" {
			return &reply{status: 200, body: `{"billingAccounts":[` + accountRow(billingC, true, "Foreign") + `,` +
				accountRow(billingA, true, "Main") + `],"nextPageToken":"p2"}`}
		}
		return &reply{status: 200, body: `{"billingAccounts":[` + accountRow(billingC, false, "Foreign 2") + `,` +
			accountRow(billingB, false, "Old") + `]}`}
	}
	out, err := e.invoke(billingAccountsListID, `{"parent":"organizations/9001","page_size":2}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got := listOf(t, out, "billingAccounts")
	if len(rows) != 1 || rows[0]["name"] != billingA || rows[0]["open"] != true || rows[0]["currencyCode"] != "EUR" ||
		got["has_more"] != true {
		t.Fatalf("page 1 = %s", out)
	}
	out, err = e.invoke(billingAccountsListID, `{"parent":"organizations/9001","page_size":2,"cursor":"`+
		got["next_cursor"].(string)+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	rows, got = listOf(t, out, "billingAccounts")
	if len(rows) != 1 || rows[0]["name"] != billingB || rows[0]["open"] != false || got["has_more"] != false {
		t.Fatalf("page 2 = %s", out)
	}
	for _, leaked := range []string{"ZZZZZZ", "Foreign"} {
		if strings.Contains(out, leaked) {
			t.Errorf("the result names %q", leaked)
		}
	}
	queries := queriesOf(e)
	if len(queries) != 2 || queries[0].Get("parent") != "organizations/9001" || queries[0].Get("pageSize") != "2" ||
		queries[1].Get("pageToken") != "p2" {
		t.Errorf("queries = %v", queries)
	}
	if paths := apiPaths(e); paths[0] != "cloudbilling.googleapis.com/v1/billingAccounts" {
		t.Errorf("path = %v", paths)
	}
}

func TestBillingAccountsListBuildsTheSubaccountFilterAndBoundsText(t *testing.T) {
	e := newEnvironment(t, []string{billingA})
	long := strings.Repeat("x", 600) + `\u0007`
	e.fake.routes["/v1/billingAccounts"] = okReply(`{"billingAccounts":[` + accountRow(billingA, true, long) + `]}`)
	out, err := e.invoke(billingAccountsListID, `{"master_billing_account":"`+billingB+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := "master_billing_account=" + billingB; queriesOf(e)[0].Get("filter") != want {
		t.Errorf("filter = %q", queriesOf(e)[0].Get("filter"))
	}
	rows, _ := listOf(t, out, "billingAccounts")
	if name := rows[0]["displayName"].(string); len(name) > maxNameText || strings.Contains(name, "\u0007") {
		t.Errorf("display name is not bounded: %d", len(name))
	}
}

func TestBillingToolsRefuseBeforeIO(t *testing.T) {
	for name, c := range map[string]struct {
		targets []string
		tool    string
		args    string
	}{
		"list unbound":              {nil, billingAccountsListID, `{}`},
		"list project target":       {[]string{"projects/my-project-123"}, billingAccountsListID, `{}`},
		"list bad parent":           {[]string{billingA}, billingAccountsListID, `{"parent":"folders/1"}`},
		"list bad master":           {[]string{billingA}, billingAccountsListID, `{"master_billing_account":"billingAccounts/x"}`},
		"list page size":            {[]string{billingA}, billingAccountsListID, `{"page_size":101}`},
		"list cursor":               {[]string{billingA}, billingAccountsListID, `{"cursor":"bogus"}`},
		"get unbound":               {nil, billingAccountsGetID, `{"billing_account":"` + billingA + `"}`},
		"get without target":        {[]string{billingB}, billingAccountsGetID, `{"billing_account":"` + billingA + `"}`},
		"get invalid name":          {[]string{billingA}, billingAccountsGetID, `{"billing_account":"billingAccounts/012345-abcdef-678901"}`},
		"get path":                  {[]string{billingA}, billingAccountsGetID, `{"billing_account":"billingAccounts/012345-ABCDEF-678901/../x"}`},
		"get bare id":               {[]string{billingA}, billingAccountsGetID, `{"billing_account":"012345-ABCDEF-678901"}`},
		"projects without target":   {[]string{billingB, "projects/my-project-123"}, billingAccountsListProjectsID, `{"billing_account":"` + billingA + `"}`},
		"projects billing only":     {[]string{billingA}, billingAccountsListProjectsID, `{"billing_account":"` + billingA + `"}`},
		"projects invalid name":     {[]string{billingA, "projects/my-project-123"}, billingAccountsListProjectsID, `{"billing_account":"x"}`},
		"projects cursor":           {[]string{billingA, "projects/my-project-123"}, billingAccountsListProjectsID, `{"billing_account":"` + billingA + `","cursor":"bogus"}`},
		"info project outside":      {[]string{"projects/other-project-1"}, projectBillingInfoGetID, `{"project_id":"my-project-123"}`},
		"info billing target only":  {[]string{billingA}, projectBillingInfoGetID, `{"project_id":"my-project-123"}`},
		"info path":                 {nil, projectBillingInfoGetID, `{"project_id":"projects/x"}`},
		"info invalid project name": {nil, projectBillingInfoGetID, `{"project_id":"UPPER"}`},
	} {
		e := newEnvironment(t, c.targets)
		if _, err := e.invoke(c.tool, c.args); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if len(e.fake.api()) != 0 || e.fake.tokenRequests() != 0 || e.lookups != 0 {
			t.Errorf("%s: api=%d tokens=%d lookups=%d", name, len(e.fake.api()), e.fake.tokenRequests(), e.lookups)
		}
	}
	// The handler itself refuses a value the schema would not have let through.
	e := newEnvironment(t, []string{billingA})
	_, err := handleBillingAccountsGet(t.Context(), e.resolved(), e.secrets(), e.red, []byte(`{"billing_account":"x"}`))
	if !isInvalidRequest(err) || len(e.fake.api()) != 0 {
		t.Errorf("err = %v", err)
	}
}

func TestBillingToolsAreOfferedOnlyToABindingThatReachesThem(t *testing.T) {
	for _, c := range []struct {
		tool    string
		targets []string
		want    bool
	}{
		{billingAccountsListID, nil, false},
		{billingAccountsGetID, []string{"organizations/1"}, false},
		{billingAccountsGetID, []string{billingA}, true},
		{billingAccountsListProjectsID, []string{billingA}, false},
		{billingAccountsListProjectsID, []string{billingA, "folders/1"}, true},
		{billingAccountsListProjectsID, []string{"projects/my-project-123"}, false},
		{projectBillingInfoGetID, nil, true},
		{projectBillingInfoGetID, []string{"projects/my-project-123"}, true},
		{projectBillingInfoGetID, []string{billingA}, false},
	} {
		if got, _ := targetReaches(c.tool, c.targets); got != c.want {
			t.Errorf("%s with %v: reaches = %v", c.tool, c.targets, got)
		}
	}
}

func TestBillingAccountsGetOpenAndClosed(t *testing.T) {
	e := newEnvironment(t, []string{billingA, billingB})
	e.fake.routes["/v1/"+billingA] = okReply(`{"name":"` + billingA + `","open":true,"displayName":"Main",` +
		`"masterBillingAccount":"` + billingC + `","parent":"organizations/9001","currencyCode":"USD"}`)
	e.fake.routes["/v1/"+billingB] = okReply(`{"name":"` + billingB + `","displayName":"Closed"}`)
	out, err := e.invoke(billingAccountsGetID, `{"billing_account":"`+billingA+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"open":true`, `"displayName":"Main"`, `"masterBillingAccount":"` + billingC + `"`,
		`"parent":"organizations/9001"`, `"currencyCode":"USD"`} {
		if !strings.Contains(out, want) {
			t.Errorf("result lacks %s: %s", want, out)
		}
	}
	out, err = e.invoke(billingAccountsGetID, `{"billing_account":"`+billingB+`"}`)
	if err != nil || !strings.Contains(out, `"open":false`) {
		t.Errorf("closed account = %s, %v", out, err)
	}
	if paths := apiPaths(e); paths[0] != "cloudbilling.googleapis.com/v1/"+billingA {
		t.Errorf("path = %v", paths)
	}
	// An answer for another account is invalid.
	e.fake.routes["/v1/"+billingB] = okReply(`{"name":"` + billingC + `"}`)
	if _, err := e.invoke(billingAccountsGetID, `{"billing_account":"`+billingB+`"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q", classOf(err))
	}
}

func TestBillingReadsMapForbiddenAndNotFound(t *testing.T) {
	for status, want := range map[int]provider.Class{403: provider.ClassPermission, 404: provider.ClassNotFound} {
		state := map[int]string{403: "PERMISSION_DENIED", 404: "NOT_FOUND"}[status]
		for tool, args := range map[string]string{
			billingAccountsGetID:          `{"billing_account":"` + billingA + `"}`,
			billingAccountsListID:         `{}`,
			billingAccountsListProjectsID: `{"billing_account":"` + billingA + `"}`,
			projectBillingInfoGetID:       `{"project_id":"my-project-123"}`,
		} {
			e := newEnvironment(t, []string{billingA, "projects/my-project-123"})
			e.fake.hook = func(_ int, req *http.Request) *reply {
				if strings.HasPrefix(req.URL.Path, "/v1/") {
					return &reply{status: status, body: errorObject(status, state, "")}
				}
				return nil
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

func TestBillingAccountsListProjectsFiltersRowsAgainstTheTargets(t *testing.T) {
	// folders/2001 -> organizations/9001, folders/7001 -> organizations/9999.
	routes := func(e *environment) {
		e.fake.routes["/v3/projects/inside-project-1"] = okReply(projectRow("1", "inside-project-1", "folders/2001"))
		e.fake.routes["/v3/projects/inside-project-2"] = okReply(projectRow("2", "inside-project-2", "folders/2001"))
		e.fake.routes["/v3/projects/outside-project-3"] = okReply(projectRow("3", "outside-project-3", "folders/7001"))
		e.fake.routes["/v3/projects/hidden-project-4"] = reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}
		e.fake.routes["/v3/folders/2001"] = okReply(`{"name":"folders/2001","parent":"organizations/9001"}`)
		e.fake.routes["/v3/folders/7001"] = okReply(`{"name":"folders/7001","parent":"organizations/9999"}`)
		e.fake.routes["/v1/"+billingA+"/projects"] = okReply(`{"projectBillingInfo":[` + strings.Join([]string{
			billedRow("inside-project-1", billingA, true),
			billedRow("outside-project-3", billingA, true),
			billedRow("direct-project-9", billingA, false),
			billedRow("hidden-project-4", billingA, true),
			billedRow("inside-project-2", "", false),
		}, ",") + `],"nextPageToken":"more"}`)
	}
	t.Run("organization and folder targets", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA, "organizations/9001", "projects/direct-project-9"})
		routes(e)
		out, err := e.invoke(billingAccountsListProjectsID, `{"billing_account":"`+billingA+`","page_size":5}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, got := listOf(t, out, "projectBillingInfo")
		var ids []string
		for _, row := range rows {
			ids = append(ids, row["projectId"].(string))
		}
		if want := []string{"inside-project-1", "direct-project-9", "inside-project-2"}; !slices.Equal(ids, want) {
			t.Errorf("ids = %v, want %v", ids, want)
		}
		if rows[0]["billingEnabled"] != true || rows[1]["billingEnabled"] != false || rows[0]["billingAccountName"] != billingA {
			t.Errorf("rows = %s", out)
		}
		if got["has_more"] != true || got["next_cursor"] == "" {
			t.Errorf("paging = %v", got)
		}
		for _, leaked := range []string{"outside-project", "hidden-project", "7001"} {
			if strings.Contains(out, leaked) {
				t.Errorf("the result names %q", leaked)
			}
		}
		// Each project below the organization costs one read; the target project costs none.
		var reads []string
		for _, p := range apiPaths(e) {
			if strings.Contains(p, "cloudresourcemanager") {
				reads = append(reads, p)
			}
		}
		if len(reads) > 8 {
			t.Errorf("reads = %v", reads)
		}
		for _, r := range e.fake.api() {
			if r.method != http.MethodGet {
				t.Errorf("method = %s", r.method)
			}
		}
	})
	t.Run("project target only costs no read", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA, "projects/direct-project-9"})
		routes(e)
		out, err := e.invoke(billingAccountsListProjectsID, `{"billing_account":"`+billingA+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := listOf(t, out, "projectBillingInfo")
		if len(rows) != 1 || rows[0]["projectId"] != "direct-project-9" {
			t.Errorf("rows = %s", out)
		}
		if len(e.fake.api()) != 1 {
			t.Errorf("requests = %v", apiPaths(e))
		}
	})
	t.Run("a row of another account is invalid", func(t *testing.T) {
		e := newEnvironment(t, []string{billingA, "projects/direct-project-9"})
		e.fake.routes["/v1/"+billingA+"/projects"] = okReply(`{"projectBillingInfo":[` + billedRow("direct-project-9", billingB, true) + `]}`)
		if _, err := e.invoke(billingAccountsListProjectsID, `{"billing_account":"`+billingA+`"}`); classOf(err) != provider.ClassInvalidResponse {
			t.Errorf("class = %q", classOf(err))
		}
	})
}

func TestBillingAccountsListProjectsBoundsTheProofReads(t *testing.T) {
	e := newEnvironment(t, []string{billingA, "organizations/9001"})
	var rows []string
	for i := range maxProofReads + 2 {
		id := "bulk-project-" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		rows = append(rows, billedRow(id, billingA, true))
		e.fake.routes["/v3/projects/"+id] = okReply(projectRow("1", id, "organizations/9001"))
	}
	e.fake.routes["/v1/"+billingA+"/projects"] = okReply(`{"projectBillingInfo":[` + strings.Join(rows, ",") + `]}`)
	_, err := e.invoke(billingAccountsListProjectsID, `{"billing_account":"`+billingA+`"}`)
	if !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	var reads int
	for _, p := range apiPaths(e) {
		if strings.Contains(p, "cloudresourcemanager") {
			reads++
		}
	}
	if reads != maxProofReads {
		t.Errorf("reads = %d, want %d", reads, maxProofReads)
	}
}

func TestProjectBillingInfoGet(t *testing.T) {
	e := newEnvironment(t, []string{"projects/my-project-123"})
	e.fake.routes["/v1/projects/my-project-123/billingInfo"] = okReply(
		`{"name":"projects/my-project-123/billingInfo","projectId":"my-project-123"}`)
	out, err := e.invoke(projectBillingInfoGetID, `{"project_id":"my-project-123"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"billingEnabled":false`) || strings.Contains(out, "billingAccountName") {
		t.Errorf("disabled billing = %s", out)
	}
	e.fake.routes["/v1/projects/my-project-123/billingInfo"] = okReply(
		`{"name":"projects/my-project-123/billingInfo","projectId":"my-project-123","billingAccountName":"` + billingC +
			`","billingEnabled":true}`)
	out, err = e.invoke(projectBillingInfoGetID, `{"project_id":"my-project-123"}`)
	if err != nil || !strings.Contains(out, `"billingEnabled":true`) || !strings.Contains(out, billingC) {
		t.Errorf("enabled billing = %s, %v", out, err)
	}
	if paths := apiPaths(e); paths[0] != "cloudbilling.googleapis.com/v1/projects/my-project-123/billingInfo" {
		t.Errorf("path = %v", paths)
	}
	e.fake.routes["/v1/projects/my-project-123/billingInfo"] = okReply(
		`{"name":"projects/my-project-123/billingInfo","billingAccountName":"not-a-billing-account"}`)
	if _, err := e.invoke(projectBillingInfoGetID, `{"project_id":"my-project-123"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("class = %q", classOf(err))
	}
}

func TestProjectBillingInfoGetProvesAProjectBelowAFolder(t *testing.T) {
	e := newEnvironment(t, []string{"folders/2001"})
	e.fake.routes["/v3/projects/inside-project-1"] = okReply(projectRow("1", "inside-project-1", "folders/2001"))
	e.fake.routes["/v3/projects/outside-project-3"] = okReply(projectRow("3", "outside-project-3", "folders/7001"))
	e.fake.routes["/v3/folders/7001"] = okReply(`{"name":"folders/7001","parent":"organizations/9999"}`)
	for _, id := range []string{"inside-project-1", "outside-project-3"} {
		e.fake.routes["/v1/projects/"+id+"/billingInfo"] = okReply(billedRow(id, billingA, true))
	}
	if _, err := e.invoke(projectBillingInfoGetID, `{"project_id":"inside-project-1"}`); err != nil {
		t.Fatal(err)
	}
	before := len(e.fake.api())
	_, err := e.invoke(projectBillingInfoGetID, `{"project_id":"outside-project-3"}`)
	if !isInvalidRequest(err) {
		t.Fatalf("err = %v", err)
	}
	for _, p := range apiPaths(e)[before:] {
		if strings.Contains(p, "cloudbilling") {
			t.Errorf("a refused project reached Cloud Billing: %s", p)
		}
	}
}

// TestNoToolChangesBilling proves that nothing registered can change billing: the provider offers exactly four
// billing tools, all of them reads, none of them names a changing Google method, and every request they send
// is a GET.
func TestNoToolChangesBilling(t *testing.T) {
	e := newEnvironment(t, []string{billingA, "projects/my-project-123"})
	metadata, _ := e.reg.ProviderMetadata(Provider)
	var billing []string
	for _, tool := range metadata.Tools {
		text := strings.ToLower(tool.ID + " " + tool.Title + " " + tool.Description)
		if strings.Contains(tool.ID, "billing") {
			billing = append(billing, tool.ID)
			if tool.Effect != config.EffectRead {
				t.Errorf("%s has the effect %v", tool.ID, tool.Effect)
			}
		}
		for _, banned := range []string{"update-billing", "updatebillinginfo", "billing-accounts.move", "billing-accounts.patch",
			"billing-accounts.create", "set-iam-policy", "setiampolicy"} {
			if strings.Contains(text, banned) {
				t.Errorf("%s mentions %s", tool.ID, banned)
			}
		}
	}
	slices.Sort(billing)
	want := []string{billingAccountsGetID, billingAccountsListID, billingAccountsListProjectsID, projectBillingInfoGetID}
	slices.Sort(want)
	if !slices.Equal(billing, want) {
		t.Errorf("billing tools = %v, want %v", billing, want)
	}
	// Every billing tool, called with a target that passes, sends only GETs to cloudbilling.
	e.fake.routes["/v1/billingAccounts"] = okReply(`{}`)
	e.fake.routes["/v1/"+billingA] = okReply(accountRow(billingA, true, "a"))
	e.fake.routes["/v1/"+billingA+"/projects"] = okReply(`{}`)
	e.fake.routes["/v1/projects/my-project-123/billingInfo"] = okReply(billedRow("my-project-123", billingA, true))
	for tool, args := range map[string]string{
		billingAccountsListID: `{}`, billingAccountsGetID: `{"billing_account":"` + billingA + `"}`,
		billingAccountsListProjectsID: `{"billing_account":"` + billingA + `"}`,
		projectBillingInfoGetID:       `{"project_id":"my-project-123"}`,
	} {
		if _, err := e.invoke(tool, args); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	for _, r := range e.fake.api() {
		u, _ := url.Parse(r.url)
		if r.method != http.MethodGet || !strings.HasSuffix(u.Host, "cloudbilling.googleapis.com") || r.body != "" ||
			strings.Contains(u.Path, ":move") {
			t.Errorf("request %s %s", r.method, r.url)
		}
	}
}

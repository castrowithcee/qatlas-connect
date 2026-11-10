package github

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// invokeDefault runs a tool as an agent does by default, without asking for the whole result.
func invokeDefault(t *testing.T, core *application.Core, operation, arguments string, confirmed bool) json.RawMessage {
	t.Helper()
	response, err := core.Invoke(context.Background(), application.InvokeRequest{Operation: operation,
		Connection: "repo", Arguments: json.RawMessage(arguments), Confirmed: confirmed})
	if err != nil {
		t.Fatalf("%s: %v", operation, err)
	}
	return response.Result
}

func memberNames(t *testing.T, result json.RawMessage) string {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(result, &object); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// Without full, the issue, pull request, and check tools answer their default fields only, and a change
// answers only which object it changed.
func TestDefaultFieldsNarrowTheAnswer(t *testing.T) {
	core := issueCore(t, 1347, documentedIssue)
	for _, call := range issueToolCalls(1347) {
		got := memberNames(t, invokeDefault(t, core, call.id, call.args, call.confirm))
		want := "id,number,repository,state,state_reason,url"
		if call.id == issuesGet.ID {
			want = "assignees,author,body,comments,created_at,draft,issue_type,labels,locked,milestone,number," +
				"repository,state,state_reason,title,updated_at,url"
		}
		if got != want {
			t.Errorf("%s answers %s, want %s", call.id, got, want)
		}
	}

	_, core = cannedCore(t, documentedPull, false, discoveryConfig)
	result := invokeDefault(t, core, pullsGet.ID, `{"number":42}`, false)
	for _, withheld := range []string{"head_detail", "base_detail", "author_detail", "links", "labels_details"} {
		if _, ok := members(t, result)[withheld]; ok {
			t.Errorf("pull-requests.get answers %s by default", withheld)
		}
	}
	if _, ok := members(t, result)["body"]; !ok {
		t.Errorf("pull-requests.get lacks the body: %s", result)
	}

	_, base := serveWidthChecks(t, constant(`{"total_count":1,"check_runs":[`+widthCheckRun+`]}`),
		constant(widthCombined))
	red := &redact.Redactor{}
	core = application.New(registry(t), coreConfig(base), resolver(red, nil), red)
	if got := memberNames(t, invokeDefault(t, core, pullChecksList.ID, `{"number":7}`, false)); got !=
		"checks,head_sha,number,overall,repository,truncated" {
		t.Errorf("pull-request-checks.list answers %s", got)
	}
}

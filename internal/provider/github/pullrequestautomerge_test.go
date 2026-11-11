package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// autoMergeFake answers the auto-merge mutations on top of fakePulls. refuse, when set, is the GraphQL error
// list the next mutation answers with; status, when set, is the HTTP answer of the next mutation instead.
type autoMergeFake struct {
	*fakePulls
	refuse string
	status func(http.ResponseWriter)
}

func (a *autoMergeFake) route(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/api/graphql" {
		return a.fakePulls.route(w, r)
	}
	w.Header().Set("Content-Type", "application/json")
	document := a.lastDocument()
	enable := strings.Contains(document, "enablePullRequestAutoMerge")
	if !enable && !strings.Contains(document, "disablePullRequestAutoMerge") {
		return a.fakePulls.route(w, r)
	}
	if a.status != nil {
		answer := a.status
		a.status = nil
		answer(w)
		return true
	}
	if a.refuse != "" {
		fmt.Fprintf(w, `{"data":null,"errors":[%s]}`, a.refuse)
		a.refuse = ""
		return true
	}
	number, _ := pullNodeNumber(a.lastVariables()["id"].(string))
	pull := &a.pulls[a.index(number)]
	alias := "disable"
	if enable {
		alias = "enable"
		pull.autoMerge = strings.ToLower(a.lastVariables()["method"].(string))
	} else {
		pull.autoMerge = ""
	}
	request := "null"
	if pull.autoMerge != "" {
		request = fmt.Sprintf(`{"mergeMethod":%q}`, strings.ToUpper(pull.autoMerge))
	}
	fmt.Fprintf(w, `{"data":{%q:{"pullRequest":{"number":%d,"autoMergeRequest":%s}}}}`, alias, number, request)
	return true
}

func (a *autoMergeFake) mutations() int {
	count := 0
	for _, request := range a.f.recorded() {
		if strings.Contains(request.document, "AutoMerge(") {
			count++
		}
	}
	return count
}

func serveAutoMerge(t *testing.T) (*autoMergeFake, *application.Core, *int) {
	t.Helper()
	f, p, base := servePulls(t)
	fake := &autoMergeFake{fakePulls: p}
	f.failure = fake.route
	reads := 0
	red := &redact.Redactor{}
	cfg := coreChangeConfig(base)
	cfg.Connections["merger"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate},
		Tools:   []string{pullsGet.ID, pullAutoMergeEnable.ID, pullAutoMergeDisable.ID}}
	cfg.Connections["wide"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Effects: []config.Effect{config.EffectRead, config.EffectUpdate},
		Tools: []string{pullAutoMergeEnable.ID, pullAutoMergeDisable.ID}}
	cfg.Connections["excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{"repos/octo-org/secret"},
		Effects: []config.Effect{config.EffectRead, config.EffectUpdate},
		Tools:   []string{pullAutoMergeEnable.ID, pullAutoMergeDisable.ID}}
	return fake, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func enableArguments(number int, sha, method string) string {
	return fmt.Sprintf(`{"number":%d,"expected_head_sha":%q,"merge_method":%q}`, number, sha, method)
}

// Auto-merge is enabled for the head the caller read, with GitHub told the same head, and disabled again;
// disabling a pull request without auto-merge sends no mutation.
func TestPullRequestAutoMergeEnableAndDisable(t *testing.T) {
	fake, core, _ := serveAutoMerge(t)
	head := strings.Repeat("a", 40)
	fake.pulls = append(fake.pulls, fakePull{number: 30, title: "x", state: "open", headSHA: head})

	result, err := invoke(t, core, "github.pull-request-auto-merge.enable", "merger",
		fmt.Sprintf(`{"number":30,"expected_head_sha":%q,"merge_method":"squash","commit_title":"T","commit_message":"M"}`,
			head), true)
	if err != nil {
		t.Fatal(err)
	}
	var state PullRequestAutoMerge
	if err := json.Unmarshal(result, &state); err != nil || state != (PullRequestAutoMerge{Number: 30, AutoMerge: true,
		MergeMethod: "squash"}) {
		t.Fatalf("enable = %s, %v", result, err)
	}
	variables := fake.lastVariables()
	if variables["oid"] != head || variables["method"] != "SQUASH" || variables["headline"] != "T" ||
		variables["body"] != "M" || variables["id"] != pullNodeID(30) {
		t.Errorf("enable variables = %+v, want the read head, method, commit text, and the node of the read", variables)
	}

	result, err = invoke(t, core, "github.pull-request-auto-merge.disable", "merger", `{"number":30}`, true)
	if err != nil {
		t.Fatal(err)
	}
	state = PullRequestAutoMerge{}
	if err := json.Unmarshal(result, &state); err != nil || state != (PullRequestAutoMerge{Number: 30}) {
		t.Fatalf("disable = %s, %v", result, err)
	}

	before := fake.mutations()
	result, err = invoke(t, core, "github.pull-request-auto-merge.disable", "merger", `{"number":30}`, true)
	if err != nil || fake.mutations() != before {
		t.Fatalf("disable of a pull request without auto-merge = %s, %v, mutations %d to %d", result, err, before,
			fake.mutations())
	}
}

// A head that differs from the read one, a pull request that is not open, an unknown number, and bad
// arguments end before any mutation.
func TestPullRequestAutoMergeEnableRefusesBeforeAMutation(t *testing.T) {
	fake, core, _ := serveAutoMerge(t)
	head, other := strings.Repeat("a", 40), strings.Repeat("b", 40)
	fake.pulls = append(fake.pulls,
		fakePull{number: 31, title: "x", state: "open", headSHA: head},
		fakePull{number: 32, title: "y", state: "closed", headSHA: head},
		fakePull{number: 33, title: "z", state: "closed", merged: true, headSHA: head})
	for name, tt := range map[string]struct {
		arguments string
		class     bool
		notFound  bool
		contains  string
	}{
		"a changed head":     {arguments: enableArguments(31, other, "merge"), class: true, contains: head},
		"a closed pull":      {arguments: enableArguments(32, head, "merge"), class: true, contains: "not open"},
		"a merged pull":      {arguments: enableArguments(33, head, "merge"), class: true, contains: "not open"},
		"an unknown number":  {arguments: enableArguments(999, head, "merge"), notFound: true},
		"a short sha":        {arguments: enableArguments(31, head[:7], "merge"), class: true},
		"a missing method":   {arguments: fmt.Sprintf(`{"number":31,"expected_head_sha":%q}`, head), class: true},
		"an unknown method":  {arguments: enableArguments(31, head, "fast-forward"), class: true},
		"an empty commit":    {arguments: fmt.Sprintf(`{"number":31,"expected_head_sha":%q,"merge_method":"merge","commit_title":" "}`, head), class: true},
		"a missing head sha": {arguments: `{"number":31,"merge_method":"merge"}`, class: true},
	} {
		t.Run(name, func(t *testing.T) {
			before := fake.mutations()
			_, err := invoke(t, core, "github.pull-request-auto-merge.enable", "merger", tt.arguments, true)
			switch {
			case tt.notFound && classOf(err) != provider.ClassNotFound:
				t.Errorf("err = %v, want not found", err)
			case tt.class && !isInvalidRequest(err):
				t.Errorf("err = %v, want an invalid request", err)
			case err == nil:
				t.Error("err = nil, want a refusal")
			case tt.contains != "" && !strings.Contains(err.Error(), tt.contains):
				t.Errorf("err = %v, want it to contain %q", err, tt.contains)
			}
			if fake.mutations() != before {
				t.Error("a refused enable sent a mutation")
			}
		})
	}
}

// GitHub's refusals become Qatlas' own messages without GitHub's text; every mutation is sent exactly once.
func TestPullRequestAutoMergeMapsGitHubRefusals(t *testing.T) {
	fake, core, _ := serveAutoMerge(t)
	head := strings.Repeat("c", 40)
	fake.pulls = append(fake.pulls, fakePull{number: 34, title: "x", state: "open", headSHA: head, autoMerge: "merge"})
	const marker = "marker-text-from-github"
	for name, tt := range map[string]struct {
		operation, arguments, refuse string
		wantClass                    provider.Class
		contains                     string
	}{
		"auto-merge not allowed": {"enable", enableArguments(34, head, "merge"),
			`{"type":"UNPROCESSABLE","message":"Auto merge is not allowed for this repository ` + marker + `"}`,
			provider.ClassProviderError, "allow auto-merge in the repository's settings"},
		"already clean": {"enable", enableArguments(34, head, "merge"),
			`{"type":"UNPROCESSABLE","message":"Pull request is in clean status ` + marker + `"}`,
			provider.ClassProviderError, "already ready to merge"},
		"head moved at github": {"enable", enableArguments(34, head, "merge"),
			`{"type":"UNPROCESSABLE","message":"Head branch was modified. Review and try the merge again."}`,
			"", head},
		"forbidden": {"enable", enableArguments(34, head, "merge"),
			`{"type":"FORBIDDEN","message":"Resource not accessible ` + marker + `"}`,
			provider.ClassPermission, "Contents: read and write"},
		"not found": {"enable", enableArguments(34, head, "merge"),
			`{"type":"NOT_FOUND","message":"x ` + marker + `"}`, provider.ClassNotFound, ""},
		"rate limited": {"enable", enableArguments(34, head, "merge"),
			`{"type":"RATE_LIMITED","message":"API rate limit exceeded"}`, provider.ClassRateLimited, ""},
		"unprocessable": {"enable", enableArguments(34, head, "merge"),
			`{"type":"UNPROCESSABLE","message":"something else ` + marker + `"}`, provider.ClassProviderError,
			"rejected the change"},
		"disable forbidden": {"disable", `{"number":34}`,
			`{"type":"FORBIDDEN","message":"Resource not accessible ` + marker + `"}`,
			provider.ClassPermission, "Contents: read and write"},
		"disable unprocessable": {"disable", `{"number":34}`,
			`{"type":"UNPROCESSABLE","message":"nope ` + marker + `"}`, provider.ClassProviderError,
			"rejected the change"},
	} {
		t.Run(name, func(t *testing.T) {
			fake.refuse = tt.refuse
			before := fake.mutations()
			_, err := invoke(t, core, "github.pull-request-auto-merge."+tt.operation, "merger", tt.arguments, true)
			// An empty class stands for an invalid request.
			if err == nil || (tt.wantClass == "" && !isInvalidRequest(err)) ||
				(tt.wantClass != "" && classOf(err) != tt.wantClass) {
				t.Fatalf("err = %v (%s), want class %q", err, classOf(err), tt.wantClass)
			}
			if strings.Contains(err.Error(), marker) || strings.Contains(err.Error(), "Head branch was modified") {
				t.Errorf("err = %v, want none of GitHub's text", err)
			}
			if tt.contains != "" && !strings.Contains(err.Error(), tt.contains) {
				t.Errorf("err = %v, want it to contain %q", err, tt.contains)
			}
			if fake.mutations()-before != 1 {
				t.Errorf("mutations = %d, want exactly one", fake.mutations()-before)
			}
		})
	}
}

// A mutation whose outcome is unclear is sent once and reports that it may have been applied.
func TestPullRequestAutoMergeUnclearOutcomeIsNeverRepeated(t *testing.T) {
	fake, core, _ := serveAutoMerge(t)
	head := strings.Repeat("d", 40)
	fake.pulls = append(fake.pulls, fakePull{number: 35, title: "x", state: "open", headSHA: head, autoMerge: "merge"})
	for name, answer := range map[string]func(http.ResponseWriter){
		"server error":    func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) },
		"gateway timeout": func(w http.ResponseWriter) { w.WriteHeader(http.StatusGatewayTimeout) },
		"unreadable":      func(w http.ResponseWriter) { fmt.Fprint(w, `{"data":`) },
		"dropped connection": func(w http.ResponseWriter) {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				connection.Close()
			}
		},
	} {
		for _, operation := range []string{"enable", "disable"} {
			t.Run(name+" "+operation, func(t *testing.T) {
				fake.status = answer
				arguments := enableArguments(35, head, "rebase")
				if operation == "disable" {
					arguments = `{"number":35}`
				}
				before := fake.mutations()
				_, err := invoke(t, core, "github.pull-request-auto-merge."+operation, "merger", arguments, true)
				if err == nil || !strings.Contains(err.Error(), "may have been applied") {
					t.Fatalf("err = %v, want an unclear outcome", err)
				}
				if fake.mutations()-before != 1 {
					t.Errorf("mutations = %d, want exactly one, never a retry", fake.mutations()-before)
				}
			})
		}
	}
}

// A repository outside the targets or inside an exclusion is refused before a secret is read or GitHub is
// contacted; enable is not offered without a tools entry.
func TestPullRequestAutoMergeIsBoundToTheRepositoryAndListed(t *testing.T) {
	fake, core, reads := serveAutoMerge(t)
	head := strings.Repeat("e", 40)
	fake.pulls = append(fake.pulls, fakePull{number: 36, title: "x", state: "open", headSHA: head})
	for name, tt := range map[string]struct{ operation, connection, arguments string }{
		"a foreign repository": {"enable", "merger",
			fmt.Sprintf(`{"number":36,"expected_head_sha":%q,"merge_method":"merge","repository":"octo-org/other"}`, head)},
		"a foreign repository on disable": {"disable", "merger", `{"number":36,"repository":"octo-org/other"}`},
		"an excluded repository": {"enable", "excluding",
			fmt.Sprintf(`{"number":36,"expected_head_sha":%q,"merge_method":"merge","repository":"octo-org/secret"}`, head)},
		"an excluded repository on disable": {"disable", "excluding", `{"number":36,"repository":"octo-org/secret"}`},
		"a pattern":                         {"disable", "wide", `{"number":36,"repository":"octo-org/*"}`},
	} {
		t.Run(name, func(t *testing.T) {
			*reads = 0
			before := len(fake.f.recorded())
			_, err := invoke(t, core, "github.pull-request-auto-merge."+tt.operation, tt.connection, tt.arguments, true)
			if !isInvalidRequest(err) {
				t.Errorf("err = %v, want an invalid request", err)
			}
			if *reads != 0 || len(fake.f.recorded()) != before {
				t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(fake.f.recorded())-before)
			}
		})
	}

	before := len(fake.f.recorded())
	_, err := invoke(t, core, "github.pull-request-auto-merge.enable", "repo", enableArguments(36, head, "merge"), true)
	if _, ok := err.(*capability.UnsupportedError); !ok {
		t.Errorf("enable without a tools entry = %T %v, want unsupported", err, err)
	}
	if len(fake.f.recorded()) != before {
		t.Error("an unsupported enable reached GitHub")
	}
	if _, err := invoke(t, core, "github.pull-request-auto-merge.enable", "merger", enableArguments(36, head, "merge"), false); err == nil {
		t.Error("an unconfirmed enable succeeded")
	}
}

// Auto-merge is never in a profile: enabling needs a tools entry, disabling is only in the not-recommended
// operator profile.
func TestPullRequestAutoMergeProfiles(t *testing.T) {
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if id == pullAutoMergeEnable.ID {
				t.Errorf("profile %s selects the listed-only enable", profile.ID)
			}
			if id == pullAutoMergeDisable.ID && (profile.ID != "pull-requests-operator" || profile.Recommended) {
				t.Errorf("profile %s selects disable", profile.ID)
			}
		}
	}
}

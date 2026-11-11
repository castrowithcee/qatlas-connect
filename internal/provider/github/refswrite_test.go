package github

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	refsMainSHA   = "1111111111111111111111111111111111111111"
	refsFeatSHA   = "2222222222222222222222222222222222222222"
	refsObjectSHA = "3333333333333333333333333333333333333333"
	refsMergeSHA  = "4444444444444444444444444444444444444444"
	refsSecret    = "SECRETTEXT"
)

// fakeRefsWrite answers the ref, merge, rename, and fork routes of octo-org/example. override replaces the answer of
// one route, named rename, sync, merge, tagobject, tagref, tagread, or tagdelete.
type fakeRefsWrite struct {
	mu       sync.Mutex
	f        *fakeGitHub
	branches map[string]string
	tags     map[string]string
	commits  map[string]string
	override map[string]func(http.ResponseWriter)
}

func (r *fakeRefsWrite) answer(rw http.ResponseWriter, route string) bool {
	if fn := r.override[route]; fn != nil {
		fn(rw)
		return true
	}
	return false
}

func refsReply(rw http.ResponseWriter, status int, body string) {
	rw.WriteHeader(status)
	fmt.Fprint(rw, body)
}

func (r *fakeRefsWrite) route(rw http.ResponseWriter, req *http.Request) bool {
	rest, ok := strings.CutPrefix(req.URL.Path, actionsPrefix)
	if !ok {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var body map[string]any
	if requests := r.f.recorded(); req.Method != http.MethodGet && len(requests) > 0 {
		body = requests[len(requests)-1].body
	}
	rw.Header().Set("Content-Type", "application/json")
	switch {
	case req.Method == http.MethodGet && strings.HasPrefix(rest, "commits/"):
		sha, exists := r.commits[strings.TrimPrefix(rest, "commits/")]
		if !exists {
			refsReply(rw, http.StatusNotFound, `{"message":"Not Found"}`)
			return true
		}
		fmt.Fprintf(rw, `{"sha":%q}`, sha)
	case req.Method == http.MethodPost && strings.HasPrefix(rest, "branches/") && strings.HasSuffix(rest, "/rename"):
		if r.answer(rw, "rename") {
			return true
		}
		name := strings.TrimSuffix(strings.TrimPrefix(rest, "branches/"), "/rename")
		newName, _ := body["new_name"].(string)
		sha, exists := r.branches[name]
		switch {
		case !exists:
			refsReply(rw, http.StatusNotFound, `{"message":"Branch not found"}`)
		case r.branches[newName] != "":
			refsReply(rw, http.StatusUnprocessableEntity, `{"message":"Validation Failed `+refsSecret+`"}`)
		default:
			delete(r.branches, name)
			r.branches[newName] = sha
			refsReply(rw, http.StatusCreated, fmt.Sprintf(`{"name":%q,"commit":{"sha":%q},"protected":false}`, newName, sha))
		}
	case req.Method == http.MethodPost && rest == "merge-upstream":
		if r.answer(rw, "sync") {
			return true
		}
		branch, _ := body["branch"].(string)
		if _, exists := r.branches[branch]; !exists {
			refsReply(rw, http.StatusNotFound, `{"message":"Not Found"}`)
			return true
		}
		fmt.Fprint(rw, `{"message":"Successfully fetched and fast-forwarded from upstream defunkt:main","merge_type":"fast-forward","base_branch":"defunkt:main"}`)
	case req.Method == http.MethodPost && rest == "merges":
		if r.answer(rw, "merge") {
			return true
		}
		base, _ := body["base"].(string)
		head, _ := body["head"].(string)
		switch {
		case r.branches[base] == "" || (r.branches[head] == "" && r.commits[head] == ""):
			refsReply(rw, http.StatusNotFound, `{"message":"Base does not exist"}`)
		case base == head:
			rw.WriteHeader(http.StatusNoContent)
		default:
			refsReply(rw, http.StatusCreated, fmt.Sprintf(`{"sha":%q,"commit":{"message":%q}}`, refsMergeSHA, refsSecret))
		}
	case req.Method == http.MethodPost && rest == "git/tags":
		if r.answer(rw, "tagobject") {
			return true
		}
		refsReply(rw, http.StatusCreated, fmt.Sprintf(`{"sha":%q,"tag":%q,"message":%q}`, refsObjectSHA, body["tag"], refsSecret))
	case req.Method == http.MethodPost && rest == "git/refs":
		if r.answer(rw, "tagref") {
			return true
		}
		ref, _ := body["ref"].(string)
		sha, _ := body["sha"].(string)
		name := strings.TrimPrefix(ref, "refs/tags/")
		if _, exists := r.tags[name]; exists {
			refsReply(rw, http.StatusUnprocessableEntity, `{"message":"Reference already exists"}`)
			return true
		}
		r.tags[name] = sha
		refsReply(rw, http.StatusCreated, fmt.Sprintf(`{"ref":%q,"object":{"sha":%q,"type":"commit"}}`, ref, sha))
	case req.Method == http.MethodGet && strings.HasPrefix(rest, "git/ref/tags/"):
		if r.answer(rw, "tagread") {
			return true
		}
		name := strings.TrimPrefix(rest, "git/ref/tags/")
		sha, exists := r.tags[name]
		if !exists {
			refsReply(rw, http.StatusNotFound, `{"message":"Not Found"}`)
			return true
		}
		fmt.Fprintf(rw, `{"ref":"refs/tags/%s","object":{"sha":%q,"type":"commit"}}`, name, sha)
	case req.Method == http.MethodDelete && strings.HasPrefix(rest, "git/refs/tags/"):
		if r.answer(rw, "tagdelete") {
			return true
		}
		name := strings.TrimPrefix(rest, "git/refs/tags/")
		if _, exists := r.tags[name]; !exists {
			refsReply(rw, http.StatusUnprocessableEntity, `{"message":"Reference does not exist"}`)
			return true
		}
		delete(r.tags, name)
		rw.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

var refsListedTools = []string{branchesRename.ID, branchesSyncFork.ID, mergesCreate.ID, tagsDelete.ID}

func refsWriteConfig(base string) *config.Config {
	cfg := contentsWriteConfig(base)
	cfg.Connections["repo-refs"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: config.Effects(), Tools: refsListedTools}
	cfg.Connections["refs-excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{"repos/octo-org/example"},
		Effects: config.Effects(), Tools: append([]string{tagsCreate.ID}, refsListedTools...)}
	return cfg
}

func refsCore(t *testing.T, reads *int) (*fakeGitHub, *fakeRefsWrite, *application.Core) {
	t.Helper()
	r := &fakeRefsWrite{branches: map[string]string{"main": refsMainSHA, "feature/login": refsFeatSHA},
		tags:     map[string]string{"v1.0.0": refsMainSHA},
		commits:  map[string]string{"main": refsMainSHA, "feature/login": refsFeatSHA, refsFeatSHA: refsFeatSHA},
		override: map[string]func(http.ResponseWriter){}}
	f := &fakeGitHub{}
	r.f = f
	f.failure = r.route
	red := &redact.Redactor{}
	return f, r, application.New(registry(t), refsWriteConfig(serve(t, f)), resolver(red, reads), red)
}

func mutations(f *fakeGitHub) int {
	n := 0
	for _, r := range f.recorded() {
		if r.method != http.MethodGet {
			n++
		}
	}
	return n
}

func methodPaths(f *fakeGitHub) []string {
	var out []string
	for _, r := range f.recorded() {
		out = append(out, r.method+" "+strings.TrimPrefix(r.path, actionsPrefix))
	}
	return out
}

func TestBranchesRenameSendsOneRequestAndNamesTheBranch(t *testing.T) {
	f, r, core := refsCore(t, nil)
	out, err := invoke(t, core, branchesRename.ID, "repo-refs", `{"branch":"main","new_name":"trunk"}`, true)
	if err != nil || !strings.Contains(string(out), `"new_name":"trunk"`) ||
		!strings.Contains(string(out), `"ref":"refs/heads/trunk"`) ||
		!strings.Contains(string(out), `"sha":"`+refsMainSHA+`"`) || strings.Contains(string(out), refsSecret) {
		t.Fatalf("rename = %s, %v", out, err)
	}
	if got := methodPaths(f); len(got) != 1 || got[0] != "POST branches/main/rename" {
		t.Errorf("requests = %v", got)
	}
	if body := f.recorded()[0].body; len(body) != 1 || body["new_name"] != "trunk" {
		t.Errorf("body = %v, want only new_name", body)
	}
	if _, ok := r.branches["trunk"]; !ok {
		t.Error("branch not renamed")
	}
}

func TestBranchesRenameMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range []struct {
		name, args string
		status     int
		body       string
		invalid    bool
		class      provider.Class
	}{
		{"taken name", `{"branch":"main","new_name":"feature/login"}`, 0, "", true, ""},
		{"missing branch", `{"branch":"nope","new_name":"x"}`, 0, "", false, provider.ClassNotFound},
		{"forbidden", `{"branch":"main","new_name":"x"}`, 403, `{"message":"Must have admin " ` + refsSecret + `}`, false, provider.ClassPermission},
		{"ruleset", `{"branch":"main","new_name":"x"}`, 422, `{"message":"Repository rule violations found ` + refsSecret + `"}`, false, provider.ClassPermission},
		{"conflict", `{"branch":"main","new_name":"x"}`, 409, `{"message":"` + refsSecret + `"}`, false, provider.ClassProviderError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, r, core := refsCore(t, nil)
			if tt.status != 0 {
				r.override["rename"] = func(rw http.ResponseWriter) { refsReply(rw, tt.status, tt.body) }
			}
			_, err := invoke(t, core, branchesRename.ID, "repo-refs", tt.args, true)
			switch {
			case err == nil, strings.Contains(err.Error(), refsSecret), mutations(f) != 1:
				t.Fatalf("err = %v, mutations %d", err, mutations(f))
			case tt.invalid && !isInvalidRequest(err), !tt.invalid && classOf(err) != tt.class:
				t.Errorf("err = %v, want invalid %t class %s", err, tt.invalid, tt.class)
			}
		})
	}
}

func TestBranchesSyncForkSendsOneRequest(t *testing.T) {
	f, _, core := refsCore(t, nil)
	out, err := invoke(t, core, branchesSyncFork.ID, "repo-refs", `{"branch":"main"}`, true)
	if err != nil || !strings.Contains(string(out), `"merge_type":"fast-forward"`) ||
		!strings.Contains(string(out), `"branch":"main"`) || strings.Contains(string(out), "Successfully") {
		t.Fatalf("sync = %s, %v", out, err)
	}
	if got := methodPaths(f); len(got) != 1 || got[0] != "POST merge-upstream" || f.recorded()[0].body["branch"] != "main" {
		t.Errorf("requests = %v body %v", got, f.recorded()[0].body)
	}
}

func TestBranchesSyncForkMapsRefusals(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		body    string
		invalid bool
		class   provider.Class
		message string
	}{
		{"conflict", 409, `{"message":"There are merge conflicts ` + refsSecret + `"}`, false, provider.ClassProviderError, "conflicts with its upstream"},
		{"not a fork", 422, `{"message":"not a fork ` + refsSecret + `"}`, true, "", "must be a fork"},
		{"forbidden", 403, `{"message":"` + refsSecret + `"}`, false, provider.ClassPermission, "Contents"},
		{"missing", 404, `{"message":"` + refsSecret + `"}`, false, provider.ClassNotFound, ""},
		{"ruleset", 422, `{"message":"Repository rule violations found"}`, false, provider.ClassPermission, "Contents"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, r, core := refsCore(t, nil)
			r.override["sync"] = func(rw http.ResponseWriter) { refsReply(rw, tt.status, tt.body) }
			_, err := invoke(t, core, branchesSyncFork.ID, "repo-refs", `{"branch":"main"}`, true)
			if err == nil || strings.Contains(err.Error(), refsSecret) || mutations(f) != 1 ||
				!strings.Contains(err.Error(), tt.message) {
				t.Fatalf("err = %v, mutations %d", err, mutations(f))
			}
			if tt.invalid != isInvalidRequest(err) || (!tt.invalid && classOf(err) != tt.class) {
				t.Errorf("err = %v, want invalid %t class %s", err, tt.invalid, tt.class)
			}
		})
	}
}

func TestBranchesSyncForkRejectsAnUnknownMergeType(t *testing.T) {
	_, r, core := refsCore(t, nil)
	r.override["sync"] = func(rw http.ResponseWriter) { refsReply(rw, 200, `{"merge_type":"other"}`) }
	if _, err := invoke(t, core, branchesSyncFork.ID, "repo-refs", `{"branch":"main"}`, true); err == nil ||
		!strings.Contains(err.Error(), "may have been applied") {
		t.Errorf("err = %v, want an unclear outcome", err)
	}
}

func TestMergesCreateReportsMergedAndNothingToMerge(t *testing.T) {
	f, _, core := refsCore(t, nil)
	out, err := invoke(t, core, mergesCreate.ID, "repo-refs",
		`{"base":"main","head":"feature/login","commit_message":"merge login"}`, true)
	if err != nil || !strings.Contains(string(out), `"result":"merged"`) ||
		!strings.Contains(string(out), `"sha":"`+refsMergeSHA+`"`) || strings.Contains(string(out), refsSecret) {
		t.Fatalf("merge = %s, %v", out, err)
	}
	body := f.recorded()[0].body
	if got := methodPaths(f); len(got) != 1 || got[0] != "POST merges" || body["base"] != "main" ||
		body["head"] != "feature/login" || body["commit_message"] != "merge login" || len(body) != 3 {
		t.Errorf("requests = %v body %v", got, body)
	}
	out, err = invoke(t, core, mergesCreate.ID, "repo-refs", `{"base":"main","head":"main"}`, true)
	if err != nil || !strings.Contains(string(out), `"result":"nothing-to-merge"`) || strings.Contains(string(out), `"sha"`) {
		t.Errorf("nothing to merge = %s, %v", out, err)
	}
	if body := f.recorded()[1].body; body["commit_message"] != nil {
		t.Errorf("an omitted commit_message was sent: %v", body)
	}
}

func TestMergesCreateMapsRefusals(t *testing.T) {
	for _, tt := range []struct {
		name    string
		status  int
		invalid bool
		class   provider.Class
		message string
	}{
		{"conflict", 409, false, provider.ClassProviderError, "conflict that cannot be resolved"},
		{"missing", 404, false, provider.ClassNotFound, ""},
		{"rejected", 422, true, "", "base must be a branch"},
		{"forbidden", 403, false, provider.ClassPermission, "workflow"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, r, core := refsCore(t, nil)
			r.override["merge"] = func(rw http.ResponseWriter) {
				refsReply(rw, tt.status, `{"message":"`+refsSecret+`"}`)
			}
			_, err := invoke(t, core, mergesCreate.ID, "repo-refs", `{"base":"main","head":"feature/login"}`, true)
			if err == nil || strings.Contains(err.Error(), refsSecret) || mutations(f) != 1 ||
				!strings.Contains(err.Error(), tt.message) ||
				tt.invalid != isInvalidRequest(err) || (!tt.invalid && classOf(err) != tt.class) {
				t.Errorf("err = %v, mutations %d", err, mutations(f))
			}
		})
	}
	_, r, core := refsCore(t, nil)
	r.override["merge"] = func(rw http.ResponseWriter) { refsReply(rw, 200, `{}`) }
	if _, err := invoke(t, core, mergesCreate.ID, "repo-refs", `{"base":"main","head":"feature/login"}`, true); err == nil ||
		!strings.Contains(err.Error(), "may have been applied") {
		t.Errorf("unexpected 200 = %v, want an unclear outcome", err)
	}
}

func TestTagsCreateLightweightAndAnnotated(t *testing.T) {
	f, r, core := refsCore(t, nil)
	out, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v1.1.0","target":"main"}`, true)
	if err != nil || !strings.Contains(string(out), `"ref":"refs/tags/v1.1.0"`) ||
		!strings.Contains(string(out), `"sha":"`+refsMainSHA+`"`) || !strings.Contains(string(out), `"annotated":false`) {
		t.Fatalf("lightweight = %s, %v", out, err)
	}
	if got := methodPaths(f); len(got) != 2 || got[0] != "GET commits/main" || got[1] != "POST git/refs" {
		t.Errorf("lightweight requests = %v", got)
	}
	before := len(f.recorded())
	out, err = invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v1.2.0","target":"main","message":"Release 1.2.0"}`, true)
	if err != nil || !strings.Contains(string(out), `"sha":"`+refsObjectSHA+`"`) ||
		!strings.Contains(string(out), `"commit_sha":"`+refsMainSHA+`"`) || !strings.Contains(string(out), `"annotated":true`) ||
		strings.Contains(string(out), refsSecret) {
		t.Fatalf("annotated = %s, %v", out, err)
	}
	requests := f.recorded()[before:]
	if len(requests) != 3 || requests[1].path != actionsPrefix+"git/tags" || requests[2].path != actionsPrefix+"git/refs" {
		t.Fatalf("annotated requests = %+v", requests)
	}
	if b := requests[1].body; b["tag"] != "v1.2.0" || b["message"] != "Release 1.2.0" || b["object"] != refsMainSHA || b["type"] != "commit" {
		t.Errorf("tag object body = %v", b)
	}
	if b := requests[2].body; b["ref"] != "refs/tags/v1.2.0" || b["sha"] != refsObjectSHA {
		t.Errorf("ref body = %v", b)
	}
	if r.tags["v1.2.0"] != refsObjectSHA {
		t.Error("annotated tag ref not stored")
	}
}

func TestTagsCreateRefusals(t *testing.T) {
	f, r, core := refsCore(t, nil)
	if _, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v1.0.0","target":"main"}`, true); !isInvalidRequest(err) ||
		!strings.Contains(err.Error(), "already holds a tag named v1.0.0") {
		t.Errorf("taken name = %v", err)
	}
	if _, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v9","target":"nope"}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("missing target = %v", err)
	}
	if mutations(f) != 1 {
		t.Errorf("mutations = %d, want only the refused ref creation", mutations(f))
	}
	r.override["tagref"] = func(rw http.ResponseWriter) {
		refsReply(rw, 422, `{"message":"Repository rule violations found `+refsSecret+`"}`)
	}
	_, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v9","target":"main"}`, true)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), refsSecret) {
		t.Errorf("ruleset = %v", err)
	}
	r.override["tagref"] = func(rw http.ResponseWriter) { refsReply(rw, 403, `{"message":"`+refsSecret+`"}`) }
	_, err = invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v9","target":"main"}`, true)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), refsSecret) || !strings.Contains(err.Error(), "Contents") {
		t.Errorf("forbidden = %v", err)
	}
}

func TestTagsCreateReportsThePartialStateOfAnAnnotatedTag(t *testing.T) {
	for _, tt := range []struct {
		name    string
		answer  func(http.ResponseWriter)
		invalid bool
		class   provider.Class
		unclear bool
	}{
		{"exists", func(rw http.ResponseWriter) { refsReply(rw, 422, `{"message":"Reference already exists"}`) }, true, "", false},
		{"forbidden", func(rw http.ResponseWriter) { refsReply(rw, 403, `{"message":"`+refsSecret+`"}`) }, false, provider.ClassPermission, false},
		{"server error", func(rw http.ResponseWriter) { rw.WriteHeader(http.StatusBadGateway) }, false, provider.ClassProviderError, true},
		{"dropped connection", func(rw http.ResponseWriter) {
			if connection, _, err := rw.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		}, false, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, r, core := refsCore(t, nil)
			r.override["tagref"] = tt.answer
			_, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v2","target":"main","message":"m"}`, true)
			if err == nil || !strings.Contains(err.Error(), "annotated tag object "+refsObjectSHA+" was created") ||
				strings.Contains(err.Error(), refsSecret) || strings.Contains(err.Error(), "may have been applied") != tt.unclear {
				t.Fatalf("err = %v", err)
			}
			if tt.invalid != isInvalidRequest(err) || (tt.class != "" && classOf(err) != tt.class) {
				t.Errorf("err = %v, want invalid %t class %s", err, tt.invalid, tt.class)
			}
			if got := methodPaths(f); len(got) != 3 || mutations(f) != 2 {
				t.Errorf("requests = %v, want one object and one ref request, no repeat", got)
			}
		})
	}
	// A failure of the object step leaves nothing behind and names no object.
	f, r, core := refsCore(t, nil)
	r.override["tagobject"] = func(rw http.ResponseWriter) { refsReply(rw, 403, `{"message":"`+refsSecret+`"}`) }
	_, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v2","target":"main","message":"m"}`, true)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), "annotated tag object") || mutations(f) != 1 {
		t.Errorf("object step = %v, mutations %d", err, mutations(f))
	}
}

func TestTagsDeleteReadsThenDeletesOnce(t *testing.T) {
	f, r, core := refsCore(t, nil)
	out, err := invoke(t, core, tagsDelete.ID, "repo-refs", `{"tag":"v1.0.0"}`, true)
	if err != nil || !strings.Contains(string(out), `"ref":"refs/tags/v1.0.0"`) ||
		!strings.Contains(string(out), `"sha":"`+refsMainSHA+`"`) {
		t.Fatalf("delete = %s, %v", out, err)
	}
	if got := methodPaths(f); len(got) != 2 || got[0] != "GET git/ref/tags/v1.0.0" || got[1] != "DELETE git/refs/tags/v1.0.0" {
		t.Errorf("requests = %v", got)
	}
	if _, exists := r.tags["v1.0.0"]; exists {
		t.Error("tag kept")
	}
	before := len(f.recorded())
	if _, err := invoke(t, core, tagsDelete.ID, "repo-refs", `{"tag":"v1.0.0"}`, true); classOf(err) != provider.ClassNotFound {
		t.Errorf("missing tag = %v", err)
	}
	if mutations(f) != 1 || len(f.recorded())-before != 1 {
		t.Errorf("a missing tag still reached the delete")
	}
}

func TestTagsDeleteMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
		class  provider.Class
	}{
		{"missing 422", 422, `{"message":"Reference does not exist ` + refsSecret + `"}`, provider.ClassNotFound},
		{"missing 404", 404, `{"message":"` + refsSecret + `"}`, provider.ClassNotFound},
		{"immutable tags ruleset 422", 422, `{"message":"Repository rule violations found ` + refsSecret + `"}`, provider.ClassPermission},
		{"ruleset 403", 403, `{"message":"` + refsSecret + `"}`, provider.ClassPermission},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, r, core := refsCore(t, nil)
			r.override["tagdelete"] = func(rw http.ResponseWriter) { refsReply(rw, tt.status, tt.body) }
			_, err := invoke(t, core, tagsDelete.ID, "repo-refs", `{"tag":"v1.0.0"}`, true)
			if classOf(err) != tt.class || err == nil || strings.Contains(err.Error(), refsSecret) || mutations(f) != 1 {
				t.Errorf("err = %v, mutations %d, want class %s", err, mutations(f), tt.class)
			}
			if tt.class == provider.ClassPermission && !strings.Contains(err.Error(), "immutable") {
				t.Errorf("permission message = %v, want the rule hint", err)
			}
		})
	}
}

// Every change is sent once; an unclear outcome is reported and never repeated.
func TestRefsWriteToolsNeverRepeatAnUnclearChange(t *testing.T) {
	for _, tt := range []struct{ tool, route, args string }{
		{branchesRename.ID, "rename", `{"branch":"main","new_name":"trunk"}`},
		{branchesSyncFork.ID, "sync", `{"branch":"main"}`},
		{mergesCreate.ID, "merge", `{"base":"main","head":"feature/login"}`},
		{tagsCreate.ID, "tagref", `{"tag":"v2","target":"main"}`},
		{tagsDelete.ID, "tagdelete", `{"tag":"v1.0.0"}`},
	} {
		for name, answer := range map[string]func(http.ResponseWriter){
			"server error":    func(rw http.ResponseWriter) { rw.WriteHeader(http.StatusBadGateway) },
			"gateway timeout": func(rw http.ResponseWriter) { rw.WriteHeader(http.StatusGatewayTimeout) },
			"unreadable":      func(rw http.ResponseWriter) { rw.WriteHeader(http.StatusCreated); fmt.Fprint(rw, "x") },
			"dropped connection": func(rw http.ResponseWriter) {
				if connection, _, err := rw.(http.Hijacker).Hijack(); err == nil {
					connection.Close()
				}
			},
		} {
			t.Run(tt.tool+" "+name, func(t *testing.T) {
				f, r, core := refsCore(t, nil)
				r.override[tt.route] = answer
				_, err := invoke(t, core, tt.tool, "repo-refs", tt.args, true)
				if tt.tool == tagsCreate.ID {
					_, err = invoke(t, core, tt.tool, "repo", tt.args, true)
				}
				if err == nil || !strings.Contains(err.Error(), "may have been applied") {
					t.Errorf("err = %v, want an unclear outcome", err)
				}
				if mutations(f) != 1 {
					t.Errorf("mutations = %d, want exactly 1", mutations(f))
				}
			})
		}
	}
}

func TestRefsWriteToolsCheckArgumentsAndTargetBeforeSecrets(t *testing.T) {
	reads := 0
	f, _, core := refsCore(t, &reads)
	for _, tt := range []struct{ tool, connection, args string }{
		{branchesRename.ID, "repo-refs", `{"branch":"main","new_name":"main"}`},
		{branchesRename.ID, "repo-refs", `{"branch":"refs/heads/main","new_name":"x"}`},
		{branchesRename.ID, "repo-refs", `{"branch":"main","new_name":"bad..name"}`},
		{branchesRename.ID, "repo-refs", `{"branch":"main"}`},
		{branchesRename.ID, "repo-refs", `{"branch":"main","new_name":"x","repository":"other-org/other"}`},
		{branchesRename.ID, "refs-excluding", `{"branch":"main","new_name":"x","repository":"octo-org/example"}`},
		{branchesSyncFork.ID, "repo-refs", `{"branch":""}`},
		{branchesSyncFork.ID, "repo-refs", `{"branch":"main","force":true}`},
		{branchesSyncFork.ID, "repo-refs", `{"branch":"main","repository":"other-org/other"}`},
		{branchesSyncFork.ID, "refs-excluding", `{"branch":"main","repository":"octo-org/example"}`},
		{mergesCreate.ID, "repo-refs", `{"base":"main"}`},
		{mergesCreate.ID, "repo-refs", `{"base":"main","head":"bad..ref"}`},
		{mergesCreate.ID, "repo-refs", `{"base":"main","head":"x","commit_message":"a\u0000b"}`},
		{mergesCreate.ID, "repo-refs", `{"base":"main","head":"x","repository":"other-org/other"}`},
		{mergesCreate.ID, "refs-excluding", `{"base":"main","head":"x","repository":"octo-org/example"}`},
		{tagsCreate.ID, "repo", `{"tag":"refs/tags/v1","target":"main"}`},
		{tagsCreate.ID, "repo", `{"tag":"v1"}`},
		{tagsCreate.ID, "repo", `{"tag":"v1","target":"main","message":"a\u0000b"}`},
		{tagsCreate.ID, "repo", `{"tag":"v1","target":"main","force":true}`},
		{tagsCreate.ID, "repo", `{"tag":"v1","target":"main","repository":"other-org/other"}`},
		{tagsCreate.ID, "refs-excluding", `{"tag":"v1","target":"main","repository":"octo-org/example"}`},
		{tagsCreate.ID, "project-only", `{"tag":"v1","target":"main"}`},
		{tagsDelete.ID, "repo-refs", `{"tag":"refs/tags/v1"}`},
		{tagsDelete.ID, "repo-refs", `{"tag":"v1","repository":"other-org/other"}`},
		{tagsDelete.ID, "refs-excluding", `{"tag":"v1","repository":"octo-org/example"}`},
	} {
		reads = 0
		before := len(f.recorded())
		if _, err := invoke(t, core, tt.tool, tt.connection, tt.args, true); err == nil {
			t.Errorf("%s %s accepted", tt.tool, tt.args)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s %s reached the credential or GitHub", tt.tool, tt.args)
		}
	}
	for _, tt := range []struct{ tool, connection, args string }{
		{branchesRename.ID, "repo-refs", `{"branch":"main","new_name":"x"}`},
		{branchesSyncFork.ID, "repo-refs", `{"branch":"main"}`},
		{mergesCreate.ID, "repo-refs", `{"base":"main","head":"x"}`},
		{tagsCreate.ID, "repo", `{"tag":"v1","target":"main"}`},
		{tagsDelete.ID, "repo-refs", `{"tag":"v1"}`},
	} {
		reads = 0
		if _, err := invoke(t, core, tt.tool, tt.connection, tt.args, false); err == nil || reads != 0 {
			t.Errorf("%s unconfirmed = %v, reads %d", tt.tool, err, reads)
		}
	}
}

func TestRefsWriteToolsAreListedOnlyExceptTagsCreate(t *testing.T) {
	_, _, core := refsCore(t, nil)
	var unsupported *capability.UnsupportedError
	for _, tt := range []struct{ tool, args string }{
		{branchesRename.ID, `{"branch":"main","new_name":"x"}`},
		{branchesSyncFork.ID, `{"branch":"main"}`},
		{mergesCreate.ID, `{"base":"main","head":"x"}`},
		{tagsDelete.ID, `{"tag":"v1.0.0"}`},
	} {
		if _, err := invoke(t, core, tt.tool, "repo", tt.args, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools entry = %v, want unsupported-capability", tt.tool, err)
		}
	}
	if tagsCreate.RequiresToolAllowList || !branchesRename.RequiresToolAllowList || !branchesSyncFork.RequiresToolAllowList ||
		!mergesCreate.RequiresToolAllowList || !tagsDelete.RequiresToolAllowList {
		t.Error("allow-list requirements differ from the contract")
	}
	if _, err := invoke(t, core, tagsCreate.ID, "repo", `{"tag":"v7","target":"main"}`, true); err != nil {
		t.Errorf("tags.create without a tools entry = %v", err)
	}
}

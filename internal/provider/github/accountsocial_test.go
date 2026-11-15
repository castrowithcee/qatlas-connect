package github

import (
	"encoding/json"
	"errors"
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

// fakeSocial answers the follower, following, block, and profile routes of the account octocat, who follows
// hubot and blocks nobody. The routes below /user/following and /user/blocks answer 204 or 404 by state.
type fakeSocial struct {
	following, blocked map[string]bool
	// read and change replace the answer of a state read and of a change.
	read   func(http.ResponseWriter)
	change func(http.ResponseWriter)
}

const socialUserBody = `{"login":"hubot","id":7,"node_id":"MDQ6VXNlcjc=","avatar_url":"https://a.invalid/7",` +
	`"gravatar_id":"","url":"https://api.github.com/users/hubot","html_url":"https://github.com/hubot",` +
	`"followers_url":"https://api.github.com/users/hubot/followers","type":"User","site_admin":false,` +
	`"name":"Hub Bot","email":"hub@example.invalid","user_view_type":"public","starred_at":"2026-01-01T00:00:00Z",` +
	`"two_factor_authentication":true}`

func (s *fakeSocial) route(w http.ResponseWriter, r *http.Request) bool {
	path, ok := strings.CutPrefix(r.URL.Path, notificationPrefix)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case path == "/user" && r.Method == http.MethodGet:
		fmt.Fprint(w, `{"login":"octocat","type":"User"}`)
	case path == "/user" && r.Method == http.MethodPatch:
		if s.change != nil {
			s.change(w)
			return true
		}
		fmt.Fprint(w, `{"login":"octocat","type":"User","name":"Octo","email":"o@example.invalid",`+
			`"twitter_username":"octo","bio":"hi\u0007 there","blog":"https://octo.invalid","hireable":true,`+
			`"two_factor_authentication":true,"plan":{"name":"pro"},"followers":3}`)
	case r.Method == http.MethodGet && (path == "/user/followers" || path == "/user/following" ||
		path == "/user/blocks" || path == "/users/hubot/followers" || path == "/users/hubot/following"):
		w.Header().Set("Link", `<https://api.github.com/next?page=2>; rel="next"`)
		fmt.Fprintf(w, "[%s]", socialUserBody)
	case strings.HasPrefix(path, "/user/following/") || strings.HasPrefix(path, "/user/blocks/"):
		state := s.following
		name := strings.TrimPrefix(path, "/user/following/")
		if strings.HasPrefix(path, "/user/blocks/") {
			state, name = s.blocked, strings.TrimPrefix(path, "/user/blocks/")
		}
		switch r.Method {
		case http.MethodGet:
			if s.read != nil {
				s.read(w)
			} else if state[name] {
				w.WriteHeader(http.StatusNoContent)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		default:
			if s.change != nil {
				s.change(w)
				return true
			}
			state[name] = r.Method == http.MethodPut
			w.WriteHeader(http.StatusNoContent)
		}
	default:
		return false
	}
	return true
}

var socialToolIDs = accountSocialToolIDs

func socialRig(t *testing.T) (*fakeGitHub, *fakeSocial, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{}
	s := &fakeSocial{following: map[string]bool{"hubot": true}, blocked: map[string]bool{}}
	f.failure = s.route
	cfg := coreConfig(serve(t, f))
	all := config.Effects()
	add := func(name string, targets, excludes []string, tools ...string) {
		cfg.Connections[name] = connectionOf(config.Connection{Service: "gh", Credential: "gh-reader",
			Targets: targets, Exclude: excludes}, all, tools)
	}
	add("open", nil, nil)
	add("open-listed", nil, nil, socialToolIDs...)
	add("me", []string{"users/octocat"}, nil, socialToolIDs...)
	add("me-hubot", []string{"users/octocat", "users/hubot"}, nil, socialToolIDs...)
	add("other", []string{"users/someone"}, nil, socialToolIDs...)
	add("org", []string{"orgs/octo-org"}, nil, socialToolIDs...)
	add("repo", []string{repoTarget}, nil, socialToolIDs...)
	add("mixed", []string{"users/octocat", repoTarget}, nil, socialToolIDs...)
	add("ex-hubot", nil, []string{"users/hubot"}, socialToolIDs...)
	add("ex-me", nil, []string{"users/octocat"}, socialToolIDs...)
	red := &redact.Redactor{}
	reads := 0
	return f, s, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func nonReads(f *fakeGitHub, from int) []recorded {
	var out []recorded
	for _, r := range sent(f, from) {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func TestSocialReadsAnswerEveryDocumentedMemberAndPage(t *testing.T) {
	f, _, core, _ := socialRig(t)
	for _, tt := range []struct{ tool, args, path, key string }{
		{followersList.ID, `{"limit":5}`, "/user/followers", "followers"},
		{followingList.ID, `{}`, "/user/following", "following"},
		{followersList.ID, `{"username":"hubot"}`, "/users/hubot/followers", "followers"},
		{followingList.ID, `{"username":"hubot"}`, "/users/hubot/following", "following"},
		{blocksList.ID, `{}`, "/user/blocks", "blocked"},
	} {
		got, err := invoke(t, core, tt.tool, "open", tt.args, false)
		if err != nil {
			t.Fatalf("%s %s = %v", tt.tool, tt.args, err)
		}
		if last := lastRequest(t, f); last.path != notificationPrefix+tt.path {
			t.Errorf("%s asked %s, want %s", tt.tool, last.path, tt.path)
		}
		var page map[string]any
		_ = json.Unmarshal(got, &page)
		if page["has_more"] != true || page["next_cursor"] == "" {
			t.Errorf("%s = %s, want a following page", tt.tool, got)
		}
		entries, _ := page[tt.key].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s = %s", tt.tool, got)
		}
		sameJSON(t, tt.tool, mustJSON(entries[0]), `{"login":"hubot","id":7,"node_id":"MDQ6VXNlcjc=",`+
			`"user_view_type":"public","site_admin":false,"avatar_url":"https://a.invalid/7",`+
			`"links":{"url":"https://api.github.com/users/hubot","followers_url":"https://api.github.com/users/hubot/followers"},`+
			`"name":"Hub Bot","email":"hub@example.invalid","type":"User","starred_at":"2026-01-01T00:00:00Z",`+
			`"url":"https://github.com/hubot"}`)
		if strings.Contains(string(got), "two_factor") {
			t.Errorf("%s shows account security of another: %s", tt.tool, got)
		}
		// A cursor belongs to its list and its user.
		next, _ := page["next_cursor"].(string)
		other := `{"username":"hubot","cursor":"` + next + `"}`
		if tt.tool == blocksList.ID {
			other = `{"cursor":"` + next + `"}`
		}
		if tt.args == `{"username":"hubot"}` || tt.tool == blocksList.ID {
			if _, err := invoke(t, core, tt.tool, "open", other, false); err != nil {
				t.Errorf("%s continuation = %v", tt.tool, err)
			}
		} else if _, err := invoke(t, core, tt.tool, "open", `{"username":"hubot","cursor":"`+next+`"}`, false); !isInvalidRequest(err) {
			t.Errorf("%s with a foreign cursor = %v, want an invalid request", tt.tool, err)
		}
	}
}

func mustJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

func TestFollowingSetIsIdempotentAndSendsOneChange(t *testing.T) {
	f, s, core, _ := socialRig(t)
	for _, tt := range []struct {
		args    string
		wantRaw string
		method  string
	}{
		{`{"username":"hubot","following":true}`, `{"username":"hubot","following":true,"changed":false}`, ""},
		{`{"username":"octobot","following":true}`, `{"username":"octobot","following":true,"changed":true}`, http.MethodPut},
		{`{"username":"octobot","following":false}`, `{"username":"octobot","following":false,"changed":true}`, http.MethodDelete},
		{`{"username":"octobot","following":false}`, `{"username":"octobot","following":false,"changed":false}`, ""},
	} {
		before := len(f.recorded())
		got, err := invoke(t, core, followingSet.ID, "open", tt.args, true)
		if err != nil {
			t.Fatalf("%s = %v", tt.args, err)
		}
		sameJSON(t, tt.args, got, tt.wantRaw)
		changes := nonReads(f, before)
		if tt.method == "" && len(changes) != 0 || tt.method != "" && (len(changes) != 1 || changes[0].method != tt.method) {
			t.Errorf("%s sent %+v, want %q once", tt.args, changes, tt.method)
		}
	}
	if s.following["octobot"] {
		t.Error("octobot is still followed")
	}
	if _, err := invoke(t, core, followingSet.ID, "open", `{"username":"hubot"}`, true); !isInvalidRequest(err) {
		t.Errorf("missing following = %v, want an invalid request", err)
	}
}

func TestBlocksAddAndRemoveAreIdempotent(t *testing.T) {
	f, s, core, _ := socialRig(t)
	for _, tt := range []struct {
		tool, want, method string
	}{
		{blocksAdd.ID, `{"username":"spam","blocked":true,"changed":true}`, http.MethodPut},
		{blocksAdd.ID, `{"username":"spam","blocked":true,"changed":false}`, ""},
		{blocksRemove.ID, `{"username":"spam","blocked":false,"changed":true}`, http.MethodDelete},
		{blocksRemove.ID, `{"username":"spam","blocked":false,"changed":false}`, ""},
	} {
		before := len(f.recorded())
		got, err := invoke(t, core, tt.tool, "open-listed", `{"username":"spam"}`, true)
		if err != nil {
			t.Fatalf("%s = %v", tt.tool, err)
		}
		sameJSON(t, tt.tool, got, tt.want)
		changes := nonReads(f, before)
		if tt.method == "" && len(changes) != 0 || tt.method != "" && (len(changes) != 1 || changes[0].method != tt.method ||
			changes[0].path != notificationPrefix+"/user/blocks/spam") {
			t.Errorf("%s sent %+v", tt.tool, changes)
		}
	}
	if s.blocked["spam"] {
		t.Error("spam is still blocked")
	}
}

func TestProfileUpdateSendsOnlyTheNamedMembers(t *testing.T) {
	f, _, core, _ := socialRig(t)
	before := len(f.recorded())
	got, err := invoke(t, core, profileUpdate.ID, "open-listed",
		`{"bio":"new","hireable":false,"location":"","blog":"https://x.invalid"}`, true)
	changes := nonReads(f, before)
	if err != nil || len(changes) != 1 || changes[0].method != http.MethodPatch || changes[0].path != notificationPrefix+"/user" ||
		len(changes[0].body) != 4 || changes[0].body["bio"] != "new" || changes[0].body["hireable"] != false ||
		changes[0].body["location"] != "" || changes[0].body["blog"] != "https://x.invalid" {
		t.Fatalf("update = %s, %v, %+v", got, err, changes)
	}
	sameJSON(t, "profile", got, `{"login":"octocat","name":"Octo","email":"o@example.invalid","twitter_username":"octo",`+
		`"bio":"hi  there","blog":"https://octo.invalid","hireable":true,"followers":3}`)
	for name, args := range map[string]string{"no member": `{}`, "control character": `{"name":"a\u0007b"}`,
		"too long bio": `{"bio":"` + strings.Repeat("b", profileBioLimit+1) + `"}`} {
		before = len(f.recorded())
		if _, err := invoke(t, core, profileUpdate.ID, "open-listed", args, true); !isInvalidRequest(err) || len(f.recorded()) != before {
			t.Errorf("%s = %v, want an invalid request without a request", name, err)
		}
	}
}

func TestSocialChangesNeedConfirmationAndTheListedOnesAreNotOfferedByEffect(t *testing.T) {
	f, _, core, _ := socialRig(t)
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, call := range []struct{ tool, args string }{
		{followingSet.ID, `{"username":"x","following":true}`}, {blocksAdd.ID, `{"username":"x"}`},
		{blocksRemove.ID, `{"username":"x"}`}, {profileUpdate.ID, `{"bio":"x"}`},
	} {
		before := len(f.recorded())
		if _, err := invoke(t, core, call.tool, "open-listed", call.args, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without confirm = %v", call.tool, err)
		}
		if len(f.recorded()) != before {
			t.Errorf("%s without confirm reached GitHub", call.tool)
		}
	}
	var unsupported *capability.UnsupportedError
	for _, call := range []struct{ tool, args string }{
		{blocksAdd.ID, `{"username":"x"}`}, {blocksRemove.ID, `{"username":"x"}`}, {profileUpdate.ID, `{"bio":"x"}`},
	} {
		if _, err := invoke(t, core, call.tool, "open", call.args, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools list = %v, want unsupported-capability", call.tool, err)
		}
	}
	for _, d := range []capability.Descriptor{blocksAdd, blocksRemove, profileUpdate} {
		if !d.RequiresToolAllowList {
			t.Errorf("%s is not listed-only", d.ID)
		}
	}
	for _, d := range []capability.Descriptor{followersList, followingList, followingSet, blocksList} {
		if d.RequiresToolAllowList {
			t.Errorf("%s is listed-only", d.ID)
		}
	}
}

var socialCalls = []struct{ tool, args string }{
	{followersList.ID, `{}`}, {followingList.ID, `{}`}, {blocksList.ID, `{}`},
	{followingSet.ID, `{"username":"x","following":true}`}, {blocksAdd.ID, `{"username":"x"}`},
	{blocksRemove.ID, `{"username":"x"}`}, {profileUpdate.ID, `{"bio":"x"}`},
}

func TestSocialToolsFollowTheAccountWideRuleBeforeAnySecret(t *testing.T) {
	f, _, core, reads := socialRig(t)
	for _, connection := range []string{"repo", "mixed", "org", "planning"} {
		for _, call := range socialCalls {
			if connection == "planning" && toolEffects[call.tool] != config.EffectRead {
				continue // the planning connection offers no change
			}
			before, requests := *reads, len(f.recorded())
			if _, err := invoke(t, core, call.tool, connection, call.args, true); !isInvalidRequest(err) {
				t.Errorf("%s on %s = %v, want an invalid request", call.tool, connection, err)
			}
			if *reads != before || len(f.recorded()) != requests {
				t.Errorf("%s on %s read a secret or reached GitHub", call.tool, connection)
			}
		}
	}
}

func TestSocialToolsNeedTheTokenAccountAmongTheUserTargets(t *testing.T) {
	f, _, core, _ := socialRig(t)
	for _, call := range socialCalls {
		before := len(f.recorded())
		if _, err := invoke(t, core, call.tool, "other", call.args, true); !isInvalidRequest(err) {
			t.Errorf("%s for a foreign account = %v, want an invalid request", call.tool, err)
		}
		if _, err := invoke(t, core, call.tool, "ex-me", call.args, true); !isInvalidRequest(err) {
			t.Errorf("%s for an excluded account = %v, want an invalid request", call.tool, err)
		}
		if changes := nonReads(f, before); len(changes) != 0 {
			t.Errorf("%s sent %+v for a foreign account", call.tool, changes)
		}
		if _, err := invoke(t, core, call.tool, "me", call.args, true); err != nil {
			t.Errorf("%s on the own account = %v", call.tool, err)
		}
	}
}

func TestSocialUsersAreCheckedBeforeAnySecret(t *testing.T) {
	f, _, core, reads := socialRig(t)
	// Another user's lists need that user as a target; the person of a change must not be excluded.
	for _, tt := range []struct{ tool, connection, args string }{
		{followersList.ID, "me", `{"username":"hubot"}`},
		{followingList.ID, "me", `{"username":"hubot"}`},
		{followersList.ID, "ex-hubot", `{"username":"hubot"}`},
		{followingSet.ID, "ex-hubot", `{"username":"hubot","following":false}`},
		{blocksAdd.ID, "ex-hubot", `{"username":"hubot"}`},
		{blocksRemove.ID, "ex-hubot", `{"username":"hubot"}`},
		{followersList.ID, "open", `{"username":"bad name"}`},
		{blocksAdd.ID, "open-listed", `{"username":"bad/name"}`},
	} {
		before, requests := *reads, len(f.recorded())
		if _, err := invoke(t, core, tt.tool, tt.connection, tt.args, true); !isInvalidRequest(err) {
			t.Errorf("%s %s on %s = %v, want an invalid request", tt.tool, tt.args, tt.connection, err)
		}
		if *reads != before || len(f.recorded()) != requests {
			t.Errorf("%s on %s read a secret or reached GitHub", tt.tool, tt.connection)
		}
	}
	if _, err := invoke(t, core, followersList.ID, "me-hubot", `{"username":"hubot"}`, false); err != nil {
		t.Errorf("list of a targeted user = %v", err)
	}
	if _, err := invoke(t, core, followingSet.ID, "me", `{"username":"hubot","following":true}`, true); err != nil {
		t.Errorf("follow by a user target of the account = %v", err)
	}
}

func TestSocialFailuresAreNamedWithoutProviderText(t *testing.T) {
	f, s, core, _ := socialRig(t)
	canary := cacheProviderCanary
	for _, tt := range []struct {
		status   int
		class    provider.Class
		fragment string
	}{
		{http.StatusForbidden, provider.ClassPermission, "GitHub refused"},
		{http.StatusNotFound, provider.ClassNotFound, "does not hold"},
		{http.StatusUnprocessableEntity, "", ""},
	} {
		s.change = func(w http.ResponseWriter) { refuse(w, tt.status) }
		for _, call := range []struct{ tool, args string }{
			{followingSet.ID, `{"username":"x","following":true}`}, {blocksAdd.ID, `{"username":"x"}`},
			{profileUpdate.ID, `{"bio":"x"}`},
		} {
			before := len(f.recorded())
			_, err := invoke(t, core, call.tool, "open-listed", call.args, true)
			if err == nil || strings.Contains(err.Error(), canary) || strings.Contains(err.Error(), uncertain) ||
				tt.class != "" && classOf(err) != tt.class || !strings.Contains(err.Error(), tt.fragment) {
				t.Errorf("%s answered %d = %v", call.tool, tt.status, err)
			}
			if len(nonReads(f, before)) != 1 {
				t.Errorf("%s answered %d sent %d changes, want one", call.tool, tt.status, len(nonReads(f, before)))
			}
		}
	}
	s.change = nil
	// A refused read ends the call without a change, and a refused list names its right.
	s.read = func(w http.ResponseWriter) { refuse(w, http.StatusForbidden) }
	before := len(f.recorded())
	if _, err := invoke(t, core, blocksAdd.ID, "open-listed", `{"username":"x"}`, true); classOf(err) != provider.ClassPermission ||
		strings.Contains(err.Error(), canary) || len(nonReads(f, before)) != 0 {
		t.Errorf("a refused state read = %v", err)
	}
	s.read = nil
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/user/blocks") {
			refuse(w, http.StatusForbidden)
			return true
		}
		return s.route(w, r)
	}
	if _, err := invoke(t, core, blocksList.ID, "open", `{}`, false); classOf(err) != provider.ClassPermission ||
		strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), "Block another user") {
		t.Errorf("a refused block list = %v", err)
	}
}

func TestSocialChangesAreNeverRepeatedAfterAnUnclearOutcome(t *testing.T) {
	f, s, core, _ := socialRig(t)
	for name, answer := range map[string]func(http.ResponseWriter){
		"gateway timeout": func(w http.ResponseWriter) { refuse(w, http.StatusGatewayTimeout) },
		"server error":    func(w http.ResponseWriter) { refuse(w, http.StatusInternalServerError) },
		"dropped connection": func(w http.ResponseWriter) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
	} {
		s.change = answer
		for _, call := range []struct{ tool, args string }{
			{followingSet.ID, `{"username":"x","following":true}`}, {blocksAdd.ID, `{"username":"x"}`},
			{blocksRemove.ID, `{"username":"hubot"}`}, {profileUpdate.ID, `{"bio":"x"}`},
		} {
			if call.tool == blocksRemove.ID {
				s.blocked["hubot"] = true
			}
			before := len(f.recorded())
			_, err := invoke(t, core, call.tool, "open-listed", call.args, true)
			if err == nil || !strings.Contains(err.Error(), uncertain) || strings.Contains(err.Error(), cacheProviderCanary) {
				t.Errorf("%s %s = %v, want an uncertain outcome", name, call.tool, err)
			}
			if got := len(nonReads(f, before)); got != 1 {
				t.Errorf("%s %s sent %d changes, want exactly one", name, call.tool, got)
			}
		}
	}
}

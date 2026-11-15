package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const addArguments = `{"repository":"octo-org/example","username":"octocat","permission":"push"}`

// addServer scripts the three routes of an invitation: the account read, the collaborator read, and the PUT.
type addServer struct {
	account string // body of GET /users/octocat
	read    int    // status of the collaborator read
	put     func(http.ResponseWriter, *http.Request)
}

const octocatAccount = `{"login":"octocat","id":583231,"type":"User"}`

func addCore(t *testing.T, s addServer) (*fakeGitHub, *int, *application.Core) {
	t.Helper()
	if s.account == "" {
		s.account = octocatAccount
	}
	if s.read == 0 {
		s.read = http.StatusNotFound
	}
	if s.put == nil {
		s.put = func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }
	}
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v3/users/octocat" || r.URL.Path == "/api/v3/users/OctoCat":
			fmt.Fprint(w, s.account)
		case r.URL.Path == collaboratorRoute && r.Method == http.MethodGet:
			w.WriteHeader(s.read)
			if s.read >= 400 {
				fmt.Fprint(w, `{"message":"SECRETTEXT"}`)
			}
		case r.URL.Path == collaboratorRoute:
			s.put(w, r)
		default:
			return false
		}
		return true
	}
	base := serve(t, f)
	cfg := coreConfig(base)
	all := config.Effects()
	add := func(name string, targets, excludes []string, tools ...string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Targets: targets,
			Exclude: excludes, Effects: all, Tools: tools}
	}
	tool := []string{repositoryCollaboratorsAdd.ID}
	add("listed", []string{"repos/octo-org/*", "users/octocat"}, nil, tool...)
	add("mixed case", []string{"repos/octo-org/*", "users/OctoCat"}, nil, tool...)
	add("unlisted", []string{"repos/octo-org/*", "users/octocat"}, nil)
	add("no user", []string{"repos/octo-org/*"}, nil, tool...)
	add("org only", []string{"repos/octo-org/*", "orgs/octocat"}, nil, tool...)
	add("other user", []string{"repos/octo-org/*", "users/hubot"}, nil, tool...)
	add("exact", []string{"repos/octo-org/other", "users/octocat"}, nil, tool...)
	add("excluded repo", []string{"repos/octo-org/*", "users/octocat"}, []string{"repos/octo-org/example"}, tool...)
	reads := 0
	red := &redact.Redactor{}
	return f, &reads, application.New(registry(t), cfg, resolver(red, &reads), red)
}

func TestCollaboratorsAddInvitesAfterTwoReadsAndOnePut(t *testing.T) {
	f, _, core := addCore(t, addServer{put: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		// A server that mirrors the invitation must not leak its ID or links.
		fmt.Fprint(w, `{"id":987654,"html_url":"https://example.test/invitations/SECRETLINK","url":"u",`+
			`"invitee":{"login":"octocat"},"permissions":"write"}`)
	}})
	for _, connection := range []string{"listed", "mixed case"} {
		result, err := invoke(t, core, repositoryCollaboratorsAdd.ID, connection, addArguments, true)
		var answer map[string]any
		if err != nil || json.Unmarshal(result, &answer) != nil || answer["result"] != "invited" ||
			answer["username"] != "octocat" || answer["permission"] != "push" ||
			answer["repository"] != "octo-org/example" || len(answer) != 4 {
			t.Fatalf("%s result = %s, %v", connection, result, err)
		}
		if strings.Contains(string(result), "987654") || strings.Contains(string(result), "SECRETLINK") ||
			strings.Contains(string(result), "http") {
			t.Errorf("result leaks an invitation: %s", result)
		}
	}
	requests := f.recorded()
	if len(requests) != 6 || requests[0].method != http.MethodGet || requests[1].method != http.MethodGet ||
		requests[2].method != http.MethodPut || len(requests[2].body) != 1 ||
		requests[2].body["permission"] != "push" {
		t.Errorf("requests = %+v, want two reads and one PUT per call", requests)
	}
}

func TestCollaboratorsAddAcceptsAnyCaseAndUsesTheCanonicalLogin(t *testing.T) {
	_, _, core := addCore(t, addServer{})
	result, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed",
		`{"repository":"octo-org/example","username":"OCTOCAT","permission":"push"}`, true)
	if err != nil || !strings.Contains(string(result), `"username":"octocat"`) {
		t.Fatalf("result = %s, %v", result, err)
	}
}

func TestCollaboratorsAddReportsADirectAddition(t *testing.T) {
	_, _, core := addCore(t, addServer{})
	result, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, true)
	if err != nil || !strings.Contains(string(result), `"result":"added"`) {
		t.Fatalf("result = %s, %v", result, err)
	}
	// A custom repository role name goes through.
	if _, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed",
		`{"repository":"octo-org/example","username":"octocat","permission":"Release manager"}`, true); err != nil {
		t.Errorf("custom role = %v", err)
	}
}

func TestCollaboratorsAddRefusesBeforeAnySecretOrRequest(t *testing.T) {
	f, reads, core := addCore(t, addServer{})
	for name, tt := range map[string]struct{ connection, arguments string }{
		"login not named":      {"other user", addArguments},
		"no user target":       {"no user", addArguments},
		"organization target":  {"org only", addArguments},
		"excluded repository":  {"excluded repo", addArguments},
		"repository outside":   {"exact", addArguments},
		"other owner":          {"listed", `{"repository":"other-org/example","username":"octocat","permission":"push"}`},
		"pattern repository":   {"listed", `{"repository":"octo-org/*","username":"octocat","permission":"push"}`},
		"e-mail address":       {"listed", `{"repository":"octo-org/example","username":"octo@example.com","permission":"push"}`},
		"at sign":              {"listed", `{"repository":"octo-org/example","username":"@octocat","permission":"push"}`},
		"digits only":          {"listed", `{"repository":"octo-org/example","username":"583231","permission":"push"}`},
		"slash login":          {"listed", `{"repository":"octo-org/example","username":"a/b","permission":"push"}`},
		"empty login":          {"listed", `{"repository":"octo-org/example","username":"","permission":"push"}`},
		"missing permission":   {"listed", `{"repository":"octo-org/example","username":"octocat"}`},
		"slash permission":     {"listed", `{"repository":"octo-org/example","username":"octocat","permission":"a/b"}`},
		"control permission":   {"listed", `{"repository":"octo-org/example","username":"octocat","permission":"a\nb"}`},
		"long permission":      {"listed", `{"repository":"octo-org/example","username":"octocat","permission":"` + strings.Repeat("a", 101) + `"}`},
		"login of an id field": {"listed", `{"repository":"octo-org/example","invitee_id":1,"permission":"push"}`},
	} {
		*reads = 0
		_, err := invoke(t, core, repositoryCollaboratorsAdd.ID, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) || *reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("%s = %v, reads %d, requests %d", name, err, *reads, len(f.recorded()))
		}
	}
}

func TestCollaboratorsAddRefusesAnAccountThatIsNoMatchingUserWithoutAWrite(t *testing.T) {
	for name, account := range map[string]string{
		"organization": `{"login":"octocat","id":1,"type":"Organization"}`,
		"bot":          `{"login":"octocat","id":1,"type":"Bot"}`,
		"other login":  `{"login":"someone-else","id":1,"type":"User"}`,
		"no id":        `{"login":"octocat","type":"User"}`,
		"empty":        `{}`,
	} {
		f, _, core := addCore(t, addServer{account: account})
		_, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, true)
		if !isInvalidRequest(err) || methods(f, http.MethodPut) != 0 || len(f.recorded()) != 1 {
			t.Errorf("%s = %v, requests %+v", name, err, f.recorded())
		}
	}
}

func TestCollaboratorsAddRefusesAnExistingCollaboratorWithoutAWrite(t *testing.T) {
	f, _, core := addCore(t, addServer{read: http.StatusNoContent})
	_, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, true)
	if !isInvalidRequest(err) || !strings.Contains(err.Error(), "github.collaborators.update") {
		t.Fatalf("err = %v, want an invalid request naming the maintenance tool", err)
	}
	if methods(f, http.MethodPut) != 0 {
		t.Error("a PUT was sent for an existing collaborator")
	}
}

func TestCollaboratorsAddIsOfferedOnlyWhereListedAndNeedsConfirmation(t *testing.T) {
	f, reads, core := addCore(t, addServer{})
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "unlisted", addArguments, true); !errors.As(err, &unsupported) {
		t.Errorf("without a tools entry = %v, want unsupported-capability", err)
	}
	if _, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, false); err == nil ||
		*reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("unconfirmed = %v, reads %d, requests %d", err, *reads, len(f.recorded()))
	}
}

func TestCollaboratorsAddMapsRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		class  provider.Class
		want   string
	}{
		{"forbidden", http.StatusForbidden, provider.ClassPermission, "Administration"},
		{"not found", http.StatusNotFound, provider.ClassNotFound, ""},
		{"unprocessable", http.StatusUnprocessableEntity, "", "refused this invitation"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _, core := addCore(t, addServer{put: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, `{"message":"SECRETTEXT"}`)
			}})
			_, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, true)
			if err == nil || strings.Contains(err.Error(), "SECRETTEXT") || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q without provider text", err, tt.want)
			}
			if tt.status == http.StatusUnprocessableEntity && !isInvalidRequest(err) {
				t.Errorf("422 = %v, want an invalid request", err)
			}
			if tt.class != "" && classOf(err) != tt.class {
				t.Errorf("class = %s, want %s", classOf(err), tt.class)
			}
			if methods(f, http.MethodPut) != 1 {
				t.Errorf("puts = %d, want 1", methods(f, http.MethodPut))
			}
		})
	}
}

func TestCollaboratorsAddMapsRefusedReads(t *testing.T) {
	f, _, core := addCore(t, addServer{read: http.StatusForbidden})
	_, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, true)
	if classOf(err) != provider.ClassPermission || strings.Contains(err.Error(), "SECRETTEXT") ||
		methods(f, http.MethodPut) != 0 {
		t.Errorf("err = %v, puts %d", err, methods(f, http.MethodPut))
	}
}

func TestCollaboratorsAddNeverRepeatsAnUnclearChange(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error":       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"unexpected success": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) },
		"dropped connection": func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, core := addCore(t, addServer{put: answer})
			_, err := invoke(t, core, repositoryCollaboratorsAdd.ID, "listed", addArguments, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("err = %v, want an unclear outcome", err)
			}
			if methods(f, http.MethodPut) != 1 {
				t.Errorf("puts = %d, want exactly 1", methods(f, http.MethodPut))
			}
		})
	}
}

func TestCollaboratorsAddReportsATimeoutOnceAndUncertain(t *testing.T) {
	f, _, core := addCore(t, addServer{put: func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := core.Invoke(ctx, application.InvokeRequest{Operation: repositoryCollaboratorsAdd.ID,
		Connection: "listed", Arguments: json.RawMessage(addArguments), Confirmed: true})
	if classOf(err) != provider.ClassTimeout || !strings.Contains(err.Error(), "may have been applied") {
		t.Errorf("err = %v, want a timeout with an unclear outcome", err)
	}
	if methods(f, http.MethodPut) != 1 {
		t.Errorf("puts = %d, want exactly 1", methods(f, http.MethodPut))
	}
}

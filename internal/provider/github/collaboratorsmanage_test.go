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

const collaboratorRoute = "/api/v3/repos/octo-org/example/collaborators/octocat"

// collaboratorCore serves the one collaborator route: reads answers with the read status, every change with
// answer.
func collaboratorCore(t *testing.T, read int, answer func(http.ResponseWriter, *http.Request)) (*fakeGitHub, *int, *application.Core) {
	t.Helper()
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != collaboratorRoute {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.WriteHeader(read)
			if read >= 400 {
				fmt.Fprint(w, `{"message":"SECRETTEXT"}`)
			}
			return true
		}
		answer(w, r)
		return true
	}
	base := serve(t, f)
	cfg := coreConfig(base)
	all := config.Effects()
	add := func(name string, targets, excludes []string, tools ...string) {
		cfg.Connections[name] = config.Connection{Service: "gh", Credential: "gh-reader", Targets: targets,
			Exclude: excludes, Effects: all, Tools: tools}
	}
	both := []string{repositoryCollaboratorsUpdate.ID, repositoryCollaboratorsRemove.ID}
	add("listed", []string{"repos/octo-org/*"}, nil, both...)
	add("unlisted", []string{"repos/octo-org/*"}, nil)
	add("exact", []string{"repos/octo-org/other"}, nil, both...)
	add("excluded", []string{"repos/octo-org/*"}, []string{"repos/octo-org/example"}, both...)
	reads := 0
	red := &redact.Redactor{}
	return f, &reads, application.New(registry(t), cfg, resolver(red, &reads), red)
}

func noContent(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func methods(f *fakeGitHub, method string) int {
	n := 0
	for _, r := range f.recorded() {
		if r.method == method {
			n++
		}
	}
	return n
}

const updateArguments = `{"repository":"octo-org/example","username":"octocat","permission":"push"}`
const removeArguments = `{"repository":"octo-org/example","username":"octocat"}`

func TestCollaboratorsUpdateReadsThenSendsOnePut(t *testing.T) {
	f, _, core := collaboratorCore(t, http.StatusNoContent, noContent)
	result, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, "listed", updateArguments, true)
	if err != nil || !strings.Contains(string(result), `"updated":true`) ||
		!strings.Contains(string(result), `"permission":"push"`) {
		t.Fatalf("result = %s, %v", result, err)
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodPut ||
		len(requests[1].body) != 1 || requests[1].body["permission"] != "push" {
		t.Errorf("requests = %+v, want a read and one PUT with only the permission", requests)
	}
	// A custom repository role name goes through.
	if _, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, "listed",
		`{"repository":"octo-org/example","username":"octocat","permission":"Release manager"}`, true); err != nil {
		t.Errorf("custom role = %v", err)
	}
}

func TestCollaboratorsUpdateRefusesANonCollaboratorWithoutAWrite(t *testing.T) {
	f, _, core := collaboratorCore(t, http.StatusNotFound, noContent)
	_, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, "listed", updateArguments, true)
	if !isInvalidRequest(err) || strings.Contains(err.Error(), "SECRETTEXT") {
		t.Fatalf("err = %v, want an invalid request", err)
	}
	if methods(f, http.MethodPut) != 0 || len(f.recorded()) != 1 {
		t.Errorf("requests = %+v, want the read only", f.recorded())
	}
}

func TestCollaboratorsUpdateReportsAnInvitationAsDeviation(t *testing.T) {
	_, _, core := collaboratorCore(t, http.StatusNoContent, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"id":1,"invitee":{"login":"SECRETTEXT"}}`)
	})
	result, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, "listed", updateArguments, true)
	if err == nil || len(result) != 0 || classOf(err) != provider.ClassInvalidResponse ||
		!strings.Contains(err.Error(), "invitation") || strings.Contains(err.Error(), "SECRETTEXT") {
		t.Errorf("result = %s, err = %v, want a deviation naming the invitation", result, err)
	}
}

func TestCollaboratorsRemoveReadsThenSendsOneDelete(t *testing.T) {
	f, _, core := collaboratorCore(t, http.StatusNoContent, noContent)
	result, err := invoke(t, core, repositoryCollaboratorsRemove.ID, "listed", removeArguments, true)
	if err != nil || !strings.Contains(string(result), `"removed":true`) ||
		!strings.Contains(string(result), `"username":"octocat"`) {
		t.Fatalf("result = %s, %v", result, err)
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodGet || requests[1].method != http.MethodDelete {
		t.Errorf("requests = %+v, want a read and one DELETE", requests)
	}
}

func TestCollaboratorsRemoveRefusesANonCollaboratorAsNotFound(t *testing.T) {
	f, _, core := collaboratorCore(t, http.StatusNotFound, noContent)
	_, err := invoke(t, core, repositoryCollaboratorsRemove.ID, "listed", removeArguments, true)
	if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), "SECRETTEXT") {
		t.Fatalf("err = %v, want not-found", err)
	}
	if methods(f, http.MethodDelete) != 0 {
		t.Error("a DELETE was sent for a non-collaborator")
	}
}

func TestCollaboratorsChecksArgumentsAndTargetsBeforeAnySecretOrRequest(t *testing.T) {
	f, reads, core := collaboratorCore(t, http.StatusNoContent, noContent)
	for name, tt := range map[string]struct{ connection, arguments string }{
		"outside the targets": {"exact", updateArguments},
		"excluded":            {"excluded", updateArguments},
		"other owner":         {"listed", `{"repository":"other-org/example","username":"octocat","permission":"push"}`},
		"bad login":           {"listed", `{"repository":"octo-org/example","username":"../x","permission":"push"}`},
		"slash login":         {"listed", `{"repository":"octo-org/example","username":"a/b","permission":"push"}`},
		"missing permission":  {"listed", removeArguments},
		"empty permission":    {"listed", `{"repository":"octo-org/example","username":"octocat","permission":""}`},
		"slash permission":    {"listed", `{"repository":"octo-org/example","username":"octocat","permission":"a/b"}`},
		"control permission":  {"listed", `{"repository":"octo-org/example","username":"octocat","permission":"a\nb"}`},
		"long permission":     {"listed", `{"repository":"octo-org/example","username":"octocat","permission":"` + strings.Repeat("a", 101) + `"}`},
		"pattern repository":  {"listed", `{"repository":"octo-org/*","username":"octocat","permission":"push"}`},
	} {
		*reads = 0
		_, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) || *reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("update %s = %v, reads %d, requests %d", name, err, *reads, len(f.recorded()))
		}
	}
	for name, tt := range map[string]struct{ connection, arguments string }{
		"outside the targets": {"exact", removeArguments},
		"excluded":            {"excluded", removeArguments},
		"bad login":           {"listed", `{"repository":"octo-org/example","username":"a b"}`},
	} {
		*reads = 0
		_, err := invoke(t, core, repositoryCollaboratorsRemove.ID, tt.connection, tt.arguments, true)
		if !isInvalidRequest(err) || *reads != 0 || len(f.recorded()) != 0 {
			t.Errorf("remove %s = %v, reads %d, requests %d", name, err, *reads, len(f.recorded()))
		}
	}
}

func TestCollaboratorsAreOfferedOnlyWhereListedAndNeedConfirmation(t *testing.T) {
	f, reads, core := collaboratorCore(t, http.StatusNoContent, noContent)
	var unsupported *capability.UnsupportedError
	for id, arguments := range map[string]string{repositoryCollaboratorsUpdate.ID: updateArguments,
		repositoryCollaboratorsRemove.ID: removeArguments} {
		if _, err := invoke(t, core, id, "unlisted", arguments, true); !errors.As(err, &unsupported) {
			t.Errorf("%s without a tools entry = %v, want unsupported-capability", id, err)
		}
		if _, err := invoke(t, core, id, "listed", arguments, false); err == nil || *reads != 0 ||
			len(f.recorded()) != 0 {
			t.Errorf("%s unconfirmed = %v, reads %d", id, err, *reads)
		}
	}
}

func TestCollaboratorsMapRefusalsWithoutProviderText(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		class  provider.Class
		want   string
	}{
		{"forbidden", http.StatusForbidden, provider.ClassPermission, "Administration"},
		{"not found", http.StatusNotFound, provider.ClassNotFound, ""},
		{"unprocessable", http.StatusUnprocessableEntity, "", "refused this permission"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f, _, core := collaboratorCore(t, http.StatusNoContent, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, `{"message":"SECRETTEXT"}`)
			})
			_, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, "listed", updateArguments, true)
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
			_, err = invoke(t, core, repositoryCollaboratorsRemove.ID, "listed", removeArguments, true)
			if err == nil || strings.Contains(err.Error(), "SECRETTEXT") {
				t.Errorf("remove err = %v", err)
			}
		})
	}
}

func TestCollaboratorsNeverRepeatAnUnclearChange(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"unexpected success": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
		"dropped connection": func(w http.ResponseWriter, _ *http.Request) {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f, _, core := collaboratorCore(t, http.StatusNoContent, answer)
			_, err := invoke(t, core, repositoryCollaboratorsUpdate.ID, "listed", updateArguments, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("update err = %v, want an unclear outcome", err)
			}
			if methods(f, http.MethodPut) != 1 {
				t.Errorf("puts = %d, want exactly 1", methods(f, http.MethodPut))
			}
			_, err = invoke(t, core, repositoryCollaboratorsRemove.ID, "listed", removeArguments, true)
			if err == nil || !strings.Contains(err.Error(), "may have been applied") {
				t.Errorf("remove err = %v, want an unclear outcome", err)
			}
			if methods(f, http.MethodDelete) != 1 {
				t.Errorf("deletes = %d, want exactly 1", methods(f, http.MethodDelete))
			}
		})
	}
}

func TestCollaboratorsReportATimeoutOnceAndUncertain(t *testing.T) {
	f, _, core := collaboratorCore(t, http.StatusNoContent, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := core.Invoke(ctx, application.InvokeRequest{Operation: repositoryCollaboratorsUpdate.ID,
		Connection: "listed", Arguments: json.RawMessage(updateArguments), Confirmed: true})
	if classOf(err) != provider.ClassTimeout || !strings.Contains(err.Error(), "may have been applied") {
		t.Errorf("err = %v, want a timeout with an unclear outcome", err)
	}
	if methods(f, http.MethodPut) != 1 {
		t.Errorf("puts = %d, want exactly 1", methods(f, http.MethodPut))
	}
}

package github

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

type maintainFixture struct {
	core *application.Core
	f    *fakeGitHub
	// status makes every mutation answer with it when set.
	status *int
	reads  *int
}

func newMaintainFixture(t *testing.T) *maintainFixture {
	t.Helper()
	d := &fakeDiscussions{}
	f := &fakeGitHub{}
	d.f = f
	status := new(int)
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		reqs := f.recorded()
		if *status != 0 && strings.Contains(reqs[len(reqs)-1].document, "mutation") {
			w.WriteHeader(*status)
			_, _ = w.Write([]byte(`{"message":"SECRETTEXT"}`))
			return true
		}
		return d.route(w, r)
	}
	red := &redact.Redactor{}
	reads := new(int)
	core := application.New(registry(t), discussionWriteConfig(serve(t, f)), resolver(red, reads), red)
	return &maintainFixture{core: core, f: f, status: status, reads: reads}
}

func (m *maintainFixture) mutations() int {
	n := 0
	for _, r := range m.f.recorded() {
		if strings.Contains(r.document, "mutation") {
			n++
		}
	}
	return n
}

func TestDiscussionMaintenanceToolsSucceed(t *testing.T) {
	m := newMaintainFixture(t)
	for _, tt := range []struct{ id, connection, args, want string }{
		{discussionsUpdate.ID, "repo", `{"number":12,"title":"New","body":"b","category_id":"DIC_cat1"}`, `"number":12`},
		{discussionsClose.ID, "repo", `{"number":12,"reason":"resolved"}`, `"closed":true`},
		{discussionsReopen.ID, "repo", `{"number":12}`, `"closed":false`},
		{discussionsLock.ID, "repo", `{"number":12,"lock_reason":"too heated"}`, `"locked":true`},
		{discussionsUnlock.ID, "repo", `{"number":12}`, `"locked":false`},
		{discussionCommentsMarkAnswer.ID, "repo", `{"comment_id":"DC_top"}`, `"answered":true`},
		{discussionCommentsUnmarkAnswer.ID, "repo", `{"comment_id":"DC_reply"}`, `"answered":false`},
		{discussionsDelete.ID, "repo-listed", `{"number":12}`, `"deleted":true`},
	} {
		out, err := invoke(t, m.core, tt.id, tt.connection, tt.args, true)
		if err != nil || !strings.Contains(string(out), tt.want) {
			t.Errorf("%s = %s, %v, want %s", tt.id, out, err, tt.want)
		}
	}
	// The mutation uses the node id of the reading and the enum spelling GitHub expects.
	var closed, locked, updated bool
	for _, r := range m.f.recorded() {
		switch {
		case strings.Contains(r.document, "closeDiscussion("):
			closed = r.variables["id"] == "D_12" && r.variables["reason"] == "RESOLVED"
		case strings.Contains(r.document, "lockLockable(input:{lockableId:$id,lockReason"):
			locked = r.variables["id"] == "D_12" && r.variables["reason"] == "TOO_HEATED"
		case strings.Contains(r.document, "updateDiscussion("):
			input, _ := r.variables["input"].(map[string]any)
			updated = input["discussionId"] == "D_12" && input["categoryId"] == "DIC_cat1" && input["title"] == "New"
		}
	}
	if !closed || !locked || !updated {
		t.Errorf("mutation variables wrong: close %v, lock %v, update %v", closed, locked, updated)
	}
}

func TestDiscussionMaintenanceRefusesBeforeMutation(t *testing.T) {
	m := newMaintainFixture(t)
	before := m.mutations()
	for _, tt := range []struct{ id, connection, args, class string }{
		{discussionsUpdate.ID, "repo", `{"number":99,"title":"x"}`, "notfound"},
		{discussionsUpdate.ID, "repo", `{"number":12,"category_id":"DIC_foreign"}`, "notfound"},
		{discussionsUpdate.ID, "repo", `{"number":12,"category_id":"DIC_unknown"}`, "notfound"},
		{discussionsClose.ID, "repo", `{"number":99}`, "notfound"},
		{discussionsReopen.ID, "repo", `{"number":99}`, "notfound"},
		{discussionsLock.ID, "repo", `{"number":99}`, "notfound"},
		{discussionsUnlock.ID, "repo", `{"number":99}`, "notfound"},
		{discussionsDelete.ID, "repo-listed", `{"number":99}`, "notfound"},
		{discussionCommentsMarkAnswer.ID, "repo", `{"comment_id":"DC_foreign"}`, "notfound"},
		{discussionCommentsMarkAnswer.ID, "repo", `{"comment_id":"DC_unknown"}`, "notfound"},
		{discussionCommentsUnmarkAnswer.ID, "repo", `{"comment_id":"DC_foreign"}`, "notfound"},
		{discussionCommentsMarkAnswer.ID, "repo", `{"comment_id":"DC_reply"}`, "invalid"},
		{discussionCommentsMarkAnswer.ID, "repo", `{"comment_id":"DC_plain"}`, "invalid"},
		{discussionsUpdate.ID, "repo", `{"number":12}`, "invalid"},
		{discussionsClose.ID, "repo", `{"number":12,"reason":"bogus"}`, "invalid"},
		{discussionsLock.ID, "repo", `{"number":12,"lock_reason":"bogus"}`, "invalid"},
	} {
		_, err := invoke(t, m.core, tt.id, tt.connection, tt.args, true)
		if (tt.class == "invalid" && !isInvalidRequest(err)) || (tt.class == "notfound" && classOf(err) != provider.ClassNotFound) {
			t.Errorf("%s %s = %v, want %s", tt.id, tt.args, err, tt.class)
		}
	}
	if m.mutations() != before {
		t.Error("a refused change was sent to GitHub")
	}
}

func TestDiscussionMaintenanceRefusesForeignTargetBeforeIO(t *testing.T) {
	m := newMaintainFixture(t)
	*m.reads = 0
	before := len(m.f.recorded())
	for _, tt := range []struct{ id, args string }{
		{discussionsUpdate.ID, `{"repository":"other/repo","number":1,"title":"x"}`},
		{discussionsClose.ID, `{"repository":"other/repo","number":1}`},
		{discussionsReopen.ID, `{"repository":"other/repo","number":1}`},
		{discussionsLock.ID, `{"repository":"other/repo","number":1}`},
		{discussionsUnlock.ID, `{"repository":"other/repo","number":1}`},
		{discussionCommentsMarkAnswer.ID, `{"repository":"other/repo","comment_id":"DC_top"}`},
		{discussionCommentsUnmarkAnswer.ID, `{"repository":"other/repo","comment_id":"DC_top"}`},
		{discussionsUpdate.ID, `{"number":0,"title":"x"}`},
		{discussionCommentsMarkAnswer.ID, `{"comment_id":"bad id!"}`},
	} {
		if _, err := invoke(t, m.core, tt.id, "repo", tt.args, true); !isInvalidRequest(err) {
			t.Errorf("%s %s = %v, want invalid-request", tt.id, tt.args, err)
		}
	}
	if _, err := invoke(t, m.core, discussionsDelete.ID, "repo-listed", `{"repository":"other/repo","number":1}`, true); !isInvalidRequest(err) {
		t.Errorf("delete of a foreign repository = %v, want invalid-request", err)
	}
	if *m.reads != 0 || len(m.f.recorded()) != before {
		t.Error("a refused request reached the credential or GitHub")
	}
}

func TestDiscussionDeleteIsListedOnlyAndConfirmed(t *testing.T) {
	m := newMaintainFixture(t)
	var unsupported *capability.UnsupportedError
	if _, err := invoke(t, m.core, discussionsDelete.ID, "repo", `{"number":12}`, true); !errors.As(err, &unsupported) {
		t.Errorf("delete without a tools list = %v, want unsupported-capability", err)
	}
	before := len(m.f.recorded())
	unconfirmed := &application.ConfirmationRequiredError{}
	for _, id := range []string{discussionsUpdate.ID, discussionsClose.ID, discussionsLock.ID,
		discussionCommentsMarkAnswer.ID, discussionsDelete.ID} {
		args := map[string]string{discussionsUpdate.ID: `{"number":12,"title":"x"}`,
			discussionCommentsMarkAnswer.ID: `{"comment_id":"DC_top"}`}[id]
		if args == "" {
			args = `{"number":12}`
		}
		if _, err := invoke(t, m.core, id, "repo-listed", args, false); !errors.As(err, &unconfirmed) {
			t.Errorf("%s without --confirm = %v, want confirmation-required", id, err)
		}
	}
	if len(m.f.recorded()) != before {
		t.Error("an unconfirmed change reached GitHub")
	}
}

func TestDiscussionMaintenanceProviderFailures(t *testing.T) {
	for _, tt := range []struct {
		status int
		class  provider.Class
	}{
		{http.StatusForbidden, provider.ClassPermission},
		{http.StatusNotFound, provider.ClassNotFound},
		{http.StatusUnprocessableEntity, provider.ClassProviderError},
		{http.StatusGatewayTimeout, provider.ClassTimeout},
	} {
		m := newMaintainFixture(t)
		*m.status = tt.status
		for id, args := range map[string]string{discussionsUpdate.ID: `{"number":12,"title":"x"}`,
			discussionsClose.ID: `{"number":12}`, discussionsLock.ID: `{"number":12}`,
			discussionCommentsMarkAnswer.ID:   `{"comment_id":"DC_top"}`,
			discussionCommentsUnmarkAnswer.ID: `{"comment_id":"DC_top"}`} {
			before := m.mutations()
			_, err := invoke(t, m.core, id, "repo", args, true)
			if classOf(err) != tt.class {
				t.Errorf("%s with %d = %v, want class %v", id, tt.status, err, tt.class)
			}
			if err != nil && strings.Contains(err.Error(), "SECRETTEXT") {
				t.Errorf("%s leaked provider text: %v", id, err)
			}
			if m.mutations()-before != 1 {
				t.Errorf("%s with %d sent %d mutations, want exactly 1", id, tt.status, m.mutations()-before)
			}
		}
	}
}

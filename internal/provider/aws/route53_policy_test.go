package aws

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	tpID  = "01234567-89ab-cdef-0123-456789abcdef"
	tpDoc = `{"AWSPolicyFormatVersion":"2015-10-01","RecordType":"A","Endpoints":{"e1":{"Type":"value","Value":"192.0.2.1"}},"StartEndpoint":"e1"}`
)

// tpArgs makes the arguments of a create or version call with a document text.
func tpArgs(doc string, rest ...string) string {
	text, _ := json.Marshal(doc)
	return `{` + strings.Join(append(rest, `"document":`+string(text)), ",") + `}`
}

func tpAnswer(version string) reply {
	r := xmlReply(`<R ` + route53NS + `><TrafficPolicy><Id>` + tpID + `</Id><Version>` + version + `</Version><Name>web</Name><Type>A</Type>` +
		`<Document>` + strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(tpDoc) + `</Document><Comment>c</Comment></TrafficPolicy></R>`)
	r.status, r.header["Location"] = 201, "https://route53.amazonaws.com/2013-04-01/trafficpolicy/"+tpID+"/"+version
	return r
}

// A policy is created, gets a second version, a new comment, and is deleted, each with the request of the model.
func TestRoute53TrafficPolicyLifecycle(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch {
		case r.method == "DELETE":
			return reply{status: 204, header: map[string]string{}}
		case strings.HasSuffix(r.path, "/trafficpolicy"):
			return tpAnswer("1")
		case strings.HasSuffix(r.path, "/2"):
			rep := tpAnswer("2")
			rep.status = 200
			return rep
		}
		return tpAnswer("2")
	})
	e := newToolsEnv(t, f, baseValues(), route53ToolIDs)
	steps := []struct{ id, args string }{
		{"aws.route53.traffic-policy.create", tpArgs(tpDoc, `"name":"web"`, `"comment":"c"`)},
		{"aws.route53.traffic-policy.create-version", tpArgs(tpDoc, `"id":"`+tpID+`"`, `"comment":"c"`)},
		{"aws.route53.traffic-policy.update-comment", `{"id":"` + tpID + `","version":2,"comment":"reviewed"}`},
		{"aws.route53.traffic-policy.delete", `{"id":"` + tpID + `","version":2}`},
	}
	for _, c := range steps {
		text, err := e.invokeConfirmed(c.id, c.args, true)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if !strings.HasSuffix(c.id, ".delete") {
			policy, _ := decodeResult(t, text)["traffic_policy"].(map[string]any)
			if policy["id"] != tpID || policy["document"] != tpDoc {
				t.Errorf("%s result = %s", c.id, text)
			}
		}
	}
	base := "/2013-04-01/trafficpolicy"
	for i, want := range []struct{ method, path string }{
		{"POST", base}, {"POST", base + "/" + tpID}, {"POST", base + "/" + tpID + "/2"}, {"DELETE", base + "/" + tpID + "/2"},
	} {
		if r := restRequests(f)[i]; r.method != want.method || r.path != want.path {
			t.Errorf("request %d = %s %s, want %s %s", i, r.method, r.path, want.method, want.path)
		}
	}
	reqs := restRequests(f)
	for _, part := range []string{"<Name>web</Name>", "<Comment>c</Comment>", "<Document>" + strings.NewReplacer("<", "&lt;", ">", "&gt;", `"`, "&#34;").Replace(tpDoc)} {
		if !strings.Contains(reqs[0].body, part) {
			t.Errorf("create body lacks %s: %s", part, reqs[0].body)
		}
	}
	if !strings.Contains(reqs[2].body, "<Comment>reviewed</Comment>") {
		t.Errorf("comment body = %s", reqs[2].body)
	}
}

// Every write needs the confirmation, and none reaches the network without it.
func TestRoute53TrafficPolicyWritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	for _, c := range []struct{ id, args string }{
		{"aws.route53.traffic-policy.create", tpArgs(tpDoc, `"name":"web"`)},
		{"aws.route53.traffic-policy.create-version", tpArgs(tpDoc, `"id":"`+tpID+`"`)},
		{"aws.route53.traffic-policy.update-comment", `{"id":"` + tpID + `","version":1,"comment":"c"}`},
		{"aws.route53.traffic-policy.delete", `{"id":"` + tpID + `","version":1}`},
	} {
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", c.id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
}

// A document that is not JSON or is longer than the model allows is refused before any request; the edge passes.
func TestRoute53TrafficPolicyDocumentIsJudgedBeforeAnyRequest(t *testing.T) {
	f := restAnswer(tpAnswer("1"))
	e := route53Env(t, f, testRegion)
	pad := func(n int) string { return `"` + strings.Repeat("a", n-2) + `"` }
	for name, doc := range map[string]string{
		"not JSON": "not json", "empty": "", "truncated": `{"a":`, "trailing text": `{} x`, "single quotes": `{'a':1}`,
		"over the limit": pad(maxPolicyDocument + 1), "over the limit in multibyte": `"` + strings.Repeat("ü", maxPolicyDocument) + `"`,
	} {
		for id, rest := range map[string]string{"aws.route53.traffic-policy.create": `"name":"web"`, "aws.route53.traffic-policy.create-version": `"id":"` + tpID + `"`} {
			if _, err := e.invokeConfirmed(id, tpArgs(doc, rest), true); err == nil {
				t.Errorf("%s: %s was accepted", id, name)
			}
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused document reached the network: %v", f.actions())
	}
	if _, err := e.invokeConfirmed("aws.route53.traffic-policy.create", tpArgs(pad(maxPolicyDocument), `"name":"web"`), true); err != nil {
		t.Errorf("document at the limit: %v", err)
	}
	// The model limit is the schema limit too: a client that skips the guard is held by it.
	for _, bad := range []string{`{"name":"web"}`, `{"document":"{}"}`, `{"name":"web","document":"{}","comment":"` + strings.Repeat("c", 1025) + `"}`,
		`{"name":"web","document":"{}","hidden":true}`} {
		if _, err := e.invokeConfirmed("aws.route53.traffic-policy.create", bad, true); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

// Route 53 errors keep their code and class, and a write is sent once.
func TestRoute53TrafficPolicyErrors(t *testing.T) {
	for _, c := range []struct {
		id, args, code string
		status         int
		notFound       bool
	}{
		{"aws.route53.traffic-policy.create", tpArgs(tpDoc, `"name":"web"`), "TrafficPolicyAlreadyExists", 409, false},
		{"aws.route53.traffic-policy.create", tpArgs(tpDoc, `"name":"web"`), "InvalidTrafficPolicyDocument", 400, false},
		{"aws.route53.traffic-policy.create", tpArgs(tpDoc, `"name":"web"`), "TooManyTrafficPolicies", 400, false},
		{"aws.route53.traffic-policy.create-version", tpArgs(tpDoc, `"id":"`+tpID+`"`), "TooManyTrafficPolicyVersionsForCurrentPolicy", 400, false},
		{"aws.route53.traffic-policy.create-version", tpArgs(tpDoc, `"id":"`+tpID+`"`), "NoSuchTrafficPolicy", 404, true},
		{"aws.route53.traffic-policy.update-comment", `{"id":"` + tpID + `","version":1,"comment":"c"}`, "NoSuchTrafficPolicy", 404, true},
		{"aws.route53.traffic-policy.delete", `{"id":"` + tpID + `","version":1}`, "TrafficPolicyInUse", 409, false},
		{"aws.route53.traffic-policy.delete", `{"id":"` + tpID + `","version":1}`, "NoSuchTrafficPolicy", 404, true},
	} {
		f := restAnswer(route53Error(c.status, c.code))
		_, err := route53Env(t, f, testRegion).invokeConfirmed(c.id, c.args, true)
		var failed *provider.Error
		if !errorsAs(err, &failed) || failed.Original == nil || failed.Original.Entries[0].Code != c.code ||
			(failed.Class == provider.ClassNotFound) != c.notFound {
			t.Errorf("%s %s: err = %v", c.id, c.code, err)
		}
		if n := len(restRequests(f)); n != 1 {
			t.Errorf("%s %s: requests = %d", c.id, c.code, n)
		}
	}
}

func TestRoute53TrafficPolicyToolsDeclareTheirRisk(t *testing.T) {
	want := map[string]struct{ effect, idempotency string }{
		"aws.route53.traffic-policy.create":         {"create", "non_idempotent"},
		"aws.route53.traffic-policy.create-version": {"create", "non_idempotent"},
		"aws.route53.traffic-policy.update-comment": {"update", "idempotent"},
		"aws.route53.traffic-policy.delete":         {"delete", "non_idempotent"},
	}
	seen := 0
	for _, d := range registered(t).Provider(Provider) {
		w, ok := want[d.ID]
		if !ok {
			continue
		}
		seen++
		if string(d.Risk.Effect) != w.effect || d.Risk.Confirmation != "required" || string(d.Risk.Idempotency) != w.idempotency || !d.Risk.OpenWorld ||
			d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
	}
	if seen != len(want) {
		t.Errorf("seen %d policy tools", seen)
	}
}

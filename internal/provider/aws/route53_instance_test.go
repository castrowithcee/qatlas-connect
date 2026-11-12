package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const tpiID = "11111111-89ab-cdef-0123-456789abcdef"

// tpiAnswer is an instance answer in the given state.
func tpiAnswer(state, message string, status int) reply {
	r := xmlReply(`<R ` + route53NS + `><TrafficPolicyInstance><Id>` + tpiID + `</Id><HostedZoneId>Z1</HostedZoneId><Name>www.example.com.</Name>` +
		`<TTL>60</TTL><State>` + state + `</State><Message>` + message + `</Message><TrafficPolicyId>` + tpID + `</TrafficPolicyId>` +
		`<TrafficPolicyVersion>2</TrafficPolicyVersion><TrafficPolicyType>A</TrafficPolicyType></TrafficPolicyInstance></R>`)
	r.status, r.header["Location"] = status, "https://route53.amazonaws.com/2013-04-01/trafficpolicyinstance/"+tpiID
	return r
}

const (
	tpiCreateArgs = `{"hosted_zone_id":"Z1","name":"www.example.com","ttl":60,"traffic_policy_id":"` + tpID + `","traffic_policy_version":2}`
	tpiUpdateArgs = `{"id":"` + tpiID + `","ttl":60,"traffic_policy_id":"` + tpID + `","traffic_policy_version":2}`
	tpiDeleteArgs = `{"id":"` + tpiID + `"}`
)

// An instance is created in the state Creating, switched, and deleted, each with the request of the model; the answer names state and message.
func TestRoute53TrafficPolicyInstanceLifecycle(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch r.method {
		case "DELETE":
			return reply{status: 202, header: map[string]string{}}
		case "POST":
			if strings.HasSuffix(r.path, "/trafficpolicyinstance") {
				return tpiAnswer("Creating", "Creating the records", 201)
			}
		}
		return tpiAnswer("Applying", "Applying the policy", 202)
	})
	e := newToolsEnv(t, f, baseValues(), route53ToolIDs)
	steps := []struct{ id, args, state, message string }{
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "Creating", "Creating the records"},
		{"aws.route53.traffic-policy-instance.update", tpiUpdateArgs, "Applying", "Applying the policy"},
		{"aws.route53.traffic-policy-instance.delete", tpiDeleteArgs, "", ""},
	}
	for _, c := range steps {
		text, err := e.invokeConfirmed(c.id, c.args, true)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if c.state == "" {
			continue
		}
		inst, _ := decodeResult(t, text)["traffic_policy_instance"].(map[string]any)
		if inst["id"] != tpiID || inst["state"] != c.state || inst["message"] != c.message || inst["traffic_policy_version"] != float64(2) {
			t.Errorf("%s result = %s", c.id, text)
		}
	}
	base := "/2013-04-01/trafficpolicyinstance"
	reqs := restRequests(f)
	for i, want := range []struct{ method, path string }{{"POST", base}, {"POST", base + "/" + tpiID}, {"DELETE", base + "/" + tpiID}} {
		if i >= len(reqs) || reqs[i].method != want.method || reqs[i].path != want.path {
			t.Fatalf("request %d = %+v, want %s %s", i, reqs, want.method, want.path)
		}
	}
	for _, part := range []string{"<HostedZoneId>Z1</HostedZoneId>", "<Name>www.example.com</Name>", "<TTL>60</TTL>",
		"<TrafficPolicyId>" + tpID + "</TrafficPolicyId>", "<TrafficPolicyVersion>2</TrafficPolicyVersion>"} {
		if !strings.Contains(reqs[0].body, part) {
			t.Errorf("create body lacks %s: %s", part, reqs[0].body)
		}
	}
	for _, part := range []string{"<TTL>60</TTL>", "<TrafficPolicyVersion>2</TrafficPolicyVersion>"} {
		if !strings.Contains(reqs[1].body, part) {
			t.Errorf("update body lacks %s: %s", part, reqs[1].body)
		}
	}
	if strings.Contains(reqs[1].body, "HostedZoneId") || strings.Contains(reqs[1].body, "<Name>") {
		t.Errorf("update body carries zone or name: %s", reqs[1].body)
	}
}

// The zone ID is sent without the prefix, whether it was given with it or not.
func TestRoute53TrafficPolicyInstanceZoneIDWithAndWithoutPrefix(t *testing.T) {
	for _, zone := range []string{"Z1", "/hostedzone/Z1"} {
		f := restAnswer(tpiAnswer("Creating", "m", 201))
		e := route53Env(t, f, testRegion)
		args := strings.Replace(tpiCreateArgs, `"Z1"`, `"`+zone+`"`, 1)
		if _, err := e.invokeConfirmed("aws.route53.traffic-policy-instance.create", args, true); err != nil {
			t.Fatalf("%s: %v", zone, err)
		}
		if body := restRequests(f)[0].body; !strings.Contains(body, "<HostedZoneId>Z1</HostedZoneId>") {
			t.Errorf("%s: body = %s", zone, body)
		}
	}
}

// Every write needs the confirmation, and none reaches the network without it.
func TestRoute53TrafficPolicyInstanceWritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	for _, c := range []struct{ id, args string }{
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs},
		{"aws.route53.traffic-policy-instance.update", tpiUpdateArgs},
		{"aws.route53.traffic-policy-instance.delete", tpiDeleteArgs},
	} {
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", c.id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
}

// A name of a bad form and values out of the model's range are refused before any request; the edges pass.
func TestRoute53TrafficPolicyInstanceInputIsJudgedBeforeAnyRequest(t *testing.T) {
	f := restAnswer(tpiAnswer("Creating", "m", 201))
	e := route53Env(t, f, testRegion)
	create := func(name, ttl, version string) string {
		return `{"hosted_zone_id":"Z1","name":` + name + `,"ttl":` + ttl + `,"traffic_policy_id":"` + tpID + `","traffic_policy_version":` + version + `}`
	}
	update := func(ttl, version string) string {
		return `{"id":"` + tpiID + `","ttl":` + ttl + `,"traffic_policy_id":"` + tpID + `","traffic_policy_version":` + version + `}`
	}
	long := `"` + strings.Repeat("a", 63) + strings.Repeat(".bbbbbbbbbb", 100) + `.com"` // over 1024 characters
	for name, args := range map[string]string{
		"create empty name": create(`""`, "60", "1"), "create space in name": create(`"a b.example.com"`, "60", "1"),
		"create empty label": create(`"a..example.com"`, "60", "1"), "create name too long": create(long, "60", "1"),
		"create negative ttl": create(`"a.example.com"`, "-1", "1"), "create ttl over the range": create(`"a.example.com"`, "2147483648", "1"),
		"create version 0": create(`"a.example.com"`, "60", "0"), "create version over the range": create(`"a.example.com"`, "60", "1001"),
		"update negative ttl": update("-1", "1"), "update ttl over the range": update("2147483648", "1"),
		"update version 0": update("60", "0"), "update version over the range": update("60", "1001"),
	} {
		id := "aws.route53.traffic-policy-instance.create"
		if strings.HasPrefix(name, "update") {
			id = "aws.route53.traffic-policy-instance.update"
		}
		if _, err := e.invokeConfirmed(id, args, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused input reached the network: %v", f.actions())
	}
	for _, args := range []string{create(`"a.example.com."`, "0", "1"), create(`"\\052.example.com"`, "2147483647", "1000")} {
		if _, err := e.invokeConfirmed("aws.route53.traffic-policy-instance.create", args, true); err != nil {
			t.Errorf("%s: %v", args, err)
		}
	}
	if _, err := e.invokeConfirmed("aws.route53.traffic-policy-instance.update", update("0", "1000"), true); err != nil {
		t.Errorf("update at the edge: %v", err)
	}
}

// Route 53 errors keep their code and class, and a write is sent once.
func TestRoute53TrafficPolicyInstanceErrors(t *testing.T) {
	for _, c := range []struct {
		id, args, code string
		status         int
		notFound       bool
	}{
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "TrafficPolicyInstanceAlreadyExists", 409, false},
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "TooManyTrafficPolicyInstances", 400, false},
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "NoSuchTrafficPolicy", 404, true},
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "NoSuchHostedZone", 404, true},
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "ConflictingTypes", 400, false},
		{"aws.route53.traffic-policy-instance.create", tpiCreateArgs, "PriorRequestNotComplete", 400, false},
		{"aws.route53.traffic-policy-instance.update", tpiUpdateArgs, "NoSuchTrafficPolicyInstance", 404, true},
		{"aws.route53.traffic-policy-instance.update", tpiUpdateArgs, "NoSuchTrafficPolicy", 404, true},
		{"aws.route53.traffic-policy-instance.update", tpiUpdateArgs, "PriorRequestNotComplete", 400, false},
		{"aws.route53.traffic-policy-instance.update", tpiUpdateArgs, "ConflictingTypes", 400, false},
		{"aws.route53.traffic-policy-instance.delete", tpiDeleteArgs, "NoSuchTrafficPolicyInstance", 404, true},
		{"aws.route53.traffic-policy-instance.delete", tpiDeleteArgs, "PriorRequestNotComplete", 400, false},
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

func TestRoute53TrafficPolicyInstanceToolsDeclareTheirRisk(t *testing.T) {
	want := map[string]struct{ effect, idempotency string }{
		"aws.route53.traffic-policy-instance.create": {"create", "non_idempotent"},
		"aws.route53.traffic-policy-instance.update": {"update", "idempotent"},
		"aws.route53.traffic-policy-instance.delete": {"delete", "non_idempotent"},
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
		text := strings.ToLower(d.Description)
		if !strings.Contains(text, "names the tool, not the arguments") || !strings.Contains(text, "state") {
			t.Errorf("%s description lacks the confirmation duty or the state: %s", d.ID, d.Description)
		}
		if strings.HasSuffix(d.ID, ".create") && (!strings.Contains(text, "billed") || !strings.Contains(text, "cost")) {
			t.Errorf("create description lacks the cost: %s", d.Description)
		}
		if strings.HasSuffix(d.ID, ".delete") && !strings.Contains(text, "records") {
			t.Errorf("delete description lacks the removed records: %s", d.Description)
		}
	}
	if seen != len(want) {
		t.Errorf("seen %d instance tools", seen)
	}
}

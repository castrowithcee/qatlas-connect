package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const cidrID = "01234567-89ab-cdef-0123-456789abcdef"

var delegationCIDRWrites = []struct{ id, args string }{
	{"aws.route53.delegation-set.create", `{}`},
	{"aws.route53.delegation-set.delete", `{"id":"N1"}`},
	{"aws.route53.cidr-collection.create", `{"name":"offices"}`},
	{"aws.route53.cidr-collection.change", `{"id":"` + cidrID + `","changes":[{"location_name":"berlin","action":"PUT","cidr_list":["192.0.2.0/24"]}]}`},
	{"aws.route53.cidr-collection.delete", `{"id":"` + cidrID + `"}`},
}

const delegationSetXML = `<DelegationSet><Id>/delegationset/N1</Id><CallerReference>c</CallerReference><NameServers>` +
	`<NameServer>ns-1.awsdns-01.org</NameServer><NameServer>ns-2.awsdns-02.com</NameServer></NameServers></DelegationSet>`

// A delegation set is created with a generated token, optionally from an existing zone, and the result names its
// name servers.
func TestRoute53DelegationSetCreateReturnsNameServersAndToken(t *testing.T) {
	f := direct(func(sent, int) reply {
		r := xmlReply(`<CreateReusableDelegationSetResponse ` + route53NS + `>` + delegationSetXML + `</CreateReusableDelegationSetResponse>`)
		r.status, r.header["Location"] = 201, "https://route53.amazonaws.com/2013-04-01/delegationset/N1"
		return r
	})
	e := route53Env(t, f, testRegion)
	if _, err := e.invoke("aws.route53.delegation-set.create", `{}`); !isConfirmationRequired(err) || len(f.requests) != 0 {
		t.Fatalf("err = %v, requests = %d, want a confirmation and no request", err, len(f.requests))
	}
	text, err := e.invokeConfirmed("aws.route53.delegation-set.create", `{}`, true)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	set, _ := got["delegation_set"].(map[string]any)
	servers, _ := set["name_servers"].([]any)
	token, _ := got["client_token"].(string)
	if token == "" || len(servers) != 2 || servers[1] != "ns-2.awsdns-02.com" || set["id"] != "/delegationset/N1" || got["location"] == nil {
		t.Fatalf("result = %s", text)
	}
	r := restRequests(f)[0]
	if r.method != "POST" || r.path != "/2013-04-01/delegationset" || !strings.Contains(r.body, "<CallerReference>"+token+"</CallerReference>") ||
		strings.Contains(r.body, "HostedZoneId") {
		t.Errorf("request = %+v", r)
	}
	// A zone to copy is sent without its prefix; a given token is sent as it is.
	args := `{"hosted_zone_id":"/hostedzone/Z1","client_token":"tok-1"}`
	if _, err := e.invokeConfirmed("aws.route53.delegation-set.create", args, true); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f)[1]; !strings.Contains(r.body, "<HostedZoneId>Z1</HostedZoneId>") || !strings.Contains(r.body, "<CallerReference>tok-1</CallerReference>") {
		t.Errorf("body = %s", r.body)
	}
}

// The delegation set ID of a delete goes into the path with or without its prefix.
func TestRoute53DelegationSetDeleteTakesBothIDForms(t *testing.T) {
	f := restAnswer(xmlReply(`<DeleteReusableDelegationSetResponse ` + route53NS + `/>`))
	e := route53Env(t, f, testRegion)
	for _, id := range []string{"N1", "/delegationset/N1"} {
		if _, err := e.invokeConfirmed("aws.route53.delegation-set.delete", `{"id":"`+id+`"}`, true); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	for i, r := range restRequests(f) {
		if r.method != "DELETE" || r.path != "/2013-04-01/delegationset/N1" {
			t.Errorf("request %d = %s %s", i, r.method, r.path)
		}
	}
	// The shape binding also strips a zone prefix here; Route 53 then answers an unknown set. It never reaches a zone.
	if _, err := e.invokeConfirmed("aws.route53.delegation-set.delete", `{"id":"/hostedzone/N1"}`, true); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f)[2]; r.path != "/2013-04-01/delegationset/N1" {
		t.Errorf("path = %s", r.path)
	}
}

// A zone is created with a delegation set, with and without the prefix; the ID goes into the body without it.
func TestRoute53CreateHostedZoneWithDelegationSet(t *testing.T) {
	f := restAnswer(xmlReply(`<CreateHostedZoneResponse ` + route53NS + `>` + zoneXML("Z1", "example.com.") + changeXML +
		`<DelegationSet>` + delegationSetXML[len(`<DelegationSet>`):] + `</CreateHostedZoneResponse>`))
	e := route53Env(t, f, testRegion)
	for _, id := range []string{"N1", "/delegationset/N1"} {
		if _, err := e.invokeConfirmed("aws.route53.hosted-zone.create", `{"name":"example.com","delegation_set_id":"`+id+`"}`, true); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	for i, r := range restRequests(f) {
		if !strings.Contains(r.body, "<DelegationSetId>N1</DelegationSetId>") {
			t.Errorf("body %d = %s", i, r.body)
		}
	}
	if _, err := e.invokeConfirmed("aws.route53.hosted-zone.create", `{"name":"example.com"}`, true); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f)[2]; strings.Contains(r.body, "DelegationSetId") {
		t.Errorf("body = %s", r.body)
	}
}

const collectionXML = `<Collection><Arn>arn:aws:route53::` + testAccount + `:cidrcollection/` + cidrID + `</Arn><Id>` + cidrID + `</Id><Name>offices</Name><Version>1</Version></Collection>`

// A collection is created with a generated token, changed with PUT and DELETE_IF_EXISTS, and deleted.
func TestRoute53CIDRCollectionLifecycle(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch {
		case r.method == "POST" && r.path == "/2013-04-01/cidrcollection":
			rep := xmlReply(`<CreateCidrCollectionResponse ` + route53NS + `>` + collectionXML + `</CreateCidrCollectionResponse>`)
			rep.status, rep.header["Location"] = 201, "https://route53.amazonaws.com/2013-04-01/cidrcollection/"+cidrID
			return rep
		case r.method == "POST":
			return xmlReply(`<ChangeCidrCollectionResponse ` + route53NS + `><Id>C1</Id></ChangeCidrCollectionResponse>`)
		}
		return xmlReply(`<DeleteCidrCollectionResponse ` + route53NS + `/>`)
	})
	e := route53Env(t, f, testRegion)
	text, err := e.invokeConfirmed("aws.route53.cidr-collection.create", `{"name":"offices"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	coll, _ := got["collection"].(map[string]any)
	token, _ := got["client_token"].(string)
	if token == "" || coll["id"] != cidrID || coll["name"] != "offices" {
		t.Fatalf("create result = %s", text)
	}
	if r := restRequests(f)[0]; !strings.Contains(r.body, "<CallerReference>"+token+"</CallerReference>") || !strings.Contains(r.body, "<Name>offices</Name>") {
		t.Errorf("create body = %s", r.body)
	}
	put := `{"id":"` + cidrID + `","collection_version":1,"changes":[{"location_name":"berlin","action":"PUT","cidr_list":["192.0.2.0/24","2001:db8::/48"]}]}`
	del := `{"id":"` + cidrID + `","changes":[{"location_name":"berlin","action":"DELETE_IF_EXISTS","cidr_list":["192.0.2.0/24"]}]}`
	for i, args := range []string{put, del} {
		text, err := e.invokeConfirmed("aws.route53.cidr-collection.change", args, true)
		if err != nil {
			t.Fatalf("change %d: %v", i, err)
		}
		if got := decodeResult(t, text); got["id"] != "C1" {
			t.Errorf("change result = %s", text)
		}
	}
	reqs := restRequests(f)
	for i, want := range []string{"<Action>PUT</Action>", "<Cidr>2001:db8::/48</Cidr>", "<CollectionVersion>1</CollectionVersion>", "<LocationName>berlin</LocationName>"} {
		if reqs[1].path != "/2013-04-01/cidrcollection/"+cidrID || !strings.Contains(reqs[1].body, want) {
			t.Errorf("put body lacks %d %s: %s (%s)", i, want, reqs[1].body, reqs[1].path)
		}
	}
	if !strings.Contains(reqs[2].body, "<Action>DELETE_IF_EXISTS</Action>") || strings.Contains(reqs[2].body, "CollectionVersion") {
		t.Errorf("delete-if-exists body = %s", reqs[2].body)
	}
	if _, err := e.invokeConfirmed("aws.route53.cidr-collection.delete", `{"id":"`+cidrID+`"}`, true); err != nil {
		t.Fatal(err)
	}
	if r := restRequests(f)[3]; r.method != "DELETE" || r.path != "/2013-04-01/cidrcollection/"+cidrID {
		t.Errorf("delete request = %s %s", r.method, r.path)
	}
}

// A block that is not in CIDR notation, a bad name, or an oversized call is refused before any request.
func TestRoute53CIDRChangeChecksTheFormBeforeAnyRequest(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	change := func(loc, block string) string {
		return `{"id":"` + cidrID + `","changes":[{"location_name":"` + loc + `","action":"PUT","cidr_list":["` + block + `"]}]}`
	}
	for _, args := range []string{
		change("a", "192.0.2.0"), change("a", "192.0.2.0/33"), change("a", "2001:db8::/129"), change("a", "not-a-block"),
		change("a", "fe80::1%eth0/64"), change("a", "192.0.2.0/24 "), change("a", ""), change("a", "192.0.2.256/24"),
		change("bad name", "192.0.2.0/24"), change("", "192.0.2.0/24"), change("*", "192.0.2.0/24"), change("0123456789abcdefg", "192.0.2.0/24"),
		`{"id":"x","changes":[{"location_name":"a","action":"PUT","cidr_list":["192.0.2.0/24"]}]}`,
		`{"id":"` + cidrID + `","changes":[{"location_name":"a","action":"REPLACE","cidr_list":["192.0.2.0/24"]}]}`,
		`{"id":"` + cidrID + `","changes":[]}`,
		`{"id":"` + cidrID + `","changes":[{"location_name":"a","action":"PUT","cidr_list":[]}]}`,
		`{"id":"` + cidrID + `","collection_version":0,"changes":[{"location_name":"a","action":"PUT","cidr_list":["192.0.2.0/24"]}]}`,
		`{"id":"` + cidrID + `","changes":[` + strings.TrimSuffix(strings.Repeat(`{"location_name":"a","action":"PUT","cidr_list":["192.0.2.0/24","192.0.2.0/25"]},`, 501), ",") + `]}`,
	} {
		if _, err := e.invokeConfirmed("aws.route53.cidr-collection.change", args, true); err == nil {
			t.Errorf("%.120s was accepted", args)
		}
	}
	if _, err := e.invokeConfirmed("aws.route53.cidr-collection.create", `{"name":"bad name"}`, true); err == nil {
		t.Error("a bad collection name was accepted")
	}
	if _, err := e.invokeConfirmed("aws.route53.cidr-collection.delete", `{"id":"not-a-uuid"}`, true); err == nil {
		t.Error("a bad collection ID was accepted")
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
}

// Errors keep their class and code, and a failed write is not repeated.
func TestRoute53DelegationAndCIDRErrors(t *testing.T) {
	for _, c := range []struct {
		id, args, code string
		status         int
		class          provider.Class
	}{
		{"aws.route53.delegation-set.create", `{}`, "DelegationSetAlreadyCreated", 400, provider.ClassProviderError},
		{"aws.route53.delegation-set.create", `{}`, "LimitsExceeded", 400, provider.ClassProviderError},
		{"aws.route53.delegation-set.delete", `{"id":"N1"}`, "DelegationSetInUse", 400, provider.ClassProviderError},
		{"aws.route53.delegation-set.delete", `{"id":"N1"}`, "NoSuchDelegationSet", 404, provider.ClassNotFound},
		{"aws.route53.hosted-zone.create", `{"name":"example.com","delegation_set_id":"N1"}`, "NoSuchDelegationSet", 404, provider.ClassNotFound},
		{"aws.route53.cidr-collection.create", `{"name":"offices"}`, "CidrCollectionAlreadyExistsException", 400, provider.ClassProviderError},
		{"aws.route53.cidr-collection.create", `{"name":"offices"}`, "LimitsExceeded", 400, provider.ClassProviderError},
		{"aws.route53.cidr-collection.change", delegationCIDRWrites[3].args, "CidrCollectionVersionMismatchException", 409, provider.ClassProviderError},
		{"aws.route53.cidr-collection.change", delegationCIDRWrites[3].args, "NoSuchCidrCollectionException", 404, provider.ClassNotFound},
		{"aws.route53.cidr-collection.delete", delegationCIDRWrites[4].args, "CidrCollectionInUseException", 400, provider.ClassProviderError},
	} {
		t.Run(c.id+"/"+c.code, func(t *testing.T) {
			f := restAnswer(route53Error(c.status, c.code))
			e := route53Env(t, f, testRegion)
			_, err := e.invokeConfirmed(c.id, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class {
				t.Fatalf("err = %v, want class %s", err, c.class)
			}
			if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code {
				t.Errorf("original = %+v", failed.Original)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a failed call was repeated", n)
			}
		})
	}
}

// Every write needs confirmation before it reaches the network.
func TestRoute53DelegationAndCIDRWritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	for _, c := range delegationCIDRWrites {
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", c.id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
}

// The five tools are confirmed writes of the open world with the contract's risk, and their descriptions carry what the
// confirmation does not show.
func TestRoute53DelegationAndCIDRToolsCarryTheirRisk(t *testing.T) {
	want := map[string]struct {
		idem  capability.Idempotency
		parts []string
	}{
		"aws.route53.delegation-set.create":  {capability.IdempotencyNonIdempotent, []string{"registrar", "client_token", "confirmation names the tool"}},
		"aws.route53.delegation-set.delete":  {capability.IdempotencyNonIdempotent, []string{"DelegationSetInUse", "confirmation names the tool"}},
		"aws.route53.cidr-collection.create": {capability.IdempotencyNonIdempotent, []string{"client_token", "confirmation names the tool"}},
		"aws.route53.cidr-collection.change": {capability.IdempotencyNonIdempotent, []string{"every change", "CidrCollectionVersionMismatchException"}},
		"aws.route53.cidr-collection.delete": {capability.IdempotencyNonIdempotent, []string{"CidrCollectionInUseException", "confirmation names the tool"}},
	}
	for id, w := range want {
		d := specOf(t, id).descriptor
		if d.Risk.Confirmation != capability.ConfirmationRequired || !d.Risk.OpenWorld || d.RequiresToolAllowList || d.Risk.Idempotency != w.idem {
			t.Errorf("%s = %+v", id, d)
		}
		for _, p := range w.parts {
			if !strings.Contains(d.Description, p) {
				t.Errorf("%s description lacks %q", id, p)
			}
		}
	}
	if d := specOf(t, "aws.route53.cidr-collection.change").descriptor; d.Risk.Effect != capability.EffectUpdate {
		t.Errorf("change effect = %s", d.Risk.Effect)
	}
}

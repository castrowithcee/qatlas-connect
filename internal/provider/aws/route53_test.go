package aws

import (
	"encoding/xml"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const route53NS = `xmlns="https://route53.amazonaws.com/doc/2013-04-01/"`

var route53ToolIDs = []string{"aws.route53.hosted-zone.list", "aws.route53.hosted-zone.list-by-name", "aws.route53.hosted-zone.get",
	"aws.route53.hosted-zone.count", "aws.route53.hosted-zone.create", "aws.route53.hosted-zone.update-comment",
	"aws.route53.hosted-zone.delete", "aws.route53.change.get", "aws.route53.tag.list", "aws.route53.tag.change",
	"aws.route53.record-set.list", "aws.route53.record-set.change", "aws.route53.dns-answer.test",
	"aws.route53.health-check.list", "aws.route53.health-check.get", "aws.route53.health-check.count",
	"aws.route53.health-check.get-status", "aws.route53.health-check.get-last-failure-reason",
	"aws.route53.health-check.create", "aws.route53.health-check.update", "aws.route53.health-check.delete",
	"aws.route53.dnssec.get", "aws.route53.dnssec.enable", "aws.route53.dnssec.disable", "aws.route53.key-signing-key.create",
	"aws.route53.key-signing-key.activate", "aws.route53.key-signing-key.deactivate", "aws.route53.key-signing-key.delete",
	"aws.route53.delegation-set.create", "aws.route53.delegation-set.delete", "aws.route53.cidr-collection.create",
	"aws.route53.cidr-collection.change", "aws.route53.cidr-collection.delete",
	"aws.route53.query-logging-config.create", "aws.route53.query-logging-config.delete",
	"aws.route53.account-limit.get", "aws.route53.checker-ip-range.list", "aws.route53.cidr-block.list",
	"aws.route53.cidr-collection.list", "aws.route53.cidr-location.list", "aws.route53.delegation-set.get",
	"aws.route53.delegation-set.get-limit", "aws.route53.delegation-set.list", "aws.route53.geo-location.get",
	"aws.route53.geo-location.list", "aws.route53.hosted-zone.get-limit", "aws.route53.hosted-zone.list-by-vpc",
	"aws.route53.query-logging-config.get", "aws.route53.query-logging-config.list", "aws.route53.tag.list-many",
	"aws.route53.traffic-policy.get", "aws.route53.traffic-policy.create", "aws.route53.traffic-policy.create-version",
	"aws.route53.traffic-policy.update-comment", "aws.route53.traffic-policy.delete", "aws.route53.traffic-policy-instance.count",
	"aws.route53.traffic-policy-instance.get", "aws.route53.traffic-policy-instance.list",
	"aws.route53.traffic-policy-instance.list-by-hosted-zone",
	"aws.route53.traffic-policy-instance.list-by-policy", "aws.route53.traffic-policy.list",
	"aws.route53.traffic-policy.list-versions", "aws.route53.vpc-association-authorization.list",
	"aws.route53.vpc-association-authorization.delete", "aws.route53.hosted-zone.associate-vpc", "aws.route53.hosted-zone.disassociate-vpc"}

// route53Env offers the Route 53 tools by name on a connection bound to the account and the given regions.
func route53Env(t *testing.T, f *fakeAWS, regions ...string) *env {
	t.Helper()
	targets := []string{"account/" + testAccount}
	for _, r := range regions {
		targets = append(targets, "region/"+r)
	}
	return newConnEnv(t, f, baseValues(), config.Connection{Tools: route53ToolIDs, Targets: targets})
}

func route53Error(status int, code string) reply {
	return reply{status: status, header: map[string]string{"Content-Type": "text/xml"},
		body: `<ErrorResponse ` + route53NS + `><Error><Type>Sender</Type><Code>` + code + `</Code><Message>m</Message></Error><RequestId>r</RequestId></ErrorResponse>`}
}

func zoneXML(id, name string) string {
	return `<HostedZone><Id>/hostedzone/` + id + `</Id><Name>` + name + `</Name><CallerReference>c</CallerReference>` +
		`<Config><Comment>c</Comment><PrivateZone>false</PrivateZone></Config><ResourceRecordSetCount>2</ResourceRecordSetCount></HostedZone>`
}

const changeXML = `<ChangeInfo><Id>/change/C1</Id><Status>PENDING</Status><SubmittedAt>2026-01-02T03:04:05.000Z</SubmittedAt></ChangeInfo>`

// A zone list over three pages continues at the marker, which the cursor hides.
func TestRoute53ListHostedZonesPagesByMarker(t *testing.T) {
	page := func(id, next string) reply {
		body := `<ListHostedZonesResponse ` + route53NS + `><HostedZones>` + zoneXML(id, id+".example.com.") + `</HostedZones><Marker></Marker>`
		if next != "" {
			body += `<IsTruncated>true</IsTruncated><NextMarker>` + next + `</NextMarker>`
		} else {
			body += `<IsTruncated>false</IsTruncated>`
		}
		return xmlReply(body + `<MaxItems>1</MaxItems></ListHostedZonesResponse>`)
	}
	f := direct(func(r sent, _ int) reply {
		switch mustQuery(t, r.rawQuery).Get("marker") {
		case "":
			return page("Z1", "Z2")
		case "Z2":
			return page("Z2", "Z3")
		}
		return page("Z3", "")
	})
	e := route53Env(t, f, testRegion)
	cursor, ids := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"page_size":1}`
		if cursor != "" {
			args = `{"page_size":1,"cursor":"` + cursor + `"}`
		}
		text, err := e.invoke("aws.route53.hosted-zone.list", args)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		got := decodeResult(t, text)
		zones, _ := got["hosted_zones"].([]any)
		zone, _ := zones[0].(map[string]any)
		ids = append(ids, zone["id"].(string))
		next, _ := got["next_cursor"].(string)
		if got["has_more"] != (i < 2) || (next != "") != (i < 2) || strings.Contains(next, "Z2") || strings.Contains(next, "Z3") {
			t.Errorf("page %d = %s", i, text)
		}
		cursor = next
	}
	if !slices.Equal(ids, []string{"/hostedzone/Z1", "/hostedzone/Z2", "/hostedzone/Z3"}) {
		t.Errorf("ids = %v", ids)
	}
	for i, r := range restRequests(f) {
		q := mustQuery(t, r.rawQuery)
		if r.method != "GET" || r.path != "/2013-04-01/hostedzone" || r.host != "route53.amazonaws.com" || q.Get("maxitems") != "1" ||
			q.Get("marker") != []string{"", "Z2", "Z3"}[i] {
			t.Errorf("request %d = %+v", i, r)
		}
	}
	if _, err := e.invoke("aws.route53.hosted-zone.list", `{"page_size":101}`); err == nil {
		t.Error("a page size above 100 was accepted")
	}
}

// A lookup by name starts at the name and the zone ID, with or without the prefix, and reports where to continue.
func TestRoute53ListByNameStartsAtNameAndID(t *testing.T) {
	f := direct(func(sent, int) reply {
		return xmlReply(`<ListHostedZonesByNameResponse ` + route53NS + `><HostedZones>` + zoneXML("Z1", "example.com.") + `</HostedZones>` +
			`<DNSName>example.com</DNSName><IsTruncated>true</IsTruncated><NextDNSName>example.org.</NextDNSName>` +
			`<NextHostedZoneId>Z9</NextHostedZoneId><MaxItems>1</MaxItems></ListHostedZonesByNameResponse>`)
	})
	e := route53Env(t, f, testRegion)
	text, err := e.invoke("aws.route53.hosted-zone.list-by-name", `{"dns_name":"example.com","hosted_zone_id":"/hostedzone/Z1","max_items":1}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	if got["next_dns_name"] != "example.org." || got["next_hosted_zone_id"] != "Z9" || got["is_truncated"] != true {
		t.Errorf("result = %s", text)
	}
	q := mustQuery(t, restRequests(f)[0].rawQuery)
	if q.Get("dnsname") != "example.com" || q.Get("hostedzoneid") != "Z1" || q.Get("maxitems") != "1" {
		t.Errorf("query = %v", q)
	}
}

// A zone is created with a generated token that the result names, and the name servers come back to enter at the
// registrar. Nothing is sent without the confirmation.
func TestRoute53CreateHostedZoneReturnsNameServersAndToken(t *testing.T) {
	f := direct(func(sent, int) reply {
		r := xmlReply(`<CreateHostedZoneResponse ` + route53NS + `>` + zoneXML("Z1", "example.com.") + changeXML +
			`<DelegationSet><NameServers><NameServer>ns-1.awsdns-01.org</NameServer><NameServer>ns-2.awsdns-02.com</NameServer></NameServers></DelegationSet>` +
			`</CreateHostedZoneResponse>`)
		r.status, r.header["Location"] = 201, "https://route53.amazonaws.com/2013-04-01/hostedzone/Z1"
		return r
	})
	e := route53Env(t, f, testRegion)
	args := `{"name":"example.com","hosted_zone_config":{"comment":"main"}}`
	if _, err := e.invoke("aws.route53.hosted-zone.create", args); !isConfirmationRequired(err) || len(f.requests) != 0 {
		t.Fatalf("err = %v, requests = %d, want a confirmation and no request", err, len(f.requests))
	}
	text, err := e.invokeConfirmed("aws.route53.hosted-zone.create", args, true)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeResult(t, text)
	token, _ := got["client_token"].(string)
	delegation, _ := got["delegation_set"].(map[string]any)
	servers, _ := delegation["name_servers"].([]any)
	if token == "" || len(servers) != 2 || servers[0] != "ns-1.awsdns-01.org" || got["location"] == nil {
		t.Fatalf("result = %s", text)
	}
	call := restRequests(f)[0]
	var body struct {
		Name            string `xml:"Name"`
		CallerReference string `xml:"CallerReference"`
	}
	if err := xml.Unmarshal([]byte(call.body), &body); err != nil || body.CallerReference != token || body.Name != "example.com" ||
		call.method != "POST" || call.path != "/2013-04-01/hostedzone" || call.host != "route53.amazonaws.com" {
		t.Errorf("request = %+v (%v), token %s", call, err, token)
	}
	// A given token is sent as it is, so a repetition after a timeout is the same request.
	if _, err := e.invokeConfirmed("aws.route53.hosted-zone.create", `{"name":"example.com","client_token":"my-token-1"}`, true); err != nil {
		t.Fatal(err)
	}
	if last := restRequests(f)[1]; !strings.Contains(last.body, "<CallerReference>my-token-1</CallerReference>") {
		t.Errorf("body = %s", last.body)
	}
}

// The errors of the zone tools keep their class, and a failed write is not repeated.
func TestRoute53ErrorsAreClassifiedAndWritesAreNotRepeated(t *testing.T) {
	for _, c := range []struct {
		id, args, code string
		status         int
		class          provider.Class
	}{
		{"aws.route53.hosted-zone.create", `{"name":"example.com"}`, "HostedZoneAlreadyExists", 409, provider.ClassProviderError},
		{"aws.route53.hosted-zone.create", `{"name":"bad..name"}`, "InvalidDomainName", 400, provider.ClassProviderError},
		{"aws.route53.hosted-zone.create", `{"name":"example.com"}`, "TooManyHostedZones", 400, provider.ClassProviderError},
		{"aws.route53.hosted-zone.delete", `{"id":"Z1"}`, "HostedZoneNotEmpty", 400, provider.ClassProviderError},
		{"aws.route53.hosted-zone.delete", `{"id":"Z1"}`, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{"aws.route53.hosted-zone.get", `{"id":"Z1"}`, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{"aws.route53.hosted-zone.update-comment", `{"id":"Z1","comment":"c"}`, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{"aws.route53.change.get", `{"id":"C1"}`, "NoSuchChange", 404, provider.ClassNotFound},
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

// A zone ID and a change ID are taken with and without their prefix, and anything else that carries a slash is refused.
func TestRoute53IDsAreTakenWithAndWithoutPrefix(t *testing.T) {
	f := direct(func(r sent, _ int) reply {
		switch {
		case strings.Contains(r.path, "/change/"):
			return xmlReply(`<GetChangeResponse ` + route53NS + `>` + changeXML + `</GetChangeResponse>`)
		case strings.Contains(r.path, "/tags/"):
			return xmlReply(`<ListTagsForResourceResponse ` + route53NS + `><ResourceTagSet><ResourceType>hostedzone</ResourceType>` +
				`<ResourceId>Z1</ResourceId><Tags><Tag><Key>team</Key><Value>a &amp; b</Value></Tag></Tags></ResourceTagSet></ListTagsForResourceResponse>`)
		}
		return xmlReply(`<GetHostedZoneResponse ` + route53NS + `>` + zoneXML("Z1", "example.com.") + `</GetHostedZoneResponse>`)
	})
	e := route53Env(t, f, testRegion)
	for _, c := range []struct{ id, args, path string }{
		{"aws.route53.hosted-zone.get", `{"id":"Z1"}`, "/2013-04-01/hostedzone/Z1"},
		{"aws.route53.hosted-zone.get", `{"id":"/hostedzone/Z1"}`, "/2013-04-01/hostedzone/Z1"},
		{"aws.route53.change.get", `{"id":"C1"}`, "/2013-04-01/change/C1"},
		{"aws.route53.change.get", `{"id":"/change/C1"}`, "/2013-04-01/change/C1"},
		{"aws.route53.tag.list", `{"resource_type":"hostedzone","resource_id":"Z1"}`, "/2013-04-01/tags/hostedzone/Z1"},
		{"aws.route53.tag.list", `{"resource_type":"hostedzone","resource_id":"/hostedzone/Z1"}`, "/2013-04-01/tags/hostedzone/Z1"},
	} {
		before := len(restRequests(f))
		if _, err := e.invoke(c.id, c.args); err != nil {
			t.Fatalf("%s %s: %v", c.id, c.args, err)
		}
		if got := restRequests(f)[before]; got.path != c.path {
			t.Errorf("%s %s sent %s, want %s", c.id, c.args, got.path, c.path)
		}
	}
	before := len(f.requests)
	for _, c := range []struct{ id, args string }{
		{"aws.route53.hosted-zone.get", `{"id":"/hostedzone/Z1/../Z2"}`},
		{"aws.route53.hosted-zone.get", `{"id":"/change/Z1"}`},
		{"aws.route53.change.get", `{"id":"/hostedzone/C1"}`},
		{"aws.route53.tag.list", `{"resource_type":"queue","resource_id":"abc"}`},
		{"aws.route53.tag.list", `{"resource_type":"hostedzone","resource_id":"Z1/x"}`},
	} {
		if _, err := e.invoke(c.id, c.args); err == nil {
			t.Errorf("%s %s was accepted", c.id, c.args)
		}
	}
	if len(f.requests) != before {
		t.Error("a refused call was sent")
	}
}

// A VPC of a private zone must name an allowed region and a well formed ID before anything is sent, and a private
// zone needs one while a public zone takes none.
func TestRoute53CreateChecksTheVPCBeforeAnyRequest(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	const private = `"hosted_zone_config":{"private_zone":true}`
	for _, args := range []string{
		`{"name":"a.example.com",` + private + `,"vpc":{"vpc_region":"ap-south-1","vpc_id":"vpc-0123456789abcdef0"}}`,
		`{"name":"a.example.com",` + private + `,"vpc":{"vpc_region":"` + testRegion + `","vpc_id":"subnet-1"}}`,
		`{"name":"a.example.com",` + private + `,"vpc":{"vpc_region":"` + testRegion + `"}}`,
		`{"name":"a.example.com",` + private + `}`,
		`{"name":"a.example.com","vpc":{"vpc_region":"` + testRegion + `","vpc_id":"vpc-0123456789abcdef0"}}`,
	} {
		if _, err := e.invokeConfirmed("aws.route53.hosted-zone.create", args, true); err == nil {
			t.Errorf("%s was accepted", args)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused create reached the network: %v", f.actions())
	}
	ok := route53Env(t, direct(func(sent, int) reply {
		return xmlReply(`<CreateHostedZoneResponse ` + route53NS + `>` + zoneXML("Z1", "a.example.com.") + changeXML +
			`<DelegationSet><NameServers><NameServer>ns-1</NameServer></NameServers></DelegationSet></CreateHostedZoneResponse>`)
	}), testRegion)
	args := `{"name":"a.example.com",` + private + `,"vpc":{"vpc_region":"` + testRegion + `","vpc_id":"vpc-0123456789abcdef0"}}`
	if _, err := ok.invokeConfirmed("aws.route53.hosted-zone.create", args, true); err != nil {
		t.Errorf("a private zone in an allowed region: %v", err)
	}
}

// A change reports PENDING and then INSYNC.
func TestRoute53ChangeStatus(t *testing.T) {
	for _, status := range []string{"PENDING", "INSYNC"} {
		f := direct(func(sent, int) reply {
			return xmlReply(`<GetChangeResponse ` + route53NS + `><ChangeInfo><Id>/change/C1</Id><Status>` + status +
				`</Status><SubmittedAt>2026-01-02T03:04:05Z</SubmittedAt></ChangeInfo></GetChangeResponse>`)
		})
		text, err := route53Env(t, f, testRegion).invoke("aws.route53.change.get", `{"id":"C1"}`)
		if err != nil {
			t.Fatal(err)
		}
		info, _ := decodeResult(t, text)["change_info"].(map[string]any)
		if info["status"] != status || info["submitted_at"] != "2026-01-02T03:04:05Z" {
			t.Errorf("result = %s", text)
		}
	}
}

// The writes need the confirmation before any request.
func TestRoute53WritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, testRegion)
	for _, c := range []struct{ id, args string }{
		{"aws.route53.hosted-zone.create", `{"name":"example.com"}`},
		{"aws.route53.hosted-zone.update-comment", `{"id":"Z1","comment":"c"}`},
		{"aws.route53.hosted-zone.delete", `{"id":"Z1"}`},
		{"aws.route53.tag.change", `{"resource_type":"hostedzone","resource_id":"Z1","add_tags":[{"key":"a","value":"b"}]}`},
		{"aws.route53.tag.change", `{"resource_type":"healthcheck","resource_id":"h1","add_tags":[{"key":"a","value":"b"}]}`},
	} {
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", c.id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
}

// The tag change sends the tags to add and the keys to remove for a hosted zone or a health check.
func TestRoute53TagChangeBody(t *testing.T) {
	f := restAnswer(xmlReply(`<ChangeTagsForResourceResponse ` + route53NS + `/>`))
	e := route53Env(t, f, testRegion)
	args := `{"resource_type":"hostedzone","resource_id":"/hostedzone/Z1","add_tags":[{"key":"team","value":"a<b"}],"remove_tag_keys":["old"]}`
	if _, err := e.invokeConfirmed("aws.route53.tag.change", args, true); err != nil {
		t.Fatal(err)
	}
	r := restRequests(f)[0]
	if r.method != "POST" || r.path != "/2013-04-01/tags/hostedzone/Z1" ||
		!strings.Contains(r.body, "<AddTags><Tag><Key>team</Key><Value>a&lt;b</Value></Tag></AddTags>") ||
		!strings.Contains(r.body, "<RemoveTagKeys><Key>old</Key></RemoveTagKeys>") {
		t.Errorf("request = %+v", r)
	}
	if _, err := e.invokeConfirmed("aws.route53.tag.change", `{"resource_type":"queue","resource_id":"x"}`, true); err == nil {
		t.Error("an unknown resource type was accepted")
	}
}

// Granting the authorization of a VPC of another account is not a tool; the authorizations are listed and withdrawn only.
func TestRoute53ExcludesTheVPCAuthorization(t *testing.T) {
	if !slices.Equal(excludedROUTE53Actions, []string{"CreateVPCAssociationAuthorization"}) {
		t.Errorf("excluded = %v", excludedROUTE53Actions)
	}
	reg := registered(t)
	tools := 0
	for _, d := range reg.Provider(Provider) {
		if d.Group != "route53" {
			continue
		}
		tools++
		if strings.Contains(d.ID, "authorization") && d.ID != "aws.route53.vpc-association-authorization.list" && d.ID != "aws.route53.vpc-association-authorization.delete" {
			t.Errorf("%s offers what is not offered: %s", d.ID, d.InputSchema)
		}
	}
	if tools != len(route53ToolIDs) {
		t.Errorf("route53 tools = %d, want %d", tools, len(route53ToolIDs))
	}
	for _, s := range generatedSpecs {
		if s.service == "route53" && s.rest.Method != "GET" && strings.HasSuffix(strings.ToLower(s.rest.URI), "/authorizevpcassociation") {
			t.Errorf("%s sends %s", s.descriptor.ID, s.rest.URI)
		}
	}
}

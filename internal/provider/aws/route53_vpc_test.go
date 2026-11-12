package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const vpcArg = `"vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}`

var vpcWrites = []struct{ id, args, method, path string }{
	{"aws.route53.hosted-zone.associate-vpc", `{"hosted_zone_id":"/hostedzone/Z1",` + vpcArg + `,"comment":"c"}`, "POST", "/2013-04-01/hostedzone/Z1/associatevpc"},
	{"aws.route53.hosted-zone.disassociate-vpc", `{"hosted_zone_id":"/hostedzone/Z1",` + vpcArg + `}`, "POST", "/2013-04-01/hostedzone/Z1/disassociatevpc"},
	{"aws.route53.vpc-association-authorization.delete", `{"hosted_zone_id":"/hostedzone/Z1",` + vpcArg + `}`, "POST", "/2013-04-01/hostedzone/Z1/deauthorizevpcassociation"},
}

// The writes send the VPC in the model's shape, drop the zone prefix, and return the change.
func TestRoute53VPCWritesSendTheirRequest(t *testing.T) {
	f := restAnswer(xmlReply(`<R ` + route53NS + `>` + changeXML + `</R>`))
	e := route53Env(t, f, "eu-central-1")
	for i, c := range vpcWrites {
		text, err := e.invokeConfirmed(c.id, c.args, true)
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		r := restRequests(f)[i]
		if r.method != c.method || r.path != c.path || !strings.Contains(r.body, "<VPCRegion>eu-central-1</VPCRegion>") ||
			!strings.Contains(r.body, "<VPCId>vpc-0123456789abcdef0</VPCId>") {
			t.Errorf("%s request = %+v", c.id, r)
		}
		if c.id == "aws.route53.vpc-association-authorization.delete" {
			continue
		}
		if info, _ := decodeResult(t, text)["change_info"].(map[string]any); info["id"] != "/change/C1" || info["status"] != "PENDING" {
			t.Errorf("%s result = %s", c.id, text)
		}
	}
	if body := restRequests(f)[0].body; !strings.Contains(body, "<Comment>c</Comment>") {
		t.Errorf("comment missing: %s", body)
	}
}

// A region outside the allowlist or a malformed VPC ID is refused before any request.
func TestRoute53VPCWritesCheckTheVPCBeforeAnyRequest(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, "eu-central-1")
	for _, c := range vpcWrites {
		for _, vpc := range []string{
			`{"vpc_region":"us-west-2","vpc_id":"vpc-0123456789abcdef0"}`, `{"vpc_region":"eu-central-1","vpc_id":"vpc-xyz"}`,
			`{"vpc_region":"eu-central-1","vpc_id":"subnet-0123456789abcdef0"}`, `{"vpc_region":"eu-central-1","vpc_id":""}`,
		} {
			args := `{"hosted_zone_id":"Z1","vpc":` + vpc + `}`
			if _, err := e.invokeConfirmed(c.id, args, true); err == nil {
				t.Errorf("%s accepted %s", c.id, vpc)
			}
		}
		if _, err := e.invokeConfirmed(c.id, `{"hosted_zone_id":"Z1"}`, true); err == nil {
			t.Errorf("%s accepted no vpc", c.id)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call reached the network: %v", f.actions())
	}
}

// Every write needs confirmation before it reaches the network.
func TestRoute53VPCWritesNeedConfirmation(t *testing.T) {
	f := restAnswer(reply{status: 500})
	e := route53Env(t, f, "eu-central-1")
	for _, c := range vpcWrites {
		if _, err := e.invoke(c.id, c.args); !isConfirmationRequired(err) {
			t.Errorf("%s: err = %v, want a confirmation", c.id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("an unconfirmed write reached the network: %v", f.actions())
	}
}

// Errors keep their class and code, and a failed write is not repeated.
func TestRoute53VPCErrors(t *testing.T) {
	assoc, disassoc, del := vpcWrites[0], vpcWrites[1], vpcWrites[2]
	for _, c := range []struct {
		id, args, code string
		status         int
		class          provider.Class
	}{
		{assoc.id, assoc.args, "ConflictingDomainExists", 400, provider.ClassProviderError},
		{assoc.id, assoc.args, "NotAuthorizedException", 401, provider.ClassProviderError},
		{assoc.id, assoc.args, "PublicZoneVPCAssociation", 400, provider.ClassProviderError},
		{assoc.id, assoc.args, "NoSuchHostedZone", 404, provider.ClassNotFound},
		{disassoc.id, disassoc.args, "LastVPCAssociation", 400, provider.ClassProviderError},
		{disassoc.id, disassoc.args, "VPCAssociationNotFound", 404, provider.ClassNotFound},
		{del.id, del.args, "VPCAssociationAuthorizationNotFound", 404, provider.ClassNotFound},
	} {
		t.Run(c.id+"/"+c.code, func(t *testing.T) {
			f := restAnswer(route53Error(c.status, c.code))
			e := route53Env(t, f, "eu-central-1")
			_, err := e.invokeConfirmed(c.id, c.args, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) {
				t.Fatalf("err = %v", err)
			}
			if failed.Original == nil || len(failed.Original.Entries) != 1 || failed.Original.Entries[0].Code != c.code {
				t.Errorf("original = %+v", failed.Original)
			}
			if c.class != provider.ClassProviderError && failed.Class != c.class {
				t.Errorf("class = %s, want %s", failed.Class, c.class)
			}
			if n := len(restRequests(f)); n != 1 {
				t.Errorf("requests = %d, a failed call was repeated", n)
			}
		})
	}
}

// The tool texts tell what the confirmation does not show and that the account of the VPC cannot be checked.
func TestRoute53VPCToolsNameTheUncheckedAccount(t *testing.T) {
	seen := 0
	for _, d := range registered(t).Provider(Provider) {
		if d.ID != vpcWrites[0].id && d.ID != vpcWrites[1].id && d.ID != vpcWrites[2].id {
			continue
		}
		seen++
		if d.Risk.Confirmation == capability.ConfirmationNone {
			t.Errorf("%s needs a confirmation", d.ID)
		}
		for _, part := range []string{"names the tool, not the arguments", "hosted_zone_id", "vpc.vpc_id", "vpc.vpc_region", "account of the VPC cannot be checked"} {
			if !strings.Contains(d.Description, part) {
				t.Errorf("%s description lacks %q", d.ID, part)
			}
		}
		if d.ID == vpcWrites[0].id && !strings.Contains(d.Description, "Remaining risk") {
			t.Errorf("%s names no remaining risk", d.ID)
		}
	}
	if seen != 3 {
		t.Errorf("tools seen = %d", seen)
	}
}

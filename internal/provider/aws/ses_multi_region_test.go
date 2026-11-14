package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	secondRegion  = "eu-west-1"
	createRoute   = `{"region":"` + testRegion + `","endpoint_name":"mrep","details":{"routes_details":[{"region":"` + secondRegion + `"}]}}`
	endpointsPath = "/v2/email/multi-region-endpoints"
	inPrimary     = `"region":"` + testRegion + `",`
	named         = `{` + inPrimary + `"endpoint_name":"mrep"}`
)

// multiRegionEnv allows two regions, so a route to the second one passes and a third one does not.
func multiRegionEnv(t *testing.T, f *fakeAWS) *env {
	return sesEnv(t, f, "account/"+testAccount, "region/"+testRegion, "region/"+secondRegion)
}

var sesMultiRegionTools = map[string]struct {
	effect capability.Effect
	idem   capability.Idempotency
}{
	"aws.ses.multi-region-endpoint.list":   {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.multi-region-endpoint.get":    {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.multi-region-endpoint.create": {capability.EffectCreate, capability.IdempotencyNonIdempotent},
	"aws.ses.multi-region-endpoint.delete": {capability.EffectDelete, capability.IdempotencyNonIdempotent},
}

// The four tools exist with the risk of the table; writes need confirmation, reads do not, and the write
// descriptions name what the confirmation covers. Creating names the cost without a price.
func TestSESMultiRegionToolsRisk(t *testing.T) {
	seen := 0
	for _, s := range generatedSpecs {
		w, ok := sesMultiRegionTools[s.descriptor.ID]
		if !ok {
			continue
		}
		seen++
		d := s.descriptor
		confirmation := capability.ConfirmationRequired
		if w.effect == capability.EffectRead {
			confirmation = capability.ConfirmationNone
		}
		if d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idem || d.Risk.Confirmation != confirmation ||
			!d.Risk.OpenWorld || d.RequiresToolAllowList || d.Group != "ses" {
			t.Errorf("%s descriptor = %+v", d.ID, d)
		}
		if w.effect != capability.EffectRead && !strings.Contains(d.Description, "endpoint_name") {
			t.Errorf("%s description does not name the endpoint: %q", d.ID, d.Description)
		}
		if d.ID == "aws.ses.multi-region-endpoint.create" && (!strings.Contains(d.Description, "details.routes_details") ||
			!strings.Contains(d.Description, "pricing page") || strings.ContainsAny(d.Description, "$€")) {
			t.Errorf("create description = %q", d.Description)
		}
	}
	if seen != len(sesMultiRegionTools) {
		t.Fatalf("%d tools, want %d", seen, len(sesMultiRegionTools))
	}
}

// Create sends the name, the route, and the tags to the primary region; get and delete address the endpoint by name.
func TestSESMultiRegionTools(t *testing.T) {
	f := sesFake(func(r sent) reply {
		switch {
		case r.method == "POST":
			return ok(`{"Status":"CREATING","EndpointId":"abc.123"}`)
		case r.method == "DELETE":
			return ok(`{"Status":"DELETING"}`)
		}
		return ok(`{"EndpointName":"mrep","EndpointId":"abc.123","Routes":[{"Region":"` + testRegion + `"},{"Region":"` + secondRegion + `"}],"Status":"READY"}`)
	})
	e := multiRegionEnv(t, f)
	text, err := e.invokeConfirmed("aws.ses.multi-region-endpoint.create", `{`+inPrimary+`"endpoint_name":"mrep","details":{"routes_details":[{"region":"`+secondRegion+`"}]},"tags":[{"key":"a","value":"b"}]}`, true)
	if err != nil || !strings.Contains(text, "abc.123") {
		t.Fatalf("create = %s, %v", text, err)
	}
	if text, err = e.invoke("aws.ses.multi-region-endpoint.get", named); err != nil || !strings.Contains(text, "READY") {
		t.Fatalf("get = %s, %v", text, err)
	}
	if text, err = e.invokeConfirmed("aws.ses.multi-region-endpoint.delete", named, true); err != nil || !strings.Contains(text, "DELETING") {
		t.Fatalf("delete = %s, %v", text, err)
	}
	calls := restCalls(f)
	if len(calls) != 3 ||
		calls[0].method != "POST" || calls[0].path != endpointsPath ||
		!jsonEqual(calls[0].body, `{"EndpointName":"mrep","Details":{"RoutesDetails":[{"Region":"`+secondRegion+`"}]},"Tags":[{"Key":"a","Value":"b"}]}`) ||
		calls[1].method != "GET" || calls[1].path != endpointsPath+"/mrep" ||
		calls[2].method != "DELETE" || calls[2].path != endpointsPath+"/mrep" {
		t.Fatalf("calls = %+v", calls)
	}
}

// The list pages by cursor over three pages and never shows the provider token.
func TestSESMultiRegionListsOverThreePages(t *testing.T) {
	item := `{"EndpointName":"m","Status":"READY","Regions":["` + testRegion + `"]}`
	f := sesFake(func(r sent) reply {
		seen := r.body + r.rawQuery
		switch {
		case strings.Contains(seen, "tok-2"):
			return ok(`{"MultiRegionEndpoints":[` + item + `]}`)
		case strings.Contains(seen, "tok-1"):
			return ok(`{"MultiRegionEndpoints":[` + item + `],"NextToken":"tok-2"}`)
		}
		return ok(`{"MultiRegionEndpoints":[` + item + `],"NextToken":"tok-1"}`)
	})
	e := multiRegionEnv(t, f)
	cursor := ""
	for i := 0; i < 3; i++ {
		args := `{` + inPrimary + `"page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		text, err := e.invoke("aws.ses.multi-region-endpoint.list", args+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		next, _ := decodeResult(t, text)["next_cursor"].(string)
		if (next != "") != (i < 2) || strings.Contains(text, "tok-") {
			t.Fatalf("page %d result = %s", i, text)
		}
		cursor = next
	}
	if n := len(restCalls(f)); n != 3 {
		t.Errorf("%d calls, want 3", n)
	}
}

// The errors of the model map to their classes, and a create or delete is sent once.
func TestSESMultiRegionErrors(t *testing.T) {
	for _, c := range []struct {
		tool, args, code string
		status           int
		class            provider.Class
		confirmed        bool
	}{
		{"get", named, "NotFoundException", 404, provider.ClassNotFound, false},
		{"get", named, "BadRequestException", 400, provider.ClassProviderError, false},
		{"get", named, "TooManyRequestsException", 429, provider.ClassRateLimited, false},
		{"list", `{"region":"` + testRegion + `"}`, "BadRequestException", 400, provider.ClassProviderError, false},
		{"list", `{"region":"` + testRegion + `"}`, "TooManyRequestsException", 429, provider.ClassRateLimited, false},
		{"create", createRoute, "AlreadyExistsException", 400, provider.ClassProviderError, true},
		{"create", createRoute, "LimitExceededException", 400, provider.ClassProviderError, true},
		{"create", createRoute, "BadRequestException", 400, provider.ClassProviderError, true},
		{"create", createRoute, "TooManyRequestsException", 429, provider.ClassRateLimited, true},
		{"delete", named, "NotFoundException", 404, provider.ClassNotFound, true},
		{"delete", named, "ConcurrentModificationException", 409, provider.ClassProviderError, true},
		{"delete", named, "BadRequestException", 400, provider.ClassProviderError, true},
		{"delete", named, "TooManyRequestsException", 429, provider.ClassRateLimited, true},
	} {
		t.Run(c.tool+"/"+c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := multiRegionEnv(t, f)
			_, err := e.invokeConfirmed("aws.ses.multi-region-endpoint."+c.tool, c.args, c.confirmed)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil ||
				failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
			if c.confirmed && len(restCalls(f)) != 1 {
				t.Errorf("sent %d times", len(restCalls(f)))
			}
		})
	}
}

// Every region of a create must be allowed, the one of the call and each one of the routes, else nothing is
// sent; a missing confirmation and a bad name are refused too.
func TestSESMultiRegionRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := multiRegionEnv(t, f)
	route := func(regions ...string) string {
		var parts []string
		for _, r := range regions {
			parts = append(parts, `{"region":"`+r+`"}`)
		}
		return `{` + inPrimary + `"endpoint_name":"mrep","details":{"routes_details":[` + strings.Join(parts, ",") + `]}}`
	}
	for _, c := range []struct{ tool, args string }{
		{"create", route("us-east-1")},
		{"create", route(secondRegion, "us-east-1")},
		{"create", route(secondRegion, testRegion)},
		{"create", `{"region":"us-east-1","endpoint_name":"mrep","details":{"routes_details":[{"region":"` + secondRegion + `"}]}}`},
		{"create", `{` + inPrimary + `"endpoint_name":"mrep","details":{"routes_details":[]}}`},
		{"create", `{` + inPrimary + `"endpoint_name":"mrep","details":{}}`},
		{"create", `{` + inPrimary + `"endpoint_name":"bad name","details":{"routes_details":[{"region":"` + secondRegion + `"}]}}`},
		{"get", `{"endpoint_name":"mrep","region":"us-east-1"}`},
		{"list", `{"region":"ap-south-1"}`},
		{"delete", `{"endpoint_name":"mrep","region":"us-east-1"}`},
		{"delete", `{` + inPrimary + `"endpoint_name":"a/b"}`},
	} {
		if _, err := e.invokeConfirmed("aws.ses.multi-region-endpoint."+c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	for _, c := range [][2]string{
		{"create", createRoute},
		{"delete", named},
	} {
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke("aws.ses.multi-region-endpoint."+c[0], c[1]); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", c[0], err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

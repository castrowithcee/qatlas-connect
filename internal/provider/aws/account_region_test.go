package aws

import (
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const otherAccount = "210987654321"

var accountRegionTools = map[string]capability.Effect{
	"aws.account.region.list":           capability.EffectRead,
	"aws.account.region.get-opt-status": capability.EffectRead,
	"aws.account.region.enable":         capability.EffectUpdate,
	"aws.account.region.disable":        capability.EffectUpdate,
}

func accountEnv(t *testing.T, f *fakeAWS) *env {
	return sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
}

// The four tools exist with the risk of the contract: reads are safe, enable and disable are idempotent updates
// with confirmation, and the account is the group.
func TestAccountRegionToolsRisk(t *testing.T) {
	seen := map[string]capability.Descriptor{}
	for _, s := range generatedSpecs {
		if strings.HasPrefix(s.descriptor.ID, "aws.account.region.") {
			seen[s.descriptor.ID] = s.descriptor
		}
	}
	if len(seen) != len(accountRegionTools) {
		t.Fatalf("%d account tools, want %d", len(seen), len(accountRegionTools))
	}
	for id, effect := range accountRegionTools {
		d := seen[id]
		idem, confirmation := capability.IdempotencyIdempotent, capability.ConfirmationRequired
		if effect == capability.EffectRead {
			idem, confirmation = capability.IdempotencySafe, capability.ConfirmationNone
		}
		if d.Risk.Effect != effect || d.Risk.Idempotency != idem || d.Risk.Confirmation != confirmation || !d.Risk.OpenWorld ||
			d.Risk.DataSensitivity != "aws-account-configuration" || d.RequiresToolAllowList || d.Group != "account" {
			t.Errorf("%s descriptor = %+v", id, d)
		}
	}
	for _, part := range []string{"Access to the region and its resources is lost", "region list of this connection", "ENABLING"} {
		if !strings.Contains(seen["aws.account.region.disable"].Description+seen["aws.account.region.enable"].Description, part) {
			t.Errorf("descriptions lack %q", part)
		}
	}
}

// List with a filter pages over three pages through the cursor, which carries the page size.
func TestAccountRegionListPages(t *testing.T) {
	f := sesFake(func(r sent) reply {
		switch {
		case !strings.Contains(r.body, "NextToken"):
			return ok(`{"Regions":[{"RegionName":"af-south-1","RegionOptStatus":"ENABLED"}],"NextToken":"tok-1"}`)
		case strings.Contains(r.body, "tok-1"):
			return ok(`{"Regions":[{"RegionName":"ap-east-1","RegionOptStatus":"ENABLING"}],"NextToken":"tok-2"}`)
		}
		return ok(`{"Regions":[{"RegionName":"me-south-1","RegionOptStatus":"ENABLED"}]}`)
	})
	e := accountEnv(t, f)
	var names []string
	cursor := ""
	for i := 0; i < 3; i++ {
		args := `{"region_opt_status_contains":["ENABLED","ENABLING"],"page_size":1`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		text, err := e.invoke("aws.account.region.list", args+`}`)
		if err != nil {
			t.Fatalf("page %d: %v", i, err)
		}
		res := decodeResult(t, text)
		items, _ := res["regions"].([]any)
		names = append(names, items[0].(map[string]any)["region_name"].(string))
		next, _ := res["next_cursor"].(string)
		if (next != "") != (i < 2) || strings.Contains(text, "tok-") {
			t.Fatalf("page %d result = %s", i, text)
		}
		cursor = next
	}
	if !slices.Equal(names, []string{"af-south-1", "ap-east-1", "me-south-1"}) {
		t.Errorf("names = %v", names)
	}
	calls := restCalls(f)
	if len(calls) != 3 || calls[0].method != "POST" || calls[0].path != "/listRegions" || calls[0].host != "account.us-east-1.amazonaws.com" ||
		!jsonEqual(calls[0].body, `{"RegionOptStatusContains":["ENABLED","ENABLING"],"MaxResults":1}`) ||
		!jsonEqual(calls[1].body, `{"RegionOptStatusContains":["ENABLED","ENABLING"],"MaxResults":1,"NextToken":"tok-1"}`) {
		t.Fatalf("calls = %+v", calls)
	}
	for _, bad := range []string{`{"page_size":51}`, `{"region_opt_status_contains":["OPEN"]}`, `{"region_opt_status_contains":["ENABLED","ENABLING","DISABLING","DISABLED","ENABLED_BY_DEFAULT","ENABLED"]}`} {
		if _, err := e.invoke("aws.account.region.list", bad); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

// Get-opt-status returns every status of the model and sends the account and the region.
func TestAccountRegionStatuses(t *testing.T) {
	for _, status := range []string{"ENABLED", "ENABLING", "DISABLING", "DISABLED", "ENABLED_BY_DEFAULT"} {
		f := sesFake(func(sent) reply { return ok(`{"RegionName":"af-south-1","RegionOptStatus":"` + status + `"}`) })
		e := accountEnv(t, f)
		text, err := e.invoke("aws.account.region.get-opt-status", `{"region_name":"af-south-1","account_id":"`+testAccount+`"}`)
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		got := decodeResult(t, text)
		calls := restCalls(f)
		if got["region_opt_status"] != status || got["region_name"] != "af-south-1" || len(calls) != 1 ||
			calls[0].path != "/getRegionOptStatus" || !jsonEqual(calls[0].body, `{"AccountId":"`+testAccount+`","RegionName":"af-south-1"}`) {
			t.Errorf("%s: result %v calls %+v", status, got, calls)
		}
	}
}

// Enable and disable need confirmation, send one request, and name what happens to the region list: it is never
// changed, and a disabled region says whether it stays listed.
func TestAccountRegionEnableDisable(t *testing.T) {
	for _, c := range []struct{ tool, path, region, want string }{
		{"aws.account.region.enable", "/enableRegion", "af-south-1", "only when region/af-south-1 is a target"},
		{"aws.account.region.disable", "/disableRegion", testRegion, "still in the region list"},
		{"aws.account.region.disable", "/disableRegion", "af-south-1", "is not in the region list"},
	} {
		f := sesFake(func(sent) reply { return ok(`{}`) })
		e := accountEnv(t, f)
		args := `{"region_name":"` + c.region + `"}`
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(c.tool, args); !errorsAs(err, &unconfirmed) || len(f.requests) != 0 {
			t.Fatalf("%s without confirmation: %v, %v", c.tool, err, f.actions())
		}
		text, err := e.invokeConfirmed(c.tool, args, true)
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		note, _ := decodeResult(t, text)["allowlist_note"].(string)
		calls := restCalls(f)
		if !strings.Contains(note, "unchanged") || !strings.Contains(note, c.want) || len(calls) != 1 || calls[0].path != c.path ||
			!jsonEqual(calls[0].body, `{"RegionName":"`+c.region+`"}`) {
			t.Errorf("%s %s: note %q calls %+v", c.tool, c.region, note, calls)
		}
	}
}

// Another account, a malformed account, and a region outside the aws partition or form are refused before any I/O.
func TestAccountRegionRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := accountEnv(t, f)
	for _, c := range []struct{ tool, args string }{
		{"aws.account.region.list", `{"account_id":"` + otherAccount + `"}`},
		{"aws.account.region.get-opt-status", `{"region_name":"af-south-1","account_id":"` + otherAccount + `"}`},
		{"aws.account.region.enable", `{"region_name":"af-south-1","account_id":"` + otherAccount + `"}`},
		{"aws.account.region.disable", `{"region_name":"af-south-1","account_id":"` + otherAccount + `"}`},
		{"aws.account.region.enable", `{"region_name":"af-south-1","account_id":"1"}`},
		{"aws.account.region.enable", `{}`},
		{"aws.account.region.enable", `{"region_name":"cn-north-1"}`},
		{"aws.account.region.enable", `{"region_name":"us-gov-west-1"}`},
		{"aws.account.region.disable", `{"region_name":"Not A Region"}`},
		{"aws.account.region.get-opt-status", `{"region_name":"../x"}`},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// The errors of the operations map to the classes of awserr and keep the code in Original, without the text of
// the provider.
func TestAccountRegionErrors(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
		class  provider.Class
	}{
		{"ConflictException", 409, provider.ClassProviderError},
		{"ValidationException", 400, provider.ClassProviderError},
		{"AccessDeniedException", 403, provider.ClassPermission},
		{"TooManyRequestsException", 429, provider.ClassRateLimited},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := accountEnv(t, f)
			_, err := e.invokeConfirmed("aws.account.region.enable", `{"region_name":"af-south-1"}`, true)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil || len(failed.Original.Entries) == 0 ||
				failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("error = %v (%+v)", err, failed)
			}
		})
	}
}

// A region name of the answer that is not the form of a region is not trusted: a long text is cut or refused,
// never passed on unbounded.
func TestAccountRegionAnswerIsBounded(t *testing.T) {
	long := strings.Repeat("x", 5000)
	f := sesFake(func(sent) reply {
		return ok(`{"Regions":[{"RegionName":"` + long + `","RegionOptStatus":"ENABLED"}]}`)
	})
	text, err := accountEnv(t, f).invoke("aws.account.region.list", `{}`)
	if err == nil && strings.Contains(text, long) {
		t.Errorf("an unbounded region name was passed on")
	}
}

package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

var sesDedicatedTools = map[string]struct {
	effect capability.Effect
	idem   capability.Idempotency
}{
	"aws.ses.dedicated-ip.list":                        {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.dedicated-ip.get":                         {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.dedicated-ip-pool.list":                   {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.dedicated-ip-pool.get":                    {capability.EffectRead, capability.IdempotencySafe},
	"aws.ses.dedicated-ip-pool.create":                 {capability.EffectCreate, capability.IdempotencyNonIdempotent},
	"aws.ses.dedicated-ip-pool.delete":                 {capability.EffectDelete, capability.IdempotencyNonIdempotent},
	"aws.ses.dedicated-ip.put-in-pool":                 {capability.EffectUpdate, capability.IdempotencyIdempotent},
	"aws.ses.dedicated-ip.put-warmup-attributes":       {capability.EffectUpdate, capability.IdempotencyIdempotent},
	"aws.ses.dedicated-ip-pool.put-scaling-attributes": {capability.EffectUpdate, capability.IdempotencyIdempotent},
}

func sesDedicatedDescriptors() map[string]capability.Descriptor {
	out := map[string]capability.Descriptor{}
	for _, s := range generatedSpecs {
		if _, ok := sesDedicatedTools[s.descriptor.ID]; ok {
			out[s.descriptor.ID] = s.descriptor
		}
	}
	return out
}

func sesDedicatedEnv(t *testing.T, f *fakeAWS) *env {
	return sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
}

// The nine tools exist with the effect and idempotency of the table; every write needs confirmation, reads do
// not, and none is listed-only.
func TestSESDedicatedToolsRisk(t *testing.T) {
	seen := sesDedicatedDescriptors()
	if len(seen) != 9 || len(sesDedicatedTools) != 9 {
		t.Fatalf("%d tools, want 9", len(seen))
	}
	for id, w := range sesDedicatedTools {
		d := seen[id]
		confirmation := capability.ConfirmationRequired
		if w.effect == capability.EffectRead {
			confirmation = capability.ConfirmationNone
		}
		if d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idem || d.Risk.Confirmation != confirmation ||
			!d.Risk.OpenWorld || d.Risk.DataSensitivity != "aws-ses-dedicated-ips" || d.RequiresToolAllowList || d.Group != "ses" {
			t.Errorf("%s descriptor = %+v", id, d)
		}
	}
}

// The text a person confirms names what is sent and, where AWS charges, the cost without a price.
func TestSESDedicatedDescriptions(t *testing.T) {
	seen := sesDedicatedDescriptors()
	has := func(id string, parts ...string) {
		t.Helper()
		for _, p := range parts {
			if !strings.Contains(seen[id].Description, p) {
				t.Errorf("%s description lacks %q: %s", id, p, seen[id].Description)
			}
		}
	}
	has("aws.ses.dedicated-ip-pool.create", "charged by AWS", "SES pricing page", "not retrieved", "region, the pool name",
		"Not repeated after an unclear outcome", "Needs confirmation")
	has("aws.ses.dedicated-ip-pool.put-scaling-attributes", "charged by AWS", "SES pricing page", "not retrieved", "region, the pool, and the mode", "Needs confirmation")
	has("aws.ses.dedicated-ip.put-in-pool", "region, the address, and the destination pool", "reputation", "Needs confirmation")
	has("aws.ses.dedicated-ip.put-warmup-attributes", "region, the address, and the percentage", "reputation", "Needs confirmation")
	has("aws.ses.dedicated-ip-pool.delete", "Needs confirmation")
	for id, d := range seen {
		if strings.Contains(d.Description, "USD") {
			t.Errorf("%s names a price: %s", id, d.Description)
		}
	}
}

// The reads send one GET each to the regional endpoint and return the members of the answer.
func TestSESDedicatedReads(t *testing.T) {
	for _, c := range []struct{ tool, args, path, query, answer, want string }{
		{"aws.ses.dedicated-ip.get", `{"ip":"192.0.2.1"}`, "/v2/email/dedicated-ips/192.0.2.1", "",
			`{"DedicatedIp":{"Ip":"192.0.2.1","WarmupStatus":"IN_PROGRESS","WarmupPercentage":25,"PoolName":"marketing"}}`, `"warmup_status":"IN_PROGRESS"`},
		{"aws.ses.dedicated-ip-pool.get", `{"pool_name":"marketing"}`, "/v2/email/dedicated-ip-pools/marketing", "",
			`{"DedicatedIpPool":{"PoolName":"marketing","ScalingMode":"MANAGED"}}`, `"scaling_mode":"MANAGED"`},
		{"aws.ses.dedicated-ip.list", `{"pool_name":"marketing"}`, "/v2/email/dedicated-ips", "PoolName=marketing",
			`{"DedicatedIps":[{"Ip":"192.0.2.1","WarmupStatus":"DONE","WarmupPercentage":100,"PoolName":"marketing"}]}`, `"warmup_percentage":100`},
		{"aws.ses.dedicated-ip-pool.list", `{}`, "/v2/email/dedicated-ip-pools", "",
			`{"DedicatedIpPools":["marketing","transactional"]}`, `"transactional"`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := sesFake(func(sent) reply { return ok(c.answer) })
			text, err := sesDedicatedEnv(t, f).invoke(c.tool, c.args)
			if err != nil || !strings.Contains(text, c.want) {
				t.Fatalf("result = %s, %v", text, err)
			}
			calls := restCalls(f)
			if len(calls) != 1 || calls[0].method != "GET" || calls[0].path != c.path || calls[0].rawQuery != c.query ||
				calls[0].host != "email."+testRegion+".amazonaws.com" {
				t.Errorf("calls = %+v", calls)
			}
		})
	}
}

// Both list tools page over three pages by NextToken and hide the token behind the cursor.
func TestSESDedicatedListsOverThreePages(t *testing.T) {
	for _, c := range []struct{ tool, key, item string }{
		{"aws.ses.dedicated-ip.list", "DedicatedIps", `{"Ip":"192.0.2.1","WarmupStatus":"DONE","WarmupPercentage":100}`},
		{"aws.ses.dedicated-ip-pool.list", "DedicatedIpPools", `"pool"`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := sesFake(func(r sent) reply {
				switch {
				case strings.Contains(r.rawQuery, "tok-2"):
					return ok(`{"` + c.key + `":[` + c.item + `]}`)
				case strings.Contains(r.rawQuery, "tok-1"):
					return ok(`{"` + c.key + `":[` + c.item + `],"NextToken":"tok-2"}`)
				}
				return ok(`{"` + c.key + `":[` + c.item + `],"NextToken":"tok-1"}`)
			})
			e := sesDedicatedEnv(t, f)
			cursor := ""
			for i := 0; i < 3; i++ {
				args := `{"page_size":1`
				if cursor != "" {
					args += `,"cursor":"` + cursor + `"`
				}
				text, err := e.invoke(c.tool, args+`}`)
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
		})
	}
}

// Every write sends the request of the operation and nothing else.
func TestSESDedicatedWrites(t *testing.T) {
	for _, c := range []struct{ tool, args, method, path, body string }{
		{"aws.ses.dedicated-ip-pool.create", `{"pool_name":"marketing","scaling_mode":"MANAGED","tags":[{"key":"team","value":"mail"}]}`,
			"POST", "/v2/email/dedicated-ip-pools", `{"PoolName":"marketing","ScalingMode":"MANAGED","Tags":[{"Key":"team","Value":"mail"}]}`},
		{"aws.ses.dedicated-ip-pool.delete", `{"pool_name":"marketing"}`, "DELETE", "/v2/email/dedicated-ip-pools/marketing", ""},
		{"aws.ses.dedicated-ip.put-in-pool", `{"ip":"192.0.2.1","destination_pool_name":"transactional"}`,
			"PUT", "/v2/email/dedicated-ips/192.0.2.1/pool", `{"DestinationPoolName":"transactional"}`},
		{"aws.ses.dedicated-ip.put-warmup-attributes", `{"ip":"192.0.2.1","warmup_percentage":50}`,
			"PUT", "/v2/email/dedicated-ips/192.0.2.1/warmup", `{"WarmupPercentage":50}`},
		{"aws.ses.dedicated-ip-pool.put-scaling-attributes", `{"pool_name":"marketing","scaling_mode":"STANDARD"}`,
			"PUT", "/v2/email/dedicated-ip-pools/marketing/scaling", `{"ScalingMode":"STANDARD"}`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := sesFake(func(sent) reply { return ok(`{}`) })
			if _, err := sesDedicatedEnv(t, f).invokeConfirmed(c.tool, c.args, true); err != nil {
				t.Fatal(err)
			}
			calls := restCalls(f)
			if len(calls) != 1 || calls[0].method != c.method || calls[0].path != c.path || !jsonEqual(calls[0].body, c.body) ||
				calls[0].host != "email."+testRegion+".amazonaws.com" || f.count("GetCallerIdentity") != 1 {
				t.Errorf("calls = %+v, want %s %s %s", calls, c.method, c.path, c.body)
			}
		})
	}
}

// Every write is refused without confirmation and sends no request.
func TestSESDedicatedWritesNeedConfirmation(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesDedicatedEnv(t, f)
	for _, c := range [][2]string{
		{"aws.ses.dedicated-ip-pool.create", `{"pool_name":"marketing"}`},
		{"aws.ses.dedicated-ip-pool.delete", `{"pool_name":"marketing"}`},
		{"aws.ses.dedicated-ip.put-in-pool", `{"ip":"192.0.2.1","destination_pool_name":"marketing"}`},
		{"aws.ses.dedicated-ip.put-warmup-attributes", `{"ip":"192.0.2.1","warmup_percentage":50}`},
		{"aws.ses.dedicated-ip-pool.put-scaling-attributes", `{"pool_name":"marketing","scaling_mode":"MANAGED"}`},
	} {
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(c[0], c[1]); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", c[0], err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// A region outside the allowlist, a missing or oversized member, a value outside the enum, and a percentage
// above 100 are refused before any I/O.
func TestSESDedicatedRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesDedicatedEnv(t, f)
	long := strings.Repeat("a", 65)
	for _, c := range []struct{ tool, args string }{
		{"aws.ses.dedicated-ip.list", `{"region":"us-east-1"}`},
		{"aws.ses.dedicated-ip-pool.list", `{"region":"us-east-1"}`},
		{"aws.ses.dedicated-ip-pool.create", `{"region":"us-east-1","pool_name":"marketing"}`},
		{"aws.ses.dedicated-ip-pool.create", `{}`},
		{"aws.ses.dedicated-ip-pool.create", `{"pool_name":"` + long + `"}`},
		{"aws.ses.dedicated-ip-pool.create", `{"pool_name":"marketing","scaling_mode":"AUTO"}`},
		{"aws.ses.dedicated-ip-pool.delete", `{"pool_name":"` + long + `"}`},
		{"aws.ses.dedicated-ip.put-in-pool", `{"ip":"192.0.2.1"}`},
		{"aws.ses.dedicated-ip.put-in-pool", `{"ip":"` + strings.Repeat("1", 46) + `","destination_pool_name":"marketing"}`},
		{"aws.ses.dedicated-ip.put-warmup-attributes", `{"ip":"192.0.2.1","warmup_percentage":101}`},
		{"aws.ses.dedicated-ip.put-warmup-attributes", `{"warmup_percentage":10}`},
		{"aws.ses.dedicated-ip-pool.put-scaling-attributes", `{"pool_name":"marketing"}`},
		{"aws.ses.dedicated-ip-pool.put-scaling-attributes", `{"pool_name":"marketing","scaling_mode":"AUTO"}`},
	} {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

// The errors of the dedicated IP operations map to the classes of awserr and keep the code in Original,
// without the text of the provider.
func TestSESDedicatedErrors(t *testing.T) {
	for _, c := range []struct {
		code   string
		status int
		class  provider.Class
	}{
		{"NotFoundException", 404, provider.ClassNotFound},
		{"AlreadyExistsException", 400, provider.ClassProviderError},
		{"LimitExceededException", 400, provider.ClassProviderError},
		{"BadRequestException", 400, provider.ClassProviderError},
		{"ConcurrentModificationException", 409, provider.ClassProviderError},
		{"TooManyRequestsException", 429, provider.ClassRateLimited},
	} {
		t.Run(c.code, func(t *testing.T) {
			f := sesFake(func(sent) reply {
				return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
			})
			e := sesDedicatedEnv(t, f)
			_, err := e.invoke("aws.ses.dedicated-ip-pool.get", `{"pool_name":"marketing"}`)
			var failed *provider.Error
			if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil ||
				failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
				t.Fatalf("read error = %v (%+v)", err, failed)
			}
			before := len(restCalls(f))
			_, err = e.invokeConfirmed("aws.ses.dedicated-ip-pool.create", `{"pool_name":"marketing"}`, true)
			if err == nil || len(restCalls(f))-before != 1 {
				t.Errorf("create: err = %v, %d calls, want one failed call", err, len(restCalls(f))-before)
			}
		})
	}
}

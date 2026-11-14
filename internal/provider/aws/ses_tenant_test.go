package aws

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
)

const (
	tenantIdentityARN = "arn:aws:ses:" + testRegion + ":" + testAccount + ":identity/example.com"
	tenantConfigARN   = "arn:aws:ses:" + testRegion + ":" + testAccount + ":configuration-set/default"
	tenantTemplateARN = "arn:aws:ses:" + testRegion + ":" + testAccount + ":template/welcome"
	tenantPolicyARN   = "arn:aws:ses:" + testRegion + ":aws:reputation-policy/standard"
	tenantARN         = "arn:aws:ses:" + testRegion + ":" + testAccount + ":tenant/t1/0123abcd"
)

var sesTenantTools = map[string]struct {
	effect      capability.Effect
	idem        capability.Idempotency
	sensitivity string
	args        string
}{
	"aws.ses.tenant.get":                                       {capability.EffectRead, capability.IdempotencySafe, "aws-ses-tenant", `{"tenant_name":"t1"}`},
	"aws.ses.tenant.list":                                      {capability.EffectRead, capability.IdempotencySafe, "aws-ses-tenant", `{"filter":{"SENDING_STATUS":"ENABLED"}}`},
	"aws.ses.tenant-resource.list":                             {capability.EffectRead, capability.IdempotencySafe, "aws-ses-tenant", `{"tenant_name":"t1","filter":{"RESOURCE_TYPE":"EMAIL_IDENTITY"}}`},
	"aws.ses.resource-tenant.list":                             {capability.EffectRead, capability.IdempotencySafe, "aws-ses-tenant", `{"resource_arn":"` + tenantIdentityARN + `"}`},
	"aws.ses.tenant.create":                                    {capability.EffectCreate, capability.IdempotencyNonIdempotent, "aws-ses-tenant", `{"tenant_name":"t1","tags":[{"key":"k","value":"v"}]}`},
	"aws.ses.tenant.delete":                                    {capability.EffectDelete, capability.IdempotencyNonIdempotent, "aws-ses-tenant", `{"tenant_name":"t1"}`},
	"aws.ses.tenant-resource.associate":                        {capability.EffectCreate, capability.IdempotencyNonIdempotent, "aws-ses-tenant", `{"tenant_name":"t1","resource_arn":"` + tenantIdentityARN + `"}`},
	"aws.ses.tenant-resource.disassociate":                     {capability.EffectDelete, capability.IdempotencyNonIdempotent, "aws-ses-tenant", `{"tenant_name":"t1","resource_arn":"` + tenantConfigARN + `"}`},
	"aws.ses.tenant-suppression-attributes.put":                {capability.EffectUpdate, capability.IdempotencyIdempotent, "aws-ses-tenant", `{"tenant_name":"t1","suppressed_reasons":["BOUNCE"],"suppression_scope":"TENANT"}`},
	"aws.ses.reputation-entity.get":                            {capability.EffectRead, capability.IdempotencySafe, "aws-ses-reputation", `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + tenantARN + `"}`},
	"aws.ses.reputation-entity.list":                           {capability.EffectRead, capability.IdempotencySafe, "aws-ses-reputation", `{"filter":{"REPUTATION_IMPACT":"HIGH"}}`},
	"aws.ses.reputation-entity.update-policy":                  {capability.EffectUpdate, capability.IdempotencyIdempotent, "aws-ses-reputation", `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + tenantTemplateARN + `","reputation_entity_policy":"` + tenantPolicyARN + `"}`},
	"aws.ses.reputation-entity.update-customer-managed-status": {capability.EffectUpdate, capability.IdempotencyIdempotent, "aws-ses-reputation", `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + tenantARN + `","sending_status":"DISABLED"}`},
}

func sesTenantDescriptors() map[string]capability.Descriptor {
	out := map[string]capability.Descriptor{}
	for _, s := range generatedSpecs {
		if _, ok := sesTenantTools[s.descriptor.ID]; ok {
			out[s.descriptor.ID] = s.descriptor
		}
	}
	return out
}

// The 13 tools exist with the effect, idempotency, and data sensitivity of the table; writes need confirmation,
// reads do not, and none is listed-only.
func TestSESTenantToolsRisk(t *testing.T) {
	seen := sesTenantDescriptors()
	if len(seen) != 13 || len(sesTenantTools) != 13 {
		t.Fatalf("%d tools, want 13", len(seen))
	}
	for id, w := range sesTenantTools {
		d := seen[id]
		confirmation := capability.ConfirmationRequired
		if w.effect == capability.EffectRead {
			confirmation = capability.ConfirmationNone
		}
		if d.Risk.Effect != w.effect || d.Risk.Idempotency != w.idem || d.Risk.Confirmation != confirmation ||
			!d.Risk.OpenWorld || d.Risk.DataSensitivity != w.sensitivity || d.RequiresToolAllowList || d.Group != "ses" {
			t.Errorf("%s descriptor = %+v", id, d)
		}
	}
}

// The tools that can pause or release sending say so, and the ARN tools name the bound account.
func TestSESTenantDescriptions(t *testing.T) {
	seen := sesTenantDescriptors()
	for id, parts := range map[string][]string{
		"aws.ses.reputation-entity.update-customer-managed-status": {"stops sending", "pause or release the sending of a tenant", "Needs confirmation"},
		"aws.ses.reputation-entity.update-policy":                  {"pause or release the sending of a tenant", "none never pauses", "strict", "Needs confirmation"},
		"aws.ses.tenant-resource.associate":                        {"bound account", "Needs confirmation"},
		"aws.ses.tenant.delete":                                    {"Needs confirmation"},
		"aws.ses.tenant.create":                                    {"charged by AWS per month", "price is not retrieved"},
	} {
		for _, p := range parts {
			if !strings.Contains(seen[id].Description, p) {
				t.Errorf("%s description lacks %q: %s", id, p, seen[id].Description)
			}
		}
	}
}

// Each tool sends the request of its operation, and a failed answer maps to its class without the provider text.
func TestSESTenantToolsSendAndFail(t *testing.T) {
	escapedRef := "/v2/email/reputation/entities/RESOURCE/" + httpbind.Escape(tenantARN)
	want := map[string]struct{ method, path string }{
		"aws.ses.tenant.get":                                       {"POST", "/v2/email/tenants/get"},
		"aws.ses.tenant.list":                                      {"POST", "/v2/email/tenants/list"},
		"aws.ses.tenant-resource.list":                             {"POST", "/v2/email/tenants/resources/list"},
		"aws.ses.resource-tenant.list":                             {"POST", "/v2/email/resources/tenants/list"},
		"aws.ses.tenant.create":                                    {"POST", "/v2/email/tenants"},
		"aws.ses.tenant.delete":                                    {"POST", "/v2/email/tenants/delete"},
		"aws.ses.tenant-resource.associate":                        {"POST", "/v2/email/tenants/resources"},
		"aws.ses.tenant-resource.disassociate":                     {"POST", "/v2/email/tenants/resources/delete"},
		"aws.ses.tenant-suppression-attributes.put":                {"POST", "/v2/email/tenant/suppression"},
		"aws.ses.reputation-entity.get":                            {"GET", escapedRef},
		"aws.ses.reputation-entity.list":                           {"POST", "/v2/email/reputation/entities"},
		"aws.ses.reputation-entity.update-customer-managed-status": {"PUT", escapedRef + "/customer-managed-status"},
		"aws.ses.reputation-entity.update-policy":                  {"PUT", "/v2/email/reputation/entities/RESOURCE/" + httpbind.Escape(tenantTemplateARN) + "/policy"},
	}
	for id, w := range sesTenantTools {
		t.Run(id, func(t *testing.T) {
			confirmed := w.effect != capability.EffectRead
			f := sesFake(func(sent) reply { return ok(`{}`) })
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			if _, err := e.invokeConfirmed(id, w.args, confirmed); err != nil {
				t.Fatal(err)
			}
			calls := restCalls(f)
			if len(calls) != 1 || calls[0].method != want[id].method || calls[0].path != want[id].path {
				t.Fatalf("calls = %+v, want %+v", calls, want[id])
			}
			for _, c := range []struct {
				code   string
				status int
				class  provider.Class
			}{
				{"NotFoundException", 404, provider.ClassNotFound},
				{"BadRequestException", 400, provider.ClassProviderError},
				{"TooManyRequestsException", 429, provider.ClassRateLimited},
			} {
				f := sesFake(func(sent) reply {
					return reply{status: c.status, header: map[string]string{"X-Amzn-Errortype": c.code}, body: `{"message":"secret text"}`}
				})
				e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
				_, err := e.invokeConfirmed(id, w.args, confirmed)
				var failed *provider.Error
				if !errorsAs(err, &failed) || failed.Class != c.class || failed.Original == nil ||
					failed.Original.Entries[0].Code != c.code || strings.Contains(err.Error(), "secret text") {
					t.Errorf("%s: error = %v (%+v)", c.code, err, failed)
				}
			}
		})
	}
}

// The answers keep their members, and the writes send exactly the members of the arguments.
func TestSESTenantAnswersAndBodies(t *testing.T) {
	f := sesFake(func(r sent) reply {
		switch {
		case r.path == "/v2/email/tenants/get":
			return ok(`{"Tenant":{"TenantName":"t1","TenantId":"id1","SendingStatus":"ENABLED","SuppressionAttributes":{"SuppressedReasons":["BOUNCE"],"SuppressionScope":"TENANT"}}}`)
		case r.method == "GET":
			return ok(`{"ReputationEntity":{"ReputationEntityReference":"` + tenantARN + `","SendingStatusAggregate":"DISABLED","ReputationImpact":"HIGH","CustomerManagedStatus":{"Status":"DISABLED","Cause":"c"}}}`)
		case r.path == "/v2/email/tenants":
			return ok(`{"TenantName":"t1","TenantId":"id1","TenantArn":"arn:aws:ses:` + testRegion + `:` + testAccount + `:tenant/t1/id1","SendingStatus":"ENABLED"}`)
		}
		return ok(`{}`)
	})
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for tool, member := range map[string]string{"aws.ses.tenant.get": "tenant", "aws.ses.reputation-entity.get": "reputation_entity"} {
		text, err := e.invoke(tool, sesTenantTools[tool].args)
		if err != nil {
			t.Fatal(err)
		}
		if _, has := decodeResult(t, text)[member]; !has {
			t.Errorf("%s result lacks %s: %s", tool, member, text)
		}
	}
	text, err := e.invokeConfirmed("aws.ses.tenant.create", sesTenantTools["aws.ses.tenant.create"].args, true)
	if err != nil || decodeResult(t, text)["tenant_id"] != "id1" {
		t.Fatalf("create = %s, %v", text, err)
	}
	for _, tool := range []string{"aws.ses.tenant-resource.associate", "aws.ses.tenant-suppression-attributes.put",
		"aws.ses.reputation-entity.update-customer-managed-status", "aws.ses.reputation-entity.update-policy"} {
		if _, err := e.invokeConfirmed(tool, sesTenantTools[tool].args, true); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	bodies := map[string]string{}
	for _, c := range restCalls(f) {
		bodies[c.method+" "+c.path] = c.body
	}
	for key, parts := range map[string][]string{
		"POST /v2/email/tenants":            {`"TenantName":"t1"`, `"Tags":[{"Key":"k","Value":"v"}]`},
		"POST /v2/email/tenants/resources":  {`"TenantName":"t1"`, `"ResourceArn":"` + tenantIdentityARN + `"`},
		"POST /v2/email/tenant/suppression": {`"SuppressedReasons":["BOUNCE"]`, `"SuppressionScope":"TENANT"`},
		"PUT /v2/email/reputation/entities/RESOURCE/" + httpbind.Escape(tenantARN) + "/customer-managed-status": {`"SendingStatus":"DISABLED"`},
		"PUT /v2/email/reputation/entities/RESOURCE/" + httpbind.Escape(tenantTemplateARN) + "/policy":          {`"ReputationEntityPolicy":"` + tenantPolicyARN + `"`},
	} {
		for _, p := range parts {
			if !strings.Contains(bodies[key], p) {
				t.Errorf("%s body %q lacks %s", key, bodies[key], p)
			}
		}
	}
}

// The list tools page over three pages by NextToken and hide the token behind the cursor.
func TestSESTenantListsOverThreePages(t *testing.T) {
	for _, c := range []struct{ tool, args, key, item string }{
		{"aws.ses.tenant.list", `{"page_size":1`, "Tenants", `{"TenantName":"t"}`},
		{"aws.ses.tenant-resource.list", `{"tenant_name":"t1","page_size":1`, "TenantResources", `{"ResourceType":"EMAIL_IDENTITY","ResourceArn":"` + tenantIdentityARN + `"}`},
		{"aws.ses.resource-tenant.list", `{"resource_arn":"` + tenantIdentityARN + `","page_size":1`, "ResourceTenants", `{"TenantName":"t"}`},
		{"aws.ses.reputation-entity.list", `{"page_size":1`, "ReputationEntities", `{"ReputationEntityReference":"` + tenantIdentityARN + `"}`},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := sesFake(func(r sent) reply {
				switch {
				case strings.Contains(r.body, "tok-2"):
					return ok(`{"` + c.key + `":[` + c.item + `]}`)
				case strings.Contains(r.body, "tok-1"):
					return ok(`{"` + c.key + `":[` + c.item + `],"NextToken":"tok-2"}`)
				}
				return ok(`{"` + c.key + `":[` + c.item + `],"NextToken":"tok-1"}`)
			})
			e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
			cursor := ""
			for i := 0; i < 3; i++ {
				args := c.args
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

// All three kinds of resource pass the check in the bound account and region.
func TestSESTenantResourceARNKinds(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for _, arn := range []string{tenantIdentityARN, tenantConfigARN, tenantTemplateARN} {
		if _, err := e.invokeConfirmed("aws.ses.tenant-resource.associate", `{"tenant_name":"t1","resource_arn":"`+arn+`"}`, true); err != nil {
			t.Errorf("%s: %v", arn, err)
		}
	}
}

// The reputation tools take a tenant and the three kinds of resource, and the policy is one of the three SES
// policies of an allowed region.
func TestSESReputationAcceptsTenantsAndManagedPolicies(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	for _, ref := range []string{tenantARN, "arn:aws:ses:" + testRegion + ":" + testAccount + ":tenant/t1", tenantIdentityARN, tenantConfigARN, tenantTemplateARN} {
		args := `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + ref + `"`
		if _, err := e.invoke("aws.ses.reputation-entity.get", args+`}`); err != nil {
			t.Errorf("get %s: %v", ref, err)
		}
		if _, err := e.invokeConfirmed("aws.ses.reputation-entity.update-customer-managed-status", args+`,"sending_status":"ENABLED"}`, true); err != nil {
			t.Errorf("status %s: %v", ref, err)
		}
	}
	for _, name := range []string{"none", "standard", "strict"} {
		policy := "arn:aws:ses:" + testRegion + ":aws:reputation-policy/" + name
		if _, err := e.invokeConfirmed("aws.ses.reputation-entity.update-policy",
			`{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"`+tenantARN+`","reputation_entity_policy":"`+policy+`"}`, true); err != nil {
			t.Errorf("policy %s: %v", name, err)
		}
	}
}

// Everything that must not reach AWS is refused before any request: a region outside the allowlist, an ARN of
// another account, region, service, partition, or resource type, a bad tenant name, and a write without confirmation.
func TestSESTenantRefusalsBeforeAnyIO(t *testing.T) {
	f := sesFake(func(sent) reply { return ok(`{}`) })
	e := sesEnv(t, f, "account/"+testAccount, "region/"+testRegion)
	resource := func(arn string) string { return `{"tenant_name":"t1","resource_arn":"` + arn + `"}` }
	entity := func(arn string) string {
		return `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + arn + `","sending_status":"DISABLED"}`
	}
	policy := func(ref, p string) string {
		return `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + ref + `","reputation_entity_policy":"` + p + `"}`
	}
	// A tenant is no resource to associate or to look up the tenants of.
	for _, tool := range []string{"aws.ses.tenant-resource.associate", "aws.ses.tenant-resource.disassociate"} {
		if _, err := e.invokeConfirmed(tool, resource(tenantARN), true); err == nil {
			t.Errorf("%s accepted a tenant ARN", tool)
		}
	}
	if _, err := e.invoke("aws.ses.resource-tenant.list", `{"resource_arn":"`+tenantARN+`"}`); err == nil {
		t.Error("resource-tenant.list accepted a tenant ARN")
	}
	badARNs := []string{
		"arn:aws:ses:" + testRegion + ":210987654321:identity/example.com",
		"arn:aws:ses:us-east-1:" + testAccount + ":identity/example.com",
		"arn:aws:ses:" + testRegion + ":aws:identity/example.com",
		"arn:aws:sns:" + testRegion + ":" + testAccount + ":identity/example.com",
		"arn:aws-cn:ses:" + testRegion + ":" + testAccount + ":identity/example.com",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":identity/a/b",
		"example.com",
	}
	type call struct{ tool, args string }
	var refused []call
	for _, arn := range []string{
		"arn:aws:ses:" + testRegion + ":210987654321:tenant/t1/id",
		"arn:aws:ses:us-east-1:" + testAccount + ":tenant/t1/id",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":tenant/",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":tenant/t1/id/x",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":tenant/" + strings.Repeat("a", 65) + "/id",
		"arn:aws:sns:" + testRegion + ":" + testAccount + ":tenant/t1/id",
	} {
		refused = append(refused,
			call{"aws.ses.reputation-entity.get", `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + arn + `"}`},
			call{"aws.ses.reputation-entity.update-customer-managed-status", entity(arn)},
			call{"aws.ses.reputation-entity.update-policy", policy(arn, tenantPolicyARN)})
	}
	for _, arn := range badARNs {
		refused = append(refused,
			call{"aws.ses.tenant-resource.associate", resource(arn)},
			call{"aws.ses.tenant-resource.disassociate", resource(arn)},
			call{"aws.ses.resource-tenant.list", `{"resource_arn":"` + arn + `"}`},
			call{"aws.ses.reputation-entity.get", `{"reputation_entity_type":"RESOURCE","reputation_entity_reference":"` + arn + `"}`},
			call{"aws.ses.reputation-entity.update-customer-managed-status", entity(arn)},
			call{"aws.ses.reputation-entity.update-policy", policy(arn, tenantPolicyARN)},
		)
	}
	for _, p := range []string{
		"arn:aws:ses:" + testRegion + ":210987654321:reputation-policy/standard",
		"arn:aws:ses:" + testRegion + ":" + testAccount + ":reputation-policy/standard",
		"arn:aws:ses:us-east-1:aws:reputation-policy/standard",
		"arn:aws:ses:" + testRegion + ":aws:reputation-policy/lenient",
		"arn:aws:ses:" + testRegion + ":aws:reputation-policy/standard/x",
		"arn:aws:ses:" + testRegion + ":aws:identity/example.com",
		"arn:aws:iam::aws:reputation-policy/standard",
		"arn:aws-cn:ses:" + testRegion + ":aws:reputation-policy/standard",
		"standard",
	} {
		refused = append(refused, call{"aws.ses.reputation-entity.update-policy", policy(tenantIdentityARN, p)})
	}
	refused = append(refused,
		call{"aws.ses.tenant.get", `{"tenant_name":"t1","region":"us-east-1"}`},
		call{"aws.ses.tenant.create", `{"tenant_name":"t1","region":"ap-south-1"}`},
		call{"aws.ses.tenant.get", `{"tenant_name":"` + strings.Repeat("a", 65) + `"}`},
		call{"aws.ses.tenant.delete", `{"tenant_name":""}`},
	)
	for _, c := range refused {
		if _, err := e.invokeConfirmed(c.tool, c.args, true); err == nil {
			t.Errorf("%s %s was accepted", c.tool, c.args)
		}
	}
	for id, w := range sesTenantTools {
		if w.effect == capability.EffectRead {
			continue
		}
		var unconfirmed *application.ConfirmationRequiredError
		if _, err := e.invoke(id, w.args); !errorsAs(err, &unconfirmed) {
			t.Errorf("%s without confirmation: %v", id, err)
		}
	}
	if len(f.requests) != 0 {
		t.Errorf("a refused call sent %v", f.actions())
	}
}

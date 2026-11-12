package aws

import (
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
)

var wantTools = []string{
	"aws.iam.access-key.get-last-used",
	"aws.iam.access-key.list",
	"aws.iam.account.get-authorization-details",
	"aws.iam.account.get-password-policy",
	"aws.iam.account.get-properties",
	"aws.iam.account.get-summary",
	"aws.iam.account.list-aliases",
	"aws.iam.attached-policy.list-for-group",
	"aws.iam.attached-policy.list-for-role",
	"aws.iam.attached-policy.list-for-user",
	"aws.iam.credential-report.generate",
	"aws.iam.credential-report.get",
	"aws.iam.delegation-request.get",
	"aws.iam.delegation-request.list",
	"aws.iam.group.get",
	"aws.iam.group.list",
	"aws.iam.group.list-for-user",
	"aws.iam.human-readable-summary.get",
	"aws.iam.inline-policy.get-for-group",
	"aws.iam.inline-policy.get-for-role",
	"aws.iam.inline-policy.get-for-user",
	"aws.iam.inline-policy.list-for-group",
	"aws.iam.inline-policy.list-for-role",
	"aws.iam.inline-policy.list-for-user",
	"aws.iam.instance-profile.add-role",
	"aws.iam.instance-profile.create",
	"aws.iam.instance-profile.delete",
	"aws.iam.instance-profile.get",
	"aws.iam.instance-profile.list",
	"aws.iam.instance-profile.list-for-role",
	"aws.iam.instance-profile.list-tags",
	"aws.iam.instance-profile.remove-role",
	"aws.iam.login-profile.get",
	"aws.iam.mfa-device.get",
	"aws.iam.mfa-device.list",
	"aws.iam.mfa-device.list-tags",
	"aws.iam.oidc-provider.get",
	"aws.iam.oidc-provider.list",
	"aws.iam.oidc-provider.list-tags",
	"aws.iam.organizations-access-report.get",
	"aws.iam.organizations-features.list",
	"aws.iam.outbound-federation.get-info",
	"aws.iam.policy-simulation.get-context-keys-custom",
	"aws.iam.policy-simulation.get-context-keys-principal",
	"aws.iam.policy-simulation.simulate-custom",
	"aws.iam.policy-simulation.simulate-principal",
	"aws.iam.policy-version.get",
	"aws.iam.policy-version.list",
	"aws.iam.policy.get",
	"aws.iam.policy.list",
	"aws.iam.policy.list-entities",
	"aws.iam.policy.list-tags",
	"aws.iam.role-template.get-version",
	"aws.iam.role.create",
	"aws.iam.role.delete",
	"aws.iam.role.delete-permissions-boundary",
	"aws.iam.role.get",
	"aws.iam.role.list",
	"aws.iam.role.list-tags",
	"aws.iam.role.put-permissions-boundary",
	"aws.iam.role.tag",
	"aws.iam.role.untag",
	"aws.iam.role.update",
	"aws.iam.role.update-trust-policy",
	"aws.iam.saml-provider.get",
	"aws.iam.saml-provider.list",
	"aws.iam.saml-provider.list-tags",
	"aws.iam.server-certificate.get",
	"aws.iam.server-certificate.list",
	"aws.iam.server-certificate.list-tags",
	"aws.iam.service-access.list-policies",
	"aws.iam.service-last-accessed.generate",
	"aws.iam.service-last-accessed.get-details",
	"aws.iam.service-last-accessed.get-entities",
	"aws.iam.service-linked-role.get-deletion-status",
	"aws.iam.service-specific-credential.list",
	"aws.iam.signing-certificate.list",
	"aws.iam.ssh-public-key.get",
	"aws.iam.ssh-public-key.list",
	"aws.iam.user.get",
	"aws.iam.user.list",
	"aws.iam.user.list-tags",
	"aws.iam.virtual-mfa-device.list",
	"aws.route53.account-limit.get",
	"aws.route53.change.get",
	"aws.route53.checker-ip-range.list",
	"aws.route53.cidr-block.list",
	"aws.route53.cidr-collection.list",
	"aws.route53.cidr-location.list",
	"aws.route53.delegation-set.get",
	"aws.route53.delegation-set.get-limit",
	"aws.route53.delegation-set.list",
	"aws.route53.dns-answer.test",
	"aws.route53.dnssec.disable",
	"aws.route53.dnssec.enable",
	"aws.route53.dnssec.get",
	"aws.route53.geo-location.get",
	"aws.route53.geo-location.list",
	"aws.route53.health-check.count",
	"aws.route53.health-check.create",
	"aws.route53.health-check.delete",
	"aws.route53.health-check.get",
	"aws.route53.health-check.get-last-failure-reason",
	"aws.route53.health-check.get-status",
	"aws.route53.health-check.list",
	"aws.route53.health-check.update",
	"aws.route53.hosted-zone.count",
	"aws.route53.hosted-zone.create",
	"aws.route53.hosted-zone.delete",
	"aws.route53.hosted-zone.get",
	"aws.route53.hosted-zone.get-limit",
	"aws.route53.hosted-zone.list",
	"aws.route53.hosted-zone.list-by-name",
	"aws.route53.hosted-zone.list-by-vpc",
	"aws.route53.hosted-zone.update-comment",
	"aws.route53.key-signing-key.activate",
	"aws.route53.key-signing-key.create",
	"aws.route53.key-signing-key.deactivate",
	"aws.route53.key-signing-key.delete",
	"aws.route53.query-logging-config.get",
	"aws.route53.query-logging-config.list",
	"aws.route53.record-set.change",
	"aws.route53.record-set.list",
	"aws.route53.tag.change",
	"aws.route53.tag.list",
	"aws.route53.tag.list-many",
	"aws.route53.traffic-policy-instance.count",
	"aws.route53.traffic-policy-instance.get",
	"aws.route53.traffic-policy-instance.list",
	"aws.route53.traffic-policy-instance.list-by-hosted-zone",
	"aws.route53.traffic-policy-instance.list-by-policy",
	"aws.route53.traffic-policy.get",
	"aws.route53.traffic-policy.list",
	"aws.route53.traffic-policy.list-versions",
	"aws.route53.vpc-association-authorization.list",
	"aws.s3.accelerate.get",
	"aws.s3.acl.get",
	"aws.s3.bucket.create",
	"aws.s3.bucket.delete",
	"aws.s3.bucket.get-location",
	"aws.s3.bucket.head",
	"aws.s3.bucket.list",
	"aws.s3.cors.delete",
	"aws.s3.cors.get",
	"aws.s3.encryption.delete",
	"aws.s3.encryption.get",
	"aws.s3.encryption.put",
	"aws.s3.lifecycle.delete",
	"aws.s3.lifecycle.get",
	"aws.s3.logging.get",
	"aws.s3.notification.get",
	"aws.s3.object-legal-hold.get",
	"aws.s3.object-lock.get",
	"aws.s3.object-retention.get",
	"aws.s3.ownership.get",
	"aws.s3.ownership.put",
	"aws.s3.public-access-block.get",
	"aws.s3.public-access-block.put",
	"aws.s3.replication.get",
	"aws.s3.request-payment.get",
	"aws.s3.tagging.delete",
	"aws.s3.tagging.get",
	"aws.s3.tagging.put",
	"aws.s3.versioning.get",
	"aws.s3.versioning.put",
	"aws.s3.website.get",
	"aws.ses.configuration-set-event-destination.get",
	"aws.ses.configuration-set.create",
	"aws.ses.configuration-set.delete",
	"aws.ses.configuration-set.get",
	"aws.ses.configuration-set.list",
	"aws.ses.configuration-set.put-archiving-options",
	"aws.ses.configuration-set.put-delivery-options",
	"aws.ses.configuration-set.put-reputation-options",
	"aws.ses.configuration-set.put-sending-options",
	"aws.ses.configuration-set.put-suppression-options",
	"aws.ses.configuration-set.put-tracking-options",
	"aws.ses.configuration-set.put-vdm-options",
	"aws.ses.email-identity-policy.create",
	"aws.ses.email-identity-policy.delete",
	"aws.ses.email-identity-policy.list",
	"aws.ses.email-identity-policy.update",
	"aws.ses.email-identity.create",
	"aws.ses.email-identity.delete",
	"aws.ses.email-identity.get",
	"aws.ses.email-identity.list",
	"aws.ses.email-identity.list-tags",
	"aws.ses.email-identity.put-configuration-set-attributes",
	"aws.ses.email-identity.put-dkim-attributes",
	"aws.ses.email-identity.put-dkim-signing-attributes",
	"aws.ses.email-identity.put-feedback-attributes",
	"aws.ses.email-identity.put-mail-from-attributes",
	"aws.ses.email-identity.tag",
	"aws.ses.email-identity.untag",
	"aws.ses.suppressed-destination.delete",
	"aws.ses.suppressed-destination.get",
	"aws.ses.suppressed-destination.list",
	"aws.ses.suppressed-destination.put",
	"aws.sts.access-key.get-info",
	"aws.sts.authorization-message.decode",
	"aws.sts.caller-identity.get",
}

func TestRegisterPublishesMetadataAndTools(t *testing.T) {
	reg := registered(t)
	metadata, ok := reg.ProviderMetadata(Provider)
	if !ok || metadata.Name != "Amazon Web Services" || !metadata.ServiceLevel || metadata.DocPath() != "aws/setup.md" {
		t.Fatalf("metadata = %+v", metadata)
	}
	var ids []string
	for _, tool := range metadata.Tools {
		ids = append(ids, tool.ID)
		// A read is open; an IAM write is offered only by a tools list naming it, an SES write by the effect of
		// the connection with confirmation.
		open := tool.Effect == config.EffectRead && !tool.RequiresToolAllowList
		write := tool.Effect != config.EffectRead && tool.RequiresToolAllowList && strings.HasPrefix(tool.ID, "aws.iam.")
		// Identity policy writes change who may send, so they are listed-only like the IAM writes.
		sesWrite := tool.Effect != config.EffectRead && strings.HasPrefix(tool.ID, "aws.ses.") &&
			tool.RequiresToolAllowList == strings.HasPrefix(tool.ID, "aws.ses.email-identity-policy.")
		s3Write := tool.Effect != config.EffectRead && !tool.RequiresToolAllowList && strings.HasPrefix(tool.ID, "aws.s3.")
		// DNSSEC writes can make a domain unreachable, so they are listed-only; every other Route 53 write is not.
		route53Write := tool.Effect != config.EffectRead && strings.HasPrefix(tool.ID, "aws.route53.") &&
			tool.RequiresToolAllowList == route53DNSSECWrite(tool.ID)
		if !(open || write || sesWrite || s3Write || route53Write) || tool.Group != strings.Split(tool.ID, ".")[1] {
			t.Errorf("tool %+v, want an open read, a listed-only IAM write, an SES write (listed-only for identity policies), an S3 or a Route 53 write of the group named by its service segment", tool)
		}
	}
	if !reflect.DeepEqual(ids, wantTools) {
		t.Errorf("tools = %v, want %v", ids, wantTools)
	}
	if !reflect.DeepEqual(metadata.SupportedEffects, []config.Effect{config.EffectRead, config.EffectCreate, config.EffectUpdate, config.EffectDelete, config.EffectExecute}) ||
		!reflect.DeepEqual(metadata.DefaultEffects, []config.Effect{config.EffectRead}) {
		t.Errorf("effects = %v / %v, want read, create, update, delete, execute supported and read by default", metadata.SupportedEffects, metadata.DefaultEffects)
	}
	groups := map[string]bool{}
	for _, g := range metadata.Groups {
		groups[g.ID] = true
	}
	if len(groups) != 5 || !groups["sts"] || !groups["iam"] || !groups["ses"] || !groups["s3"] || !groups["route53"] {
		t.Errorf("groups = %+v, want sts, iam, ses, s3 and route53", metadata.Groups)
	}
	profile, ok := metadata.RecommendedProfile()
	if !ok || profile.ID != "read" || !reflect.DeepEqual(append([]string{}, profile.Tools...), []string{
		"aws.sts.caller-identity.get", "aws.sts.access-key.get-info", "aws.sts.authorization-message.decode",
		"aws.iam.user.list", "aws.iam.user.get", "aws.iam.user.list-tags",
		"aws.iam.group.list", "aws.iam.group.get", "aws.iam.group.list-for-user",
		"aws.iam.role.list", "aws.iam.role.get", "aws.iam.role.list-tags",
		"aws.iam.access-key.list", "aws.iam.access-key.get-last-used"}) {
		t.Errorf("profile = %+v", profile)
	}
	if !metadata.Target.Required || !metadata.Target.Multiple || metadata.Target.Wildcard != "" {
		t.Errorf("target = %+v, want required, multiple, and no wildcard", metadata.Target)
	}
}

func TestSecretRoles(t *testing.T) {
	reg := registered(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	got := map[string]bool{}
	var order []string
	for _, role := range metadata.SecretRoles {
		got[role.Name] = role.Optional
		order = append(order, role.Name)
	}
	want := map[string]bool{"access_key_id": false, "secret_access_key": false, "session_token": true,
		"role_arn": true, "external_id": true}
	if !reflect.DeepEqual(got, want) || len(order) != 5 {
		t.Errorf("roles = %v, want %v", got, want)
	}
}

func TestBaseURLIsFixed(t *testing.T) {
	reg := registered(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, good := range []string{"https://aws.amazon.com", "https://aws.amazon.com/"} {
		if err := metadata.ValidateBaseURL(good); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v", good, err)
		}
	}
	for _, bad := range []string{"https://sts.amazonaws.com", "http://aws.amazon.com", "https://aws.amazon.com/x",
		"https://evil.example", (&url.URL{Scheme: "https", User: url.UserPassword("u", "p"), Host: "aws.amazon.com"}).String(), "https://aws.amazon.com?x=1"} {
		if err := metadata.ValidateBaseURL(bad); err == nil {
			t.Errorf("ValidateBaseURL(%q) = nil, want a refusal", bad)
		}
	}
}

// readOnlyIdentityProviderTools are reads whose IDs name an identity provider or federation setting; they
// return metadata only, and the operations that create or change trust are excluded.
var readOnlyIdentityProviderTools = map[string]bool{
	"aws.iam.saml-provider.list": true, "aws.iam.saml-provider.get": true, "aws.iam.saml-provider.list-tags": true,
	"aws.iam.outbound-federation.get-info": true,
}

// Every read is credential-free with the full risk, every other tool is a listed-only IAM write, a listed-only SES identity policy write, or an SES
// write with confirmation, and every tool has the strict schema. The AWS actions that return a credential or token are no
// tools, and the actions the STS tools send are exactly the three STS reads.
func TestSTSToolsAreTheCredentialFreeActions(t *testing.T) {
	reg := registered(t)
	for _, d := range reg.Provider(Provider) {
		want := capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
			Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: d.Risk.DataSensitivity}
		if d.Risk.Effect != capability.EffectRead {
			want.Effect, want.Idempotency, want.Confirmation = d.Risk.Effect, d.Risk.Idempotency, capability.ConfirmationRequired
			listedIAM := d.RequiresToolAllowList && strings.HasPrefix(d.ID, "aws.iam.")
			sesWrite := strings.HasPrefix(d.ID, "aws.ses.") &&
				d.RequiresToolAllowList == strings.HasPrefix(d.ID, "aws.ses.email-identity-policy.")
			s3Write := !d.RequiresToolAllowList && (strings.HasPrefix(d.ID, "aws.s3.") || strings.HasPrefix(d.ID, "aws.route53.") && !route53DNSSECWrite(d.ID))
			route53Listed := d.RequiresToolAllowList && route53DNSSECWrite(d.ID)
			if !(listedIAM || sesWrite || s3Write || route53Listed) || d.Risk.Idempotency == capability.IdempotencySafe {
				t.Errorf("%s is a write that is neither a listed-only IAM tool nor an SES or S3 tool: %+v", d.ID, d)
			}
		}
		if d.Risk != want || d.Risk.DataSensitivity == "" {
			t.Errorf("%s risk = %+v", d.ID, d.Risk)
		}
		if !strings.Contains(string(d.InputSchema), `"additionalProperties":false`) {
			t.Errorf("%s input schema is open", d.ID)
		}
		for _, banned := range []string{"assume", "session", "federation", "token", "root", "delegated", "saml", "web-identity"} {
			if strings.Contains(d.ID, banned) && !readOnlyIdentityProviderTools[d.ID] {
				t.Errorf("%s names a credential action (%s)", d.ID, banned)
			}
		}
	}
	for _, action := range excludedSTSActions {
		for _, d := range reg.Provider(Provider) {
			if strings.EqualFold(strings.ReplaceAll(d.ID, "-", ""), "aws.sts."+action) {
				t.Errorf("%s is the excluded action %s", d.ID, action)
			}
		}
	}
	if len(excludedSTSActions) != 8 {
		t.Errorf("the model has eight credential actions, the list holds %d", len(excludedSTSActions))
	}

	f := direct(func(r sent, _ int) reply {
		switch r.action {
		case "GetAccessKeyInfo":
			return ok(`<GetAccessKeyInfoResponse><GetAccessKeyInfoResult><Account>210987654321</Account></GetAccessKeyInfoResult></GetAccessKeyInfoResponse>`)
		case "DecodeAuthorizationMessage":
			return ok(`<DecodeAuthorizationMessageResponse><DecodeAuthorizationMessageResult><DecodedMessage>{}</DecodedMessage></DecodeAuthorizationMessageResult></DecodeAuthorizationMessageResponse>`)
		}
		return reply{status: 500}
	})
	e := newEnv(t, f, baseValues())
	for _, call := range [][2]string{
		{"aws.sts.caller-identity.get", `{}`},
		{"aws.sts.access-key.get-info", `{"access_key_id":"` + testKeyID + `"}`},
		{"aws.sts.authorization-message.decode", `{"encoded_message":"abc"}`},
	} {
		if _, err := e.invoke(call[0], call[1]); err != nil {
			t.Fatalf("%s: %v", call[0], err)
		}
	}
	allowed := map[string]bool{"GetCallerIdentity": true, "GetAccessKeyInfo": true, "DecodeAuthorizationMessage": true}
	for _, action := range f.actions() {
		if !allowed[action] {
			t.Errorf("the tools sent %s", action)
		}
	}
}

// Every tool's service segment has an endpoint and a segment table entry; a tool of a service without one
// would be a registration error.
func TestToolSegmentsAreMapped(t *testing.T) {
	reg := registered(t)
	for _, d := range reg.Provider(Provider) {
		segment := strings.Split(d.ID, ".")[1]
		if _, err := endpoints.ModelOf(segment); err != nil {
			t.Errorf("%s: segment %q is not in the segment table: %v", d.ID, segment, err)
		}
		if _, err := endpoints.IsGlobal(endpoints.Service(segment)); err != nil {
			t.Errorf("%s: segment %q has no endpoint: %v", d.ID, segment, err)
		}
	}
}

var sessionNameForm = regexp.MustCompile(`^[\w+=,.@-]{2,64}$`)

func TestSessionNameAndAgent(t *testing.T) {
	name := sessionName("prod")
	if !sessionNameForm.MatchString(name) || !strings.HasPrefix(name, "qatlas-") || name == sessionName("other") ||
		strings.Contains(name, "prod") {
		t.Errorf("sessionName = %q", name)
	}
	for in, want := range map[string]string{
		"1.2.3": "qatlas/1.2.3", "v0.4.0-rc.1+build": "qatlas/v0.4.0-rc.1+build", "dev": "qatlas/dev",
		"": "qatlas/unknown", "1 2\r\nX: y": "qatlas/12Xy", strings.Repeat("9", 65): "qatlas/unknown",
	} {
		if got := agentFor(in); got != want {
			t.Errorf("agentFor(%q) = %q, want %q", in, got, want)
		}
	}
}

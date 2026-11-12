package gen

import (
	"slices"
	"strings"
	"testing"
)

// A sensitive member that is an identifier stays open only with a table release and its reason; without one
// the generation stops, a release without a reason or for a member that is not sensitive stops it, and a
// member whose name looks like a credential is never released.
func TestSensitiveMembersAreOpenOnlyByRelease(t *testing.T) {
	m := syntheticModel(t)
	base := readOp("GetSecret")
	base.Fields = map[string]Field{"Name": {Description: "n"}, "Value": {Description: "v"}}
	base.HiddenOutputs = []Hidden{{Member: "SecretAccessKey", Reason: "a credential"}}
	if _, err := build(m, &syntheticTable, base); err == nil || !strings.Contains(err.Error(), "Value: the member is sensitive") {
		t.Fatalf("without a release: %v", err)
	}
	withSecretName := base
	withSecretName.Fields = map[string]Field{"Name": {Description: "n"}, "Value": {Description: "v"}, "SecretAccessKey": {Description: "k"}}
	withSecretName.HiddenOutputs = nil
	released := base
	released.Release = []Release{{Member: "Value", Reason: "an identifier, not a secret"}}
	if err := released.validate(); err != nil {
		t.Fatal(err)
	}
	tl, err := build(m, &syntheticTable, released)
	if err != nil {
		t.Fatalf("with a release: %v", err)
	}
	if !strings.Contains(tl.outputSchema, `"value"`) {
		t.Errorf("output schema = %s", tl.outputSchema)
	}
	// The credential name rule stays hard: SecretAccessKey is refused although the rest is released.
	withSecretName.Release = released.Release
	if _, err := build(m, &syntheticTable, withSecretName); err == nil || !strings.Contains(err.Error(), "SecretAccessKey: the member looks like a credential") {
		t.Errorf("credential-like member next to a release: %v", err)
	}
	for _, c := range []struct {
		name    string
		release []Release
		want    string
		onBuild bool
	}{
		{"no reason", []Release{{Member: "Value"}}, "needs a member and a reason", false},
		{"blank reason", []Release{{Member: "Value", Reason: "  "}}, "needs a member and a reason", false},
		{"credential name", []Release{{Member: "SecretAccessKey", Reason: "r"}}, "looks like a credential", false},
		{"credential name below the top level", []Release{{Member: "Value.PrivateKey", Reason: "r"}}, "looks like a credential", false},
		{"twice", []Release{{Member: "Value", Reason: "r"}, {Member: "Value", Reason: "r"}}, "listed twice", false},
		{"unknown member", []Release{{Member: "Value", Reason: "r"}, {Member: "Nothing", Reason: "r"}}, "release Nothing is not a sensitive member", true},
		{"member that is not sensitive", []Release{{Member: "Value", Reason: "r"}, {Member: "Name", Reason: "r"}}, "release Name is not a sensitive member", true},
	} {
		op := base
		op.Release = c.release
		var err error
		if c.onBuild {
			_, err = build(m, &syntheticTable, op)
		} else {
			err = op.validate()
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// A release hides nothing else: a hidden member is skipped, so a release for it names nothing.
	hidden := released
	hidden.Fields = map[string]Field{"Name": {Description: "n"}}
	hidden.HiddenOutputs = []Hidden{{Member: "Value", Reason: "r"}, {Member: "SecretAccessKey", Reason: "r"}}
	if _, err := build(m, &syntheticTable, hidden); err == nil || !strings.Contains(err.Error(), "release Value is not a sensitive member") {
		t.Errorf("release of a hidden member: %v", err)
	}
}

// The real tables release the KMS key ID only for the two encryption tools and the website and contact
// addresses only for the two SES account tools that carry the production access request, each with a reason,
// and no other tool of the tables shows a sensitive member.
func TestReleasesOfTheTables(t *testing.T) {
	var released []string
	for _, table := range Tables {
		for _, op := range table.Operations {
			for _, r := range op.Release {
				released = append(released, op.Operation+" "+r.Member)
				if strings.TrimSpace(r.Reason) == "" {
					t.Errorf("%s: release %s has no reason", op.Operation, r.Member)
				}
			}
		}
	}
	slices.Sort(released)
	want := []string{
		"CopyObject SSEKMSKeyId",
		"GetAccount Details.AdditionalContactEmailAddresses",
		"GetAccount Details.WebsiteURL",
		"GetBucketEncryption ServerSideEncryptionConfiguration.Rules.ApplyServerSideEncryptionByDefault.KMSMasterKeyID",
		"GetObject SSEKMSKeyId",
		"HeadObject SSEKMSKeyId",
		"PutAccountDetails AdditionalContactEmailAddresses",
		"PutAccountDetails WebsiteURL",
		"PutBucketEncryption ServerSideEncryptionConfiguration.Rules.ApplyServerSideEncryptionByDefault.KMSMasterKeyID",
		"PutObject SSEKMSKeyId",
	}
	if !slices.Equal(released, want) {
		t.Errorf("releases = %v, want %v", released, want)
	}
}

// Limits, guards and readbacks are checked against the names and the model.
func TestLimitsGuardsAndReadbacksAreChecked(t *testing.T) {
	m := syntheticModel(t)
	op := readOp("ListThings")
	op.Arguments = map[string]Argument{"Prefix": {Description: "p"}, "Filters": {Description: "f"}}
	op.Fields = map[string]Field{"Things": {Description: "t"}, "Tags": {Description: "g"}}
	op.Limits = []InputLimit{{Member: "Prefix", MaxLen: 10, Reason: "r"}}
	tl, err := build(m, &syntheticTable, op)
	if err != nil || !strings.Contains(tl.inputSchema, `"maxLength":10`) {
		t.Fatalf("limit: %v %v", err, tl)
	}
	op.Limits = []InputLimit{{Member: "Nothing", MaxLen: 10, Reason: "r"}}
	if _, err := build(m, &syntheticTable, op); err == nil || !strings.Contains(err.Error(), "limit Nothing is not a string or list member") {
		t.Errorf("unknown limit: %v", err)
	}
	for _, bad := range [][]InputLimit{{{Member: "Prefix", Reason: "r"}}, {{Member: "Prefix", MaxLen: 1, MaxItems: 1, Reason: "r"}},
		{{Member: "Prefix", MaxLen: 1}}, {{Member: "Prefix", MaxLen: -1, Reason: "r"}}} {
		o := readOp("ListThings")
		o.Limits = bad
		if err := o.validate(); err == nil {
			t.Errorf("limit %+v was accepted", bad)
		}
	}
	for _, c := range []struct {
		name string
		edit func(*Operation)
		want string
	}{
		{"unknown guard", func(o *Operation) { o.Guards = []string{"nothing"} }, "unknown guard"},
		{"unknown readback", func(o *Operation) { o.Readback, o.ReadbackAnswer = "nothing", "<x/>" }, "unknown readback"},
		{"readback on a read", func(o *Operation) { o.Readback, o.ReadbackAnswer = "pab-tighten", "<x/>" }, "not a read"},
		{"readback without an answer", func(o *Operation) {
			o.Effect, o.Idempotency, o.Confirm, o.Readback = "update", "idempotent", true, "pab-tighten"
		}, "needs the answer"},
		{"answer without a readback", func(o *Operation) { o.ReadbackAnswer = "<x/>" }, "without a readback"},
	} {
		o := readOp("ListThings")
		c.edit(&o)
		if err := o.validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// The tables name the steps they use, and only for the tools that need one.
	for _, table := range Tables {
		for _, o := range table.Operations {
			if (o.Readback != "") != (o.Operation == "PutPublicAccessBlock") {
				t.Errorf("%s: readback %q", o.Operation, o.Readback)
			}
			if len(o.Guards) > 0 && o.Operation != "CreateBucket" && o.Operation != "PutBucketEncryption" && o.Operation != "PutBucketOwnershipControls" &&
				o.Operation != "CreateHostedZone" && o.Operation != "ChangeResourceRecordSets" && o.Operation != "TestDNSAnswer" &&
				o.Operation != "CreateHealthCheck" && o.Operation != "UpdateHealthCheck" && o.Operation != "CreateKeySigningKey" && o.Operation != "ListHostedZonesByVPC" &&
				o.Operation != "CreateConfigurationSet" && o.Operation != "PutConfigurationSetTrackingOptions" && o.Operation != "PutConfigurationSetArchivingOptions" &&
				o.Operation != "CopyObject" && o.Operation != "PutObject" {
				t.Errorf("%s: guards %v", o.Operation, o.Guards)
			}
		}
	}
}

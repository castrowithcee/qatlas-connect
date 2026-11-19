package aws

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// Exactly the tools an expected_<service>_test.go lists carry a confirmation text, and a text for an unknown tool
// or a tool without confirmation is a registration error.
func TestConfirmationTextsBelongToTheListedToolsOnly(t *testing.T) {
	ops, err := generatedTools()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, op := range ops {
		if op.ConfirmationSummary != nil {
			got = append(got, op.Descriptor.ID)
		}
	}
	slices.Sort(got)
	if want := slices.Sorted(maps.Keys(expectedSummary)); !slices.Equal(got, want) {
		t.Errorf("tools with a confirmation text = %v, want %v", got, want)
	}
	defer delete(summaries, "aws.nothing.put")
	summaries["aws.nothing.put"] = func(json.RawMessage) string { return "" }
	if _, err := generatedTools(); err == nil {
		t.Error("a text for an unknown tool was accepted")
	}
	delete(summaries, "aws.nothing.put")
	for _, s := range generatedSpecs {
		if s.descriptor.Risk.Confirmation != capability.ConfirmationRequired {
			summaries[s.descriptor.ID] = func(json.RawMessage) string { return "" }
			if _, err := generatedTools(); err == nil {
				t.Errorf("a text for %s, which needs no confirmation, was accepted", s.descriptor.ID)
			}
			delete(summaries, s.descriptor.ID)
			break
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("a second registration did not panic")
		}
	}()
	registerSummary("aws.s3.request-payment.put", func(json.RawMessage) string { return "" })
}

func confirmationSummary(t *testing.T, e *env, id, args string) string {
	t.Helper()
	_, err := e.invokeConfirmed(id, args, false)
	var required *application.ConfirmationRequiredError
	if !errors.As(err, &required) {
		t.Fatalf("%s: err = %v, want confirmation required", id, err)
	}
	return required.Summary
}

// Each text names bucket, region and effect, and reaches the confirmation-required error.
func TestS3ConfirmationTexts(t *testing.T) {
	until := `"retain_until_date":"2027-01-02T03:04:05Z"`
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{"aws.s3.object-lock.put", `{"bucket":"my-bucket","region":"` + testRegion + `","object_lock_configuration":` + lockConfig("GOVERNANCE") + `}`,
			[]string{"my-bucket", testRegion, "cannot be undone", "GOVERNANCE", "30 days"}},
		{"aws.s3.object-lock.put", `{"bucket":"my-bucket","region":"` + testRegion + `","object_lock_configuration":{"object_lock_enabled":"Enabled","rule":{"default_retention":{"mode":"GOVERNANCE","years":2}}}}`,
			[]string{"my-bucket", testRegion, "cannot be undone", "2 years"}},
		{"aws.s3.object-lock.put", `{"bucket":"my-bucket","region":"` + testRegion + `","object_lock_configuration":{"object_lock_enabled":"Enabled"}}`,
			[]string{"my-bucket", testRegion, "cannot be undone", "No default retention"}},
		{"aws.s3.object-retention.put", `{"bucket":"my-bucket","region":"` + testRegion + `","key":"a/c.csv","version_id":"v1","retention":{"mode":"GOVERNANCE",` + until + `}}`,
			[]string{"my-bucket", testRegion, "a/c.csv", "version v1", "2027-01-02T03:04:05Z", "cannot be deleted or overwritten"}},
		{"aws.s3.object-retention.put", `{"bucket":"my-bucket","key":"a.csv","retention":{"mode":"GOVERNANCE",` + until + `}}`,
			[]string{"my-bucket", "the one region the connection allows", "current version"}},
		{"aws.s3.object-legal-hold.put", `{"bucket":"my-bucket","region":"` + testRegion + `","key":"a.csv","version_id":"v2","legal_hold":{"status":"ON"}}`,
			[]string{"my-bucket", testRegion, "a.csv", "version v2", "to ON", "no end date"}},
		{"aws.s3.object-legal-hold.put", `{"bucket":"my-bucket","key":"a.csv","legal_hold":{"status":"OFF"}}`,
			[]string{"my-bucket", "to OFF", "hold is removed"}},
		{"aws.s3.request-payment.put", `{"bucket":"my-bucket","region":"` + testRegion + `","request_payment_configuration":{"payer":"Requester"}}`,
			[]string{"my-bucket", testRegion, "Requester", "costs of transfer and requests", "anonymous access is no longer possible"}},
		{"aws.s3.request-payment.put", `{"bucket":"my-bucket","region":"` + testRegion + `","request_payment_configuration":{"payer":"BucketOwner"}}`,
			[]string{"my-bucket", testRegion, "BucketOwner", "reverted"}},
	} {
		f := restAnswer(reply{status: 200})
		e := s3WriteEnv(t, f, testRegion)
		got := confirmationSummary(t, e, c.id, c.args)
		for _, part := range c.want {
			if !strings.Contains(got, part) {
				t.Errorf("%s: summary %q lacks %q", c.id, got, part)
			}
		}
		if len(f.requests) != 0 {
			t.Errorf("%s: an unconfirmed call reached the network", c.id)
		}
	}
}

// A long or hostile value is cut, and an unreadable argument gives no text.
func TestS3ConfirmationTextBoundsValues(t *testing.T) {
	long := strings.Repeat("k", 1000) + `\n`
	got := requestPaymentSummary([]byte(`{"bucket":"` + long + `","request_payment_configuration":{"payer":"Requester"}}`))
	if strings.Contains(got, "\n") || strings.Contains(got, `\n`) || len(got) > 700 || !strings.Contains(got, "anonymous") {
		t.Errorf("summary = %q", got)
	}
	for _, f := range []func(json.RawMessage) string{objectLockSummary, objectRetentionSummary, objectLegalHoldSummary, requestPaymentSummary} {
		if f([]byte(`{"bucket":1}`)) != "" || f([]byte(`nope`)) != "" {
			t.Error("an unreadable argument gave a text")
		}
	}
}

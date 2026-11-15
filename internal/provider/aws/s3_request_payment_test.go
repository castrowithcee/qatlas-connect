package aws

import (
	"strings"
	"testing"
)

// The payer put sends PUT ?requestPayment with the payer and the owner header, and nothing without the confirmation.
func TestS3RequestPaymentPut(t *testing.T) {
	f := restAnswer(reply{status: 200})
	e := s3WriteEnv(t, f, testRegion)
	args := `{"bucket":"my-bucket","request_payment_configuration":{"payer":"Requester"}}`
	if _, err := e.invoke("aws.s3.request-payment.put", args); !isConfirmationRequired(err) {
		t.Fatalf("without confirmation: %v", err)
	}
	if len(f.requests) != 0 {
		t.Fatalf("an unconfirmed call reached the network: %v", f.actions())
	}
	if _, err := e.invokeConfirmed("aws.s3.request-payment.put", args, true); err != nil {
		t.Fatal(err)
	}
	reqs := restRequests(f)
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	r := reqs[0]
	if r.method != "PUT" || r.rawQuery != "requestPayment" || r.host != "my-bucket.s3."+testRegion+".amazonaws.com" ||
		r.header.Get("X-Amz-Expected-Bucket-Owner") != testAccount ||
		!strings.Contains(r.body, `<RequestPaymentConfiguration `+s3NS+`><Payer>Requester</Payer></RequestPaymentConfiguration>`) {
		t.Errorf("request = %+v", r)
	}
}

// The description names the shift of costs, the end of anonymous access, and the needed permission.
func TestS3RequestPaymentDescription(t *testing.T) {
	for _, d := range registered(t).Provider(Provider) {
		if d.ID != "aws.s3.request-payment.put" {
			continue
		}
		for _, want := range []string{"costs of transfer and requests", "anonymous access", "x-amz-request-payer", "reverted", "s3:PutBucketRequestPayment"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("description lacks %q", want)
			}
		}
		return
	}
	t.Error("aws.s3.request-payment.put is not registered")
}

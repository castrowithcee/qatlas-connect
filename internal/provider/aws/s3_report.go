package aws

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/endpoints"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// The destination rule of the S3 inventory and analytics writes. A report that lands in a bucket of another
// account is a standing data path out of the bound account, so the destination must be a bucket of the bound
// account. The guard judges what the arguments say; the readback asks S3 who owns the bucket.

const (
	reportRule     = "the report destination must be a bucket of the account this connection is bound to"
	reportHeadTool = "aws.s3.bucket.head"
	s3BucketARN    = "arn:aws:s3:::"
)

// reportDestination finds the S3 destination of an inventory or analytics configuration in the arguments, with
// the argument that carries the account of the destination bucket. found is false for an analytics
// configuration without a data export, which has no destination.
func reportDestination(args map[string]any) (dest map[string]any, accountKey string, found bool) {
	if inv, ok := args["inventory_configuration"].(map[string]any); ok {
		d, _ := inv["destination"].(map[string]any)
		s3, _ := d["s3_bucket_destination"].(map[string]any)
		return s3, "account_id", true
	}
	an, _ := args["analytics_configuration"].(map[string]any)
	sca, _ := an["storage_class_analysis"].(map[string]any)
	export, ok := sca["data_export"].(map[string]any)
	if !ok {
		return nil, "", false
	}
	d, _ := export["destination"].(map[string]any)
	s3, _ := d["s3_bucket_destination"].(map[string]any)
	return s3, "bucket_account_id", true
}

// reportBucket returns the bucket name of an ARN of the form arn:aws:s3:::NAME.
func reportBucket(value string) (string, bool) {
	name, ok := strings.CutPrefix(value, s3BucketARN)
	return name, ok && httpbind.ValidBucketName(name)
}

// checkReportDestination holds the destination of a report to the bound account without any I/O: a bucket ARN
// of the S3 form, an account ID that is set and is the bound account (a bucket ARN carries no account, and a
// name alone does not exclude a bucket of another account, so a missing ID is refused and not filled in), and
// an SSE-KMS key of the bound account.
func checkReportDestination(b binding, _ string, args map[string]any) error {
	dest, accountKey, found := reportDestination(args)
	if !found {
		return nil
	}
	if account, _ := dest[accountKey].(string); account == "" || account != b.account {
		return &provider.InvalidRequestError{Message: accountKey + " must be set to the account this connection is bound to: " + reportRule}
	}
	if bucket, _ := dest["bucket"].(string); bucket == "" {
		return &provider.InvalidRequestError{Message: "bucket must be set: " + reportRule}
	} else if _, ok := reportBucket(bucket); !ok {
		return &provider.InvalidRequestError{Message: "bucket must be a bucket ARN of the form arn:aws:s3:::NAME: " + reportRule}
	}
	enc, _ := dest["encryption"].(map[string]any)
	if kms, set := enc["ssekms"].(map[string]any); set {
		if key, _ := kms["key_id"].(string); !kmsARN(b, key) || !strings.Contains(key, ":key/") {
			return &provider.InvalidRequestError{Message: "ssekms.key_id must be the ARN of a KMS key in the account this connection is bound to and a region it allows"}
		}
	}
	return nil
}

// reportOwner asks S3 with HeadBucket whether the destination bucket belongs to the bound account: the expected
// owner header makes S3 answer 403 for a bucket of another account. A failure or a different owner refuses the
// change. A bucket in another region than the one called is not refused for that: the other regions the
// connection allows are tried, since S3 does not follow the redirect, and the first answer that confirms the
// owner is enough.
func reportOwner(ctx context.Context, c *Client, s toolSpec, creds sigv4.Credentials, region string, args map[string]any) error {
	dest, _, found := reportDestination(args)
	if !found {
		return nil
	}
	arn, _ := dest["bucket"].(string)
	name, ok := reportBucket(arn)
	read := slices.IndexFunc(generatedSpecs, func(g toolSpec) bool { return g.descriptor.ID == reportHeadTool })
	if !ok || read < 0 || s.service != endpoints.S3 {
		return provider.Fail(s.op, "the tool description is invalid")
	}
	_, send, err := generatedSpecs[read].encode(map[string]any{"bucket": name}, nil, c.binding.account)
	if err != nil {
		return err
	}
	var failure *provider.Error
	for _, try := range slices.Concat([]string{region}, slices.DeleteFunc(slices.Clone(c.binding.regions), func(r string) bool { return r == region })) {
		var out map[string]any
		if _, failure = send(ctx, c, creds, try, &out); failure == nil {
			return nil
		}
		if !otherRegion(failure) {
			break
		}
	}
	return &provider.Error{Class: failure.Class, Op: s.op, Message: "the destination bucket could not be confirmed as a bucket of the bound " +
		"account (this needs s3:ListBucket on it), so the change was not sent: " + failure.Message}
}

// otherRegion reports the answers a HEAD request gets from a bucket in another region: a redirect, or a 400 for
// the signature of the wrong region. HEAD answers carry no body that could say more.
func otherRegion(failure *provider.Error) bool {
	if failure.Class != provider.ClassProviderError || failure.Original == nil {
		return false
	}
	switch failure.Original.Status {
	case http.StatusMovedPermanently, http.StatusTemporaryRedirect, http.StatusPermanentRedirect, http.StatusBadRequest:
		return true
	}
	return false
}

package aws

import (
	"context"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/httpbind"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/sigv4"
)

// The S3 location of an SES import or export job. SES does not check that the bucket belongs to the caller, so
// a bucket of a third party would take the exported data or feed the import. The guard judges the form of the
// URL; the readback asks S3 who owns the bucket.

const (
	sesJobRule = "s3_url must be of the form s3://BUCKET/KEY for a bucket of the account this connection is bound to"
	// maxS3URL bounds the URL: a bucket name of 63 characters, a key of at most 1024 bytes, and the scheme.
	maxS3URL = 5 + 63 + 1 + 1024
)

// sesJobURL returns the S3 URL a job request names, if it names one. The import data source always does; the
// destination of an export may leave it out.
func sesJobURL(args map[string]any) (url string, named bool) {
	for _, member := range []string{"import_data_source", "export_destination"} {
		if m, ok := args[member].(map[string]any); ok {
			url, named = m["s3_url"].(string)
			return url, named
		}
	}
	return "", false
}

// sesJobBucket returns the bucket of an S3 URL of the form s3://BUCKET/KEY.
func sesJobBucket(url string) (string, bool) {
	rest, ok := strings.CutPrefix(url, "s3://")
	bucket, key, found := strings.Cut(rest, "/")
	if !ok || !found || key == "" || len(url) > maxS3URL || !httpbind.ValidBucketName(bucket) {
		return "", false
	}
	return bucket, true
}

// checkSESJobURL holds the S3 URL of a job to its form without any I/O. The request is refused without quoting
// the value.
func checkSESJobURL(_ binding, _ string, args map[string]any) error {
	url, named := sesJobURL(args)
	_, valid := sesJobBucket(url)
	_, isImport := args["import_data_source"]
	if (named && !valid) || (isImport && !named) {
		return &provider.InvalidRequestError{Message: sesJobRule}
	}
	return nil
}

// sesJobOwner confirms that the bucket of the job is a bucket of the bound account in an allowed region before
// anything is sent to SES. An export without a bucket has nothing to confirm.
func sesJobOwner(ctx context.Context, c *Client, s toolSpec, creds sigv4.Credentials, region string, args map[string]any) error {
	url, named := sesJobURL(args)
	if !named {
		return nil
	}
	bucket, ok := sesJobBucket(url)
	if !ok {
		return &provider.InvalidRequestError{Message: sesJobRule}
	}
	return bucketOwned(ctx, c, s.op, creds, region, bucket, "bucket of the job")
}

package awserr

import (
	"net/http"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func TestClassifyXML(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		code   string
		class  provider.Class
	}{
		{"no such bucket", 404, "NoSuchBucket", provider.ClassNotFound},
		{"no such hosted zone", 404, "NoSuchHostedZone", provider.ClassNotFound},
		{"no bucket encryption", 404, "ServerSideEncryptionConfigurationNotFoundError", provider.ClassNotFound},
		{"no bucket tags", 404, "NoSuchTagSet", provider.ClassNotFound},
		{"no public access block", 404, "NoSuchPublicAccessBlockConfiguration", provider.ClassNotFound},
		{"no lifecycle configuration", 404, "NoSuchLifecycleConfiguration", provider.ClassNotFound},
		{"no cors configuration", 404, "NoSuchCORSConfiguration", provider.ClassNotFound},
		{"no website configuration", 404, "NoSuchWebsiteConfiguration", provider.ClassNotFound},
		{"no replication configuration", 404, "ReplicationConfigurationNotFoundError", provider.ClassNotFound},
		{"no ownership controls", 404, "OwnershipControlsNotFoundError", provider.ClassNotFound},
		{"bucket not empty", 409, "BucketNotEmpty", provider.ClassProviderError},
		{"bucket operation aborted", 409, "OperationAborted", provider.ClassProviderError},
		{"too many buckets", 400, "TooManyBuckets", provider.ClassProviderError},
		{"invalid bucket name", 400, "InvalidBucketName", provider.ClassProviderError},
		{"malformed xml", 400, "MalformedXML", provider.ClassProviderError},
		{"failed precondition", 412, "PreconditionFailed", provider.ClassProviderError},
		{"conditional request conflict", 409, "ConditionalRequestConflict", provider.ClassProviderError},
		{"restore in progress", 409, "RestoreAlreadyInProgress", provider.ClassProviderError},
		{"no such version", 404, "NoSuchVersion", provider.ClassNotFound},
		{"invalid range", 416, "InvalidRange", provider.ClassProviderError},
		{"method not allowed", 405, "MethodNotAllowed", provider.ClassProviderError},
		{"404 without code", 404, "", provider.ClassNotFound},
		{"404 with an unknown code is not guessed", 404, "NoSuchNothing", provider.ClassInvalidResponse},
		{"slow down at 503", 503, "SlowDown", provider.ClassRateLimited},
		{"slow down at 400 stays unknown", 400, "SlowDown", provider.ClassInvalidResponse},
		{"prior request not complete", 400, "PriorRequestNotComplete", provider.ClassRateLimited},
		{"throttling", 400, "Throttling", provider.ClassRateLimited},
		{"invalid change batch", 400, "InvalidChangeBatch", provider.ClassProviderError},
		{"access denied", 403, "AccessDenied", provider.ClassPermission},
		{"archived object with 403", 403, "InvalidObjectState", provider.ClassProviderError},
		{"unknown with 403", 403, "Whatever", provider.ClassPermission},
		{"too many hosted zones", 400, "TooManyHostedZones", provider.ClassProviderError},
		{"not authorized", 401, "NotAuthorizedException", provider.ClassPermission},
		{"server", 500, "InternalError", provider.ClassProviderError},
		{"no code at 400", 400, "", provider.ClassProviderError},
		// An Error element in a 2xx answer.
		{"in success, known", 200, "NoSuchKey", provider.ClassNotFound},
		{"in success, denied", 200, "AccessDenied", provider.ClassPermission},
		{"in success, unknown", 200, "Odd", provider.ClassProviderError},
		{"in success, no code", 200, "", provider.ClassProviderError},
		{"in success, slow down is not a rate limit", 200, "SlowDown", provider.ClassProviderError},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, _ := ClassifyXML("aws.test", c.status, http.Header{}, c.code, "Sender")
			if got.Class != c.class || got.Original == nil || got.Original.Status != c.status {
				t.Fatalf("got %s %+v, want %s", got.Class, got.Original, c.class)
			}
			if c.code != "" && (len(got.Original.Entries) != 1 || got.Original.Entries[0].Code != c.code || got.Original.Entries[0].Type != "Sender") {
				t.Errorf("code and type are not kept: %+v", got.Original)
			}
		})
	}
}

// The rows of the restXml services do not change what the other protocols answer.
func TestSharedTableIsUnchangedByXMLRows(t *testing.T) {
	for _, code := range []string{"NoSuchBucket", "InvalidChangeBatch", "PriorRequestNotComplete"} {
		if got, _ := ClassifyStrict("aws.test", 400, http.Header{}, code, ""); got.Class != provider.ClassInvalidResponse {
			t.Errorf("%s is known to the shared table: %s", code, got.Class)
		}
	}
	if !Defines("NoSuchBucket") || !Defines("AccessDenied") || Defines("SlowDown") || Defines("Nothing") {
		t.Error("Defines is wrong")
	}
}

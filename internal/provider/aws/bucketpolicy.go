package aws

import (
	"fmt"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// maxBucketPolicyBytes is the size AWS documents as the limit of a bucket policy, 20 KB. The model gives the
// member no bound; the limit is checked in bytes, the narrower measure, before a call.
const maxBucketPolicyBytes = 20 * 1024

// checkBucketPolicy admits a bucket policy only when every statement, whatever its effect, names principals of
// the bound account or AWS service principals that a condition ties to the bound account; a wildcard
// principal is refused in a Deny statement too. It is the check of a resource policy with the size limit of
// S3 and StringLike as one more operator of the source conditions.
func checkBucketPolicy(b binding, name, value string) error {
	if len(value) > maxBucketPolicyBytes {
		return &provider.InvalidRequestError{Message: name + ": " +
			fmt.Sprintf("the policy document is longer than %d bytes", maxBucketPolicyBytes)}
	}
	return checkOwnAccountDocument(b, name, value, true)
}

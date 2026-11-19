package googlemerchant

import (
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

// Reasons of a 429 answer: the rolling per-minute limit and the daily limit of a method group.
const (
	reasonMinuteQuota = "QUOTA_REQUEST_RATE_TOO_HIGH"
	reasonDayQuota    = "QUOTA_TOO_MANY_REQUESTS"
)

// googleError maps a failed answer of the Merchant API to a class. 401, 403, and 429 have wording of their
// own, the other statuses use the standard classification. Google's message and details never reach the
// message; they go along as the original.
func googleError(op string, status int, body []byte) *provider.Error {
	var mapped *provider.Error
	switch status {
	case http.StatusUnauthorized:
		mapped = &provider.Error{Class: provider.ClassAuth, Op: op,
			Message: "Google rejected the access token of the credential"}
	case http.StatusForbidden:
		mapped = &provider.Error{Class: provider.ClassPermission, Op: op,
			Message: "Google refused the request: the user or service account of the credential has no access " +
				"to this Merchant Center account or lacks the role the method needs, the Merchant API is not " +
				"enabled in the Cloud project, or the Cloud project is not registered as a developer"}
	case http.StatusTooManyRequests:
		mapped = &provider.Error{Class: provider.ClassRateLimited, Op: op, Message: rateLimitMessage(body)}
	default:
		mapped = provider.ClassifyStatus(op, status, provider.StatusTexts{Subject: subject,
			NotFound: "Google did not find the Merchant Center account or resource, or does not show it to this credential"})
	}
	mapped.Original = provider.ErrorOriginal(status, body, googleapi.Fields)
	return mapped
}

func rateLimitMessage(body []byte) string {
	_, reason := googleapi.StatusOf(body)
	switch reason {
	case reasonMinuteQuota:
		return "Google is rate-limiting the Merchant API: the per-minute limit of this method group is exceeded; " +
			"wait a minute and try again"
	case reasonDayQuota:
		return "Google is rate-limiting the Merchant API: the daily limit of this method group is used up and resets " +
			"at 12:00 UTC; the limit belongs to the Merchant Center account, or to the advanced account for its sub-accounts"
	}
	return "Google is rate-limiting the Merchant API; wait and try again"
}

package googletagmanager

import (
	"fmt"
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

// googleError maps a failed answer of the Tag Manager API to a class. Only the answers observed from the API
// are mapped: 401 (CREDENTIALS_MISSING), 403 (PERMISSION_DENIED), and 429 (RATE_LIMIT_EXCEEDED). Every other
// status is not established for this API and stays invalid-provider-response instead of a guessed class.
// Google's message and details never reach the message; they go along as the original.
func googleError(op string, status int, body []byte) *provider.Error {
	var mapped *provider.Error
	switch status {
	case http.StatusUnauthorized:
		mapped = &provider.Error{Class: provider.ClassAuth, Op: op,
			Message: "Google rejected the access token of the credential"}
	case http.StatusForbidden:
		mapped = &provider.Error{Class: provider.ClassPermission, Op: op,
			Message: "Google refused the request: the account of the credential has no access to this Tag Manager " +
				"account or container, or the Tag Manager API is not enabled in the Cloud project of the OAuth client"}
	case http.StatusTooManyRequests:
		mapped = &provider.Error{Class: provider.ClassRateLimited, Op: op,
			Message: "Google is rate-limiting the Cloud project of the OAuth client; wait and try again"}
	default:
		mapped = provider.InvalidResponse(op, fmt.Sprintf("Google answered with HTTP %d, which is not mapped for "+
			"the Tag Manager API; the original carries Google's status", status))
	}
	mapped.Original = provider.ErrorOriginal(status, body, googleapi.Fields)
	return mapped
}

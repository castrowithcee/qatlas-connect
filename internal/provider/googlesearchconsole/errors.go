package googlesearchconsole

import (
	"net/http"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

// failure is a provider error of the generic class.
func failure(op, message string) *provider.Error {
	return &provider.Error{Class: provider.ClassProviderError, Op: op, Message: message}
}

// badRequest is the refusal of an argument. It names the argument and the rule, never the value.
func badRequest(message string) error {
	return &provider.InvalidRequestError{Message: message}
}

const (
	notFoundSite = "Google did not find the property for this credential; check site_url and that the " +
		"account has access to the property"
	notFoundSitemap = "Google did not find the sitemap in this property; check feedpath and site_url"
	permissionText  = "Google refused the request; check that the account of the credential has access to " +
		"the property and that the Search Console API is enabled in the Cloud project of the OAuth client or " +
		"service account"
	authText = "Google did not accept the access token of the credential, also after renewing it once; " +
		"check the credential and give the consent again if needed"
)

// googleError maps a failed Google answer to a provider error. Only values seen in Google's answers decide
// beyond the HTTP status: 401 with CREDENTIALS_MISSING or UNAUTHENTICATED is auth, 403 with
// PERMISSION_DENIED is permission, and 429 with RATE_LIMIT_EXCEEDED or RESOURCE_EXHAUSTED is rate-limited.
// Any other value does not choose a class: the HTTP status decides, and a 400 is invalid-provider-response
// because Qatlas checked the arguments before sending. The message and details of the body never reach the
// message; they go along as the Original.
func googleError(op string, status int, body []byte, notFound string) *provider.Error {
	failed := func(class provider.Class, message string) *provider.Error {
		return &provider.Error{Class: class, Op: op, Message: message,
			Original: provider.ErrorOriginal(status, body, googleapi.Fields)}
	}
	state, reason := googleapi.StatusOf(body)
	switch {
	case status == http.StatusUnauthorized && (reason == "CREDENTIALS_MISSING" || state == "UNAUTHENTICATED"):
		return failed(provider.ClassAuth, authText)
	case status == http.StatusForbidden && state == "PERMISSION_DENIED":
		return failed(provider.ClassPermission, permissionText)
	case status == http.StatusTooManyRequests && (reason == "RATE_LIMIT_EXCEEDED" || state == "RESOURCE_EXHAUSTED"):
		return failed(provider.ClassRateLimited, "Google rate-limited the account or the Cloud project; wait and try again")
	case status == http.StatusBadRequest:
		return failed(provider.ClassInvalidResponse, "Google rejected the request as invalid (HTTP 400) although "+
			"Qatlas Connect checked it; the API may have changed")
	}
	mapped := provider.ClassifyStatus(op, status, provider.StatusTexts{
		Subject:    "Google",
		Auth:       authText,
		Permission: permissionText,
		NotFound:   notFound,
	})
	mapped.Original = provider.ErrorOriginal(status, body, googleapi.Fields)
	return mapped
}

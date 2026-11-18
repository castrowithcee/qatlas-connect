package googlebusinessprofile

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

func invalidArgument(message string) error { return &provider.InvalidRequestError{Message: message} }

const (
	notFoundAccount = "Google did not find the account for this credential; check the account and that the " +
		"consenting account manages it"
	notFoundLocation = "Google did not find the location for this credential; check the location and that the " +
		"consenting account manages it"
	permissionText = "Google refused the request; check that the API is enabled in the Cloud project of the " +
		"OAuth client and that the consenting account has a role on the account or location"
	authText = "Google did not accept the access token of the credential, also after renewing it once; " +
		"check the credential and give the consent again if needed"
	notApprovedText = "Google grants this Cloud project no requests to the Business Profile APIs: the project is " +
		"not approved for them yet, or the approval is pending; request Basic API Access for the project of the " +
		"OAuth client and try again once Google has granted it"
)

// googleError maps a failed Google answer to a provider error. Beyond the HTTP status only values seen in
// Google's answers decide: 401 with CREDENTIALS_MISSING or UNAUTHENTICATED is auth, 403 with
// PERMISSION_DENIED is permission, and 429 with RATE_LIMIT_EXCEEDED or RESOURCE_EXHAUSTED is rate-limited. A
// rate limit whose quota is zero, or any rate limit on the first request of a connection test, is the
// missing approval of the Cloud project. A 400 is invalid-provider-response because every argument was
// checked before sending. The message and details of the body never reach the message; they go along as
// the Original.
func googleError(op string, status int, body []byte, notFound string, first bool) *provider.Error {
	failed := func(class provider.Class, message string) *provider.Error {
		return &provider.Error{Class: class, Op: op, Message: message,
			Original: provider.ErrorOriginal(status, body, googleapi.Fields)}
	}
	state, reason := googleapi.StatusOf(body)
	switch {
	case status == http.StatusTooManyRequests && (first || zeroQuota(body)):
		return failed(ClassAccessNotApproved, notApprovedText)
	case status == http.StatusUnauthorized && (reason == "CREDENTIALS_MISSING" || state == "UNAUTHENTICATED"):
		return failed(provider.ClassAuth, authText)
	case status == http.StatusForbidden && state == "PERMISSION_DENIED":
		return failed(provider.ClassPermission, permissionText)
	case status == http.StatusTooManyRequests && (reason == "RATE_LIMIT_EXCEEDED" || state == "RESOURCE_EXHAUSTED"):
		return failed(provider.ClassRateLimited, "Google rate-limited the Cloud project or the location; wait and try again")
	case status == http.StatusBadRequest:
		return failed(provider.ClassInvalidResponse, "Google rejected the request as invalid (HTTP 400) although "+
			"Qatlas Connect checked it; the API may have changed")
	}
	mapped := provider.ClassifyStatus(op, status, provider.StatusTexts{
		Subject:    subject,
		Auth:       authText,
		Permission: permissionText,
		NotFound:   notFound,
	})
	mapped.Original = provider.ErrorOriginal(status, body, googleapi.Fields)
	return mapped
}

// zeroQuota reports whether a Google error names a quota limit of zero in the metadata of an ErrorInfo
// detail, which is the quota of a project that is not approved. The value is only compared, never shown.
func zeroQuota(body []byte) bool {
	var answer struct {
		Error struct {
			Details []struct {
				Type     string         `json:"@type"`
				Metadata map[string]any `json:"metadata"`
			} `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil {
		return false
	}
	for _, detail := range answer.Error.Details {
		if strings.HasSuffix(detail.Type, "google.rpc.ErrorInfo") && detail.Metadata["quota_limit_value"] == any("0") {
			return true
		}
	}
	return false
}

package googleads

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
)

// apiName names the API for the message of a disabled API. It is fixed here and never read from an answer.
const apiName = "the Google Ads API (googleads.googleapis.com)"

// failure is a provider error of the generic class.
func failure(op, message string) *provider.Error {
	return &provider.Error{Class: provider.ClassProviderError, Op: op, Message: message}
}

// badRequest is the refusal of an argument. It names the argument and the rule, never the value.
func badRequest(message string) error {
	return &provider.InvalidRequestError{Message: message}
}

// failureWire is the REST form of a failed Google Ads call: the Google error object whose details carry a
// GoogleAdsFailure with the errors and the request ID.
type failureWire struct {
	Error struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		Details []struct {
			Type      string      `json:"@type"`
			Errors    []errorWire `json:"errors"`
			RequestID string      `json:"requestId"`
		} `json:"details"`
	} `json:"error"`
}

// errorWire is one GoogleAdsError. errorCode holds exactly one member: the category, such as
// authorizationError, with the name of its enum value. trigger is left out, because it echoes request values.
type errorWire struct {
	ErrorCode map[string]json.RawMessage `json:"errorCode"`
	Message   string                     `json:"message"`
	Location  struct {
		FieldPathElements []struct {
			FieldName string `json:"fieldName"`
			Index     *int   `json:"index"`
		} `json:"fieldPathElements"`
	} `json:"location"`
	Details struct {
		QuotaErrorDetails *struct {
			RetryDelay string `json:"retryDelay"`
		} `json:"quotaErrorDetails"`
	} `json:"details"`
}

const failureTypePrefix = "type.googleapis.com/google.ads.googleads."
const failureTypeSuffix = ".errors.GoogleAdsFailure"

var (
	categoryPattern  = regexp.MustCompile(`^[a-z][A-Za-z]{0,63}$`)
	codePattern      = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,99}$`)
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// adsCode is one error of a GoogleAdsFailure: its category and the name of its enum value.
type adsCode struct{ category, value string }

// mapped is what Qatlas makes of a documented error code: the class and the guidance of the message.
type mapped struct {
	class   provider.Class
	message string
}

const (
	tokenRefused  = "did not accept the access token of the credential"
	rateGuidance  = "wait and try again"
	refusedPrefix = "refused the request"
)

// knownCodes are the error codes the reference of the API documents for the calls of this provider and that
// decide more than the HTTP status does. Only these names reach a message; every other code is provider
// text and goes along in the original only.
var knownCodes = map[adsCode]mapped{
	{"authenticationError", "AUTHENTICATION_ERROR"}:                      {provider.ClassAuth, tokenRefused},
	{"authenticationError", "OAUTH_TOKEN_EXPIRED"}:                       {provider.ClassAuth, tokenRefused},
	{"authenticationError", "OAUTH_TOKEN_INVALID"}:                       {provider.ClassAuth, tokenRefused},
	{"authenticationError", "OAUTH_TOKEN_REVOKED"}:                       {provider.ClassAuth, tokenRefused},
	{"authenticationError", "OAUTH_TOKEN_DISABLED"}:                      {provider.ClassAuth, tokenRefused},
	{"authenticationError", "OAUTH_TOKEN_HEADER_INVALID"}:                {provider.ClassAuth, tokenRefused},
	{"authenticationError", "GOOGLE_ACCOUNT_AUTHENTICATION_FAILED"}:      {provider.ClassAuth, tokenRefused},
	{"authenticationError", "GOOGLE_ACCOUNT_DELETED"}:                    {provider.ClassAuth, tokenRefused},
	{"authenticationError", "GOOGLE_ACCOUNT_USER_AND_ADS_USER_MISMATCH"}: {provider.ClassAuth, tokenRefused},
	{"authenticationError", "NOT_ADS_USER"}: {provider.ClassAuth, "refused the credential because its Google " +
		"account has access to no Google Ads account; a person adds the account or service account as a user of an account"},
	{"authenticationError", "TWO_STEP_VERIFICATION_NOT_ENROLLED"}: {provider.ClassAuth, "refused the credential " +
		"because the account requires 2-Step Verification on the Google account of the credential"},
	{"authenticationError", "ADVANCED_PROTECTION_NOT_ENROLLED"}: {provider.ClassAuth, "refused the credential " +
		"because the account requires Advanced Protection on the Google account of the credential"},
	{"authenticationError", "CUSTOMER_NOT_FOUND"}: {provider.ClassNotFound, "found no account with this customer ID"},
	{"authenticationError", "ORGANIZATION_NOT_APPROVED"}: {provider.ClassPermission, refusedPrefix + " because the " +
		"Cloud organization of the project of the credential is not approved for production access"},
	{"authenticationError", "ORGANIZATION_NOT_RECOGNIZED"}: {provider.ClassPermission, refusedPrefix + " because " +
		"it does not recognize the Cloud organization of the project of the credential"},
	{"authorizationError", "USER_PERMISSION_DENIED"}: {provider.ClassPermission, refusedPrefix + " because the " +
		"credential may not access this account; if the access runs through a manager account, the service option " +
		optionLoginCustomerID + " must name that manager"},
	{"authorizationError", "CLOUD_PROJECT_NOT_APPROVED_FOR_PRODUCTION"}: {provider.ClassPermission, refusedPrefix +
		" because the Cloud project of the credential has test access, which reaches test accounts only; a person " +
		"applies for Explorer, Basic or Standard access on the Google Ads API page of the Google Cloud console"},
	{"authorizationError", "CLOUD_PROJECT_NOT_UNDER_ORGANIZATION"}: {provider.ClassPermission, refusedPrefix +
		" because the Cloud project of the credential is not under the organization Google requires"},
	{"authorizationError", "CUSTOMER_NOT_ENABLED"}: {provider.ClassPermission, refusedPrefix + " because the " +
		"account is not enabled yet or was deactivated"},
	{"authorizationError", "INVALID_LOGIN_CUSTOMER_ID_SERVING_CUSTOMER_ID_COMBINATION"}: {provider.ClassPermission,
		refusedPrefix + " because the manager account of the service option " + optionLoginCustomerID +
			" has no access to this account"},
	{"authorizationError", "PROJECT_DISABLED"}: {provider.ClassPermission, refusedPrefix + " because the Cloud " +
		"project of the credential may not use the Google Ads API"},
	{"authorizationError", "MISSING_TOS"}: {provider.ClassPermission, refusedPrefix + " because the terms of " +
		"service of the Google Ads API are not accepted on its page in the Google Cloud console"},
	{"authorizationError", "ACTION_NOT_PERMITTED"}:                       {provider.ClassPermission, refusedPrefix},
	{"authorizationError", "ACTION_NOT_PERMITTED_FOR_SUSPENDED_ACCOUNT"}: {provider.ClassPermission, refusedPrefix},
	{"authorizationError", "ACCESS_DENIED_FOR_ACCOUNT_TYPE"}:             {provider.ClassPermission, refusedPrefix},
	{"authorizationError", "SERVICE_ACCESS_DENIED"}:                      {provider.ClassPermission, refusedPrefix},
	{"authorizationError", "METRIC_ACCESS_DENIED"}:                       {provider.ClassPermission, refusedPrefix},
	{"authorizationError", "INCOMPLETE_SIGNUP"}:                          {provider.ClassPermission, refusedPrefix},
	{"authorizationError", "AUTHORIZATION_ERROR"}:                        {provider.ClassPermission, refusedPrefix},
	{"quotaError", "RESOURCE_TEMPORARILY_EXHAUSTED"}: {provider.ClassRateLimited, "received too many requests " +
		"in a short time; " + rateGuidance},
	{"quotaError", "RESOURCE_EXHAUSTED"}: {provider.ClassRateLimited, "counted more requests or operations than " +
		"the access level of the Cloud project allows; " + rateGuidance},
	{"quotaError", "EXCESSIVE_SHORT_TERM_QUERY_RESOURCE_CONSUMPTION"}: {provider.ClassRateLimited, "counted too " +
		"many resource-intensive queries in a short time; " + rateGuidance},
	{"quotaError", "EXCESSIVE_LONG_TERM_QUERY_RESOURCE_CONSUMPTION"}: {provider.ClassRateLimited, "counted too " +
		"many resource-intensive queries over a long time; " + rateGuidance},
	{"quotaError", "ACCESS_PROHIBITED"}: {provider.ClassPermission, refusedPrefix + " because access to the resource is not permitted"},
	{"budgetNetworkSettingError", "CUSTOMER_NOT_ALLOWLISTED"}: {provider.ClassPermission, refusedPrefix + " because " +
		"the account is not allowlisted for budget network settings"},
	{"headerError", "INVALID_LOGIN_CUSTOMER_ID"}: {provider.ClassProviderError, refusedPrefix + " because the " +
		"service option " + optionLoginCustomerID + " names no account Google accepts there"},
}

// adsError maps a failed answer to a provider error, and to the wait Google asked for before the next request
// when it says so. A documented error code of a GoogleAdsFailure decides first; an answer without one is the
// plain Google error object, mapped by its status and the reason of its ErrorInfo. Google's text never
// reaches the message; it goes along as the original.
func adsError(op string, status int, body []byte, change bool) (*provider.Error, time.Duration) {
	return adsErrorFor(op, status, body, change, false)
}

// adsErrorFor is adsError for a call whose arguments Google judges: with input set, a 400 with a documented
// error code of a read names the code as Google's refusal of the input, as it does for a change.
func adsErrorFor(op string, status int, body []byte, change, input bool) (*provider.Error, time.Duration) {
	var answer failureWire
	_ = json.Unmarshal(body, &answer)
	var codes []adsCode
	var paths, messages []string
	var hold time.Duration
	requestID := ""
	adsFailure := false
	for _, detail := range answer.Error.Details {
		if !strings.HasPrefix(detail.Type, failureTypePrefix) || !strings.HasSuffix(detail.Type, failureTypeSuffix) {
			continue
		}
		adsFailure = true
		if requestIDPattern.MatchString(detail.RequestID) {
			requestID = detail.RequestID
		}
		for _, e := range detail.Errors {
			code := codeOf(e.ErrorCode)
			codes, messages, paths = append(codes, code), append(messages, e.Message), append(paths, pathOf(e))
			if quota := e.Details.QuotaErrorDetails; quota != nil {
				if delay, err := time.ParseDuration(quota.RetryDelay); err == nil && delay > 0 {
					hold = max(hold, delay)
				}
			}
		}
	}
	original := provider.ErrorOriginal(status, body, googleapi.Fields)
	if adsFailure {
		original = &provider.Original{Status: status}
		if answer.Error.Status != "" || answer.Error.Message != "" {
			original.Entries = append(original.Entries, provider.OriginalEntry{Code: answer.Error.Status,
				Message: answer.Error.Message})
		}
		for i, code := range codes {
			original.Entries = append(original.Entries, provider.OriginalEntry{Type: code.category, Code: code.value,
				Path: paths[i], Message: messages[i]})
		}
		if requestID != "" {
			original.Entries = append(original.Entries, provider.OriginalEntry{Type: "requestId", Code: requestID})
		}
	}
	failed := func(class provider.Class, message string) *provider.Error {
		return &provider.Error{Class: class, Op: op, Message: subject + " " + message, Original: original}
	}
	for _, code := range codes {
		if known, ok := knownCodes[code]; ok {
			message := known.message
			if known.message == refusedPrefix {
				message += " (" + code.category + " " + code.value + ")"
			}
			return failed(known.class, message), hold
		}
		if code.category == "notAllowlistedError" {
			return failed(provider.ClassPermission, refusedPrefix+" because the feature is not allowlisted for this "+
				"account or the Cloud project of the credential"), hold
		}
	}
	if change && adsFailure && status == http.StatusBadRequest && len(codes) > 0 && codes[0] != (adsCode{}) {
		// Google validated the change and refused it: the code names the rule, the original carries the field.
		return failed(provider.ClassProviderError, "rejected the change ("+codes[0].category+" "+codes[0].value+
			"), nothing was changed"), hold
	}
	if input && !change && adsFailure && status == http.StatusBadRequest && len(codes) > 0 && codes[0] != (adsCode{}) {
		// Google judged the input of a read and refused it: the code names the rule, the original carries the field.
		return failed(provider.ClassProviderError, "rejected the request as invalid input ("+codes[0].category+" "+
			codes[0].value+")"), hold
	}
	return statusFailure(op, status, body, original, failed), hold
}

// statusFailure maps an answer without a documented error code by its HTTP status. The status string of the
// Google error object is a google.rpc.Code and the reason of its ErrorInfo comes from google.api.ErrorReason.
func statusFailure(op string, status int, body []byte, original *provider.Original,
	failed func(provider.Class, string) *provider.Error) *provider.Error {
	state, reason := googleapi.StatusOf(body)
	switch status {
	case http.StatusBadRequest:
		return failed(provider.ClassInvalidResponse, "rejected the request as invalid (HTTP 400) although Qatlas "+
			"Connect built it; the API may have changed")
	case http.StatusUnauthorized:
		return failed(provider.ClassAuth, tokenRefused)
	case http.StatusForbidden:
		switch {
		case reason == "SERVICE_DISABLED":
			return failed(provider.ClassPermission, refusedPrefix+" because "+apiName+" is not enabled in the "+
				"Cloud project of the credential; a person enables it there")
		case reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT":
			return &provider.Error{Class: provider.ClassScopeMissing, Op: op, Original: original,
				Message: "the access token of the credential lacks the scope this tool needs"}
		}
		return failed(provider.ClassPermission, refusedPrefix)
	case http.StatusNotFound:
		if state == "NOT_FOUND" {
			return failed(provider.ClassNotFound, "did not find the resource, or the credential may not see it")
		}
	case http.StatusTooManyRequests:
		return failed(provider.ClassRateLimited, "is rate-limiting requests or the request was too large; "+rateGuidance)
	}
	if status >= 400 && status < 500 {
		return failed(provider.ClassInvalidResponse, "answered with HTTP "+strconv.Itoa(status)+
			" and an error that Qatlas Connect does not know")
	}
	result := provider.ClassifyStatus(op, status, provider.StatusTexts{Subject: subject})
	result.Original = original
	return result
}

// codeOf reads the one member of an errorCode. A member that is not a category with the name of an enum value
// yields an empty code, which matches nothing.
func codeOf(members map[string]json.RawMessage) adsCode {
	for category, raw := range members {
		var value string
		if json.Unmarshal(raw, &value) == nil && categoryPattern.MatchString(category) && codePattern.MatchString(value) {
			return adsCode{category: category, value: value}
		}
	}
	return adsCode{}
}

// pathOf joins the field path of an error, such as operations[0].create.name.
func pathOf(e errorWire) string {
	var b strings.Builder
	for i, element := range e.Location.FieldPathElements {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(element.FieldName)
		if element.Index != nil {
			b.WriteString("[" + strconv.Itoa(*element.Index) + "]")
		}
	}
	return b.String()
}

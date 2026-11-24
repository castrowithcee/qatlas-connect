package googlesearchconsole

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	indexingMetadataID      = Provider + ".indexing.get-metadata"
	indexingMetadataBatchID = Provider + ".indexing.get-metadata-batch"
	indexingPublishID       = Provider + ".indexing.publish"
	indexingPublishBatchID  = Provider + ".indexing.publish-batch"
)

// scopeIndexing is the only scope the discovery document of the Indexing API names, for reading and
// publishing alike.
const scopeIndexing = "https://www.googleapis.com/auth/indexing"

// The Indexing API has its own fixed origin; the paths come from its discovery document (v3, revision
// 20261107) and the batch path from Google's Indexing API guide.
const (
	indexingRoot = "https://indexing.googleapis.com"
	metadataPath = "/v3/urlNotifications/metadata"
	publishPath  = "/v3/urlNotifications:publish"
	batchPath    = "/batch"
)

// maxBatch is the number of calls Google combines into one batch.
const maxBatch = 100

// Google counts the Indexing API per Cloud project: 180 metadata reads and 380 requests of any method per
// minute, and a batch counts once per entry. The spacing below keeps one credential of this process within
// those per-minute quotas. The daily quota of notifications is counted apart, in indexing_quota.go.
const (
	indexingInterval = 158 * time.Millisecond // 60 s / 380
	metadataInterval = 334 * time.Millisecond // 60 s / 180
)

var (
	// indexingLimiters space every Indexing API request of one credential.
	indexingLimiters = ratelimit.NewRegistry(indexingInterval)
	// metadataLimiters space the metadata reads of one credential, which Google counts apart as well.
	metadataLimiters = ratelimit.NewRegistry(metadataInterval)
)

const (
	urlUpdated = "URL_UPDATED"
	urlDeleted = "URL_DELETED"
)

var notificationTypes = []string{urlUpdated, urlDeleted}

const (
	indexingPermissionText = "Google refused the request; check that the account of the credential is an owner of " +
		"the property of the URL in Search Console and that the Indexing API is enabled in the Cloud project of the " +
		"OAuth client or service account"
	notFoundIndexing = "Google did not find the Indexing API method; the API may have changed"
)

// The Indexing API as its discovery document documents it. The answer types bound their texts like those of
// the Search Console API.
type (
	urlNotification struct {
		URL        text  `json:"url,omitempty"`
		Type       short `json:"type,omitempty"`
		NotifyTime short `json:"notifyTime,omitempty"`
	}
	notificationMetadata struct {
		URL          text             `json:"url,omitempty"`
		LatestUpdate *urlNotification `json:"latestUpdate,omitempty"`
		LatestRemove *urlNotification `json:"latestRemove,omitempty"`
	}
	publishAnswer struct {
		URLNotificationMetadata *notificationMetadata `json:"urlNotificationMetadata,omitempty"`
	}
	// entryError is the failure of one entry of a batch, in Qatlas' own words; Google's text stays out.
	entryError struct {
		Class   provider.Class `json:"class"`
		Message string         `json:"message"`
		Status  int            `json:"status,omitempty"`
	}
	metadataEntry struct {
		URL      string                `json:"url"`
		Metadata *notificationMetadata `json:"metadata,omitempty"`
		Error    *entryError           `json:"error,omitempty"`
	}
	metadataBatchResult struct {
		Entries   []metadataEntry `json:"entries"`
		Succeeded int             `json:"succeeded"`
		Failed    int             `json:"failed"`
	}
	publishEntry struct {
		URL                     string                `json:"url"`
		Type                    string                `json:"type"`
		URLNotificationMetadata *notificationMetadata `json:"urlNotificationMetadata,omitempty"`
		Error                   *entryError           `json:"error,omitempty"`
	}
	publishBatchResult struct {
		Entries   []publishEntry `json:"entries"`
		Succeeded int            `json:"succeeded"`
		Failed    int            `json:"failed"`
	}
)

const (
	notificationSchema = `{"type":"object","properties":{"url":{"type":"string"},"type":{"type":"string"},` +
		`"notifyTime":{"type":"string"}},"additionalProperties":false}`
	metadataSchema = `{"type":"object","properties":{"url":{"type":"string"},"latestUpdate":` + notificationSchema +
		`,"latestRemove":` + notificationSchema + `},"additionalProperties":false}`
	publishSchema = `{"type":"object","properties":{"urlNotificationMetadata":` + metadataSchema +
		`},"additionalProperties":false}`
	entryErrorSchema = `{"type":"object","properties":{"class":{"type":"string"},"message":{"type":"string"},` +
		`"status":{"type":"integer"}},"required":["class","message"],"additionalProperties":false}`
	metadataBatchSchema = `{"type":"object","properties":{"entries":{"type":"array","items":{"type":"object",` +
		`"properties":{"url":{"type":"string"},"metadata":` + metadataSchema + `,"error":` + entryErrorSchema +
		`},"required":["url"],"additionalProperties":false}},"succeeded":{"type":"integer"},"failed":{"type":"integer"}},` +
		`"required":["entries","succeeded","failed"],"additionalProperties":false}`
	publishBatchSchema = `{"type":"object","properties":{"entries":{"type":"array","items":{"type":"object",` +
		`"properties":{"url":{"type":"string"},"type":{"type":"string"},"urlNotificationMetadata":` + metadataSchema +
		`,"error":` + entryErrorSchema + `},"required":["url","type"],"additionalProperties":false}},` +
		`"succeeded":{"type":"integer"},"failed":{"type":"integer"}},"required":["entries","succeeded","failed"],` +
		`"additionalProperties":false}`
)

var (
	notificationTypeSchema = enumSchema(notificationTypes)
	urlsSchema             = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxBatch) + `,"items":` +
		sitemapAddressSchema + `}`
	notificationsSchema = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxBatch) +
		`,"items":{"type":"object","properties":{"url":` + sitemapAddressSchema + `,"type":` + notificationTypeSchema +
		`},"required":["url","type"],"additionalProperties":false}}`
)

const (
	// indexingUse is the limit Google sets on the Indexing API; Qatlas cannot check it.
	indexingUse = "Google accepts the Indexing API only for URLs whose content carries JobPosting or " +
		"BroadcastEvent (embedded in a VideoObject) structured data; Qatlas does not check that, and keeping to " +
		"it is up to the user. "
	indexingBound = "A connection bound to a property refuses a URL outside it before anything is sent; the URL " +
		"is never fetched. "
	publishEffect = "The notification takes effect at Google and cannot be withdrawn: only a URL_DELETED " +
		"notification counters a URL_UPDATED one. Before URL_DELETED the URL must answer 404 or 410 or carry a " +
		"noindex robots meta tag. Not idempotent: every call is a new notification that Google counts, and a " +
		"failed call is never repeated. Google allows 200 notifications per day per Cloud project by default; " +
		"Qatlas counts the notifications it sends per credential, a batch once per entry, and refuses a call as " +
		"rate-limited before sending when it would pass 200 in the quota day, which ends at midnight Pacific " +
		"Time. Google's own count stays authoritative. "
	publishListed = "Needs confirmation and an entry in the tools list of the connection. "
)

var metadataFields = []capability.Field{
	{Name: "url", Description: "the URL the metadata refers to"},
	{Name: "latestUpdate", Description: "the latest URL_UPDATED notification Google received: url, type, notifyTime (RFC 3339)"},
	{Name: "latestRemove", Description: "the latest URL_DELETED notification Google received: url, type, notifyTime (RFC 3339)"},
}

var urlArgument = capability.Argument{Name: "url", Required: true,
	Description: "Absolute http or https URL of the content, of at most 2048 characters, without user information " +
		"or fragment, such as https://www.example.com/jobs/42"}

// indexingRisk is the risk of the metadata reads: they read what Google received and change nothing.
var indexingRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: "URLs of the account's websites and the times Google received Indexing API notifications for them",
}

// publishRisk is the risk of a notification: it acts at Google, outside the account, and every call is a new
// notification that counts against the daily quota.
var publishRisk = capability.Risk{
	Effect:          capability.EffectExecute,
	Idempotency:     capability.IdempotencyNonIdempotent,
	Confirmation:    capability.ConfirmationRequired,
	OpenWorld:       true,
	DataSensitivity: "URLs of the account's websites, sent to Google for crawling or removal from its index",
}

var indexingMetadata = capability.Descriptor{
	ID: indexingMetadataID, Version: 1, Title: "Get the Indexing API status of a URL",
	Description: "Read what Google last received through the Indexing API for one URL: type and time of the " +
		"latest URL_UPDATED and URL_DELETED notification. It does not tell whether or when Google indexes or " +
		"removes the URL. A URL without a notification so far gets a result without latestUpdate and " +
		"latestRemove, not an error. " + indexingUse + indexingBound + untrustedNote,
	Tags: []string{"google", "search-console", "indexing"}, Risk: indexingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"url":` + sitemapAddressSchema +
		`},"required":["url"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(metadataSchema),
	Arguments:    []capability.Argument{urlArgument},
	Fields:       metadataFields,
	Examples: []capability.Example{{Description: "Status of one URL",
		Arguments: json.RawMessage(`{"url":"https://www.example.com/jobs/42"}`)}},
}

var indexingMetadataBatch = capability.Descriptor{
	ID: indexingMetadataBatchID, Version: 1, Title: "Get the Indexing API status of several URLs",
	Description: "Read what Google last received through the Indexing API for up to 100 URLs in one batch " +
		"request. Each entry holds the metadata of its URL or its own error; one failed entry does not fail the " +
		"others. Google counts every entry as one read. A URL without a notification so far gets metadata " +
		"without latestUpdate and latestRemove. " + indexingUse + indexingBound + untrustedNote,
	Tags: []string{"google", "search-console", "indexing"}, Risk: indexingRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"urls":` + urlsSchema +
		`},"required":["urls"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(metadataBatchSchema),
	Arguments: []capability.Argument{{Name: "urls", Required: true,
		Description: "1 to 100 absolute http or https URLs, each as described for url of indexing.get-metadata"}},
	Fields: []capability.Field{
		{Name: "url", Description: "the URL as it was asked for"},
		{Name: "metadata", Description: "url, latestUpdate and latestRemove as in indexing.get-metadata"},
		{Name: "error", Description: "class, message and HTTP status of a failed entry"},
	},
	Examples: []capability.Example{{Description: "Status of two URLs",
		Arguments: json.RawMessage(`{"urls":["https://www.example.com/jobs/42","https://www.example.com/jobs/43"]}`)}},
}

var indexingPublish = capability.Descriptor{
	ID: indexingPublishID, Version: 1, Title: "Notify Google of an updated or removed URL",
	Description: "Notify Google through the Indexing API that a URL was updated (URL_UPDATED) or removed " +
		"(URL_DELETED), so Google may crawl it soon or drop it from its index. " + indexingUse + publishEffect +
		indexingBound + publishListed + untrustedNote,
	Tags: []string{"google", "search-console", "indexing"}, Risk: publishRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"url":` + sitemapAddressSchema + `,"type":` +
		notificationTypeSchema + `},"required":["url","type"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(publishSchema),
	Arguments: []capability.Argument{urlArgument,
		{Name: "type", Required: true, Description: "URL_UPDATED for new or changed content, URL_DELETED for removed content"}},
	Fields: []capability.Field{{Name: "urlNotificationMetadata",
		Description: "what Google now holds for the URL: url, latestUpdate and latestRemove as in indexing.get-metadata"}},
	Examples: []capability.Example{{Description: "Report a changed job posting",
		Arguments: json.RawMessage(`{"url":"https://www.example.com/jobs/42","type":"URL_UPDATED"}`)}},
}

var indexingPublishBatch = capability.Descriptor{
	ID: indexingPublishBatchID, Version: 1, Title: "Notify Google of several updated or removed URLs",
	Description: "Send up to 100 Indexing API notifications in one batch request. Each entry holds Google's " +
		"answer for its URL or its own error, and entries succeed or fail on their own; Google counts every " +
		"entry against the quota. An entry without a readable answer may still have reached Google: check it " +
		"with indexing.get-metadata before sending it again. " + indexingUse + publishEffect + indexingBound +
		publishListed + untrustedNote,
	Tags: []string{"google", "search-console", "indexing"}, Risk: publishRisk, Provider: Provider,
	RequiresToolAllowList: true,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"notifications":` + notificationsSchema +
		`},"required":["notifications"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(publishBatchSchema),
	Arguments: []capability.Argument{{Name: "notifications", Required: true,
		Description: "1 to 100 notifications, each an object with url and type as in indexing.publish"}},
	Fields: []capability.Field{
		{Name: "url", Description: "the URL as it was sent"},
		{Name: "type", Description: "the notification type as it was sent"},
		{Name: "urlNotificationMetadata", Description: "what Google now holds for the URL, as in indexing.publish"},
		{Name: "error", Description: "class, message and HTTP status of a failed entry"},
	},
	Examples: []capability.Example{{Description: "Report one changed and one removed job posting",
		Arguments: json.RawMessage(`{"notifications":[{"url":"https://www.example.com/jobs/42","type":"URL_UPDATED"},` +
			`{"url":"https://www.example.com/jobs/7","type":"URL_DELETED"}]}`)}},
}

type notificationArguments struct {
	URL  string `json:"url"`
	Type string `json:"type"`
}

type indexingArguments struct {
	URL           string                  `json:"url"`
	Type          string                  `json:"type"`
	URLs          []string                `json:"urls"`
	Notifications []notificationArguments `json:"notifications"`
}

// checkIndexURL checks a URL of the Indexing API and, for a connection bound to a property, that it lies
// under that property. The Indexing API names no property, so the binding is enforced here alone.
func checkIndexURL(bound, name, value string) error {
	if err := checkAddress(name, value); err != nil {
		return err
	}
	if bound != "" && !underProperty(bound, value) {
		return badRequest(name + " lies outside the property this connection is bound to")
	}
	return nil
}

func checkType(name, value string) error {
	if !slices.Contains(notificationTypes, value) {
		return badRequest(name + " must be URL_UPDATED or URL_DELETED")
	}
	return nil
}

// readIndexingArguments decodes the validated arguments and settles the binding before any secret is read.
func readIndexingArguments(op string, resolved *config.Resolved, raw json.RawMessage) (indexingArguments, string, error) {
	var arguments indexingArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return arguments, "", failure(op, "the validated arguments could not be read")
	}
	bound, err := boundProperty(resolved)
	return arguments, bound, err
}

// slot is one limiter and the interval it keeps.
type slot struct {
	limiter  *ratelimit.Limiter
	interval time.Duration
}

// indexingSlots are the limiters a request of the Indexing API waits for: every request, and a metadata read
// as well.
func (c *client) indexingSlots(metadata bool) []slot {
	slots := []slot{{indexingLimiters.For(c.key), indexingInterval}}
	if metadata {
		slots = append(slots, slot{metadataLimiters.For(c.key), metadataInterval})
	}
	return slots
}

// reserve takes the next slot of every limiter for a request Google counts n times. A batch does not wait for
// its other entries beforehand; it moves the next request back by their spacing instead, so a batch of 100
// fits into one invoke.
func reserve(ctx context.Context, op string, n int, slots []slot) error {
	for _, s := range slots {
		if err := s.limiter.Wait(ctx); err != nil {
			return provider.Waited(op, "Google", err)
		}
		if n > 1 {
			s.limiter.HoldFor(time.Duration(n) * s.interval)
		}
	}
	return nil
}

// holdAfter429 delays the next request of every limiter after a rate-limit answer. A missing retry time still
// means too many requests: the next call waits, nothing repeats this one.
func holdAfter429(header http.Header, slots []slot) {
	wait := min(max(provider.RetryAfter(header), time.Second), maxHold)
	for _, s := range slots {
		s.limiter.HoldFor(wait)
	}
}

// indexingError maps a failed answer of the Indexing API like any Google answer, with a permission text that
// names the owner role the Indexing API needs.
func indexingError(op string, status int, body []byte) *provider.Error {
	failed := googleError(op, status, body, notFoundIndexing)
	if failed.Class == provider.ClassPermission {
		failed.Message = indexingPermissionText
	}
	return failed
}

// noNotification reports Google's answer for a URL without any notification so far: 404 with NOT_FOUND.
func noNotification(status int, body []byte) bool {
	state, _ := googleapi.StatusOf(body)
	return status == http.StatusNotFound && state == "NOT_FOUND"
}

func metadataAddress(address string) string {
	return indexingRoot + metadataPath + "?" + url.Values{"url": {address}}.Encode()
}

func handleIndexingMetadata(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "indexing get metadata"
	arguments, bound, err := readIndexingArguments(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := checkIndexURL(bound, "url", arguments.URL); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	slots := c.indexingSlots(true)
	if err := reserve(ctx, op, 1, slots); err != nil {
		return nil, err
	}
	response, err := c.roundTrip(ctx, op, indexingMetadataID, http.MethodGet, metadataAddress(arguments.URL), nil, "",
		"application/json", false)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body := provider.ReadErrorBody(response.Body)
		if noNotification(response.StatusCode, body) {
			return notificationMetadata{URL: text(arguments.URL)}, nil
		}
		if response.StatusCode == http.StatusTooManyRequests {
			holdAfter429(response.Header, slots)
		}
		return nil, indexingError(op, response.StatusCode, body)
	}
	var answer notificationMetadata
	if err := readJSONAnswer(op, response, maxBytes, &answer); err != nil {
		return nil, err
	}
	return answer, nil
}

func handleIndexingPublish(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "indexing publish"
	arguments, bound, err := readIndexingArguments(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	if err := checkType("type", arguments.Type); err != nil {
		return nil, err
	}
	if err := checkIndexURL(bound, "url", arguments.URL); err != nil {
		return nil, err
	}
	body, err := json.Marshal(notificationArguments{URL: arguments.URL, Type: arguments.Type})
	if err != nil {
		return nil, failure(op, "the request body could not be built")
	}
	if err := checkConnection(resolved, secrets); err != nil {
		return nil, err
	}
	if err := countNotifications(op, resolved, 1); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	slots := c.indexingSlots(false)
	if err := reserve(ctx, op, 1, slots); err != nil {
		return nil, err
	}
	response, err := c.roundTrip(ctx, op, indexingPublishID, http.MethodPost, indexingRoot+publishPath, body,
		"application/json", "application/json", true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		body := provider.ReadErrorBody(response.Body)
		if response.StatusCode == http.StatusTooManyRequests {
			holdAfter429(response.Header, slots)
		}
		return nil, indexingError(op, response.StatusCode, body)
	}
	var answer publishAnswer
	if err := readJSONAnswer(op, response, maxBytes, &answer); err != nil {
		var failed *provider.Error
		if errors.As(err, &failed) {
			failed.Message = "Google accepted the notification, but its answer could not be read; do not send it again"
		}
		return nil, err
	}
	return answer, nil
}

func handleIndexingMetadataBatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "indexing get metadata batch"
	arguments, bound, err := readIndexingArguments(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	if len(arguments.URLs) == 0 || len(arguments.URLs) > maxBatch {
		return nil, badRequest("urls must hold 1 to 100 URLs")
	}
	requests := make([]batchRequest, len(arguments.URLs))
	for i, address := range arguments.URLs {
		if err := checkIndexURL(bound, fmt.Sprintf("urls[%d]", i), address); err != nil {
			return nil, err
		}
		requests[i] = batchRequest{method: http.MethodGet, path: metadataPath + "?" + url.Values{"url": {address}}.Encode()}
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	answers, err := c.batch(ctx, op, indexingMetadataBatchID, requests, c.indexingSlots(true), false)
	if err != nil {
		return nil, err
	}
	result := metadataBatchResult{Entries: make([]metadataEntry, len(answers))}
	for i, answer := range answers {
		entry := metadataEntry{URL: arguments.URLs[i]}
		var metadata notificationMetadata
		switch {
		case answer.missing:
			entry.Error = &entryError{Class: provider.ClassInvalidResponse, Message: "Google sent no readable answer for this entry"}
		case noNotification(answer.status, answer.body):
			entry.Metadata = &notificationMetadata{URL: text(arguments.URLs[i])}
		case answer.status < 200 || answer.status > 299:
			entry.Error = partError(op, answer)
		case !answer.decode(&metadata):
			entry.Error = &entryError{Class: provider.ClassInvalidResponse, Status: answer.status,
				Message: "Google's answer for this entry could not be read"}
		default:
			entry.Metadata = &metadata
		}
		if entry.Error != nil {
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Entries[i] = entry
	}
	return result, nil
}

func handleIndexingPublishBatch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "indexing publish batch"
	arguments, bound, err := readIndexingArguments(op, resolved, raw)
	if err != nil {
		return nil, err
	}
	if len(arguments.Notifications) == 0 || len(arguments.Notifications) > maxBatch {
		return nil, badRequest("notifications must hold 1 to 100 entries")
	}
	requests := make([]batchRequest, len(arguments.Notifications))
	for i, notification := range arguments.Notifications {
		name := fmt.Sprintf("notifications[%d]", i)
		if err := checkType(name+".type", notification.Type); err != nil {
			return nil, err
		}
		if err := checkIndexURL(bound, name+".url", notification.URL); err != nil {
			return nil, err
		}
		body, err := json.Marshal(notificationArguments{URL: notification.URL, Type: notification.Type})
		if err != nil {
			return nil, failure(op, "the request body could not be built")
		}
		requests[i] = batchRequest{method: http.MethodPost, path: publishPath, body: body}
	}
	if err := checkConnection(resolved, secrets); err != nil {
		return nil, err
	}
	if err := countNotifications(op, resolved, len(requests)); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	answers, err := c.batch(ctx, op, indexingPublishBatchID, requests, c.indexingSlots(false), true)
	if err != nil {
		return nil, err
	}
	result := publishBatchResult{Entries: make([]publishEntry, len(answers))}
	for i, answer := range answers {
		entry := publishEntry{URL: arguments.Notifications[i].URL, Type: arguments.Notifications[i].Type}
		var published publishAnswer
		switch {
		case answer.missing:
			entry.Error = &entryError{Class: provider.ClassInvalidResponse, Message: "Google sent no readable " +
				"answer for this entry; it may have taken the notification, so check with indexing.get-metadata " +
				"before sending it again"}
		case answer.status < 200 || answer.status > 299:
			entry.Error = partError(op, answer)
		case !answer.decode(&published):
			entry.Error = &entryError{Class: provider.ClassInvalidResponse, Status: answer.status,
				Message: "Google accepted the notification, but its answer could not be read; do not send it again"}
		default:
			entry.URLNotificationMetadata = published.URLNotificationMetadata
		}
		if entry.Error != nil {
			result.Failed++
		} else {
			result.Succeeded++
		}
		result.Entries[i] = entry
	}
	return result, nil
}

// partError maps the failed answer of one batch entry to the error of that entry.
func partError(op string, answer partAnswer) *entryError {
	failed := indexingError(op, answer.status, answer.body)
	return &entryError{Class: failed.Class, Message: failed.Message, Status: answer.status}
}

// publishSummary phrases a single notification for its confirmation.
func publishSummary(raw json.RawMessage) string {
	var arguments notificationArguments
	if json.Unmarshal(raw, &arguments) != nil {
		return ""
	}
	return "Notify Google through the Indexing API: " + arguments.Type + " for " + arguments.URL +
		". It takes effect at Google and cannot be withdrawn."
}

// publishBatchSummary phrases a batch of notifications for its confirmation.
func publishBatchSummary(raw json.RawMessage) string {
	var arguments indexingArguments
	if json.Unmarshal(raw, &arguments) != nil || len(arguments.Notifications) == 0 {
		return ""
	}
	counts := map[string]int{}
	for _, notification := range arguments.Notifications {
		counts[notification.Type]++
	}
	return fmt.Sprintf("Send %d Indexing API notifications to Google (%d URL_UPDATED, %d URL_DELETED), the first "+
		"for %s. They take effect at Google and cannot be withdrawn.", len(arguments.Notifications),
		counts[urlUpdated], counts[urlDeleted], arguments.Notifications[0].URL)
}

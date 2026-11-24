package googlesearchconsole

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
)

var indexingTools = []string{indexingMetadataID, indexingMetadataBatchID, indexingPublishID, indexingPublishBatchID}

const jobURL = "https://www.example.com/jobs/42"

// innerRequest is one part of a batch request as Google would read it.
type innerRequest struct {
	contentID, method, path, query, contentType, authorization string
	body                                                       string
}

// readBatchRequest splits a recorded batch request into its parts.
func readBatchRequest(t *testing.T, call request) []innerRequest {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(call.contentType)
	if err != nil || mediaType != "multipart/mixed" || params["boundary"] == "" {
		t.Fatalf("batch content type = %q", call.contentType)
	}
	reader := multipart.NewReader(strings.NewReader(call.body), params["boundary"])
	var parts []innerRequest
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return parts
		}
		if err != nil {
			t.Fatalf("batch body: %v", err)
		}
		if part.Header.Get("Content-Type") != "application/http" {
			t.Fatalf("part content type = %q", part.Header.Get("Content-Type"))
		}
		inner, err := http.ReadRequest(bufio.NewReader(part))
		if err != nil {
			t.Fatalf("part request: %v", err)
		}
		body, _ := io.ReadAll(inner.Body)
		parts = append(parts, innerRequest{contentID: part.Header.Get("Content-ID"), method: inner.Method,
			path: inner.URL.Path, query: inner.URL.RawQuery, contentType: inner.Header.Get("Content-Type"),
			authorization: inner.Header.Get("Authorization"), body: string(body)})
	}
}

// batchPart is one answer part: the Content-ID it names and the raw HTTP answer it holds.
type batchPart struct{ id, http string }

func okPart(n int, body string) batchPart {
	return batchPart{fmt.Sprintf("<response-item-%d>", n),
		"HTTP/1.1 200 OK\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n" + body}
}

func failedPart(n, status int, body string) batchPart {
	return batchPart{fmt.Sprintf("<response-item-%d>", n), fmt.Sprintf(
		"HTTP/1.1 %d %s\r\nContent-Type: application/json; charset=UTF-8\r\n\r\n%s", status, http.StatusText(status), body)}
}

// batchReply answers a batch with the given parts in the given order.
func batchReply(parts ...batchPart) func(*http.Request, int) (*http.Response, error) {
	return func(r *http.Request, _ int) (*http.Response, error) {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		_ = writer.SetBoundary("batch_sample_boundary")
		for _, p := range parts {
			part, _ := writer.CreatePart(textproto.MIMEHeader{"Content-Type": {"application/http"}, "Content-ID": {p.id}})
			_, _ = part.Write([]byte(p.http))
		}
		_ = writer.Close()
		return respond(r, http.StatusOK, "multipart/mixed; boundary=batch_sample_boundary", buffer.String()), nil
	}
}

func metadataAnswer(address string) string {
	return fmt.Sprintf(`{"url":%q,"latestUpdate":{"url":%q,"type":"URL_UPDATED","notifyTime":"2026-11-01T10:00:00.5Z"},`+
		`"latestRemove":{"url":%q,"type":"URL_DELETED","notifyTime":"2026-10-01T10:00:00Z"},"extra":1}`, address, address, address)
}

func jobURLs(n int) []string {
	urls := make([]string, n)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://www.example.com/jobs/%d", i+1)
	}
	return urls
}

func urlsArgument(urls []string) string {
	raw, _ := json.Marshal(map[string]any{"urls": urls})
	return string(raw)
}

func notificationsArgument(urls []string, kind string) string {
	notifications := make([]map[string]string, len(urls))
	for i, address := range urls {
		notifications[i] = map[string]string{"url": address, "type": kind}
	}
	raw, _ := json.Marshal(map[string]any{"notifications": notifications})
	return string(raw)
}

func TestIndexingDescriptorsCarryRiskUseLimitAndQuota(t *testing.T) {
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, d := range []capability.Descriptor{indexingMetadata, indexingMetadataBatch, indexingPublish, indexingPublishBatch} {
		r := d.Risk
		publishing := d.ID == indexingPublishID || d.ID == indexingPublishBatchID
		if publishing {
			if r.Effect != capability.EffectExecute || r.Idempotency != capability.IdempotencyNonIdempotent ||
				r.Confirmation != capability.ConfirmationRequired || !r.OpenWorld || !d.RequiresToolAllowList {
				t.Errorf("%s risk = %+v, listed-only %t", d.ID, r, d.RequiresToolAllowList)
			}
			for _, text := range []string{"JobPosting", "BroadcastEvent", "cannot be withdrawn", "URL_DELETED",
				"counts the notifications it sends", "never repeated", "tools list"} {
				if !strings.Contains(d.Description, text) {
					t.Errorf("%s description lacks %q", d.ID, text)
				}
			}
			for _, profile := range metadata.Profiles {
				for _, id := range profile.Tools {
					if id == d.ID {
						t.Errorf("profile %s holds %s", profile.ID, id)
					}
				}
			}
		} else if r.Effect != capability.EffectRead || r.Idempotency != capability.IdempotencySafe ||
			r.Confirmation != capability.ConfirmationNone || !r.OpenWorld || d.RequiresToolAllowList {
			t.Errorf("%s risk = %+v", d.ID, r)
		}
		if r.DataSensitivity == "" {
			t.Errorf("%s has no data sensitivity", d.ID)
		}
		if strings.Contains(strings.ToLower(d.Description+d.Title+strings.Join(d.Tags, " ")), "pages") {
			t.Errorf("%s text contains the search word pages", d.ID)
		}
	}
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			if profile.ID == "read" && (id == indexingMetadataID || id == indexingMetadataBatchID) {
				t.Errorf("the read profile holds %s, whose scope also allows notifications", id)
			}
		}
	}
}

func TestIndexingMetadataReadsWithTheIndexingScopeAndBoundsTheAnswer(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, metadataAnswer(jobURL))
	out, err := env.invoke(t, indexingMetadataID, "all", `{"url":"https://www.example.com/jobs/42?ref=a&b=c d"}`)
	if err == nil {
		t.Fatalf("a URL with a space was accepted: %v", out)
	}
	out, err = env.invoke(t, indexingMetadataID, "all", `{"url":"https://www.example.com/jobs/42?ref=a&b=%2F"}`)
	if err != nil {
		t.Fatal(err)
	}
	update, _ := out["latestUpdate"].(map[string]any)
	if out["url"] != jobURL || update["type"] != "URL_UPDATED" || update["notifyTime"] != "2026-11-01T10:00:00.5Z" ||
		out["latestRemove"] == nil {
		t.Errorf("result = %v", out)
	}
	if _, ok := out["extra"]; ok {
		t.Errorf("an undocumented member reached the result: %v", out)
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 {
		t.Fatalf("%d requests", len(calls))
	}
	call := calls[0]
	query, _ := url.ParseQuery(call.rawQuery)
	if call.method != http.MethodGet || call.host != "indexing.googleapis.com" || call.escapedPath != metadataPath ||
		len(query) != 1 || query.Get("url") != "https://www.example.com/jobs/42?ref=a&b=%2F" ||
		call.authorization != "Bearer "+accessPrefix+"1" || call.body != "" || strings.Contains(call.full, accessPrefix) {
		t.Errorf("request = %+v", call)
	}
}

func TestIndexingMetadataWithoutAnyNotificationHasEmptyFields(t *testing.T) {
	for name, answer := range map[string]func(*http.Request, int) (*http.Response, error){
		"404 NOT_FOUND": reply(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")),
		"only the url":  reply(http.StatusOK, fmt.Sprintf(`{"url":%q}`, jobURL)),
	} {
		env := newEnvironment(t, nil)
		env.fake.answer = answer
		out, err := env.invoke(t, indexingMetadataID, "all", `{"url":"`+jobURL+`"}`)
		if err != nil || out["url"] != jobURL || out["latestUpdate"] != nil || out["latestRemove"] != nil {
			t.Errorf("%s: result = %v, %v", name, out, err)
		}
	}
	// A 404 without Google's error object is no answer about the URL.
	env := newEnvironment(t, nil)
	env.fake.answer = func(r *http.Request, _ int) (*http.Response, error) {
		return respond(r, http.StatusNotFound, "text/html", "<p>gone</p>"), nil
	}
	if _, err := env.invoke(t, indexingMetadataID, "all", `{"url":"`+jobURL+`"}`); classOf(err) != provider.ClassNotFound {
		t.Errorf("an HTML 404 = %v, want not-found", err)
	}
}

func TestIndexingPublishNeedsConfirmationAndAToolsListEntry(t *testing.T) {
	env := newEnvironment(t, nil)
	for _, c := range []struct{ id, args string }{
		{indexingPublishID, `{"url":"` + jobURL + `","type":"URL_UPDATED"}`},
		{indexingPublishBatchID, notificationsArgument([]string{jobURL}, urlUpdated)},
	} {
		_, err := env.core.Invoke(context.Background(), application.InvokeRequest{
			Operation: c.id, Connection: "index", Arguments: json.RawMessage(c.args)})
		var needed *application.ConfirmationRequiredError
		if !errors.As(err, &needed) || !strings.Contains(needed.Summary, "cannot be withdrawn") {
			t.Errorf("%s without confirm: error = %v, want confirmation required with a summary", c.id, err)
		}
		// Effects alone never offer a listed-only tool, not even read and execute together.
		for _, connection := range []string{"indexeffects", "edit", "all"} {
			if _, err := env.change(t, c.id, connection, c.args); err == nil {
				t.Errorf("%s ran on %s without a tools list entry", c.id, connection)
			}
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Fatalf("%d requests and %d grants, want none", len(calls), grants)
	}
	// The effects connection still offers the metadata read.
	env.fake.answer = reply(http.StatusOK, metadataAnswer(jobURL))
	if _, err := env.invoke(t, indexingMetadataID, "indexeffects", `{"url":"`+jobURL+`"}`); err != nil {
		t.Errorf("metadata on the effects connection: %v", err)
	}
}

func TestIndexingPublishSendsOneJSONNotificationThroughTheMutationPath(t *testing.T) {
	for _, kind := range notificationTypes {
		env := newEnvironment(t, nil)
		env.fake.answer = reply(http.StatusOK, `{"urlNotificationMetadata":`+metadataAnswer(jobURL)+`}`)
		out, err := env.change(t, indexingPublishID, "index", `{"url":"`+jobURL+`","type":"`+kind+`"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := out["urlNotificationMetadata"].(map[string]any); got["url"] != jobURL || got["latestUpdate"] == nil {
			t.Errorf("result = %v", out)
		}
		calls, grants := env.fake.calls()
		if len(calls) != 1 || grants != 1 {
			t.Fatalf("%d requests, %d grants", len(calls), grants)
		}
		call := calls[0]
		var body map[string]string
		if err := json.Unmarshal([]byte(call.body), &body); err != nil || len(body) != 2 || body["url"] != jobURL ||
			body["type"] != kind {
			t.Errorf("body = %s", call.body)
		}
		if call.method != http.MethodPost || call.host != "indexing.googleapis.com" || call.escapedPath != "/v3/urlNotifications:publish" ||
			call.rawQuery != "" || call.contentType != "application/json" || call.authorization != "Bearer "+accessPrefix+"1" {
			t.Errorf("request = %+v", call)
		}
	}
}

func TestIndexingRefusesBadArgumentsAndForeignURLsBeforeAnyRequest(t *testing.T) {
	env := newEnvironment(t, nil)
	refused := []struct{ id, connection, args string }{
		{indexingPublishID, "index", `{"url":"` + jobURL + `","type":"URL_REMOVED"}`},
		{indexingPublishID, "index", `{"url":"` + jobURL + `","type":"URL_NOTIFICATION_TYPE_UNSPECIFIED"}`},
		{indexingPublishID, "index", `{"url":"` + jobURL + `"}`},
		{indexingPublishID, "index", `{"url":"ftp://www.example.com/jobs/1","type":"URL_UPDATED"}`},
		{indexingPublishID, "index", `{"url":"https://user:pw` + "@" + `www.example.com/jobs/1","type":"URL_UPDATED"}`},
		{indexingPublishID, "index", `{"url":"https://www.example.com/jobs/1#top","type":"URL_UPDATED"}`},
		{indexingPublishID, "index", `{"url":"https://www.example.com/jobs/1\r\nX-Injected: 1","type":"URL_UPDATED"}`},
		{indexingPublishID, "index", `{"url":"sc-domain:example.com","type":"URL_UPDATED"}`},
		{indexingPublishBatchID, "index", `{"notifications":[{"url":"` + jobURL + `","type":"URL_MOVED"}]}`},
		{indexingPublishBatchID, "index", `{"notifications":[]}`},
		{indexingMetadataBatchID, "index", `{"urls":[]}`},
		{indexingMetadataBatchID, "index", `{"urls":["` + jobURL + `","/jobs/2"]}`},
		// A bound connection reaches URLs under its property only.
		{indexingPublishID, "indexprefix", `{"url":"https://shop.example.com/jobs/1","type":"URL_UPDATED"}`},
		{indexingPublishID, "indexprefix", `{"url":"http://www.example.com/jobs/1","type":"URL_UPDATED"}`},
		{indexingPublishID, "indexdomain", `{"url":"https://example.com.evil.invalid/jobs/1","type":"URL_DELETED"}`},
		{indexingMetadataID, "prefix", `{"url":"https://www.example.org/jobs/1"}`},
		{indexingMetadataID, "domain", `{"url":"https://notexample.com/jobs/1"}`},
		{indexingMetadataBatchID, "indexdomain", `{"urls":["https://jobs.example.com/1","https://www.example.net/2"]}`},
		{indexingPublishBatchID, "indexprefix", `{"notifications":[{"url":"` + jobURL + `","type":"URL_UPDATED"},` +
			`{"url":"https://www.example.com.evil.invalid/2","type":"URL_UPDATED"}]}`},
	}
	for _, c := range refused {
		// The input schema may refuse first; that is a refusal before any request as well.
		if _, err := env.change(t, c.id, c.connection, c.args); err == nil {
			t.Errorf("%s %s %s was accepted", c.id, c.connection, c.args)
		} else if strings.Contains(err.Error(), "evil") || strings.Contains(err.Error(), "example.net") {
			t.Errorf("%s: the refusal quotes the URL: %v", c.id, err)
		}
	}
	if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
		t.Fatalf("%d requests and %d grants, want none", len(calls), grants)
	}
	// The handler refuses an unknown type on its own as well, should the schema ever allow one.
	if err := checkType("type", "URL_NOTIFICATION_TYPE_UNSPECIFIED"); err == nil {
		t.Error("checkType accepted the unspecified type")
	}
}

func TestIndexingAcceptsURLsUnderTheBinding(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.answer = reply(http.StatusOK, `{}`)
	for _, c := range []struct{ connection, address string }{
		{"indexdomain", "https://example.com/jobs/1"},
		{"indexdomain", "http://Careers.Example.COM/jobs/1?id=2"},
		{"indexprefix", "https://WWW.example.com/jobs/1"},
		{"indexprefix", "https://www.example.com/"},
	} {
		if _, err := env.change(t, indexingPublishID, c.connection, `{"url":"`+c.address+`","type":"URL_UPDATED"}`); err != nil {
			t.Errorf("%s %s: %v", c.connection, c.address, err)
		}
	}
}

func TestIndexingBatchOfOneHundredSucceedsAndOneMoreIsRefused(t *testing.T) {
	urls := jobURLs(maxBatch)
	parts := make([]batchPart, maxBatch)
	for i, address := range urls {
		// Google may answer in another order; the Content-ID decides.
		parts[maxBatch-1-i] = okPart(i+1, metadataAnswer(address))
	}
	env := newEnvironment(t, nil)
	env.fake.answer = batchReply(parts...)
	out, err := env.invoke(t, indexingMetadataBatchID, "all", urlsArgument(urls))
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := out["entries"].([]any)
	if len(entries) != maxBatch || out["succeeded"] != float64(maxBatch) || out["failed"] != float64(0) {
		t.Fatalf("%d entries, result %v %v", len(entries), out["succeeded"], out["failed"])
	}
	for i, raw := range entries {
		entry := raw.(map[string]any)
		metadata, _ := entry["metadata"].(map[string]any)
		if entry["url"] != urls[i] || metadata["url"] != urls[i] || entry["error"] != nil {
			t.Fatalf("entry %d = %v", i, entry)
		}
	}
	calls, _ := env.fake.calls()
	if len(calls) != 1 {
		t.Fatalf("%d requests", len(calls))
	}
	call := calls[0]
	if call.method != http.MethodPost || call.host != "indexing.googleapis.com" || call.escapedPath != "/batch" ||
		call.rawQuery != "" || call.authorization != "Bearer "+accessPrefix+"1" {
		t.Errorf("batch request = %+v", call)
	}
	inner := readBatchRequest(t, call)
	if len(inner) != maxBatch {
		t.Fatalf("%d parts", len(inner))
	}
	for i, part := range inner {
		query, _ := url.ParseQuery(part.query)
		if part.contentID != fmt.Sprintf("<item-%d>", i+1) || part.method != http.MethodGet || part.path != metadataPath ||
			query.Get("url") != urls[i] || len(query) != 1 || part.authorization != "" || part.body != "" {
			t.Fatalf("part %d = %+v", i, part)
		}
	}

	for _, c := range []struct{ id, args string }{
		{indexingMetadataBatchID, urlsArgument(jobURLs(maxBatch + 1))},
		{indexingPublishBatchID, notificationsArgument(jobURLs(maxBatch+1), urlUpdated)},
	} {
		env := newEnvironment(t, nil)
		if _, err := env.change(t, c.id, "index", c.args); err == nil {
			t.Errorf("%s accepted 101 entries", c.id)
		}
		if calls, grants := env.fake.calls(); len(calls) != 0 || grants != 0 {
			t.Errorf("%s: %d requests and %d grants, want none", c.id, len(calls), grants)
		}
	}
}

func TestIndexingPublishBatchOfOneHundredBuildsFixedParts(t *testing.T) {
	urls := jobURLs(maxBatch)
	parts := make([]batchPart, maxBatch)
	for i, address := range urls {
		parts[i] = okPart(i+1, `{"urlNotificationMetadata":`+metadataAnswer(address)+`}`)
	}
	env := newEnvironment(t, nil)
	env.fake.answer = batchReply(parts...)
	out, err := env.change(t, indexingPublishBatchID, "index", notificationsArgument(urls, urlDeleted))
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := out["entries"].([]any); len(entries) != maxBatch || out["succeeded"] != float64(maxBatch) {
		t.Fatalf("result = %v", out)
	}
	calls, grants := env.fake.calls()
	if len(calls) != 1 || grants != 1 {
		t.Fatalf("%d requests, %d grants", len(calls), grants)
	}
	for i, part := range readBatchRequest(t, calls[0]) {
		var body map[string]string
		if err := json.Unmarshal([]byte(part.body), &body); err != nil || len(body) != 2 || body["url"] != urls[i] ||
			body["type"] != urlDeleted {
			t.Fatalf("part %d body = %s", i, part.body)
		}
		if part.method != http.MethodPost || part.path != "/v3/urlNotifications:publish" || part.query != "" ||
			part.contentType != "application/json" || part.authorization != "" {
			t.Fatalf("part %d = %+v", i, part)
		}
	}
}

func TestIndexingPublishBatchReportsPartialSuccessAndMixedErrors(t *testing.T) {
	urls := jobURLs(7)
	env := newEnvironment(t, nil)
	env.fake.answer = batchReply(
		okPart(2, `{"urlNotificationMetadata":`+metadataAnswer(urls[1])+`}`),
		failedPart(1, http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "")),
		failedPart(3, http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")),
		// Unparsable answers fail their entry only.
		batchPart{"<response-item-4>", "not an HTTP answer " + detailCanary},
		okPart(5, `{"urlNotificationMetadata":`),
		// An unknown or repeated entry is skipped, and entry 7 gets no answer at all.
		okPart(2, `{}`),
		okPart(99, `{}`),
		batchPart{"<item-6>", "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n{}"},
		failedPart(6, http.StatusInternalServerError, errorObject(500, "INTERNAL", "")),
	)
	out, err := env.change(t, indexingPublishBatchID, "index", notificationsArgument(urls, urlUpdated))
	if err != nil {
		t.Fatal(err)
	}
	if out["succeeded"] != float64(1) || out["failed"] != float64(6) {
		t.Fatalf("counts = %v / %v", out["succeeded"], out["failed"])
	}
	entries := out["entries"].([]any)
	want := []struct {
		class  provider.Class
		status float64
		hint   string
	}{
		{provider.ClassPermission, 403, "owner"},
		{"", 0, ""},
		{provider.ClassRateLimited, 429, ""},
		{provider.ClassInvalidResponse, 0, "may have taken"},
		{provider.ClassInvalidResponse, 200, "do not send it again"},
		{provider.ClassProviderError, 500, ""},
		{provider.ClassInvalidResponse, 0, "may have taken"},
	}
	for i, raw := range entries {
		entry := raw.(map[string]any)
		if entry["url"] != urls[i] || entry["type"] != urlUpdated {
			t.Errorf("entry %d = %v", i, entry)
		}
		w := want[i]
		if w.class == "" {
			if entry["error"] != nil || entry["urlNotificationMetadata"] == nil {
				t.Errorf("entry %d = %v, want success", i, entry)
			}
			continue
		}
		failure, _ := entry["error"].(map[string]any)
		status, _ := failure["status"].(float64)
		message, _ := failure["message"].(string)
		if failure["class"] != string(w.class) || status != w.status || !strings.Contains(message, w.hint) {
			t.Errorf("entry %d error = %v, want %s %v %q", i, failure, w.class, w.status, w.hint)
		}
	}
	if raw, _ := json.Marshal(out); strings.Contains(string(raw), detailCanary) || strings.Contains(string(raw), accessPrefix) {
		t.Errorf("the result carries provider text or a token: %s", raw)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one", len(calls))
	}
}

func TestIndexingMetadataBatchMixesMetadataEmptyFieldsAndErrors(t *testing.T) {
	urls := jobURLs(3)
	env := newEnvironment(t, nil)
	env.fake.answer = batchReply(
		okPart(1, metadataAnswer(urls[0])),
		failedPart(2, http.StatusNotFound, errorObject(404, "NOT_FOUND", "")),
		failedPart(3, http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")),
	)
	out, err := env.invoke(t, indexingMetadataBatchID, "all", urlsArgument(urls))
	if err != nil {
		t.Fatal(err)
	}
	entries := out["entries"].([]any)
	first, second, third := entries[0].(map[string]any), entries[1].(map[string]any), entries[2].(map[string]any)
	if m, _ := first["metadata"].(map[string]any); m["latestUpdate"] == nil {
		t.Errorf("first = %v", first)
	}
	if m, _ := second["metadata"].(map[string]any); m["url"] != urls[1] || m["latestUpdate"] != nil || second["error"] != nil {
		t.Errorf("second = %v, want metadata without notifications", second)
	}
	if e, _ := third["error"].(map[string]any); e["class"] != string(provider.ClassAuth) {
		t.Errorf("third = %v", third)
	}
	if out["succeeded"] != float64(2) || out["failed"] != float64(1) {
		t.Errorf("counts = %v / %v", out["succeeded"], out["failed"])
	}
}

func TestIndexingBatchAnswerThatIsNoBatchFailsTheCall(t *testing.T) {
	for _, c := range []struct{ id, connection, args, hint string }{
		{indexingMetadataBatchID, "all", urlsArgument(jobURLs(2)), "not a batch answer"},
		{indexingPublishBatchID, "index", notificationsArgument(jobURLs(2), urlUpdated), "may have taken"},
	} {
		env := newEnvironment(t, nil)
		env.fake.answer = reply(http.StatusOK, `{"entries":[]}`)
		_, err := env.change(t, c.id, c.connection, c.args)
		if classOf(err) != provider.ClassInvalidResponse || !strings.Contains(err.Error(), c.hint) {
			t.Errorf("%s: error = %v", c.id, err)
		}
	}
}

func TestIndexingFailuresMapToClassesWithoutProviderText(t *testing.T) {
	cases := map[string]struct {
		answer   func(*http.Request, int) (*http.Response, error)
		class    provider.Class
		readReqs int
	}{
		"403 without owner right": {reply(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "")),
			provider.ClassPermission, 1},
		"429 without Retry-After": {reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED",
			"RATE_LIMIT_EXCEEDED")), provider.ClassRateLimited, 1},
		// A read is sent once more after a renewed token; a notification never.
		"401": {reply(http.StatusUnauthorized, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING")),
			provider.ClassAuth, 2},
		"500": {reply(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, 1},
	}
	tools := []struct {
		id, connection, args string
		read                 bool
	}{
		{indexingMetadataID, "all", `{"url":"` + jobURL + `"}`, true},
		{indexingMetadataBatchID, "all", urlsArgument(jobURLs(2)), true},
		{indexingPublishID, "index", `{"url":"` + jobURL + `","type":"URL_UPDATED"}`, false},
		{indexingPublishBatchID, "index", notificationsArgument(jobURLs(2), urlUpdated), false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			for _, tool := range tools {
				env := newEnvironment(t, nil)
				env.fake.answer = c.answer
				_, err := env.change(t, tool.id, tool.connection, tool.args)
				if classOf(err) != c.class {
					t.Fatalf("%s: error = %v (class %q), want %q", tool.id, err, classOf(err), c.class)
				}
				if strings.Contains(err.Error(), detailCanary) || strings.Contains(err.Error(), accessPrefix) {
					t.Errorf("%s: the message carries provider text or a token: %v", tool.id, err)
				}
				if c.class == provider.ClassPermission && !strings.Contains(err.Error(), "owner") {
					t.Errorf("%s: the permission message does not name the owner role: %v", tool.id, err)
				}
				want := 1
				if tool.read {
					want = c.readReqs
				}
				if calls, _ := env.fake.calls(); len(calls) != want {
					t.Errorf("%s: %d requests, want %d", tool.id, len(calls), want)
				}
			}
		})
	}
}

func TestIndexingTooManyRequestsHoldsTheNextCall(t *testing.T) {
	env := newEnvironment(t, nil)
	indexingLimiters = ratelimit.NewRegistry(time.Millisecond)
	env.fake.answer = reply(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"))
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	request := application.InvokeRequest{Operation: indexingPublishID, Connection: "index",
		Arguments: json.RawMessage(`{"url":"` + jobURL + `","type":"URL_UPDATED"}`), Confirmed: true}
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("first call = %v", err)
	}
	// The hold of at least a second outlasts the deadline, so the second call ends without a request.
	if _, err := env.core.Invoke(ctx, request); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("second call = %v, want rate-limited before sending", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one", len(calls))
	}
}

func TestIndexingBatchCountsEveryEntryAgainstTheSpacing(t *testing.T) {
	env := newEnvironment(t, nil)
	// 100 metadata reads at 10 ms each hold the next read for about a second.
	metadataLimiters = ratelimit.NewRegistry(10 * time.Millisecond)
	urls := jobURLs(maxBatch)
	parts := make([]batchPart, maxBatch)
	for i, address := range urls {
		parts[i] = okPart(i+1, metadataAnswer(address))
	}
	env.fake.answer = batchReply(parts...)
	if _, err := env.invoke(t, indexingMetadataBatchID, "all", urlsArgument(urls)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := env.core.Invoke(ctx, application.InvokeRequest{Operation: indexingMetadataID, Connection: "all",
		Arguments: json.RawMessage(`{"url":"` + jobURL + `"}`)})
	if classOf(err) != provider.ClassRateLimited {
		t.Fatalf("read after the batch = %v, want rate-limited before sending", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 1 {
		t.Errorf("%d requests, want one", len(calls))
	}
}

func TestIndexingToolsAskForTheIndexingScope(t *testing.T) {
	env := newEnvironment(t, nil)
	env.fake.tokenAnswer = func(int) (int, string) {
		return http.StatusOK, `{"access_token":"` + accessPrefix + `1","token_type":"Bearer","expires_in":3600,` +
			`"scope":"` + scopeRead + `"}`
	}
	// A consent without the Indexing scope ends the tool before Google is contacted.
	_, err := env.invoke(t, indexingMetadataID, "all", `{"url":"`+jobURL+`"}`)
	if classOf(err) != provider.ClassScopeMissing {
		t.Errorf("error = %v, want scope-missing", err)
	}
	if calls, _ := env.fake.calls(); len(calls) != 0 {
		t.Errorf("%d requests, want none", len(calls))
	}
	resolved, err := env.cfg.Resolve("index", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(resolved.TokenScopes, " ") != scopeIndexing {
		t.Errorf("scopes of the indexing connection = %v", resolved.TokenScopes)
	}
}

func TestBatchAnswerReadingIsBounded(t *testing.T) {
	// A part beyond its bound and a part that is no application/http answer leave their entries without one.
	long := strings.Repeat("x", maxPartBytes)
	answers, ok := readBatch(strings.NewReader("--b\r\nContent-Type: application/http\r\nContent-ID: <response-item-1>\r\n\r\n"+
		"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n\""+long+"\"\r\n"+
		"--b\r\nContent-Type: text/plain\r\nContent-ID: <response-item-2>\r\n\r\nHTTP/1.1 200 OK\r\n\r\n{}\r\n--b--\r\n"),
		"multipart/mixed; boundary=b", 2)
	if !ok || !answers[0].missing || !answers[1].missing {
		t.Errorf("answers = %+v, %t", answers, ok)
	}
	if _, ok := readBatch(strings.NewReader(""), "application/json", 1); ok {
		t.Error("a JSON answer was read as a batch")
	}
	for _, id := range []string{"<response-item-0>", "<response-item-3>", "<item-1>", "<response-item-x>", ""} {
		if _, ok := answerIndex(id, 2); ok {
			t.Errorf("answerIndex(%q) matched", id)
		}
	}
}

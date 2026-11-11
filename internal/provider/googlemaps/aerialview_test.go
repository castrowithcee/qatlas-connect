package googlemaps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	videoAddress = "1600 Amphitheatre Parkway, Mountain View, CA"
	videoUID     = "vid-123_ABC"
	// signedHost is the host of the addresses in the answers. The transport refuses it, so any fetch of an
	// address would fail the test and show up as an error or a second request.
	signedHost = "videos.example.test"
)

// aerialRewrite sends the Aerial View host to the test server and refuses every other host.
type aerialRewrite struct {
	target *url.URL
	base   http.RoundTripper
	seen   *[]string
}

func (r aerialRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	*r.seen = append(*r.seen, req.URL.Host)
	if req.URL.Host != "aerialview.googleapis.com" || req.URL.Scheme != "https" {
		return nil, errors.New("unexpected host")
	}
	clone := req.Clone(req.Context())
	clone.Header.Set(originalHost, req.URL.Host)
	clone.URL.Scheme, clone.URL.Host, clone.Host = r.target.Scheme, r.target.Host, ""
	return r.base.RoundTrip(clone)
}

type aerialEnvironment struct {
	*environment
	hosts *[]string
}

func newAerialEnvironment(t *testing.T, f *fakeGoogle) *aerialEnvironment {
	t.Helper()
	server := httptest.NewTLSServer(f)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	previous := transport
	hosts := &[]string{}
	transport = aerialRewrite{target: target, base: server.Client().Transport, seen: hosts}
	t.Cleanup(func() { transport = previous })
	t.Cleanup(limiters.Replace(keyValue, ratelimit.New(0, time.Now, func(context.Context, time.Duration) error { return nil })))
	reads := 0
	red := &redact.Redactor{}
	lookup := func(name string) string {
		reads++
		if name == keyEnv {
			return keyValue
		}
		return ""
	}
	cfg := coreConfig()
	cfg.Connections["change"] = config.Connection{Service: "maps", Credential: "maps-key",
		Effects: []config.Effect{config.EffectRead, config.EffectCreate}}
	cfg.Connections["listed"] = config.Connection{Service: "maps", Credential: "maps-key",
		Tools: []string{videosLookupID, videosMetadataID}}
	return &aerialEnvironment{environment: &environment{fake: f,
		core: application.New(registry(t), cfg, secret.NewWith(lookup, nil, nil, red), red),
		red:  red, reads: &reads, t: t}, hosts: hosts}
}

const (
	videoActive = `{"state":"ACTIVE","metadata":{"videoId":"` + videoUID + `","captureDate":{"year":2023,"month":6},` +
		`"duration":"43.5s","state":"ACTIVE","surprise":1},"uris":{"MP4_HIGH":{"landscapeUri":` +
		`"https://` + signedHost + `/a.mp4?X-Goog-Signature=abc","portraitUri":"https://` + signedHost + `/b.mp4?sig=1","x":1},` +
		`"IMAGE":{"landscapeUri":"https://` + signedHost + `/t.jpg"},"HLS":{"landscapeUri":"http://` + signedHost + `/h"},` +
		`"NEW_FORMAT":{"portraitUri":"https://` + signedHost + `/n"},"lower_case":{"landscapeUri":"https://` + signedHost + `/l"}},` +
		`"extra":"x"}`
	videoProcessing = `{"state":"PROCESSING"}`
)

func decodeVideo(t *testing.T, result string) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatalf("result %q: %v", result, err)
	}
	return got
}

func TestVideoLookupByAddressAndByID(t *testing.T) {
	for name, tc := range map[string]struct{ arguments, query string }{
		"address": {`{"address":"` + videoAddress + `"}`, "address=" + url.QueryEscape(videoAddress)},
		"id":      {`{"video_id":"` + videoUID + `"}`, "videoId=" + videoUID},
	} {
		env := newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", videoActive)})
		result, err := env.invokeTool(videosLookupID, "listed", tc.arguments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		requests := env.fake.recorded()
		if len(requests) != 1 {
			t.Fatalf("%s: %d requests", name, len(requests))
		}
		r := requests[0]
		if r.method != http.MethodGet || r.path != "/v1/videos:lookupVideo" || r.rawQuery != tc.query || r.body != "" ||
			r.header.Get(originalHost) != "aerialview.googleapis.com" || r.header.Get("X-Goog-Api-Key") != keyValue ||
			r.header.Get("X-Goog-FieldMask") != "" {
			t.Errorf("%s: request = %+v", name, r)
		}
		got := decodeVideo(t, result)
		meta, _ := got["metadata"].(map[string]any)
		uris, _ := got["uris"].(map[string]any)
		if got["state"] != "ACTIVE" || meta["videoId"] != videoUID || meta["duration"] != "43.5s" || meta["state"] != "ACTIVE" ||
			meta["surprise"] != nil || got["extra"] != nil {
			t.Errorf("%s: result = %s", name, result)
		}
		if date, _ := meta["captureDate"].(map[string]any); date["year"] != 2023.0 || date["month"] != 6.0 {
			t.Errorf("%s: captureDate = %v", name, meta["captureDate"])
		}
		high, _ := uris["MP4_HIGH"].(map[string]any)
		if high["landscapeUri"] != "https://"+signedHost+"/a.mp4?X-Goog-Signature=abc" ||
			high["portraitUri"] != "https://"+signedHost+"/b.mp4?sig=1" || high["x"] != nil {
			t.Errorf("%s: MP4_HIGH = %v", name, high)
		}
		if _, ok := uris["IMAGE"]; !ok {
			t.Errorf("%s: IMAGE missing", name)
		}
		if _, ok := uris["NEW_FORMAT"]; !ok {
			t.Errorf("%s: an unknown media format must be passed on", name)
		}
		for _, dropped := range []string{"HLS", "lower_case"} {
			if _, ok := uris[dropped]; ok {
				t.Errorf("%s: %s must be left out", name, dropped)
			}
		}
		if strings.Contains(r.rawQuery+r.path+r.body, keyValue) || strings.Contains(result, keyValue) ||
			strings.Contains(env.red.Apply("x "+keyValue), keyValue) {
			t.Errorf("%s: the key is outside the header or not redacted", name)
		}
		if len(*env.hosts) != 1 || (*env.hosts)[0] != "aerialview.googleapis.com" {
			t.Errorf("%s: hosts = %v; a returned address was fetched", name, *env.hosts)
		}
	}
}

func TestVideoLookupProcessingAndUnknownState(t *testing.T) {
	for state, answer := range map[string]string{"PROCESSING": videoProcessing, "FAILED": `{"state":"FAILED"}`,
		"BRAND_NEW": `{"state":"BRAND_NEW"}`} {
		env := newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", answer)})
		result, err := env.invokeTool(videosLookupID, "listed", `{"address":"`+videoAddress+`"}`)
		got := decodeVideo(t, result)
		if err != nil || got["state"] != state || got["uris"] != nil || got["metadata"] != nil {
			t.Errorf("%s: result = %s, err = %v", state, result, err)
		}
	}
}

func TestVideoMetadataByAddressAndByID(t *testing.T) {
	answer := `{"videoId":"` + videoUID + `","captureDate":{"year":2023,"month":6},"duration":"3.5s","state":"ACTIVE","x":1}`
	for name, tc := range map[string]struct{ arguments, query string }{
		"address": {`{"address":"` + videoAddress + `"}`, "address=" + url.QueryEscape(videoAddress)},
		"id":      {`{"video_id":"` + videoUID + `"}`, "videoId=" + videoUID},
	} {
		env := newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", answer)})
		result, err := env.invokeTool(videosMetadataID, "read", tc.arguments)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r := env.fake.recorded()[0]
		if r.method != http.MethodGet || r.path != "/v1/videos:lookupVideoMetadata" || r.rawQuery != tc.query {
			t.Errorf("%s: request = %+v", name, r)
		}
		got := decodeVideo(t, result)
		if got["videoId"] != videoUID || got["duration"] != "3.5s" || got["state"] != "ACTIVE" || got["x"] != nil ||
			got["uris"] != nil {
			t.Errorf("%s: result = %s", name, result)
		}
	}
}

func TestVideoSelectorIsRefusedBeforeAnyIO(t *testing.T) {
	long := strings.Repeat("a", 300)
	for name, arguments := range map[string]string{
		"both":            `{"video_id":"x","address":"y"}`,
		"neither":         `{}`,
		"blank id":        `{"video_id":"  "}`,
		"blank address":   `{"address":" "}`,
		"control id":      `{"video_id":"a\u0000b"}`,
		"control address": `{"address":"a\nb"}`,
		"long id":         `{"video_id":"` + long + `"}`,
		"long address":    `{"address":"` + long + `"}`,
		"key":             `{"address":"a","key":"x"}`,
		"path":            `{"address":"a","path":"/v1/x"}`,
	} {
		for _, tool := range []string{videosLookupID, videosMetadataID} {
			env := newAerialEnvironment(t, &fakeGoogle{})
			if _, err := env.invokeTool(tool, "listed", arguments); err == nil {
				t.Errorf("%s %s: no error", tool, name)
			}
			if len(env.fake.recorded()) != 0 || *env.reads != 0 || len(*env.hosts) != 0 {
				t.Errorf("%s %s: %d requests, %d secret reads", tool, name, len(env.fake.recorded()), *env.reads)
			}
		}
	}
	// The handlers check on their own, before any secret access.
	resolved := &config.Resolved{BaseURL: apiRoot}
	for name, call := range map[string]func() error{
		"lookup": func() error {
			_, err := handleVideosLookup(context.Background(), resolved, nil, nil, json.RawMessage(`{}`))
			return err
		},
		"metadata": func() error {
			_, err := handleVideosMetadata(context.Background(), resolved, nil, nil, json.RawMessage(`{"video_id":"a","address":"b"}`))
			return err
		},
		"render": func() error {
			_, err := handleVideosRender(context.Background(), resolved, nil, nil, json.RawMessage(`{}`))
			return err
		},
	} {
		var invalid *provider.InvalidRequestError
		if err := call(); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want an invalid request before any secret access", name, err)
		}
	}
	for name, resolved := range map[string]*config.Resolved{"other origin": {BaseURL: aerialViewRoot}, "target": {BaseURL: apiRoot, Target: "x"}} {
		if _, err := handleVideosLookup(context.Background(), resolved, nil, nil, json.RawMessage(`{"address":"a"}`)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestVideoLookupNeedsAnExplicitToolList(t *testing.T) {
	env := newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", videoActive)})
	if _, err := env.invokeTool(videosLookupID, "read", `{"address":"a"}`); err == nil {
		t.Error("a connection by effect offered the lookup with signed addresses")
	}
	if len(env.fake.recorded()) != 0 || *env.reads != 0 {
		t.Errorf("%d requests, %d secret reads", len(env.fake.recorded()), *env.reads)
	}
}

func TestVideoRenderNeedsConfirmationAndSendsOneRequest(t *testing.T) {
	env := newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json",
		`{"state":"ACTIVE","metadata":{"videoId":"`+videoUID+`","state":"ACTIVE","duration":"1s"},"uris":{"MP4_HIGH":{"landscapeUri":"https://`+signedHost+`/x"}}}`)})
	arguments := `{"address":"` + videoAddress + `"}`
	_, err := env.invokeTool(videosRenderID, "change", arguments)
	var needed *application.ConfirmationRequiredError
	if !errors.As(err, &needed) {
		t.Fatalf("err = %v, want a confirmation request", err)
	}
	if len(env.fake.recorded()) != 0 || *env.reads != 0 {
		t.Fatalf("an unconfirmed call sent %d requests and read %d secrets", len(env.fake.recorded()), *env.reads)
	}
	if _, err := env.invokeTool(videosRenderID, "read", arguments); err == nil || errors.As(err, &needed) {
		t.Errorf("a read connection offered the render: %v", err)
	}
	result, err := env.confirmed(videosRenderID, "change", arguments)
	if err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	requests := env.fake.recorded()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	r := requests[0]
	if r.method != http.MethodPost || r.path != "/v1/videos:renderVideo" || r.rawQuery != "" ||
		r.header.Get(originalHost) != "aerialview.googleapis.com" || r.header.Get("X-Goog-Api-Key") != keyValue ||
		r.header.Get("Content-Type") != "application/json" || r.body != `{"address":"`+videoAddress+`"}` {
		t.Errorf("request = %+v", r)
	}
	got := decodeVideo(t, result)
	meta, _ := got["metadata"].(map[string]any)
	if got["state"] != "ACTIVE" || meta["videoId"] != videoUID || got["uris"] != nil {
		t.Errorf("result = %s", result)
	}
	env = newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", videoProcessing)})
	if result, err := env.confirmed(videosRenderID, "change", arguments); err != nil || decodeVideo(t, result)["state"] != "PROCESSING" {
		t.Errorf("processing: %s, %v", result, err)
	}
}

func TestVideoRenderReportsUncertaintyWithoutRepeating(t *testing.T) {
	arguments := `{"address":"` + videoAddress + `"}`
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"500":        reply(500, "application/json", errorObject(500, "INTERNAL", "X")),
		"503":        reply(503, "", ""),
		"504":        reply(504, "", ""),
		"not json":   reply(200, "text/html", "<html>"),
		"empty body": reply(200, "application/json", ""),
		"unreadable": reply(200, "application/json", "{"),
		"redirect":   reply(302, "", ""),
		"null body":  reply(200, "application/json", "null"),
		"array":      reply(200, "application/json", "[]"),
		"no state":   reply(200, "application/json", `{"surprise":1}`),
	} {
		env := newAerialEnvironment(t, &fakeGoogle{answer: answer})
		_, err := env.confirmed(videosRenderID, "change", arguments)
		if err == nil || !strings.Contains(err.Error(), "uncertain") || strings.Contains(err.Error(), detailCanary) ||
			strings.Contains(err.Error(), keyValue) {
			t.Errorf("%s: err = %v", name, err)
		}
		if n := len(env.fake.recorded()); n != 1 {
			t.Errorf("%s: %d requests, want exactly one", name, n)
		}
	}
	for name, cause := range map[string]error{
		"timeout": context.DeadlineExceeded, "reset": fmt.Errorf("read tcp: %w", syscall.ECONNRESET),
		"cancelled": context.Canceled, "unknown": errors.New("boom " + keyValue),
	} {
		env := newAerialEnvironment(t, &fakeGoogle{})
		transport = failingTransport{err: cause}
		_, err := env.confirmed(videosRenderID, "change", arguments)
		if err == nil || !strings.Contains(err.Error(), "uncertain") || strings.Contains(err.Error(), keyValue) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// A refusal that says Google did nothing is not uncertain.
	for status, body := range map[int]string{400: errorObject(400, "INVALID_ARGUMENT", "X"),
		401: errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), 403: errorObject(403, "PERMISSION_DENIED", "X"),
		429: errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED")} {
		env := newAerialEnvironment(t, &fakeGoogle{answer: reply(status, "application/json", body)})
		if _, err := env.confirmed(videosRenderID, "change", arguments); err == nil || strings.Contains(err.Error(), "uncertain") {
			t.Errorf("%d: err = %v", status, err)
		}
	}
}

func TestVideoErrorFormsKeepProviderTextApart(t *testing.T) {
	for _, tool := range []string{videosLookupID, videosMetadataID} {
		for _, tc := range []struct {
			status int
			body   string
			class  provider.Class
			text   string
		}{
			{401, errorObject(401, "UNAUTHENTICATED", "CREDENTIALS_MISSING"), provider.ClassAuth, ""},
			{403, errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED"), provider.ClassPermission, "Aerial View API"},
			{429, errorObject(429, "RESOURCE_EXHAUSTED", "RATE_LIMIT_EXCEEDED"), provider.ClassRateLimited, ""},
			{404, errorObject(404, "NOT_FOUND", "X"), provider.ClassNotFound, "no Aerial View video"},
			{400, errorObject(400, "INVALID_ARGUMENT", "X"), provider.ClassInvalidResponse, "US address"},
			{500, errorObject(500, "INTERNAL", "X"), provider.ClassProviderError, ""},
			// Not-JSON and legacy-shaped bodies carry no usable Google error object; the status decides.
			{404, `{"status":"ZERO_RESULTS"}`, provider.ClassNotFound, "no Aerial View video"},
			{403, `{"status":"REQUEST_DENIED","error_message":"` + detailCanary + `"}`, provider.ClassPermission, "Aerial View API"},
			{404, "<html>" + detailCanary, provider.ClassNotFound, "no Aerial View video"},
		} {
			env := newAerialEnvironment(t, &fakeGoogle{answer: reply(tc.status, "application/json", tc.body)})
			_, err := env.invokeTool(tool, "listed", `{"address":"`+videoAddress+`"}`)
			if classOf(err) != tc.class {
				t.Errorf("%s %d: class = %q, err = %v", tool, tc.status, classOf(err), err)
				continue
			}
			var failed *provider.Error
			_ = errors.As(err, &failed)
			if strings.Contains(failed.Message, detailCanary) || strings.Contains(err.Error(), keyValue) ||
				strings.Contains(failed.Message, "Places") || strings.Contains(failed.Message, "Solar") ||
				!strings.Contains(failed.Message, tc.text) || strings.Contains(failed.Message, "uncertain") {
				t.Errorf("%s %d: message = %q", tool, tc.status, failed.Message)
			}
			if tc.status == 404 || tc.status == 400 || tc.status == 403 {
				if failed.Original == nil && strings.HasPrefix(tc.body, "{") {
					t.Errorf("%s %d: the provider text is not kept apart", tool, tc.status)
				}
			}
			if len(env.fake.recorded()) != 1 {
				t.Errorf("%s %d: %d requests, want one", tool, tc.status, len(env.fake.recorded()))
			}
		}
	}
}

func TestVideoUnusableAnswersAndTimeout(t *testing.T) {
	for name, answer := range map[string]func(http.ResponseWriter, *http.Request){
		"not json":     reply(200, "text/plain", "x"),
		"empty":        reply(200, "application/json", `{}`),
		"only unknown": reply(200, "application/json", `{"surprise":1}`),
		"array":        reply(200, "application/json", `[]`),
		"null":         reply(200, "application/json", `null`),
		"unreadable":   reply(200, "application/json", `{"state":`),
		"redirect":     reply(302, "", ""),
	} {
		for _, tool := range []string{videosLookupID, videosMetadataID} {
			env := newAerialEnvironment(t, &fakeGoogle{answer: answer})
			_, err := env.invokeTool(tool, "listed", `{"address":"a"}`)
			if classOf(err) != provider.ClassInvalidResponse && classOf(err) != provider.ClassProviderError {
				t.Errorf("%s %s: err = %v", tool, name, err)
			}
			if n := len(env.fake.recorded()); n != 1 {
				t.Errorf("%s %s: %d requests", tool, name, n)
			}
		}
	}
	for _, tool := range []string{videosLookupID, videosMetadataID} {
		env := newAerialEnvironment(t, &fakeGoogle{})
		transport = failingTransport{err: context.DeadlineExceeded}
		if _, err := env.invokeTool(tool, "listed", `{"address":"a"}`); classOf(err) != provider.ClassTimeout {
			t.Errorf("%s timeout err = %v", tool, err)
		}
	}
}

func TestVideoStringsAndAddressesAreBounded(t *testing.T) {
	long := strings.Repeat("é", 5000)
	signed := "https://" + signedHost + "/"
	answer := map[string]any{"state": long, "metadata": map[string]any{"videoId": strings.Repeat("v", 1025*4),
		"duration": long, "state": long, "captureDate": map[string]any{"year": 2023.0}},
		"uris": map[string]any{"AAA_LOW": map[string]any{"landscapeUri": signed + strings.Repeat("a", maxVideoURIBytes),
			"portraitUri": signed + strings.Repeat("a", 3000)}}}
	for i := 0; i < 40; i++ {
		answer["uris"].(map[string]any)[fmt.Sprintf("F%02d", i)] = map[string]any{"landscapeUri": signed + "x"}
	}
	payload, _ := json.Marshal(answer)
	env := newAerialEnvironment(t, &fakeGoogle{answer: reply(200, "application/json", string(payload))})
	result, err := env.invokeTool(videosLookupID, "listed", `{"address":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeVideo(t, result)
	meta := got["metadata"].(map[string]any)
	if n := len([]rune(got["state"].(string))); n > maxShortRunes+1 {
		t.Errorf("state has %d characters", n)
	}
	if len([]rune(meta["duration"].(string))) > maxShortRunes+1 || meta["videoId"] != nil {
		t.Errorf("metadata = %v", meta)
	}
	uris := got["uris"].(map[string]any)
	if len(uris) != maxVideoFormats {
		t.Errorf("%d formats, want %d", len(uris), maxVideoFormats)
	}
	low := uris["AAA_LOW"].(map[string]any)
	if low["landscapeUri"] != nil || low["portraitUri"] == nil {
		t.Errorf("MP4_LOW = %v", low)
	}
}

func TestVideoLinkRule(t *testing.T) {
	for value, ok := range map[string]bool{
		"https://example.test/v?X-Goog-Signature=abc": true,
		"https://example.test":                        true,
		"http://example.test/v":                       false,
		"https://user:pw@example.test/v":              false,
		"https://example.test/v w":                    false,
		"https://example.test/v\u0000":                false,
		"https://example.test\\evil":                  false,
		"javascript:alert(1)":                         false,
		"//example.test/v":                            false,
		"https:///v":                                  false,
		"":                                            false,
		"https://example.test/" + strings.Repeat("a", maxVideoURIBytes): false,
	} {
		if (videoLink(value) != "") != ok {
			t.Errorf("videoLink(%.40q) shown = %v, want %v", value, videoLink(value) != "", ok)
		}
	}
}

func TestVideoToolsRiskProfilesAndDescription(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	listed := map[string]bool{}
	for _, profile := range metadata.Profiles {
		for _, id := range profile.Tools {
			listed[id] = true
		}
	}
	if !listed[videosMetadataID] || listed[videosLookupID] || listed[videosRenderID] {
		t.Errorf("profiles = %+v", metadata.Profiles)
	}
	read := capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
		Confirmation: capability.ConfirmationNone, OpenWorld: true, DataSensitivity: addressDataSensitivity}
	create := capability.Risk{Effect: capability.EffectCreate, Idempotency: capability.IdempotencyNonIdempotent,
		Confirmation: capability.ConfirmationRequired, OpenWorld: true, DataSensitivity: addressDataSensitivity}
	for _, tc := range []struct {
		d        capability.Descriptor
		risk     capability.Risk
		required int
		allow    bool
	}{
		{videosLookup, read, 0, true}, {videosMetadataTool, read, 0, false}, {videosRender, create, 1, false},
	} {
		d := tc.d
		if d.Risk != tc.risk || d.RequiresToolAllowList != tc.allow || d.Provider != Provider {
			t.Errorf("%s: risk = %+v, allow list %v", d.ID, d.Risk, d.RequiresToolAllowList)
		}
		for _, want := range []string{"US addresses only", "Aerial View in the Pro category", "pay-as-you-go",
			"attribution", "Google Maps", "must not be stored", "video ID", "enabled", "anonymous probe", "quota",
			"untrusted", "personal data"} {
			if !strings.Contains(d.Description, want) {
				t.Errorf("%s: description lacks %q", d.ID, want)
			}
		}
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if json.Unmarshal(d.InputSchema, &schema) != nil || len(schema.Required) != tc.required {
			t.Fatalf("%s: input schema = %s", d.ID, d.InputSchema)
		}
		for name := range schema.Properties {
			found := false
			for _, argument := range d.Arguments {
				found = found || argument.Name == name
			}
			if !found {
				t.Errorf("%s: argument %s is undocumented", d.ID, name)
			}
		}
		for _, argument := range d.Arguments {
			if schema.Properties[argument.Name] == nil {
				t.Errorf("%s: documented argument %s is not in the schema", d.ID, argument.Name)
			}
		}
		for _, banned := range []string{"key", "target", "url", "path", "header"} {
			if schema.Properties[banned] != nil {
				t.Errorf("%s: tool takes %s", d.ID, banned)
			}
		}
		var out struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if json.Unmarshal(d.OutputSchema, &out) != nil {
			t.Fatalf("%s: output schema = %s", d.ID, d.OutputSchema)
		}
		for name := range out.Properties {
			found := false
			for _, field := range d.Fields {
				found = found || field.Name == name
			}
			if !found {
				t.Errorf("%s: result member %s is undocumented", d.ID, name)
			}
		}
	}
	if aerialViewRoot != "https://aerialview.googleapis.com" || metadata.ValidateBaseURL(aerialViewRoot) == nil {
		t.Error("the Aerial View origin is not fixed or may be a base URL")
	}
}

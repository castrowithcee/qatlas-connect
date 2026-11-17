package googleanalytics

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// annotationEnv adds connections that list the five annotation tools, with the bindings of the stream tests.
func annotationEnv(t *testing.T, fake *fakeGoogle) *testEnv {
	t.Helper()
	env := newEnv(t, fake)
	tools := []string{annotationsListID, annotationsGetID, annotationsCreateID, annotationsUpdateID, annotationsDeleteID}
	for name, targets := range map[string][]string{"sfree": nil, "saccount": {"accounts/100"},
		"sproperty": {"properties/200"}, "smixed": {"accounts/100", "properties/901"}} {
		env.cfg.Connections[name] = config.Connection{Service: "ga", Credential: "login", Targets: targets, Tools: tools}
	}
	if err := env.cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return env
}

const (
	dayAnnotationJSON = `{"name":"properties/200/reportingDataAnnotations/3001","title":"Site relaunch",` +
		`"description":"New checkout","color":"BLUE","systemGenerated":false,` +
		`"annotationDate":{"year":2026,"month":11,"day":20},"extra":1}`
	rangeAnnotationJSON = `{"name":"properties/200/reportingDataAnnotations/3002","title":"Holiday sale","color":"RED",` +
		`"annotationDateRange":{"startDate":{"year":2026,"month":12,"day":24},"endDate":{"year":2026,"month":12,"day":31}}}`
	systemAnnotationJSON = `{"name":"properties/200/reportingDataAnnotations/3003","title":"Traffic change",` +
		`"color":"ORANGE","systemGenerated":true,"annotationDate":{"year":2026,"month":11,"day":1}}`
)

type annotationsOut struct {
	Annotations []annotation `json:"annotations"`
	NextCursor  string       `json:"next_cursor"`
	HasMore     bool         `json:"has_more"`
}

func TestAnnotationsListPagesThroughThreePagesWithTheFilter(t *testing.T) {
	pages := []string{
		`{"reportingDataAnnotations":[` + dayAnnotationJSON + `],"nextPageToken":"t2"}`,
		`{"reportingDataAnnotations":[` + rangeAnnotationJSON + `],"nextPageToken":"t3"}`,
		`{"reportingDataAnnotations":[` + systemAnnotationJSON + `]}`,
	}
	env := annotationEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", pages[n-1]), nil
	}})
	const filter = `title:Sale OR color=RED`
	cursor, titles := "", []string{}
	for i := 0; i < 3; i++ {
		args := `{"property":"properties/200","page_size":1,"filter":"` + filter + `"`
		if cursor != "" {
			args += `,"cursor":"` + cursor + `"`
		}
		result, err := env.call(annotationsListID, "sfree", args+`}`)
		if err != nil {
			t.Fatal(err)
		}
		var out annotationsOut
		if err := json.Unmarshal([]byte(result), &out); err != nil || len(out.Annotations) != 1 || out.HasMore != (i < 2) ||
			strings.Contains(result, "extra") {
			t.Fatalf("page %d = %s, %v", i, result, err)
		}
		titles = append(titles, string(out.Annotations[0].Title))
		cursor = out.NextCursor
	}
	requests, _ := env.fake.seen()
	if !slices.Equal(titles, []string{"Site relaunch", "Holiday sale", "Traffic change"}) || len(requests) != 3 ||
		requests[0].method != http.MethodGet || requests[0].path != alphaPrefix+"properties/200/reportingDataAnnotations" ||
		!strings.Contains(requests[0].rawQuery, "pageSize=1") ||
		!strings.Contains(requests[0].rawQuery, "filter=title%3ASale+OR+color%3DRED") ||
		!strings.Contains(requests[1].rawQuery, "pageToken=t2") || !strings.Contains(requests[2].rawQuery, "pageToken=t3") {
		t.Errorf("titles %v, requests %+v", titles, requests)
	}
	// The cursor fits only the same property and the same filter.
	env = annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"reportingDataAnnotations":[],"nextPageToken":"t2"}`)})
	result, err := env.call(annotationsListID, "sfree", `{"property":"properties/200","filter":"a"}`)
	var first annotationsOut
	if err != nil || json.Unmarshal([]byte(result), &first) != nil || first.NextCursor == "" {
		t.Fatalf("%v %s", err, result)
	}
	for _, args := range []string{`{"property":"properties/201","filter":"a","cursor":"` + first.NextCursor + `"}`,
		`{"property":"properties/200","filter":"b","cursor":"` + first.NextCursor + `"}`,
		`{"property":"properties/200","cursor":"` + first.NextCursor + `"}`} {
		if _, err := env.call(annotationsListID, "sfree", args); !refusedInvalid(err) {
			t.Errorf("%s: %v", args, err)
		}
	}
}

func TestAnnotationsReadEveryMember(t *testing.T) {
	answers := map[string]string{"3001": dayAnnotationJSON, "3002": rangeAnnotationJSON, "3003": systemAnnotationJSON}
	env := annotationEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]]), nil
	}})
	var got [3]annotation
	for i, id := range []string{"3001", "3002", "3003"} {
		result, err := env.call(annotationsGetID, "sfree", `{"name":"properties/200/reportingDataAnnotations/`+id+`"}`)
		if err != nil || json.Unmarshal([]byte(result), &got[i]) != nil || strings.Contains(result, "extra") {
			t.Fatalf("%s: %v %s", id, err, result)
		}
	}
	day, span, system := got[0], got[1], got[2]
	if day.Title != "Site relaunch" || day.Description != "New checkout" || day.Color != "BLUE" ||
		day.SystemGenerated == nil || *day.SystemGenerated || day.AnnotationDate == nil ||
		*day.AnnotationDate != (annotationDay{Year: 2026, Month: 11, Day: 20}) || day.AnnotationDateRange != nil ||
		span.AnnotationDateRange == nil || *span.AnnotationDateRange.StartDate != (annotationDay{Year: 2026, Month: 12, Day: 24}) ||
		*span.AnnotationDateRange.EndDate != (annotationDay{Year: 2026, Month: 12, Day: 31}) || span.AnnotationDate != nil ||
		system.SystemGenerated == nil || !*system.SystemGenerated || system.Color != "ORANGE" {
		t.Errorf("annotations = %+v %+v %+v", day, span, system)
	}
}

func TestAnnotationTextIsBoundedAndOtherPropertiesAreDropped(t *testing.T) {
	env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"reportingDataAnnotations":[`+
		`{"name":"properties/200/reportingDataAnnotations/1","title":"`+strings.Repeat("t", 400)+`","description":"`+
		strings.Repeat("d", 1500)+`","color":"RED"},`+
		`{"name":"properties/999/reportingDataAnnotations/2","title":"x"},{"name":"nonsense"},{"title":"unnamed"}]}`)})
	result, err := env.call(annotationsListID, "sfree", `{"property":"properties/200"}`)
	var out annotationsOut
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || len(out.Annotations) != 1 ||
		len([]rune(string(out.Annotations[0].Title))) != maxTextRunes+1 ||
		len([]rune(string(out.Annotations[0].Description))) != maxNoteRunes+1 {
		t.Errorf("result %d bytes, %v", len(result), err)
	}
	env = annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, dayAnnotationJSON)})
	if _, err := env.call(annotationsGetID, "sfree", `{"name":"properties/200/reportingDataAnnotations/9"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("get with another name: %v", err)
	}
}

func TestAnnotationsCreateWithADateAndWithARange(t *testing.T) {
	for _, c := range []struct{ args, answer, body string }{
		{`{"property":"properties/200","title":"Site relaunch","description":"New checkout","color":"BLUE","annotation_date":"2026-11-20"}`,
			dayAnnotationJSON, `{"title":"Site relaunch","description":"New checkout","color":"BLUE","annotationDate":{"year":2026,"month":11,"day":20}}`},
		{`{"property":"properties/200","title":"Holiday sale","color":"RED","annotation_date_range":{"start_date":"2026-12-24","end_date":"2026-12-31"}}`,
			rangeAnnotationJSON, `{"title":"Holiday sale","color":"RED","annotationDateRange":{"startDate":{"year":2026,"month":12,"day":24},"endDate":{"year":2026,"month":12,"day":31}}}`},
	} {
		env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, c.answer)})
		if _, err := env.call(annotationsCreateID, "sfree", c.args); err == nil {
			t.Fatal("create without confirm was accepted")
		}
		env.noIO(t)
		result, err := env.confirmed(annotationsCreateID, "sfree", c.args)
		requests, _ := env.fake.seen()
		if err != nil || len(requests) != 1 || requests[0].method != http.MethodPost ||
			requests[0].path != alphaPrefix+"properties/200/reportingDataAnnotations" || requests[0].rawQuery != "" ||
			requests[0].body != c.body || requests[0].contentType != "application/json" || !strings.Contains(result, "300") {
			t.Fatalf("%v, %+v, %s", err, requests, result)
		}
	}
}

func TestAnnotationsCreateRefusesBadArgumentsBeforeAnyIO(t *testing.T) {
	env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, dayAnnotationJSON)})
	base := `{"property":"properties/200","title":"T","color":"RED",`
	rng := `"annotation_date_range":{"start_date":"2026-12-24","end_date":"2026-12-31"}`
	for name, args := range map[string]string{
		"no date":            `{"property":"properties/200","title":"T","color":"RED"}`,
		"both dates":         base + `"annotation_date":"2026-11-20",` + rng + `}`,
		"no title":           `{"property":"properties/200","color":"RED","annotation_date":"2026-11-20"}`,
		"no color":           `{"property":"properties/200","title":"T","annotation_date":"2026-11-20"}`,
		"blank title":        `{"property":"properties/200","title":"  ","color":"RED","annotation_date":"2026-11-20"}`,
		"control in title":   `{"property":"properties/200","title":"a\u0001","color":"RED","annotation_date":"2026-11-20"}`,
		"long title":         `{"property":"properties/200","title":"` + strings.Repeat("t", 301) + `","color":"RED","annotation_date":"2026-11-20"}`,
		"long description":   base + `"description":"` + strings.Repeat("d", 1001) + `","annotation_date":"2026-11-20"}`,
		"control in text":    base + `"description":"a\u0000","annotation_date":"2026-11-20"}`,
		"orange":             `{"property":"properties/200","title":"T","color":"ORANGE","annotation_date":"2026-11-20"}`,
		"unspecified":        `{"property":"properties/200","title":"T","color":"COLOR_UNSPECIFIED","annotation_date":"2026-11-20"}`,
		"lowercase color":    `{"property":"properties/200","title":"T","color":"red","annotation_date":"2026-11-20"}`,
		"bad date":           base + `"annotation_date":"2026-02-30"}`,
		"date form":          base + `"annotation_date":"2026-2-3"}`,
		"year zero":          base + `"annotation_date":"0000-01-01"}`,
		"range reversed":     base + `"annotation_date_range":{"start_date":"2026-12-31","end_date":"2026-12-24"}}`,
		"range bad end":      base + `"annotation_date_range":{"start_date":"2026-12-24","end_date":"soon"}}`,
		"range missing end":  base + `"annotation_date_range":{"start_date":"2026-12-24"}}`,
		"unknown argument":   base + `"annotation_date":"2026-11-20","systemGenerated":true}`,
		"property not name":  `{"property":"nonsense","title":"T","color":"RED","annotation_date":"2026-11-20"}`,
		"property with path": `{"property":"properties/200/x","title":"T","color":"RED","annotation_date":"2026-11-20"}`,
	} {
		_, err := env.confirmed(annotationsCreateID, "sfree", args)
		if !refusedInvalid(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	env.noIO(t)
	// A same-day range is a valid range.
	env = annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, rangeAnnotationJSON)})
	if _, err := env.confirmed(annotationsCreateID, "sfree", base+`"annotation_date_range":{"start_date":"2026-12-24","end_date":"2026-12-24"}}`); err != nil {
		t.Errorf("same-day range: %v", err)
	}
}

func TestAnnotationsUpdateMaskComesFromSetArguments(t *testing.T) {
	const name = `"name":"properties/200/reportingDataAnnotations/3001"`
	for _, c := range []struct{ args, mask, body string }{
		{`"title":"New"`, "title", `{"title":"New"}`},
		{`"description":""`, "description", `{"description":""}`},
		{`"color":"GREEN","title":"New","description":"d"`, "title,description,color",
			`{"title":"New","description":"d","color":"GREEN"}`},
		{`"annotation_date":"2026-11-21"`, "annotation_date", `{"annotationDate":{"year":2026,"month":11,"day":21}}`},
		{`"annotation_date_range":{"start_date":"2026-12-01","end_date":"2026-12-02"}`, "annotation_date_range",
			`{"annotationDateRange":{"startDate":{"year":2026,"month":12,"day":1},"endDate":{"year":2026,"month":12,"day":2}}}`},
	} {
		env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, dayAnnotationJSON)})
		args := `{` + name + `,` + c.args + `}`
		if _, err := env.call(annotationsUpdateID, "sfree", args); err == nil {
			t.Fatal("update without confirm was accepted")
		}
		env.noIO(t)
		if _, err := env.confirmed(annotationsUpdateID, "sfree", args); err != nil {
			t.Fatal(err)
		}
		requests, _ := env.fake.seen()
		if len(requests) != 1 || requests[0].method != http.MethodPatch ||
			requests[0].path != alphaPrefix+"properties/200/reportingDataAnnotations/3001" ||
			requests[0].rawQuery != "updateMask="+strings.ReplaceAll(c.mask, ",", "%2C") || requests[0].body != c.body {
			t.Errorf("%s: %+v", c.args, requests)
		}
	}
}

func TestAnnotationsUpdateAndDeleteRefuseBeforeAnyIO(t *testing.T) {
	env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, dayAnnotationJSON)})
	const name = `"name":"properties/200/reportingDataAnnotations/3001"`
	for name, args := range map[string]string{
		"nothing to change": `{` + name + `}`,
		"both dates":        `{` + name + `,"annotation_date":"2026-11-20","annotation_date_range":{"start_date":"2026-12-01","end_date":"2026-12-02"}}`,
		"blank title":       `{` + name + `,"title":""}`,
		"orange":            `{` + name + `,"color":"ORANGE"}`,
		"bad date":          `{` + name + `,"annotation_date":"20261120"}`,
		"stream name":       `{"name":"properties/200/dataStreams/1","title":"T"}`,
		"property name":     `{"name":"properties/200","title":"T"}`,
		"leading zero":      `{"name":"properties/200/reportingDataAnnotations/01","title":"T"}`,
		"property argument": `{` + name + `,"property":"properties/200","title":"T"}`,
	} {
		if _, err := env.confirmed(annotationsUpdateID, "sfree", args); !refusedInvalid(err) {
			t.Errorf("update %s: %v", name, err)
		}
	}
	for name, args := range map[string]string{
		"stream name":   `{"name":"properties/200/dataStreams/1"}`,
		"property name": `{"name":"properties/200"}`,
		"extra":         `{` + name + `,"title":"T"}`,
	} {
		if _, err := env.confirmed(annotationsDeleteID, "sfree", args); !refusedInvalid(err) {
			t.Errorf("delete %s: %v", name, err)
		}
	}
	env.noIO(t)
}

func TestAnnotationsDeleteSendsOneRequestAndReturnsTheName(t *testing.T) {
	env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, `{}`)})
	args := `{"name":"properties/200/reportingDataAnnotations/3001"}`
	if _, err := env.call(annotationsDeleteID, "sfree", args); err == nil {
		t.Fatal("delete without confirm was accepted")
	}
	env.noIO(t)
	result, err := env.confirmed(annotationsDeleteID, "sfree", args)
	requests, _ := env.fake.seen()
	var out annotationDeleted
	if err != nil || json.Unmarshal([]byte(result), &out) != nil || !out.Deleted ||
		out.Name != "properties/200/reportingDataAnnotations/3001" || len(requests) != 1 ||
		requests[0].method != http.MethodDelete || requests[0].path != alphaPrefix+"properties/200/reportingDataAnnotations/3001" ||
		requests[0].rawQuery != "" || requests[0].body != "" {
		t.Errorf("%v, %s, %+v", err, result, requests)
	}
}

func TestAnnotationsDeleteOfASystemGeneratedAnnotationIsAnError(t *testing.T) {
	// Google's refusal class is not verified live; any failing status must come back as an error without the
	// provider text and after exactly one request.
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusPreconditionFailed} {
		env := annotationEnv(t, &fakeGoogle{api: always(status, errorObject(status, "FAILED_PRECONDITION", ""))})
		_, err := env.confirmed(annotationsDeleteID, "sfree", `{"name":"properties/200/reportingDataAnnotations/3003"}`)
		if err == nil || leaks(err.Error()) != "" || strings.Contains(err.Error(), "unknown") {
			t.Errorf("status %d: %v", status, err)
		}
		if requests, _ := env.fake.seen(); len(requests) != 1 {
			t.Errorf("status %d: requests = %d", status, len(requests))
		}
	}
}

func TestAnnotationsFollowTheBindingBeforeAnyIO(t *testing.T) {
	for _, tool := range []struct{ id, arguments, method string }{
		{annotationsListID, `{"property":"PROP"}`, "GET"},
		{annotationsGetID, `{"name":"PROP/reportingDataAnnotations/3001"}`, "GET"},
		{annotationsCreateID, `{"property":"PROP","title":"T","color":"RED","annotation_date":"2026-11-20"}`, "POST"},
		{annotationsUpdateID, `{"name":"PROP/reportingDataAnnotations/3001","title":"T"}`, "PATCH"},
		{annotationsDeleteID, `{"name":"PROP/reportingDataAnnotations/3001"}`, "DELETE"},
	} {
		for _, c := range streamBindingCases {
			answers := map[string]string{
				"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100"}`,
				"/v1beta/properties/201": `{"name":"properties/201","parent":"accounts/100"}`,
				"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900"}`,
			}
			env := annotationEnv(t, &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
				body, ok := answers[r.URL.Path]
				if !ok {
					name := strings.TrimPrefix(r.URL.Path, "/v1alpha/")
					if r.Method == http.MethodPost {
						name += "/9"
					}
					body = `{"reportingDataAnnotations":[],"name":"` + name + `"}`
				}
				return respond(r, http.StatusOK, "application/json", body), nil
			}})
			args := strings.ReplaceAll(tool.arguments, "PROP", c.property)
			result, err := env.confirmed(tool.id, c.connection, args)
			requests, _ := env.fake.seen()
			want := slices.Clone(c.methods)
			if i := slices.Index(want, "X"); i >= 0 {
				want[i] = tool.method
			}
			if (err != nil) != c.refused || !slices.Equal(methodsOf(requests), want) {
				t.Errorf("%s %s %s: %v, requests %v, want %v", tool.id, c.connection, c.property, err,
					methodsOf(requests), want)
			}
			if c.refused && (!refusedInvalid(err) || strings.Contains(err.Error(), "901") || strings.Contains(result, "901")) {
				t.Errorf("%s %s %s: refusal %v", tool.id, c.connection, c.property, err)
			}
			if c.refused && len(c.methods) == 0 {
				env.noIO(t)
			}
		}
	}
}

func TestAnnotationErrorsAreClassifiedAndChangesNeverRepeat(t *testing.T) {
	calls := []struct{ id, arguments string }{
		{annotationsListID, `{"property":"properties/200"}`},
		{annotationsGetID, `{"name":"properties/200/reportingDataAnnotations/3001"}`},
		{annotationsCreateID, `{"property":"properties/200","title":"T","color":"RED","annotation_date":"2026-11-20"}`},
		{annotationsUpdateID, `{"name":"properties/200/reportingDataAnnotations/3001","title":"T"}`},
		{annotationsDeleteID, `{"name":"properties/200/reportingDataAnnotations/3001"}`},
	}
	for _, c := range []struct {
		name   string
		api    func(int, *http.Request) (*http.Response, error)
		class  provider.Class
		unsure bool
	}{
		{"403", always(http.StatusForbidden, errorObject(403, "PERMISSION_DENIED", "ACCESS_TOKEN_SCOPE_INSUFFICIENT")),
			provider.ClassPermission, false},
		{"404", always(http.StatusNotFound, errorObject(404, "NOT_FOUND", "")), provider.ClassNotFound, false},
		{"429", always(http.StatusTooManyRequests, errorObject(429, "RESOURCE_EXHAUSTED", "")), provider.ClassRateLimited, false},
		{"500", always(http.StatusInternalServerError, errorObject(500, "INTERNAL", "")), provider.ClassProviderError, true},
	} {
		for _, call := range calls {
			t.Run(c.name+" "+call.id, func(t *testing.T) {
				env := annotationEnv(t, &fakeGoogle{api: c.api})
				_, err := env.confirmed(call.id, "sfree", call.arguments)
				if classOf(err) != c.class || leaks(err.Error()) != "" {
					t.Fatalf("error = %v", err)
				}
				if got := strings.Contains(err.Error(), "outcome of the change is unknown"); got != (c.unsure && effectOf(call.id) != config.EffectRead) {
					t.Errorf("unknown outcome reported = %t: %v", got, err)
				}
				if requests, _ := env.fake.seen(); len(requests) != 1 {
					t.Errorf("requests = %d, want exactly one", len(requests))
				}
			})
		}
	}
}

func TestAnnotation429WithoutRetryAfterHoldsTheNextRequest(t *testing.T) {
	env := annotationEnv(t, &fakeGoogle{api: func(n int, r *http.Request) (*http.Response, error) {
		if n == 1 {
			return respond(r, http.StatusTooManyRequests, "application/json", errorObject(429, "RESOURCE_EXHAUSTED", "")), nil
		}
		return respond(r, http.StatusOK, "application/json", dayAnnotationJSON), nil
	}})
	args := `{"name":"properties/200/reportingDataAnnotations/3001"}`
	if _, err := env.call(annotationsGetID, "sfree", args); classOf(err) != provider.ClassRateLimited {
		t.Fatalf("error = %v", err)
	}
	if _, err := env.call(annotationsGetID, "sfree", args); err != nil {
		t.Fatal(err)
	}
	if requests, _ := env.fake.seen(); len(requests) != 2 {
		t.Errorf("requests = %d, want 2 without a retry", len(requests))
	}
	if waits := *env.waits; len(waits) == 0 || slices.Max(waits) < 900_000_000 {
		t.Errorf("waits = %v, want the next request held for about a second", waits)
	}
}

func TestAnnotationWritesDistrustAnAnswerOfAnotherAnnotation(t *testing.T) {
	env := annotationEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/999/reportingDataAnnotations/1"}`)})
	if _, err := env.confirmed(annotationsUpdateID, "sfree", `{"name":"properties/200/reportingDataAnnotations/3001","title":"T"}`); classOf(err) != provider.ClassProviderError ||
		!strings.Contains(err.Error(), "unknown") {
		t.Errorf("update: %v", err)
	}
	if _, err := env.confirmed(annotationsCreateID, "sfree", `{"property":"properties/200","title":"T","color":"RED","annotation_date":"2026-11-20"}`); classOf(err) != provider.ClassProviderError {
		t.Errorf("create: %v", err)
	}
}

func TestAnnotationToolsAreInTheProfilesAndNotListedOnly(t *testing.T) {
	reg := registry(t)
	metadata, _ := reg.ProviderMetadata(Provider)
	profiles := map[string][]string{}
	for _, p := range metadata.Profiles {
		profiles[p.ID] = p.Tools
	}
	for _, id := range alphaTools[5:] {
		descriptor, _, ok := reg.Lookup(id)
		if !ok || descriptor.RequiresToolAllowList {
			t.Errorf("%s: registered %t, listed-only %t", id, ok, descriptor.RequiresToolAllowList)
		}
		if !strings.Contains(descriptor.Description, "visible to all users of the property") ||
			!strings.Contains(descriptor.Description, "untrusted") {
			t.Errorf("%s description lacks the visibility or the untrusted note", id)
		}
		if effectOf(id) != config.EffectRead && (descriptor.Risk.Confirmation != "required" || reg.ConfirmationSummary(id) == nil) {
			t.Errorf("%s needs a confirmation and a summary", id)
		}
	}
	if slices.Contains(profiles["read"], annotationsCreateID) || slices.Contains(profiles["read"], annotationsDeleteID) ||
		!slices.Contains(profiles["read"], annotationsListID) || !slices.Contains(profiles["read"], annotationsGetID) ||
		!slices.Contains(profiles["write"], annotationsDeleteID) {
		t.Errorf("profiles = %+v", profiles)
	}
	create := reg.ConfirmationSummary(annotationsCreateID)(json.RawMessage(
		`{"property":"properties/200","title":"Sale","color":"RED","annotation_date_range":{"start_date":"2026-12-24","end_date":"2026-12-31"}}`))
	update := reg.ConfirmationSummary(annotationsUpdateID)(json.RawMessage(
		`{"name":"properties/200/reportingDataAnnotations/3001","color":"RED","title":"X"}`))
	remove := reg.ConfirmationSummary(annotationsDeleteID)(json.RawMessage(
		`{"name":"properties/200/reportingDataAnnotations/3001"}`))
	for got, wants := range map[string][]string{
		create: {"Create", `"Sale"`, "properties/200", "2026-12-24", "2026-12-31", "all users"},
		update: {"title, color", "properties/200/reportingDataAnnotations/3001"},
		remove: {"Delete", "properties/200/reportingDataAnnotations/3001"}} {
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("summary %q lacks %q", got, want)
			}
		}
	}
}

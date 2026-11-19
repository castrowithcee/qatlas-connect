package googleroute

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	toursPath   = "/v1/projects/" + testProject + ":optimizeTours"
	modelCanary = "model-canary-5e2b"
)

// toursModel is a small valid model. The label is a canary that must never reach an error text.
func toursModel(shipments, vehicles int) string {
	var s, v []string
	for i := 0; i < shipments; i++ {
		s = append(s, fmt.Sprintf(`{"label":"%s","deliveries":[{"arrivalLocation":{"latitude":48.%d,"longitude":11.5}}]}`,
			modelCanary, i))
	}
	for i := 0; i < vehicles; i++ {
		v = append(v, `{"startLocation":{"latitude":48.0,"longitude":11.4}}`)
	}
	return `{"shipments":[` + strings.Join(s, ",") + `],"vehicles":[` + strings.Join(v, ",") + `]}`
}

func toursArguments(project string, shipments, vehicles int, extra string) string {
	return `{"project":"` + project + `","model":` + toursModel(shipments, vehicles) + extra + `}`
}

const toursAnswer = `{"routes":[{"vehicleIndex":0,"visits":[{"shipmentIndex":0,"startTime":"2026-11-11T08:10:00Z"}],` +
	`"metrics":{"travelDistanceMeters":1200}}],"skippedShipments":[{"index":1,"reasons":[{"code":"CODE_UNSPECIFIED"}]}],` +
	`"validationErrors":[{"code":3,"displayName":"x","errorMessage":"` + detailCanary + `"}],` +
	`"metrics":{"usedVehicleCount":1}}`

func executeEnvironment(t *testing.T, targets []string) *environment {
	t.Helper()
	return newEnvironmentWith(t, targets, config.EffectRead, config.EffectExecute)
}

func TestToursOptimizeOneAndTwoVehicles(t *testing.T) {
	for _, vehicles := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d vehicles", vehicles), func(t *testing.T) {
			e := executeEnvironment(t, []string{"projects/" + testProject})
			e.fake.routes[toursPath] = okReply(toursAnswer)
			out, err := e.invokeConfirmed(toursOptimizeID, toursArguments(testProject, 2, vehicles, ""), true)
			if err != nil {
				t.Fatal(err)
			}
			got := decodeMap(t, out)
			if got["complete"] != true || got["routes"] == nil || got["skippedShipments"] == nil ||
				got["validationErrors"] == nil {
				t.Errorf("result = %v", got)
			}
			r := e.fake.api()
			if len(r) != 1 || r[0].method != http.MethodPost || r[0].url != apiRoot+toursPath ||
				!strings.HasPrefix(r[0].auth, "Bearer synthetic-access-") {
				t.Fatalf("requests = %+v", r)
			}
		})
	}
}

func TestToursOptimizeSendsTheCheckedBodyOnce(t *testing.T) {
	e := executeEnvironment(t, nil)
	var sent map[string]any
	var contentType string
	e.fake.hook = func(_ int, req *http.Request) *reply {
		raw, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(raw, &sent)
		contentType = req.Header.Get("Content-Type")
		return nil
	}
	e.fake.routes[toursPath] = okReply(`{}`)
	if _, err := e.invokeConfirmed(toursOptimizeID,
		toursArguments(testProject, 1, 1, `,"populate_polylines":true,"timeout":"20s"`), true); err != nil {
		t.Fatal(err)
	}
	model, _ := sent["model"].(map[string]any)
	if contentType != "application/json" || sent["populatePolylines"] != true || sent["timeout"] != "20s" ||
		model["shipments"] == nil || len(sent) != 3 {
		t.Errorf("content type %q, body %v", contentType, sent)
	}
}

func TestToursOptimizeNeedsConfirmationAndNamesTheCall(t *testing.T) {
	for _, c := range []struct {
		vehicles int
		sku      string
	}{{1, "Single Vehicle Routing"}, {2, "Fleet Routing"}, {5, "Fleet Routing"}} {
		e := executeEnvironment(t, nil)
		e.fake.routes[toursPath] = okReply(toursAnswer)
		_, err := e.invoke(toursOptimizeID, toursArguments(testProject, 3, c.vehicles, ""))
		if err == nil {
			t.Fatal("an unconfirmed call was accepted")
		}
		if classOf(err) == provider.ClassProviderError || !strings.Contains(err.Error(), "requires confirmation") {
			t.Errorf("err = %v, want a refusal for the missing confirmation", err)
		}
		summary := e.reg.ConfirmationSummary(toursOptimizeID)
		if summary == nil {
			t.Fatal("the tool has no confirmation summary")
		}
		text := summary(json.RawMessage(toursArguments(testProject, 3, c.vehicles, "")))
		for _, want := range []string{testProject, "3 shipments", fmt.Sprintf("%d vehicles", c.vehicles), c.sku} {
			if !strings.Contains(text, want) {
				t.Errorf("%d vehicles: confirmation %q lacks %q", c.vehicles, text, want)
			}
		}
		if strings.Contains(text, modelCanary) {
			t.Errorf("confirmation %q repeats the model", text)
		}
		if len(e.fake.api()) != 0 || e.lookups != 0 || e.fake.tokenRequests() != 0 {
			t.Errorf("an unconfirmed call reached I/O: requests %d, lookups %d", len(e.fake.api()), e.lookups)
		}
	}
	if toursSummary(json.RawMessage(`{`)) != "" {
		t.Error("an unreadable argument object got a summary")
	}
}

func TestToursOptimizeNeedsTheExecuteEffect(t *testing.T) {
	e := newEnvironment(t, nil)
	_, err := e.invokeConfirmed(toursOptimizeID, toursArguments(testProject, 1, 1, ""), true)
	if err == nil || len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("err = %v, requests = %d, lookups = %d", err, len(e.fake.api()), e.lookups)
	}
}

func TestToursOptimizeRefusesBeforeSecretAndIO(t *testing.T) {
	cases := map[string]struct {
		targets []string
		args    string
	}{
		"project outside the targets": {[]string{"projects/" + otherProject}, toursArguments(testProject, 1, 1, "")},
		"no model":                    {nil, `{"project":"` + testProject + `"}`},
		"model of the wrong type":     {nil, `{"project":"` + testProject + `","model":[]}`},
		"unknown argument":            {nil, toursArguments(testProject, 1, 1, `,"url":"https://example.invalid"`)},
		"bad enumeration":             {nil, toursArguments(testProject, 1, 1, `,"solving_mode":"FAST"`)},
		"unknown model member":        {nil, `{"project":"` + testProject + `","model":{"shipments":[],"vehicles":[],"extra":1}}`},
		"project as a name":           {nil, toursArguments("projects/"+testProject, 1, 1, "")},
		"timeout over the bound":      {nil, toursArguments(testProject, 1, 1, `,"timeout":"46s"`)},
		"timeout of zero":             {nil, toursArguments(testProject, 1, 1, `,"timeout":"0s"`)},
		"model over the size bound": {nil, `{"project":"` + testProject + `","model":{"shipments":[{"label":"` +
			strings.Repeat("x", maxToursBodyBytes) + `"}],"vehicles":[]}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := executeEnvironment(t, c.targets)
			e.fake.routes[toursPath] = okReply(toursAnswer)
			_, err := e.invokeConfirmed(toursOptimizeID, c.args, true)
			if err == nil {
				t.Fatal("the call was accepted")
			}
			if len(e.fake.api()) != 0 || e.lookups != 0 || e.fake.tokenRequests() != 0 {
				t.Errorf("requests = %d, lookups = %d, tokens = %d", len(e.fake.api()), e.lookups, e.fake.tokenRequests())
			}
			if strings.Contains(err.Error(), modelCanary) || strings.Contains(err.Error(), "example.invalid") {
				t.Errorf("error %q repeats a value", err)
			}
		})
	}
}

func TestToursOptimizeNeverRepeats(t *testing.T) {
	cases := []struct {
		name   string
		reply  reply
		class  provider.Class
		unsure bool
	}{
		{"bad request", reply{status: 400, body: errorObject(400, "INVALID_ARGUMENT", "")}, provider.ClassProviderError, false},
		{"unauthorized", reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "")}, provider.ClassAuth, false},
		{"expired token", reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "ACCESS_TOKEN_EXPIRED")}, provider.ClassAuth, false},
		{"permission", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		{"api disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission, false},
		{"rate limit", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "")}, provider.ClassRateLimited, false},
		{"server error", reply{status: 500, body: errorObject(500, "INTERNAL", "")}, provider.ClassProviderError, true},
		{"unavailable", reply{status: 503, body: `{}`}, provider.ClassUnreachable, true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, true},
		{"abort", reply{err: context.Canceled}, provider.ClassUnreachable, true},
		{"unreachable", reply{err: &net.OpError{Op: "read", Err: io.ErrUnexpectedEOF}}, provider.ClassUnreachable, true},
		{"unreadable answer", reply{status: 200, body: `{"routes":`}, provider.ClassInvalidResponse, true},
		{"not an object", reply{status: 200, body: `[]`}, provider.ClassInvalidResponse, true},
		{"not json", reply{status: 200, body: `<html>`, header: http.Header{"Content-Type": {"text/html"}}}, provider.ClassInvalidResponse, true},
		{"answer over the bound", reply{status: 200, body: `{"requestLabel":"` + strings.Repeat("x", maxOptimizeResponseBytes) +
			`"}`}, provider.ClassInvalidResponse, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := executeEnvironment(t, nil)
			e.fake.routes[toursPath] = c.reply
			_, err := e.invokeConfirmed(toursOptimizeID, toursArguments(testProject, 1, 1, ""), true)
			if classOf(err) != c.class {
				t.Fatalf("err = %v (%s), want class %s", err, classOf(err), c.class)
			}
			if got := len(e.fake.api()); got != 1 {
				t.Errorf("requests = %d, want exactly 1", got)
			}
			text := err.Error()
			if got := strings.Contains(text, "may have been billed"); got != c.unsure {
				t.Errorf("billing note = %v, want %v in %q", got, c.unsure, text)
			}
			for _, secret := range []string{detailCanary, modelCanary, "synthetic-access", refreshValue} {
				if strings.Contains(text, secret) {
					t.Errorf("error %q carries %q", text, secret)
				}
			}
		})
	}
}

func TestToursOptimizeNamesGoogleStatusOfARejectedModel(t *testing.T) {
	failed := rejectedModel("op", []byte(errorObject(400, "INVALID_ARGUMENT", "")))
	if !strings.Contains(failed.Message, "INVALID_ARGUMENT") || strings.Contains(failed.Message, detailCanary) ||
		failed.Original == nil {
		t.Errorf("message = %q, original = %+v", failed.Message, failed.Original)
	}
	other := rejectedModel("op", []byte(errorObject(400, "provider text", "")))
	if strings.Contains(other.Message, "provider text") {
		t.Errorf("message = %q carries an unknown status", other.Message)
	}
}

func TestToursOptimizeRenewsTheTokenOnlyForTheNextCall(t *testing.T) {
	e := executeEnvironment(t, nil)
	e.fake.routes[toursPath] = reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "")}
	args := toursArguments(testProject, 1, 1, "")
	if _, err := e.invokeConfirmed(toursOptimizeID, args, true); classOf(err) != provider.ClassAuth {
		t.Fatalf("err = %v", err)
	}
	if e.fake.tokenRequests() != 1 || len(e.fake.api()) != 1 {
		t.Fatalf("tokens = %d, requests = %d after a 401", e.fake.tokenRequests(), len(e.fake.api()))
	}
	e.fake.routes[toursPath] = okReply(`{}`)
	if _, err := e.invokeConfirmed(toursOptimizeID, args, true); err != nil {
		t.Fatal(err)
	}
	r := e.fake.api()
	if e.fake.tokenRequests() != 2 || len(r) != 2 || r[0].auth == r[1].auth {
		t.Errorf("tokens = %d, requests = %+v: the next call must use a renewed token", e.fake.tokenRequests(), r)
	}
}

func TestToursOptimizeRateLimitHoldsTheNextCall(t *testing.T) {
	e := executeEnvironment(t, nil)
	e.fake.routes[toursPath] = reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", ""),
		header: http.Header{"Retry-After": {"20"}}}
	args := toursArguments(testProject, 1, 1, "")
	_, _ = e.invokeConfirmed(toursOptimizeID, args, true)
	_, _ = e.invokeConfirmed(toursOptimizeID, args, true)
	if len(e.sleeps) == 0 || e.sleeps[len(e.sleeps)-1] < 19*time.Second {
		t.Errorf("sleeps = %v, want the wait Google asked for", e.sleeps)
	}
	if len(e.fake.api()) != 2 {
		t.Errorf("requests = %d, want one per call", len(e.fake.api()))
	}
}

func TestToursOptimizeValidationErrorsAreNotAnError(t *testing.T) {
	e := executeEnvironment(t, nil)
	e.fake.routes[toursPath] = okReply(`{"validationErrors":[{"code":3,"displayName":"x","errorMessage":"bad"}]}`)
	out, err := e.invokeConfirmed(toursOptimizeID, toursArguments(testProject, 1, 1, ""), true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeMap(t, out); got["validationErrors"] == nil || got["complete"] != true {
		t.Errorf("result = %v", got)
	}
}

func TestToursOptimizeReadsAnAnswerBeyondTheOrdinaryBound(t *testing.T) {
	e := executeEnvironment(t, nil)
	// Every polyline is valid; together they exceed 2 MiB but not the reading bound, and the result is cut.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, `{"vehicleIndex":`+fmt.Sprint(i)+`,"routePolyline":{"encodedPolyline":"`+
			strings.Repeat(testPolyline, 4000)+`"}}`)
	}
	body := `{"routes":[` + strings.Join(lines, ",") + `]}`
	if len(body) <= 2<<20 || len(body) >= maxOptimizeResponseBytes {
		t.Fatalf("test body is %d bytes", len(body))
	}
	e.fake.routes[toursPath] = okReply(body)
	out, err := e.invokeConfirmed(toursOptimizeID, toursArguments(testProject, 1, 1, ""), true)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeMap(t, out)
	if got["complete"] != false || got["truncation"] == nil || len(out) > maxResponsePolylineTotal+(256<<10) {
		t.Errorf("complete = %v, result is %d bytes", got["complete"], len(out))
	}
}

func TestToursOptimizeBoundsTheRequestTime(t *testing.T) {
	// The longest solving time plus the slack leaves room in one invoke of 60 seconds for the limiter and token.
	if toursMaxTimeout+optimizeSlack+10*time.Second > 60*time.Second {
		t.Errorf("request time %v leaves no room in 60 seconds", toursMaxTimeout+optimizeSlack)
	}
	var seenDeadline time.Duration
	e := executeEnvironment(t, nil)
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if deadline, ok := req.Context().Deadline(); ok {
			seenDeadline = time.Until(deadline)
		}
		return nil
	}
	e.fake.routes[toursPath] = okReply(`{}`)
	if _, err := e.invokeConfirmed(toursOptimizeID, toursArguments(testProject, 1, 1, `,"timeout":"10s"`), true); err != nil {
		t.Fatal(err)
	}
	if seenDeadline > 10*time.Second+optimizeSlack || seenDeadline < 10*time.Second {
		t.Errorf("request deadline in %v, want about 15s", seenDeadline)
	}
}

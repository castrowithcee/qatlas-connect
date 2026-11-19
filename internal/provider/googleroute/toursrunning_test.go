package googleroute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const (
	startPath = "/v1/projects/" + testProject + ":optimizeToursLongRunning"
	startName = "projects/" + testProject + "/locations/europe-west1/operations/run-1"
	startMeta = `"metadata":{"@type":"type.googleapis.com/google.maps.routeoptimization.v1.OptimizeToursLongRunningMetadata"}`
)

func startAnswer(name string) reply { return okReply(`{"name":"` + name + `",` + startMeta + `}`) }

func start(e *environment, extra string) (string, error) {
	return e.invokeConfirmed(toursLongRunningID, toursArguments(testProject, 2, 1, extra), true)
}

func TestLongRunningStartReturnsTheRunningOperationAndNeverWaits(t *testing.T) {
	e := executeEnvironment(t, []string{"projects/" + testProject})
	e.fake.routes[startPath] = startAnswer(startName)
	out, err := start(e, "")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeMap(t, out)
	if got["name"] != startName || got["done"] == true || got["response"] != nil || got["metadata"] == nil {
		t.Errorf("result = %v", got)
	}
	if err := application.ValidateJSON(toursLongRunning.OutputSchema, []byte(out)); err != nil {
		t.Errorf("output %s: %v", out, err)
	}
	r := e.fake.api()
	if len(r) != 1 || r[0].method != http.MethodPost || r[0].url != apiRoot+startPath ||
		!strings.HasPrefix(r[0].auth, "Bearer synthetic-access-") {
		t.Fatalf("requests = %+v, want one POST and no read of the operation", r)
	}
}

func TestLongRunningStartSendsTheCheckedBody(t *testing.T) {
	cases := map[string]struct {
		extra   string
		timeout string
		large   any
	}{
		"default":         {"", "1800s", nil},
		"given":           {`,"timeout":"600s"`, "600s", nil},
		"large and long":  {`,"timeout":"3600s","allow_large_deadline_despite_interruption_risk":true`, "3600s", true},
		"large but short": {`,"allow_large_deadline_despite_interruption_risk":true`, "1800s", true},
		"switch off":      {`,"allow_large_deadline_despite_interruption_risk":false`, "1800s", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := executeEnvironment(t, nil)
			var sent map[string]any
			e.fake.hook = func(_ int, req *http.Request) *reply {
				raw, _ := io.ReadAll(req.Body)
				_ = json.Unmarshal(raw, &sent)
				return nil
			}
			e.fake.routes[startPath] = startAnswer(startName)
			if _, err := start(e, c.extra); err != nil {
				t.Fatal(err)
			}
			if sent["timeout"] != c.timeout || sent["allowLargeDeadlineDespiteInterruptionRisk"] != c.large ||
				sent["model"] == nil || sent["allow_large_deadline_despite_interruption_risk"] != nil {
				t.Errorf("body = %v", sent)
			}
		})
	}
}

func TestLongRunningDeadlineDependsOnTheSwitch(t *testing.T) {
	build := func(timeout, large string) (toursBody, error) {
		extra := ""
		if large != "" {
			extra = `,"allow_large_deadline_despite_interruption_risk":` + large
		}
		return optimizeToursLongRunning.body(json.RawMessage(`{"project":"` + testProject + `","model":{},` +
			`"timeout":"` + timeout + `"` + extra + `}`))
	}
	for _, c := range []struct {
		timeout, large string
		want           time.Duration
	}{
		{"1s", "", time.Second}, {"1800s", "", 30 * time.Minute}, {"1800s", "false", 30 * time.Minute},
		{"1800s", "true", 30 * time.Minute}, {"1800.5s", "true", 30*time.Minute + 500*time.Millisecond},
		{"3600s", "true", time.Hour},
	} {
		built, err := build(c.timeout, c.large)
		if err != nil || built.Timeout != c.want {
			t.Errorf("timeout %s, switch %q: %v, %v", c.timeout, c.large, built.Timeout, err)
		}
	}
	for _, c := range []struct{ timeout, large, hint string }{
		{"1800.000000001s", "", "allow_large_deadline_despite_interruption_risk"},
		{"3600s", "", "allow_large_deadline_despite_interruption_risk"},
		{"3600s", "false", "allow_large_deadline_despite_interruption_risk"},
		{"3601s", "true", "at most 3600s"},
		{"7200s", "true", "at most 3600s"},
		{"0s", "true", "longer than 0"},
	} {
		_, err := build(c.timeout, c.large)
		var invalid *provider.InvalidRequestError
		if !errors.As(err, &invalid) || !strings.Contains(err.Error(), c.hint) {
			t.Errorf("timeout %s, switch %q: want a refusal with %q, got %v", c.timeout, c.large, c.hint, err)
		}
	}
	if _, err := optimizeToursLongRunning.body(json.RawMessage(`{"project":"` + testProject + `","model":{},` +
		`"timeout":"3600s","allow_large_deadline_despite_interruption_risk":"true"}`)); err == nil {
		t.Error("a switch that is not a boolean raised the bound")
	}
	built, err := optimizeToursLongRunning.body(json.RawMessage(`{"project":"` + testProject + `","model":{}}`))
	if err != nil || built.Timeout != toursLongDefaultTimeout || !strings.Contains(string(built.Payload), `"timeout":"1800s"`) {
		t.Errorf("default timeout: %+v, %v", built, err)
	}
	// The synchronous request knows neither the switch nor a longer time.
	if _, err := optimizeTours.body(json.RawMessage(`{"project":"` + testProject + `","model":{},` +
		`"allow_large_deadline_despite_interruption_risk":true}`)); err == nil {
		t.Error("the synchronous request takes the switch")
	}
}

func TestLongRunningStartTimeIsIndependentOfTheSolvingTime(t *testing.T) {
	if toursStartTimeout+optimizeSlack+10*time.Second > 60*time.Second || toursStartTimeout >= toursDefaultTimeout {
		t.Errorf("start time %v does not fit an invoke of 60 seconds", toursStartTimeout+optimizeSlack)
	}
	e := executeEnvironment(t, nil)
	var seenDeadline time.Duration
	e.fake.hook = func(_ int, req *http.Request) *reply {
		if deadline, ok := req.Context().Deadline(); ok {
			seenDeadline = time.Until(deadline)
		}
		return nil
	}
	e.fake.routes[startPath] = startAnswer(startName)
	_, err := start(e, `,"timeout":"3600s","allow_large_deadline_despite_interruption_risk":true`)
	if err != nil {
		t.Fatal(err)
	}
	if seenDeadline > toursStartTimeout+optimizeSlack || seenDeadline < toursStartTimeout {
		t.Errorf("request deadline in %v, want about %v", seenDeadline, toursStartTimeout+optimizeSlack)
	}
}

func TestLongRunningStartNeedsConfirmationAndNamesCostAndLimits(t *testing.T) {
	for _, c := range []struct {
		vehicles int
		sku      string
	}{{1, "Single Vehicle Routing"}, {3, "Fleet Routing"}} {
		e := executeEnvironment(t, nil)
		e.fake.routes[startPath] = startAnswer(startName)
		_, err := e.invoke(toursLongRunningID, toursArguments(testProject, 4, c.vehicles, ""))
		if err == nil || !strings.Contains(err.Error(), "requires confirmation") {
			t.Fatalf("err = %v, want a refusal for the missing confirmation", err)
		}
		summary := e.reg.ConfirmationSummary(toursLongRunningID)
		if summary == nil {
			t.Fatal("the tool has no confirmation summary")
		}
		text := summary(json.RawMessage(toursArguments(testProject, 4, c.vehicles, "")))
		for _, want := range []string{testProject, "4 shipments", fmt.Sprintf("%d vehicles", c.vehicles), c.sku,
			"cannot be cancelled or listed", "billed until it ends", "operation name is unknown", "new operation"} {
			if !strings.Contains(text, want) {
				t.Errorf("confirmation %q lacks %q", text, want)
			}
		}
		if strings.Contains(text, modelCanary) {
			t.Errorf("confirmation %q repeats the model", text)
		}
		if len(e.fake.api()) != 0 || e.lookups != 0 || e.fake.tokenRequests() != 0 {
			t.Errorf("an unconfirmed call reached I/O")
		}
	}
	if toursLongRunningSummary(json.RawMessage(`{`)) != "" {
		t.Error("an unreadable argument object got a summary")
	}
	for _, want := range []string{"COSTS MONEY", "cannot be cancelled and not listed", "never waits", "operations.get",
		"30 minutes", "60 minutes", "allow_large_deadline_despite_interruption_risk", "routeoptimization.operations.create",
		"name is then unknown"} {
		if !strings.Contains(toursLongRunning.Description, want) {
			t.Errorf("the description lacks %q", want)
		}
	}
}

func TestLongRunningStartNeedsTheExecuteEffect(t *testing.T) {
	e := newEnvironment(t, nil)
	_, err := start(e, "")
	if err == nil || len(e.fake.api()) != 0 || e.lookups != 0 {
		t.Errorf("err = %v, requests = %d, lookups = %d", err, len(e.fake.api()), e.lookups)
	}
}

func TestLongRunningStartRefusesBeforeSecretAndIO(t *testing.T) {
	cases := map[string]struct {
		targets []string
		args    string
	}{
		"project outside the targets": {[]string{"projects/" + otherProject}, toursArguments(testProject, 1, 1, "")},
		"no model":                    {nil, `{"project":"` + testProject + `"}`},
		"a location":                  {nil, toursArguments(testProject, 1, 1, `,"location":"us-central1"`)},
		"project as a name":           {nil, toursArguments("projects/"+testProject, 1, 1, "")},
		"timeout over 30 minutes":     {nil, toursArguments(testProject, 1, 1, `,"timeout":"1801s"`)},
		"timeout over 60 minutes": {nil, toursArguments(testProject, 1, 1,
			`,"timeout":"3601s","allow_large_deadline_despite_interruption_risk":true`)},
		"a switch of the wrong type": {nil, toursArguments(testProject, 1, 1,
			`,"allow_large_deadline_despite_interruption_risk":"yes"`)},
		"bad enumeration": {nil, toursArguments(testProject, 1, 1, `,"search_mode":"SLOW"`)},
		"model over the size bound": {nil, `{"project":"` + testProject + `","model":{"shipments":[{"label":"` +
			strings.Repeat("x", maxToursBodyBytes) + `"}],"vehicles":[]}}`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := executeEnvironment(t, c.targets)
			e.fake.routes[startPath] = startAnswer(startName)
			_, err := e.invokeConfirmed(toursLongRunningID, c.args, true)
			if err == nil {
				t.Fatal("the call was accepted")
			}
			if len(e.fake.api()) != 0 || e.lookups != 0 || e.fake.tokenRequests() != 0 {
				t.Errorf("requests = %d, lookups = %d, tokens = %d", len(e.fake.api()), e.lookups, e.fake.tokenRequests())
			}
			if strings.Contains(err.Error(), modelCanary) || strings.Contains(err.Error(), otherProject) {
				t.Errorf("error %q repeats a value", err)
			}
		})
	}
}

func TestLongRunningStartNeverRepeats(t *testing.T) {
	cases := []struct {
		name   string
		reply  reply
		class  provider.Class
		unsure bool
	}{
		{"bad request", reply{status: 400, body: errorObject(400, "INVALID_ARGUMENT", "")}, provider.ClassProviderError, false},
		{"unauthorized", reply{status: 401, body: errorObject(401, "UNAUTHENTICATED", "")}, provider.ClassAuth, false},
		{"missing right", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "")}, provider.ClassPermission, false},
		{"api disabled", reply{status: 403, body: errorObject(403, "PERMISSION_DENIED", "SERVICE_DISABLED")}, provider.ClassPermission, false},
		{"rate limit", reply{status: 429, body: errorObject(429, "RESOURCE_EXHAUSTED", "")}, provider.ClassRateLimited, false},
		{"server error", reply{status: 500, body: errorObject(500, "INTERNAL", "")}, provider.ClassProviderError, true},
		{"unavailable", reply{status: 503, body: `{}`}, provider.ClassUnreachable, true},
		{"timeout", reply{err: context.DeadlineExceeded}, provider.ClassTimeout, true},
		{"abort", reply{err: context.Canceled}, provider.ClassUnreachable, true},
		{"unreachable", reply{err: &net.OpError{Op: "read", Err: io.ErrUnexpectedEOF}}, provider.ClassUnreachable, true},
		{"unreadable answer", reply{status: 200, body: `{"name":`}, provider.ClassInvalidResponse, true},
		{"not an object", reply{status: 200, body: `[]`}, provider.ClassInvalidResponse, true},
		{"not json", reply{status: 200, body: `<html>`, header: http.Header{"Content-Type": {"text/html"}}}, provider.ClassInvalidResponse, true},
		{"no name", reply{status: 200, body: `{"done":false}`}, provider.ClassInvalidResponse, true},
		{"finished with an error", reply{status: 200, body: `{"name":"` + startName + `","done":true,` +
			`"error":{"code":13,"message":"` + detailCanary + `"}}`}, provider.ClassProviderError, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := executeEnvironment(t, nil)
			e.fake.routes[startPath] = c.reply
			_, err := start(e, "")
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

func TestLongRunningStartRefusesAnInvalidOperationName(t *testing.T) {
	for name, answer := range map[string]string{
		"another project":       "projects/" + otherProject + "/locations/us-central1/operations/run-1",
		"the project by number": "projects/415104041262/locations/us-central1/operations/run-1",
		"no location":           "projects/" + testProject + "/operations/run-1",
		"extra segment":         startName + "/more",
		"not an operation":      "projects/" + testProject + "/locations/us-central1/jobs/run-1",
		"query":                 startName + "?x=1",
		"empty id":              "projects/" + testProject + "/locations/us-central1/operations/",
		"empty":                 "",
	} {
		t.Run(name, func(t *testing.T) {
			e := executeEnvironment(t, nil)
			e.fake.routes[startPath] = okReply(`{"name":"` + answer + `"}`)
			out, err := start(e, "")
			if classOf(err) != provider.ClassInvalidResponse || out != "" {
				t.Fatalf("out = %q, err = %v, want invalid-provider-response", out, err)
			}
			if !strings.Contains(err.Error(), "may have been billed") || strings.Contains(err.Error(), answer) && answer != "" {
				t.Errorf("error %q", err)
			}
			if len(e.fake.api()) != 1 {
				t.Errorf("requests = %d, want exactly 1", len(e.fake.api()))
			}
		})
	}
}

func TestLongRunningStartForAProjectNumberKeepsTheNumberInTheName(t *testing.T) {
	const number = "415104041262"
	e := executeEnvironment(t, nil)
	name := "projects/" + number + "/locations/us-central1/operations/run-1"
	e.fake.routes["/v1/projects/"+number+":optimizeToursLongRunning"] = startAnswer(name)
	out, err := e.invokeConfirmed(toursLongRunningID, toursArguments(number, 1, 1, ""), true)
	if err != nil || decodeMap(t, out)["name"] != name {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestLongRunningStartThenReadTheTypedResponse(t *testing.T) {
	e := executeEnvironment(t, []string{"projects/" + testProject})
	e.fake.routes[startPath] = startAnswer(startName)
	out, err := start(e, "")
	if err != nil {
		t.Fatal(err)
	}
	name, _ := decodeMap(t, out)["name"].(string)
	e.fake.routes["/v1/"+name] = okReply(`{"name":"` + name + `","done":true,` + startMeta + `,"response":{` +
		`"@type":"type.googleapis.com/google.maps.routeoptimization.v1.OptimizeToursResponse",` +
		`"routes":[{"vehicleIndex":0,"visits":[{"shipmentIndex":0}]}],"skippedShipments":[{"index":1}],` +
		`"validationErrors":[{"code":3,"errorMessage":"` + detailCanary + `"}]}}`)
	read, err := e.invoke(operationsGetID, argumentsFor(name))
	if err != nil {
		t.Fatal(err)
	}
	if err := application.ValidateJSON(operationsGet.OutputSchema, []byte(read)); err != nil {
		t.Errorf("output %s: %v", read, err)
	}
	response, _ := decodeMap(t, read)["response"].(map[string]any)
	if response["routes"] == nil || response["skippedShipments"] == nil || response["validationErrors"] == nil ||
		response["complete"] != true {
		t.Errorf("response = %v", response)
	}
}

func TestLongRunningStartFinishedAtOnceCarriesTheTypedResponse(t *testing.T) {
	e := executeEnvironment(t, nil)
	e.fake.routes[startPath] = okReply(`{"name":"` + startName + `","done":true,"response":{"routes":[{"vehicleIndex":0}]}}`)
	out, err := start(e, "")
	if err != nil {
		t.Fatal(err)
	}
	got := decodeMap(t, out)
	response, _ := got["response"].(map[string]any)
	if got["done"] != true || response["routes"] == nil || response["complete"] != true {
		t.Errorf("result = %v", got)
	}
}

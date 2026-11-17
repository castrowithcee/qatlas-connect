package googletagmanager

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
)

const testDestination = testContainer + "/destinations/4"

func TestDestinationsListPagesWithCursor(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(req *http.Request) (int, string, http.Header) {
		switch req.URL.Query().Get("pageToken") {
		case "":
			return 200, `{"destination":[{"destinationId":"G-EXAMPLE1","name":"a","path":"` + testDestination +
				`","destinationLinkId":"9","containerId":"2000002","accountId":"1000001","fingerprint":"1",` +
				`"tagManagerUrl":"https://tagmanager.google.com/x"}],"nextPageToken":"two"}`, nil
		case "two":
			return 200, `{"destination":[{"destinationId":"G-EXAMPLE2","name":"b"}]}`, nil
		}
		return 400, `{}`, nil
	}
	first, err := h.call(t, handleDestinationsList, `{"container":"`+testContainer+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	cursor, _ := first["next_cursor"].(string)
	list, _ := first["destinations"].([]any)
	if cursor == "" || len(list) != 1 {
		t.Fatalf("first = %v", first)
	}
	one := list[0].(map[string]any)
	for _, k := range []string{"path", "accountId", "containerId", "destinationId", "destinationLinkId", "name",
		"fingerprint", "tagManagerUrl"} {
		if one[k] == nil || one[k] == "" {
			t.Errorf("member %s is missing", k)
		}
	}
	encoded, _ := json.Marshal(first)
	if err := application.ValidateJSON(destinationsList.OutputSchema, encoded); err != nil {
		t.Errorf("result violates the output schema: %v", err)
	}
	second, err := h.call(t, handleDestinationsList, `{"container":"`+testContainer+`","cursor":"`+cursor+`"}`)
	if err != nil || second["next_cursor"] != nil || second["destinations"].([]any)[0].(map[string]any)["destinationId"] != "G-EXAMPLE2" {
		t.Fatalf("second = %v, %v", second, err)
	}
	before := len(h.google.seen())
	other := `{"container":"accounts/1000001/containers/2000009","cursor":"` + cursor + `"}`
	if _, err := h.call(t, handleDestinationsList, other); !isInvalidRequest(err) || len(h.google.seen()) != before {
		t.Errorf("a cursor of another container was taken: %v", err)
	}
	if got := h.sent(t); got[0].path != "/tagmanager/v2/"+testContainer+"/destinations" || got[0].query != "" ||
		got[1].query != "pageToken=two" {
		t.Errorf("requests = %+v", got)
	}
}

func TestEmptyDestinationListIsAnEmptyResult(t *testing.T) {
	h := newHarness(t, "", testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, `{}`, nil }
	out, err := h.call(t, handleDestinationsList, `{"container":"`+testContainer+`"}`)
	if list, ok := out["destinations"].([]any); err != nil || !ok || len(list) != 0 {
		t.Errorf("destinations = %v, %v", out["destinations"], err)
	}
}

func TestDestinationsGetReadsOneDestination(t *testing.T) {
	h := newHarness(t, testContainer, testClientID)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"path":"` + testDestination + `","destinationId":"G-EXAMPLE1","name":"a"}`, nil
	}
	out, err := h.call(t, handleDestinationsGet, `{"destination":"`+testDestination+`"}`)
	if err != nil || out["destinationId"] != "G-EXAMPLE1" {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	if got := h.sent(t); len(got) != 1 || got[0].path != "/tagmanager/v2/"+testDestination {
		t.Errorf("sent = %+v", got)
	}
}

func TestDestinationsOutsideTheBindingAreRefusedBeforeAnyRequest(t *testing.T) {
	for _, c := range []struct{ target, path string }{
		{testContainer, "accounts/1000001/containers/2000009/destinations/4"},
		{testContainer, "accounts/1000009/containers/2000002/destinations/4"},
		{testAccount, "accounts/1000009/containers/2000002/destinations/4"},
	} {
		h := newHarness(t, c.target, testClientID)
		container, _, _ := strings.Cut(c.path, "/destinations/")
		reachNothing(t, h, handleDestinationsGet, `{"destination":"`+c.path+`"}`)
		reachNothing(t, h, handleDestinationsList, `{"container":"`+container+`"}`)
	}
	for _, bad := range []string{"accounts/1/containers/2/destinations/04", "accounts/1/containers/2", "accounts/1/containers/2/destinations/4/x",
		"accounts/1/containers/2/destinations/4?a=b", "accounts/1/containers/2/workspaces/3/tags/4"} {
		reachNothing(t, newHarness(t, "", testClientID), handleDestinationsGet, `{"destination":"`+bad+`"}`)
	}
}

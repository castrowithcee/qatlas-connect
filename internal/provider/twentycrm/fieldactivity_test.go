package twentycrm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

func isInvalid(err error) bool {
	var invalid *provider.InvalidRequestError
	return errors.As(err, &invalid)
}

// inactiveState is a metadata page that deactivates the field city and the relation company of person.
const inactiveState = `{"data":[{"id":"` + personID + `","nameSingular":"person","fields":[` +
	`{"id":"` + otherID + `","name":"city","type":"TEXT","isActive":false},` +
	`{"id":"` + otherID + `","name":"company","type":"RELATION","isActive":false},` +
	`{"id":"` + otherID + `","name":"age","type":"NUMBER","isActive":true}]}],` +
	`"totalCount":1,"pageInfo":{"hasNextPage":false}}`

func TestObjectsGetMarksInactiveFields(t *testing.T) {
	serveFieldStates(t, inactiveState)
	recordRequests(t, `{}`)
	out, err := runObjectsTool(t, invokeObjectsGet, `{"object":"person"}`)
	if err != nil {
		t.Fatal(err)
	}
	var info ObjectInfo
	if err := json.Unmarshal(out, &info); err != nil {
		t.Fatal(err)
	}
	byName := map[string]FieldInfo{}
	for _, field := range info.Fields {
		byName[field.Name] = field
	}
	if f := byName["city"]; f.Active == nil || *f.Active || f.Writable {
		t.Errorf("city = %+v, want inactive and not writable", f)
	}
	if f := byName["companyId"]; f.Active == nil || *f.Active {
		t.Errorf("companyId = %+v, want inactive with its relation", f)
	}
	if f := byName["name"]; f.Active != nil {
		t.Errorf("name = %+v, want no state for a field the metadata does not name", f)
	}
}

func TestInactiveFieldsAreNotReadFilteredOrSorted(t *testing.T) {
	serveFieldStates(t, inactiveState)
	recordRequests(t, `{"data":{"people":[{"id":"`+personID+`","city":"Berlin","emails":["a"]}]},`+
		`"pageInfo":{"hasNextPage":false}}`)
	out, err := runRecords(t, true, `{"object":"person"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "Berlin") || !strings.Contains(string(out), `"emails"`) {
		t.Errorf("list = %s, want city left out and emails kept", out)
	}
	recordRequests(t, `{"data":{"person":{"id":"`+personID+`","city":"Berlin","emails":["a"]}}}`)
	out, err = runRecords(t, false, `{"object":"person","id":"`+personID+`"}`)
	if err != nil || strings.Contains(string(out), "Berlin") {
		t.Errorf("get = %s, %v, want city left out", out, err)
	}
	for name, args := range map[string]string{
		"requested field": `{"object":"person","fields":["city"]}`,
		"sort":            `{"object":"person","order_by":"city"}`,
	} {
		if _, err := runRecords(t, true, args); !isInvalid(err) {
			t.Errorf("%s: err = %v, want invalid-request", name, err)
		}
	}
}

func TestSearchRefusesAnInactiveFieldAndHidesItsValue(t *testing.T) {
	serveFieldStates(t, inactiveState)
	requests := searchRequests(t, `{"data":{"people":[{"id":"`+personID+`","city":"Berlin","age":3}]},`+
		`"pageInfo":{"hasNextPage":false}}`)
	_, err := runTool(t, invokeRecordsSearch,
		`{"object":"person","conditions":[{"field":"city","operator":"eq","value":"Berlin"}]}`)
	if !isInvalid(err) {
		t.Errorf("filter on inactive field: err = %v, want invalid-request", err)
	}
	if got := dataRequests(*requests); len(got) != 0 {
		t.Errorf("data requests = %v, want none", got)
	}
	out, err := runTool(t, invokeRecordsSearch,
		`{"object":"person","conditions":[{"field":"age","operator":"eq","value":3}]}`)
	if err != nil || strings.Contains(string(out), "Berlin") || !strings.Contains(string(out), `"age"`) {
		t.Errorf("search = %s, %v", out, err)
	}
}

func TestWritesRefuseInactiveFieldsBeforeTheRequest(t *testing.T) {
	serveFieldStates(t, inactiveState)
	calls := serveWrites(t, http.StatusOK, createdAnswer("create"))
	_, err := runWrite(t, false, `{"object":"person","id":"`+personID+`","fields":{"city":"x"}}`)
	if !isInvalid(err) || !strings.Contains(err.Error(), "inactive") {
		t.Errorf("update: err = %v, want invalid-request naming the inactive field", err)
	}
	_, err = runWrite(t, true, `{"object":"person","fields":{`+coveredCreate+`}}`)
	if !isInvalid(err) {
		t.Errorf("create with an inactive relation id: err = %v, want invalid-request", err)
	}
	_, err = runBatch(t, "update", `{"object":"person","ids":`+idList(personID)+`,"fields":{"city":"x"}}`)
	if !isInvalid(err) {
		t.Errorf("batch update: err = %v, want invalid-request", err)
	}
	if sent := writes(*calls); len(sent) != 0 {
		t.Errorf("writing requests = %v, want none", sent)
	}
}

func TestFieldStatesWithoutTheDataModelPermissionChangeNothing(t *testing.T) {
	recordRequests(t, `{"data":{"people":[{"id":"`+personID+`","city":"Berlin"}]},"pageInfo":{"hasNextPage":false}}`)
	out, err := runRecords(t, true, `{"object":"person","fields":["city"]}`)
	if err != nil || !strings.Contains(string(out), "Berlin") {
		t.Errorf("list = %s, %v, want city read as before", out, err)
	}
	info, err := runObjectsTool(t, invokeObjectsGet, `{"object":"person"}`)
	if err != nil || strings.Contains(string(info), `"active"`) {
		t.Errorf("objects.get = %s, %v, want no active member", info, err)
	}
}

func TestFieldStatesAreReadOncePerClient(t *testing.T) {
	reads := 0
	serve(t, func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != schemaPath {
			t.Errorf("unexpected request %s", request.URL.Path)
		}
		return jsonResponse(http.StatusOK, recordsSchema), nil
	})
	previous := fieldStateBody
	t.Cleanup(func() { fieldStateBody = previous })
	// Count the reads by wrapping the transport the previous serve installed.
	inner := transport
	transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == metadataObjectsPath {
			reads++
		}
		return inner.RoundTrip(request)
	})
	t.Cleanup(func() { transport = inner })
	c, _ := client(t)
	for i := 0; i < 3; i++ {
		if _, err := c.GetObject(context.Background(), "person"); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 1 {
		t.Errorf("field-state reads = %d, want 1", reads)
	}
}

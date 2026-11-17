package googlecloud

import (
	"net/url"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

const keyMaterial = `"privateKeyData":"UFJJVkFURS1LRVk=","publicKeyData":"UFVCTElDLUtFWQ==","privateKeyType":"TYPE_PKCS12_FILE"`

func keyRow(id, keyType, extra string) string {
	return `{"name":"` + keyPrefix + id + `","keyAlgorithm":"KEY_ALG_RSA_2048","keyOrigin":"GOOGLE_PROVIDED",` +
		`"keyType":"` + keyType + `","validAfterTime":"2026-01-01T00:00:00Z","validBeforeTime":"2036-01-01T00:00:00Z"` + extra + `}`
}

func TestServiceAccountKeysListFiltersByTypeAndDropsKeyMaterial(t *testing.T) {
	e := newEnvironment(t, nil)
	e.fake.routes[acctPath+acctEmail+"/keys"] = okReply(`{"keys":[` + keyRow("k1", "USER_MANAGED", ","+keyMaterial) + `,` +
		keyRow("k2", "USER_MANAGED", `,"disabled":true,"disableReason":"SERVICE_ACCOUNT_KEY_DISABLE_REASON_EXPOSED",`+
			`"extendedStatus":[{"key":"SERVICE_ACCOUNT_KEY_EXTENDED_STATUS_KEY_EXPOSED","value":"public repository"}]`) + `]}`)
	out, err := e.invoke(serviceAccountKeysListID,
		`{"project_id":"example-project-123","account":"`+acctEmail+`","key_types":["USER_MANAGED","SYSTEM_MANAGED"]}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"privateKeyData", "publicKeyData", "UFJJVkFURS1LRVk=", "UFVCTElDLUtFWQ==", "privateKeyType"} {
		if strings.Contains(out, leaked) {
			t.Errorf("result holds key material %q: %s", leaked, out)
		}
	}
	keys := decode(t, out)["keys"].([]any)
	second := keys[1].(map[string]any)
	if len(keys) != 2 || second["disabled"] != true ||
		second["disableReason"] != "SERVICE_ACCOUNT_KEY_DISABLE_REASON_EXPOSED" ||
		second["extendedStatus"].([]any)[0].(map[string]any)["value"] != "public repository" {
		t.Errorf("keys = %s", out)
	}
	api := e.fake.api()
	query, _ := url.ParseQuery(strings.SplitN(api[0].url, "?", 2)[1])
	if len(api) != 1 || strings.Join(query["keyTypes"], ",") != "USER_MANAGED,SYSTEM_MANAGED" {
		t.Errorf("requests = %+v", api)
	}
	e = newEnvironment(t, nil)
	e.fake.routes[acctPath+acctEmail+"/keys"] = okReply(`{}`)
	out, err = e.invoke(serviceAccountKeysListID, `{"project_id":"example-project-123","account":"`+acctEmail+`","key_types":["SYSTEM_MANAGED"]}`)
	if err != nil || strings.Contains(out, "USER_MANAGED") {
		t.Errorf("empty list = %s, %v", out, err)
	}
	if !strings.HasSuffix(e.fake.api()[0].url, "/keys?keyTypes=SYSTEM_MANAGED") {
		t.Errorf("url = %s", e.fake.api()[0].url)
	}
}

func TestServiceAccountKeysListRefusesBadFilters(t *testing.T) {
	for _, types := range []string{`["KEY_TYPE_UNSPECIFIED"]`, `["USER_MANAGED","USER_MANAGED"]`, `[]`, `"USER_MANAGED"`} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(serviceAccountKeysListID,
			`{"project_id":"example-project-123","account":"`+acctEmail+`","key_types":`+types+`}`); err == nil {
			t.Errorf("key_types %s accepted", types)
		}
		noIO(t, e, "key_types "+types)
	}
}

func TestServiceAccountKeysOfAForeignProjectAreRefusedBeforeIO(t *testing.T) {
	e := newEnvironment(t, nil)
	for _, tool := range []string{serviceAccountKeysListID, serviceAccountKeysGetID} {
		args := `{"project_id":"example-project-123","account":"deploy@example-other-9.iam.gserviceaccount.com","key_id":"k1"}`
		if tool == serviceAccountKeysListID {
			args = `{"project_id":"example-project-123","account":"deploy@example-other-9.iam.gserviceaccount.com"}`
		}
		if _, err := e.invoke(tool, args); err == nil {
			t.Errorf("%s read the keys of another project", tool)
		}
	}
	noIO(t, e, "keys of another project")
}

func TestServiceAccountKeysGetSendsNoPublicKeyTypeAndDropsKeyMaterial(t *testing.T) {
	e := newEnvironment(t, []string{"projects/example-project-123"})
	e.fake.routes[acctPath+acctEmail+"/keys/k1"] = okReply(keyRow("k1", "USER_MANAGED", ","+keyMaterial+`,"disabled":true`))
	out, err := e.invoke(serviceAccountKeysGetID,
		`{"project_id":"example-project-123","account":"`+acctEmail+`","key_id":"k1"}`)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"privateKeyData", "publicKeyData", "UFJJVkFURS1LRVk=", "UFVCTElDLUtFWQ=="} {
		if strings.Contains(out, leaked) {
			t.Errorf("result holds key material %q: %s", leaked, out)
		}
	}
	if got := decode(t, out); got["keyType"] != "USER_MANAGED" || got["disabled"] != true || got["name"] != keyPrefix+"k1" {
		t.Errorf("result = %s", out)
	}
	if api := e.fake.api(); len(api) != 1 || strings.Contains(api[0].url, "?") || strings.Contains(api[0].url, "publicKeyType") {
		t.Errorf("requests = %+v", api)
	}
	e = newEnvironment(t, nil)
	if _, err := e.invoke(serviceAccountKeysGetID, `{"project_id":"example-project-123","account":"1034","key_id":"k1","publicKeyType":"TYPE_RAW_PUBLIC_KEY"}`); err == nil {
		t.Error("publicKeyType was accepted")
	}
	noIO(t, e, "publicKeyType")
}

func TestServiceAccountKeysGetRefusesBadKeyIDsAndForeignAnswers(t *testing.T) {
	for _, id := range []string{"../k", "a/b", "k?x=1", ""} {
		e := newEnvironment(t, nil)
		if _, err := e.invoke(serviceAccountKeysGetID,
			`{"project_id":"example-project-123","account":"1034","key_id":"`+id+`"}`); err == nil {
			t.Errorf("key_id %q accepted", id)
		}
		noIO(t, e, "key_id "+id)
	}
	e := newEnvironment(t, nil)
	e.fake.routes[acctPath+"1034/keys/k1"] = okReply(`{"name":"projects/example-other-9/serviceAccounts/x@example-other-9.iam.gserviceaccount.com/keys/k1"}`)
	if _, err := e.invoke(serviceAccountKeysGetID, `{"project_id":"example-project-123","account":"1034","key_id":"k1"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("foreign key: err = %v", err)
	}
	e = newEnvironment(t, nil)
	e.fake.routes[acctPath+"1034/keys"] = okReply(`{"keys":[` + keyRow("k1", "USER_MANAGED", "") + `,{"name":"projects/example-other-9/serviceAccounts/x/keys/k2"}]}`)
	if _, err := e.invoke(serviceAccountKeysListID, `{"project_id":"example-project-123","account":"1034"}`); classOf(err) != provider.ClassInvalidResponse {
		t.Errorf("foreign key in list: err = %v", err)
	}
}

package googleanalytics

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// accountsOf lists account by account the properties a result shows, as "accounts/A:properties/P,...".
func accountsOf(t *testing.T, result string) []string {
	t.Helper()
	var shown []string
	for _, summary := range decodeSummaries(t, result).AccountSummaries {
		var properties []string
		for _, p := range summary.PropertySummaries {
			properties = append(properties, p.Property)
		}
		shown = append(shown, summary.Account+":"+strings.Join(properties, ","))
	}
	return shown
}

func TestSummariesShowWhatTheBindingReaches(t *testing.T) {
	for connection, want := range map[string][]string{
		"read":     {"accounts/100:properties/200,properties/201", "accounts/900:properties/901,properties/902"},
		"account":  {"accounts/100:properties/200,properties/201"},
		"property": {"accounts/100:properties/200"},
		"mixed":    {"accounts/100:properties/200,properties/201", "accounts/900:properties/901"},
	} {
		env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, twoAccounts)})
		result, err := env.invoke(connection, `{}`)
		if err != nil {
			t.Fatalf("%s: %v", connection, err)
		}
		if got := accountsOf(t, result); !reflect.DeepEqual(got, want) {
			t.Errorf("%s shows %v, want %v", connection, got, want)
		}
		if requests, _ := env.fake.seen(); len(requests) != 1 || requests[0].rawQuery != "" {
			t.Errorf("%s requests = %+v", connection, requests)
		}
	}
}

// A summary whose names Google sent in another form cannot be matched to a target and is dropped on a bound
// connection.
func TestSummariesWithoutDocumentedNamesAreDroppedWhenBound(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"accountSummaries":[{"account":"accounts/0100",`+
		`"propertySummaries":[{"property":"properties/200 "}]}]}`)})
	result, err := env.invoke("property", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if got := accountsOf(t, result); len(got) != 0 {
		t.Errorf("shown = %v", got)
	}
}

func TestBindingDecidesBeforeAnyRequest(t *testing.T) {
	unbound := binding{}
	accountOnly := binding{accounts: []string{"accounts/100"}}
	propertyOnly := binding{properties: []string{"properties/200"}}
	for name, c := range map[string]struct {
		bind          binding
		property      string
		proof, refuse bool
	}{
		"unbound reaches every property":         {unbound, "properties/999", false, false},
		"bound property":                         {propertyOnly, "properties/200", false, false},
		"foreign property without bound account": {propertyOnly, "properties/999", false, true},
		"property below a bound account":         {accountOnly, "properties/999", true, false},
	} {
		proof, err := c.bind.property(c.property)
		if proof != c.proof || (err != nil) != c.refuse {
			t.Errorf("%s: proof %t, error %v", name, proof, err)
		}
		if err != nil && strings.Contains(err.Error(), c.property) {
			t.Errorf("%s: the refusal names the property: %v", name, err)
		}
	}
	if unbound.account("accounts/900") != nil || accountOnly.account("accounts/100") != nil {
		t.Error("an account the binding reaches was refused")
	}
	for _, bind := range []binding{accountOnly, propertyOnly} {
		err := bind.account("accounts/900")
		var refused *provider.InvalidRequestError
		if !errors.As(err, &refused) || strings.Contains(err.Error(), "900") {
			t.Errorf("foreign account = %v, want a refusal that does not name it", err)
		}
	}
	for _, value := range []string{"accounts/1", "properties/0", "properties/1/x", ""} {
		if _, err := checkPropertyName("property", value); err == nil {
			t.Errorf("checkPropertyName(%q) passed", value)
		}
	}
	for _, value := range []string{"properties/1", "accounts/0", "accounts/1/x"} {
		if _, err := checkAccountName("account", value); err == nil {
			t.Errorf("checkAccountName(%q) passed", value)
		}
	}
	if got, err := checkAccountName("account", "accounts/7"); err != nil || got != "accounts/7" {
		t.Errorf("checkAccountName = %q, %v", got, err)
	}
}

func TestBindingOfRefusesWhatNoRequestMayStartFrom(t *testing.T) {
	for name, resolved := range map[string]*config.Resolved{
		"no connection": nil,
		"other origin":  {BaseURL: "https://evil.example"},
		"bad target":    {Targets: []string{"accounts/1", "sites/2"}},
		"exclusion":     {Targets: []string{"accounts/1"}, Exclude: []string{"properties/2"}},
	} {
		if _, err := bindingOf(resolved); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	bind, err := bindingOf(&config.Resolved{Targets: []string{"properties/3", "accounts/2", "properties/3", "accounts/1"}})
	if err != nil || !reflect.DeepEqual(bind, binding{accounts: []string{"accounts/1", "accounts/2"},
		properties: []string{"properties/3"}}) {
		t.Errorf("binding = %+v, %v", bind, err)
	}
}

func TestReadPropertyProvesTheAccountOfAPropertyThatIsNotBound(t *testing.T) {
	answers := map[string]string{
		"/v1beta/properties/200": `{"name":"properties/200","parent":"accounts/100","account":"accounts/100"}`,
		"/v1beta/properties/201": `{"name":"properties/201","parent":"properties/200","account":"accounts/100"}`,
		"/v1beta/properties/901": `{"name":"properties/901","parent":"accounts/900","account":"accounts/900"}`,
		"/v1beta/properties/905": `{"name":"properties/905","parent":"properties/901","account":"accounts/900"}`,
		"/v1beta/properties/906": `{"name":"properties/999","parent":"accounts/100"}`,
	}
	fake := &fakeGoogle{api: func(_ int, r *http.Request) (*http.Response, error) {
		return respond(r, http.StatusOK, "application/json", answers[r.URL.Path]), nil
	}}
	cases := []struct {
		connection, property string
		requests             int
		refused              bool
		class                provider.Class
	}{
		{"account", "properties/200", 1, false, ""},
		{"account", "properties/201", 1, false, ""}, // a subproperty names its account in account
		{"account", "properties/901", 1, true, ""},  // proof read shows another account
		{"account", "properties/905", 1, true, ""},  // below a property of another account
		{"account", "properties/906", 1, false, provider.ClassInvalidResponse},
		{"property", "properties/200", 1, false, ""},
		{"property", "properties/901", 0, true, ""}, // refused before I/O
		{"property", "property/200", 0, true, ""},
		{"read", "properties/901", 1, false, ""},
	}
	for _, c := range cases {
		env := newEnv(t, fake)
		before, _ := fake.seen()
		client := env.open(t, c.connection)
		got, err := client.readProperty(context.Background(), "read property", summariesID, c.property)
		after, _ := fake.seen()
		var refused *provider.InvalidRequestError
		switch {
		case len(after)-len(before) != c.requests:
			t.Errorf("%s %s: %d requests, want %d", c.connection, c.property, len(after)-len(before), c.requests)
		case c.refused && (!errors.As(err, &refused) || strings.Contains(err.Error(), "901")):
			t.Errorf("%s %s: %v, want a refusal that names nothing", c.connection, c.property, err)
		case c.class != "" && classOf(err) != c.class:
			t.Errorf("%s %s: %v, want %s", c.connection, c.property, err, c.class)
		case !c.refused && c.class == "" && (err != nil || string(got.Name) != c.property):
			t.Errorf("%s %s: %+v, %v", c.connection, c.property, got, err)
		}
	}
}

func TestPropertyCarriesEveryDocumentedMember(t *testing.T) {
	env := newEnv(t, &fakeGoogle{api: always(http.StatusOK, `{"name":"properties/200","propertyType":`+
		`"PROPERTY_TYPE_ORDINARY","createTime":"2024-01-02T03:04:05.123456Z","updateTime":"2024-02-03T04:05:06Z",`+
		`"parent":"accounts/100","displayName":"Shop web","industryCategory":"SHOPPING","timeZone":"Europe/Zurich",`+
		`"currencyCode":"CHF","serviceLevel":"GOOGLE_ANALYTICS_STANDARD","deleteTime":"2025-01-01T00:00:00Z",`+
		`"expireTime":"2025-02-01T00:00:00Z","account":"accounts/100","unknown":"x"}`)})
	got, err := env.open(t, "read").getProperty(context.Background(), "get property", summariesID, "properties/200")
	want := property{Name: "properties/200", PropertyType: "PROPERTY_TYPE_ORDINARY",
		CreateTime: "2024-01-02T03:04:05.123456Z", UpdateTime: "2024-02-03T04:05:06Z", Parent: "accounts/100",
		DisplayName: "Shop web", IndustryCategory: "SHOPPING", TimeZone: "Europe/Zurich", CurrencyCode: "CHF",
		ServiceLevel: "GOOGLE_ANALYTICS_STANDARD", DeleteTime: "2025-01-01T00:00:00Z", ExpireTime: "2025-02-01T00:00:00Z",
		Account: "accounts/100"}
	if err != nil || got != want {
		t.Errorf("property = %+v, %v", got, err)
	}
}

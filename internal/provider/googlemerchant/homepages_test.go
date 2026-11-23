package googlemerchant

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// homepageCall is one request the fake saw: method, path with query, and the body.
type homepageCall struct{ method, path, body string }

func (h *harness) homepageCalls(t *testing.T) []homepageCall {
	t.Helper()
	var out []homepageCall
	for _, req := range h.google.seen() {
		if req.URL.Host != "merchantapi.googleapis.com" || req.URL.Scheme != "https" {
			t.Errorf("request left the fixed origin: %s", req.URL.Host)
		}
		if req.Header.Get("Authorization") != "Bearer "+testAccess || strings.Contains(req.URL.String(), testAccess) {
			t.Errorf("token missing from the header or present in the URL of %s", req.URL.Path)
		}
		body := ""
		if req.Body != nil {
			b, _ := io.ReadAll(req.Body)
			body = string(b)
		}
		path := req.URL.Path
		if req.URL.RawQuery != "" {
			path += "?" + req.URL.RawQuery
		}
		out = append(out, homepageCall{req.Method, path, body})
	}
	return out
}

const testHomepageAnswer = `{"name":"accounts/1000001/homepage","uri":"https://shop.example.invalid/","claimed":true,"extra":"never held"}`

var homepageWriters = map[string]struct {
	handler capability.Handler
	args    string
}{
	"update":  {handleHomepagesUpdate, `{"account":"1000001","uri":"https://shop.example.invalid/"}`},
	"claim":   {handleHomepagesClaim, `{"account":"1000001"}`},
	"unclaim": {handleHomepagesUnclaim, `{"account":"1000001"}`},
}

func TestHomepageDescriptorsCarryRiskToolListRequirementAndProfileExclusion(t *testing.T) {
	for _, c := range []struct {
		d       capability.Descriptor
		effect  capability.Effect
		confirm bool
	}{
		{homepagesGet, capability.EffectRead, false},
		{homepagesUpdate, capability.EffectUpdate, true},
		{homepagesClaim, capability.EffectUpdate, true},
		{homepagesUnclaim, capability.EffectUpdate, true},
	} {
		r := c.d.Risk
		if c.d.Version != 1 || r.Effect != c.effect || !r.OpenWorld || c.d.RequiresToolAllowList != c.confirm ||
			(r.Confirmation == capability.ConfirmationRequired) != c.confirm {
			t.Errorf("%s: version %d, risk %+v, tools list %t", c.d.ID, c.d.Version, r, c.d.RequiresToolAllowList)
		}
		if c.confirm && (r.Idempotency != capability.IdempotencyIdempotent || !strings.Contains(c.d.Description, "several minutes") ||
			!strings.Contains(c.d.Description, "never repeated")) {
			t.Errorf("%s: idempotency %s, or the description lacks the delay or the no-repeat note", c.d.ID, r.Idempotency)
		}
	}
	reg := capability.NewRegistry()
	if err := Register(reg); err != nil {
		t.Fatal(err)
	}
	if err := reg.ValidateProfiles(); err != nil {
		t.Fatal(err)
	}
	metadata, _ := reg.ProviderMetadata(Provider)
	for _, d := range homepageDescriptors() {
		if slices.Contains(metadata.Profiles[0].Tools, d.ID) != (d.ID == homepagesGetID) {
			t.Errorf("the read profile holds %s: %t", d.ID, !(d.ID == homepagesGetID))
		}
	}
	for _, name := range []string{"update_mask", "updateMask", "name", "claimed"} {
		for _, d := range homepageDescriptors() {
			if strings.Contains(string(d.InputSchema), `"`+name+`"`) {
				t.Errorf("%s takes %s", d.ID, name)
			}
		}
	}
	if !strings.Contains(homepagesClaim.Description, "WARNING") || !strings.Contains(string(homepagesClaim.InputSchema), `"overwrite"`) {
		t.Error("claim does not warn about overwrite")
	}
	if strings.Contains(string(homepagesUpdate.InputSchema), `"overwrite"`) || strings.Contains(string(homepagesUnclaim.InputSchema), `"uri"`) {
		t.Error("an argument is on the wrong tool")
	}
}

func TestHomepagesGetReadsTheHomepageOfTheAccount(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testHomepageAnswer, nil }
	out, err := h.call(t, handleHomepagesGet, `{}`)
	if err != nil || out["name"] != "accounts/1000001/homepage" || out["uri"] != "https://shop.example.invalid/" ||
		out["claimed"] != true || len(out) != 3 {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	if got := h.homepageCalls(t); len(got) != 1 || got[0] != (homepageCall{"GET", "/accounts/v1/accounts/1000001/homepage", ""}) {
		t.Errorf("requests = %v", got)
	}
}

func TestHomepagesGetShowsAnUnclaimedHomepageAndCutsLongAddresses(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/5/homepage","uri":"https://shop.example.invalid/` + strings.Repeat("a", 3000) + `"}`, nil
	}
	out, err := h.call(t, handleHomepagesGet, `{"account":"5"}`)
	uri, _ := out["uri"].(string)
	if err != nil || out["claimed"] != false || !strings.HasSuffix(uri, "…") || len([]rune(uri)) != maxURLRunes {
		t.Fatalf("claimed = %v, uri length %d, err = %v", out["claimed"], len([]rune(uri)), err)
	}
}

func TestHomepageToolsCheckTheAccountBeforeSecretAndRequest(t *testing.T) {
	handlers := map[string]capability.Handler{"get": handleHomepagesGet}
	for name, w := range homepageWriters {
		handlers[name] = w.handler
	}
	for tool, handler := range handlers {
		for name, c := range map[string]struct {
			targets []string
			args    string
		}{
			"outside the account target": {[]string{"accounts/1"}, `{"account":"2","uri":"https://shop.example.invalid/"}`},
			"not an ID":                  {nil, `{"account":"1/homepage","uri":"https://shop.example.invalid/"}`},
			"leading zero":               {nil, `{"account":"01","uri":"https://shop.example.invalid/"}`},
			"two targets, no argument":   {[]string{"accounts/1", "accounts/2"}, `{"uri":"https://shop.example.invalid/"}`},
		} {
			t.Run(tool+"/"+name, func(t *testing.T) {
				h := newHarness(t, c.targets...)
				if _, err := h.call(t, handler, c.args); !isInvalidRequest(err) {
					t.Errorf("err = %v, want invalid request", err)
				}
				h.untouched(t)
			})
		}
	}
	for name, w := range homepageWriters {
		t.Run(name+"/account is named explicitly", func(t *testing.T) {
			h := newHarness(t, "accounts/1000001")
			if _, err := h.call(t, w.handler, `{"uri":"https://shop.example.invalid/"}`); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			h.untouched(t)
		})
	}
}

func TestHomepagesUpdateRefusesAddressesThatAreNotPlainHTTPSBeforeSecretAndRequest(t *testing.T) {
	const user = "user"
	for name, uri := range map[string]string{
		"http":              "http://shop.example.invalid/",
		"no scheme":         "shop.example.invalid",
		"other scheme":      "javascript:alert(1)",
		"opaque":            "https:shop.example.invalid",
		"no host":           "https:///path",
		"scheme relative":   "//shop.example.invalid/",
		"user information":  "https://" + user + at + "shop.example.invalid/",
		"user and password": "https://" + user + ":word" + at + "shop.example.invalid/",
		"backslash":         `https://shop.example.invalid\` + at + "other.example.invalid/",
		"space":             "https://shop.example.invalid/ x",
		"control character": "https://shop.example.invalid/\nx",
		"empty":             "",
		"longer than 2048":  "https://shop.example.invalid/" + strings.Repeat("a", maxURLRunes),
		"template host":     "https://{store}.example.invalid/",
		"bad port":          "https://shop.example.invalid:port/",
		"trailing space":    "https://shop.example.invalid/ ",
		"uppercase http":    "HTTP://shop.example.invalid/",
		"file":              "file:///etc/hosts",
		"only user":         "https://" + user + at + "/",
		"ftp":               "ftp://shop.example.invalid/",
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "accounts/1000001")
			args, _ := json.Marshal(map[string]string{"account": "1000001", "uri": uri})
			if _, err := h.call(t, handleHomepagesUpdate, string(args)); !isInvalidRequest(err) {
				t.Errorf("%q: err = %v, want invalid request", uri, err)
			}
			h.untouched(t)
		})
	}
	t.Run("uri missing", func(t *testing.T) {
		h := newHarness(t, "accounts/1000001")
		if _, err := h.call(t, handleHomepagesUpdate, `{"account":"1000001"}`); !isInvalidRequest(err) {
			t.Errorf("err = %v, want invalid request", err)
		}
		h.untouched(t)
	})
}

func TestHomepagesUpdateSendsOnlyTheAddressWithItsMask(t *testing.T) {
	h := newHarness(t, "accounts/1000001")
	h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testHomepageAnswer, nil }
	out, err := h.call(t, handleHomepagesUpdate, `{"account":"1000001","uri":"https://shop.example.invalid/store?lang=en"}`)
	if err != nil || out["claimed"] != true || out["name"] != "accounts/1000001/homepage" {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	want := homepageCall{"PATCH", "/accounts/v1/accounts/1000001/homepage?updateMask=uri",
		`{"uri":"https://shop.example.invalid/store?lang=en"}`}
	if got := h.homepageCalls(t); len(got) != 1 || got[0] != want {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

func TestHomepagesClaimSendsOverwriteOnlyWhenAsked(t *testing.T) {
	for args, want := range map[string]string{
		`{"account":"1000001"}`:                   `{}`,
		`{"account":"1000001","overwrite":false}`: `{}`,
		`{"account":"1000001","overwrite":true}`:  `{"overwrite":true}`,
	} {
		h := newHarness(t)
		h.google.api = func(*http.Request) (int, string, http.Header) { return 200, testHomepageAnswer, nil }
		if _, err := h.call(t, handleHomepagesClaim, args); err != nil {
			t.Fatal(err)
		}
		got := h.homepageCalls(t)
		if len(got) != 1 || got[0] != (homepageCall{"POST", "/accounts/v1/accounts/1000001/homepage:claim", want}) {
			t.Errorf("%s: requests = %v", args, got)
		}
	}
}

func TestHomepagesUnclaimSendsNoBody(t *testing.T) {
	h := newHarness(t)
	h.google.api = func(*http.Request) (int, string, http.Header) {
		return 200, `{"name":"accounts/1000001/homepage","uri":"https://shop.example.invalid/"}`, nil
	}
	out, err := h.call(t, handleHomepagesUnclaim, `{"account":"1000001"}`)
	if err != nil || out["claimed"] != false {
		t.Fatalf("out = %v, err = %v", out, err)
	}
	got := h.homepageCalls(t)
	if len(got) != 1 || got[0] != (homepageCall{"POST", "/accounts/v1/accounts/1000001/homepage:unclaim", ""}) {
		t.Errorf("requests = %v", got)
	}
}

func TestHomepageChangesAreSentOnceAndAnUnclearOutcomeIsReported(t *testing.T) {
	for name, w := range homepageWriters {
		for status, want := range map[int]provider.Class{
			401: provider.ClassAuth, 403: provider.ClassPermission, 404: provider.ClassNotFound,
			429: provider.ClassRateLimited, 500: provider.ClassProviderError, 503: provider.ClassUnreachable,
		} {
			t.Run(name+"/"+http.StatusText(status), func(t *testing.T) {
				h := newHarness(t)
				h.google.api = func(*http.Request) (int, string, http.Header) {
					return status, `{"error":{"status":"X"}}`, nil
				}
				_, err := h.call(t, w.handler, w.args)
				if classOf(err) != want {
					t.Fatalf("class = %q, want %q (%v)", classOf(err), want, err)
				}
				if unknown := strings.Contains(err.Error(), "outcome of the change is unknown"); unknown != (status >= 500) {
					t.Errorf("status %d: unknown outcome reported = %t", status, unknown)
				}
				if n := len(h.google.seen()); n != 1 {
					t.Errorf("%d requests, want exactly one", n)
				}
			})
		}
		t.Run(name+"/transport failure", func(t *testing.T) {
			h := newHarness(t)
			h.google.fail = io.ErrUnexpectedEOF
			_, err := h.call(t, w.handler, w.args)
			if err == nil || !strings.Contains(err.Error(), "outcome of the change is unknown") || len(h.google.seen()) != 1 {
				t.Errorf("err = %v, %d requests", err, len(h.google.seen()))
			}
		})
	}
}

func TestHomepageChangesProveASubAccountFirstAndRefuseOthersWithoutAChange(t *testing.T) {
	for name, w := range homepageWriters {
		t.Run(name+"/sub-account", func(t *testing.T) {
			h := newHarness(t, "advanced-accounts/1")
			h.google.api = func(req *http.Request) (int, string, http.Header) {
				if strings.HasSuffix(req.URL.Path, ":listSubaccounts") {
					return 200, `{"accounts":[` + accountRow("1000001", "Sub") + `]}`, nil
				}
				return 200, testHomepageAnswer, nil
			}
			if _, err := h.call(t, w.handler, w.args); err != nil {
				t.Fatal(err)
			}
			got := h.homepageCalls(t)
			if len(got) != 2 || got[0].method != "GET" || !strings.HasSuffix(got[0].path, ":listSubaccounts?pageSize=500") ||
				got[1].method == "GET" || !strings.Contains(got[1].path, "/accounts/1000001/homepage") {
				t.Errorf("requests = %v", got)
			}
		})
		t.Run(name+"/not a sub-account", func(t *testing.T) {
			h := newHarness(t, "advanced-accounts/1")
			h.google.api = func(*http.Request) (int, string, http.Header) {
				return 200, `{"accounts":[` + accountRow("2", "Other") + `]}`, nil
			}
			if _, err := h.call(t, w.handler, w.args); !isInvalidRequest(err) {
				t.Errorf("err = %v, want invalid request", err)
			}
			for _, c := range h.homepageCalls(t) {
				if c.method != "GET" {
					t.Errorf("a change was sent: %v", c)
				}
			}
		})
	}
}

func TestHomepageSummariesNameWhatTheConfirmationApproves(t *testing.T) {
	s := homepageSummaries()
	for _, c := range []struct {
		id, args string
		want     []string
	}{
		{homepagesUpdateID, `{"account":"1","uri":"https://shop.example.invalid/"}`,
			[]string{"account 1", `"https://shop.example.invalid/"`, "customers"}},
		{homepagesClaimID, `{"account":"1"}`, []string{"Claim", "account 1"}},
		{homepagesClaimID, `{"account":"1","overwrite":true}`, []string{"account 1", "overwrite", "WARNING", "another account"}},
		{homepagesUnclaimID, `{"account":"1"}`, []string{"Remove the claim", "account 1"}},
	} {
		got := s[c.id](json.RawMessage(c.args))
		for _, w := range c.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s summary %q lacks %q", c.id, got, w)
			}
		}
	}
	if got := s[homepagesClaimID](json.RawMessage(`{"account":"1"}`)); strings.Contains(got, "overwrite") {
		t.Errorf("a claim without overwrite mentions it: %q", got)
	}
}

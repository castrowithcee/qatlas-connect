package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// providerSecret is text GitHub sends in a refusal; it must never reach a Qatlas message.
const providerSecret = "provider-only refusal text"

// referenceFake answers the reference routes with every member the REST description documents.
type referenceFake struct {
	// gateway answers every reference route with 504.
	gateway bool
	// html is the rendered HTML /markdown answers.
	html string
}

func (r *referenceFake) list(prefix string, count int) string {
	entries := make([]string, count)
	for i := range entries {
		entries[i] = fmt.Sprintf(`"%s%d/32"`, prefix, i)
	}
	return "[" + strings.Join(entries, ",") + "]"
}

func (r *referenceFake) route(w http.ResponseWriter, req *http.Request) bool {
	path := strings.TrimPrefix(req.URL.Path, "/api/v3")
	if referenceSubject(path) == "" {
		return false
	}
	if r.gateway {
		w.WriteHeader(http.StatusGatewayTimeout)
		return true
	}
	if path == "/markdown" {
		w.Header().Set("Content-Type", "text/html;charset=utf-8")
		fmt.Fprint(w, r.html)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	if strings.HasSuffix(path, "/unknown") {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"`+providerSecret+`"}`)
		return true
	}
	bucket := `{"limit":5000,"remaining":4999,"reset":1700000000,"used":1}`
	license := `"key":"mit","name":"MIT License","spdx_id":"MIT","url":"https://api.github.com/licenses/mit",` +
		`"node_id":"MDc6TGljZW5zZTEz","html_url":"http://choosealicense.com/licenses/mit/"`
	switch path {
	case "/rate_limit":
		fmt.Fprint(w, `{"resources":{"core":`+bucket+`,"graphql":`+bucket+`,"search":`+bucket+`,`+
			`"dependency_snapshots":`+bucket+`,"Bad Name":`+bucket+`},"rate":`+bucket+`}`)
	case "/meta":
		domains := `{"website":["github.com"],"codespaces":["*.github.dev"],"copilot":["*.githubcopilot.com"],` +
			`"packages":["ghcr.io"],"storage":["*.blob.core.windows.net"],"actions":["*.actions.githubusercontent.com"],` +
			`"actions_inbound":{"full_domains":["github.com"],"wildcard_domains":["*.github.com"]},` +
			`"artifact_attestations":{"trust_domain":"","services":["*.actions.githubusercontent.com"]}}`
		fmt.Fprint(w, `{"verifiable_password_authentication":false,"ssh_key_fingerprints":{"SHA256_RSA":"a",`+
			`"SHA256_DSA":"b","SHA256_ECDSA":"c","SHA256_ED25519":"d"},"ssh_keys":["ssh-ed25519 AAAA"],`+
			`"hooks":`+r.list("192.30.252.", 3)+`,"web":["140.82.112.0/20"],"api":["140.82.112.0/20"],`+
			`"git":["140.82.112.0/20"],"github_enterprise_importer":["20.99.172.64/28"],"packages":["140.82.121.33/32"],`+
			`"pages":["185.199.108.0/22"],"importer":["52.23.85.212/32"],"actions":`+r.list("4.148.0.", 150)+`,`+
			`"actions_macos":["13.105.117.0/31"],"codespaces":["20.42.11.16/28"],"dependabot":["192.0.2.1/32"],`+
			`"copilot":["20.85.130.105/32"],"shared_ingress":["192.0.2.2/32"],"commit_signing_keys":["key"],`+
			`"domains":`+domains+`}`)
	case "/licenses":
		if req.URL.Query().Get("page") == "1" {
			w.Header().Set("Link", `<https://api.github.com/licenses?page=2>; rel="next"`)
		}
		fmt.Fprint(w, `[{`+license+`}]`)
	case "/licenses/mit":
		fmt.Fprint(w, `{`+license+`,"description":"A short permissive license","implementation":"Create a file",`+
			`"permissions":["commercial-use","modifications"],"conditions":["include-copyright"],`+
			`"limitations":["liability","warranty"],"body":"`+strings.Repeat("b", referenceTextLimit+10)+`",`+
			`"featured":true}`)
	case "/gitignore/templates":
		fmt.Fprint(w, `["Go","Node","C++"]`)
	case "/gitignore/templates/Go":
		fmt.Fprint(w, `{"name":"Go","source":"`+strings.Repeat("s", referenceTextLimit+1)+`"}`)
	case "/codes_of_conduct":
		fmt.Fprint(w, `[{"key":"contributor_covenant","name":"Contributor Covenant",`+
			`"url":"https://api.github.com/codes_of_conduct/contributor_covenant",`+
			`"html_url":"https://www.contributor-covenant.org/version/2/0/code_of_conduct/"}]`)
	case "/codes_of_conduct/contributor_covenant":
		fmt.Fprint(w, `{"key":"contributor_covenant","name":"Contributor Covenant",`+
			`"url":"https://api.github.com/codes_of_conduct/contributor_covenant",`+
			`"html_url":"https://www.contributor-covenant.org/version/2/0/code_of_conduct/",`+
			`"body":"`+strings.Repeat("c", referenceTextLimit+5)+`"}`)
	case "/emojis":
		fmt.Fprint(w, `{"+1":"https://github.githubassets.com/images/icons/emoji/unicode/1f44d.png",`+
			`"heart":"https://github.githubassets.com/h.png","heart_eyes":"https://github.githubassets.com/he.png",`+
			`"broken_heart":"https://github.githubassets.com/bh.png","smile":"https://github.githubassets.com/s.png",`+
			`"Bad Heart":"https://github.githubassets.com/x.png"}`)
	default:
		return false
	}
	return true
}

// referenceConfig adds to discoveryConfig a connection without targets that excludes one repository.
func referenceConfig(base string) *config.Config {
	cfg := discoveryConfig(base)
	cfg.Connections["excluding"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Exclude: []string{"repos/octo-org/secret"}, Effects: config.Effects()}
	return cfg
}

func serveReference(t *testing.T) (*fakeGitHub, *referenceFake, *application.Core, *int) {
	t.Helper()
	r := &referenceFake{html: "<p>Fixes <a href=\"https://github.com/octo-org/example/issues/1\">#1</a></p>"}
	f := &fakeGitHub{failure: r.route}
	base := serve(t, f)
	reads := 0
	red := &redact.Redactor{}
	return f, r, application.New(registry(t), referenceConfig(base), resolver(red, &reads), red), &reads
}

// schemaMembers returns the top-level members of an output schema, or of the entries of its list member.
func schemaMembers(t *testing.T, raw json.RawMessage, list string) []string {
	t.Helper()
	var schema struct {
		Properties map[string]struct {
			Items struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"items"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	var names []string
	if list == "" {
		for name := range schema.Properties {
			names = append(names, name)
		}
	} else {
		for name := range schema.Properties[list].Items.Properties {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func keysOf(object map[string]json.RawMessage) []string {
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Every reference tool answers every member the REST description documents for a complete answer.
func TestReferenceToolsAnswerEveryDocumentedMember(t *testing.T) {
	f, _, core, _ := serveReference(t)
	for _, tt := range []struct {
		descriptor     string
		arguments      string
		list           string
		absent         []string
		route, request string
	}{
		{rateLimitGet.ID, `{}`, "", nil, "GET", "/api/v3/rate_limit"},
		{metaGet.ID, `{}`, "", nil, "GET", "/api/v3/meta"},
		{licensesList.ID, `{"featured":true,"limit":5}`, "licenses", nil, "GET", "/api/v3/licenses"},
		{licensesGet.ID, `{"license":"mit"}`, "", nil, "GET", "/api/v3/licenses/mit"},
		{gitignoreTemplatesList.ID, `{}`, "", nil, "GET", "/api/v3/gitignore/templates"},
		{gitignoreTemplatesGet.ID, `{"name":"Go"}`, "", nil, "GET", "/api/v3/gitignore/templates/Go"},
		// The list answer of GitHub carries no body, so neither does the entry.
		{codesOfConductList.ID, `{}`, "codes_of_conduct", []string{"body"}, "GET", "/api/v3/codes_of_conduct"},
		{codesOfConductGet.ID, `{"key":"contributor_covenant"}`, "", nil, "GET",
			"/api/v3/codes_of_conduct/contributor_covenant"},
		{emojisList.ID, `{"name":"heart"}`, "emojis", nil, "GET", "/api/v3/emojis"},
		{markdownRender.ID, `{"text":"Fixes #1","mode":"gfm","context":"octo-org/example"}`, "", nil, "POST",
			"/api/v3/markdown"},
	} {
		descriptor, _, _ := registry(t).Lookup(tt.descriptor)
		before := len(f.recorded())
		got, err := invoke(t, core, tt.descriptor, "open", tt.arguments, false)
		if err != nil {
			t.Fatalf("%s = %v", tt.descriptor, err)
		}
		requests := f.recorded()[before:]
		if len(requests) != 1 || requests[0].method != tt.route || requests[0].path != tt.request {
			t.Errorf("%s requests = %+v, want one %s %s", tt.descriptor, requests, tt.route, tt.request)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(got, &object); err != nil {
			t.Fatalf("%s = %s", tt.descriptor, got)
		}
		want := schemaMembers(t, descriptor.OutputSchema, tt.list)
		have := keysOf(object)
		if tt.list != "" {
			var entries []map[string]json.RawMessage
			if json.Unmarshal(object[tt.list], &entries) != nil || len(entries) == 0 {
				t.Fatalf("%s %s = %s", tt.descriptor, tt.list, got)
			}
			have = keysOf(entries[0])
		}
		var missing []string
		for _, name := range want {
			if !strings.Contains(","+strings.Join(have, ",")+",", ","+name+",") &&
				!strings.Contains(strings.Join(tt.absent, ","), name) && name != "next_cursor" {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			t.Errorf("%s lacks %v: %s", tt.descriptor, missing, got)
		}
	}
}

func TestReferenceToolsReadAsGitHubDocumentsThem(t *testing.T) {
	f, r, core, _ := serveReference(t)

	got, _ := invoke(t, core, rateLimitGet.ID, "org", `{}`, false)
	var limits RateLimitStatus
	if json.Unmarshal(got, &limits) != nil || len(limits.Resources) != 4 || limits.Resources["dependency_snapshots"].Limit == nil ||
		limits.Rate == nil || *limits.Resources["core"].Remaining != 4999 {
		t.Errorf("rate limit on an owner-only connection = %s, want every well-named group as GitHub sent it", got)
	}

	// Without services every list is cut at the default bound and says so; with services the named lists come
	// in full and nothing else does.
	got, _ = invoke(t, core, metaGet.ID, "open", `{}`, false)
	var meta Meta
	if json.Unmarshal(got, &meta) != nil || len(meta.Actions) != metaListLimit || !meta.Truncated || len(meta.Hooks) != 3 ||
		meta.Domains == nil || meta.Domains.ArtifactAttestations == nil || meta.Domains.ArtifactAttestations.TrustDomain != nil {
		t.Errorf("meta = %s, want the lists cut at %d", got, metaListLimit)
	}
	got, _ = invoke(t, core, metaGet.ID, "open", `{"services":["actions","domains"]}`, false)
	meta = Meta{}
	if json.Unmarshal(got, &meta) != nil || len(meta.Actions) != 150 || meta.Truncated || meta.Hooks != nil ||
		meta.Domains == nil || meta.SSHKeyFingerprints == nil {
		t.Errorf("meta with services = %s, want only actions and domains in full", got)
	}

	before := len(f.recorded())
	got, _ = invoke(t, core, licensesList.ID, "open", `{"featured":true,"limit":5}`, false)
	var licenses LicenseList
	if json.Unmarshal(got, &licenses) != nil || !licenses.HasMore || licenses.Licenses[0].URL == nil ||
		licenses.Licenses[0].Links["url"] != "https://api.github.com/licenses/mit" {
		t.Errorf("licenses = %s", got)
	}
	if query := f.recorded()[before].query; !strings.Contains(query, "featured=true") ||
		!strings.Contains(query, "per_page=5") || !strings.Contains(query, "page=1") {
		t.Errorf("license list query = %q", query)
	}
	next, err := invoke(t, core, licensesList.ID, "open", `{"featured":true,"cursor":"`+licenses.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(f.recorded()[len(f.recorded())-1].query, "page=2") ||
		strings.Contains(string(next), `"has_more":true`) {
		t.Errorf("license continuation = %s, %v", next, err)
	}
	if _, err := invoke(t, core, licensesList.ID, "open", `{"cursor":"`+licenses.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("license cursor without its filter = %v, want invalid request", err)
	}

	// Template texts are untrusted data, cut at a fixed length, and say so.
	got, _ = invoke(t, core, licensesGet.ID, "open", `{"license":"mit"}`, false)
	var license License
	if json.Unmarshal(got, &license) != nil || !license.Truncated || utf8.RuneCountInString(*license.Body) != referenceTextLimit ||
		len(license.Permissions) != 2 || license.Featured == nil || !*license.Featured {
		t.Errorf("license = %.300s, want the body cut", got)
	}
	got, _ = invoke(t, core, gitignoreTemplatesGet.ID, "open", `{"name":"Go"}`, false)
	var template GitignoreTemplate
	if json.Unmarshal(got, &template) != nil || !template.Truncated || len(*template.Source) != referenceTextLimit {
		t.Errorf("template = %.200s, want the source cut", got)
	}
	got, _ = invoke(t, core, codesOfConductGet.ID, "open", `{"key":"contributor_covenant"}`, false)
	var code CodeOfConduct
	if json.Unmarshal(got, &code) != nil || !code.Truncated || len(*code.Body) != referenceTextLimit {
		t.Errorf("code of conduct = %.200s, want the body cut", got)
	}
	got, _ = invoke(t, core, gitignoreTemplatesList.ID, "open", `{}`, false)
	if !strings.Contains(string(got), `"templates":["Go","Node","C++"]`) {
		t.Errorf("templates = %s", got)
	}

	// The Markdown text and context travel in one POST; the HTML comes back as text, cut at its bound.
	before = len(f.recorded())
	got, err = invoke(t, core, markdownRender.ID, "excluding", `{"text":"Fixes #1","mode":"gfm","context":"octo-org/example"}`, false)
	sent := f.recorded()[before]
	if err != nil || sent.body["text"] != "Fixes #1" || sent.body["mode"] != "gfm" || sent.body["context"] != "octo-org/example" ||
		!strings.Contains(string(got), `"context":"octo-org/example"`) || !strings.Contains(string(got), `"truncated":false`) {
		t.Errorf("render = %s, %v; sent %+v", got, err, sent.body)
	}
	before = len(f.recorded())
	if _, err := invoke(t, core, markdownRender.ID, "org", `{"text":"plain"}`, false); err != nil ||
		f.recorded()[before].body["mode"] != "markdown" || f.recorded()[before].body["context"] != nil {
		t.Errorf("render without context = %v, sent %+v; want mode markdown and no context", err, f.recorded()[before].body)
	}
	r.html = strings.Repeat("h", markdownHTMLLimit+100)
	got, _ = invoke(t, core, markdownRender.ID, "open", `{"text":"long"}`, false)
	var rendered RenderedMarkdown
	if json.Unmarshal(got, &rendered) != nil || !rendered.Truncated || len(rendered.HTML) != markdownHTMLLimit {
		t.Errorf("long render truncated = %v with %d bytes, want a cut at %d", rendered.Truncated, len(rendered.HTML), markdownHTMLLimit)
	}

	// Emojis come in batches of names in alphabetical order, bound to the name filter.
	first, _ := invoke(t, core, emojisList.ID, "open", `{"name":"heart","limit":2}`, false)
	var emojis EmojiList
	if json.Unmarshal(first, &emojis) != nil || len(emojis.Emojis) != 2 || emojis.Emojis[0].Name != "broken_heart" ||
		emojis.Emojis[1].Name != "heart" || !emojis.HasMore || emojis.Emojis[0].URL == nil {
		t.Fatalf("emojis = %s", first)
	}
	last, err := invoke(t, core, emojisList.ID, "open", `{"name":"heart","cursor":"`+emojis.NextCursor+`"}`, false)
	if err != nil || !strings.Contains(string(last), `"name":"heart_eyes"`) || strings.Contains(string(last), `"has_more":true`) {
		t.Errorf("emoji continuation = %s, %v", last, err)
	}
	for name, cursor := range alteredCursors(t, emojis.NextCursor) {
		if _, err := invoke(t, core, emojisList.ID, "open", `{"name":"heart","cursor":"`+cursor+`"}`, false); !isInvalidRequest(err) {
			t.Errorf("emojis with %s = %v, want invalid request", name, err)
		}
	}
	if _, err := invoke(t, core, emojisList.ID, "open", `{"name":"smile","cursor":"`+emojis.NextCursor+`"}`, false); !isInvalidRequest(err) {
		t.Errorf("emoji cursor with another filter = %v, want invalid request", err)
	}
	all, _ := invoke(t, core, emojisList.ID, "open", `{}`, false)
	if strings.Contains(string(all), "Bad Heart") || !strings.Contains(string(all), `"name":"+1"`) {
		t.Errorf("emojis = %s, want names outside the emoji form left out", all)
	}
}

// A connection whose targets name a repository or a project may read none of them, and a refused argument
// never reaches the credential or GitHub.
func TestReferenceRefusalsSettleBeforeIO(t *testing.T) {
	f, _, core, reads := serveReference(t)
	tools := map[string]string{rateLimitGet.ID: `{}`, metaGet.ID: `{}`, licensesList.ID: `{}`,
		licensesGet.ID: `{"license":"mit"}`, gitignoreTemplatesList.ID: `{}`, gitignoreTemplatesGet.ID: `{"name":"Go"}`,
		codesOfConductList.ID: `{}`, codesOfConductGet.ID: `{"key":"contributor_covenant"}`, emojisList.ID: `{}`,
		markdownRender.ID: `{"text":"hi"}`}
	type refusal struct{ id, connection, arguments, want string }
	var cases []refusal
	for id, arguments := range tools {
		for _, connection := range []string{"repo", "planning", "starrepo"} {
			cases = append(cases, refusal{id, connection, arguments, "name a repository or a project"})
		}
	}
	// The account-wide rule holds with context as well.
	cases = append(cases, refusal{markdownRender.ID, "starrepo", `{"text":"#1","mode":"gfm","context":"octo-org/example"}`,
		"name a repository or a project"})
	for _, name := range []string{".", "..", "a/b", "%2e%2e", "-x", ""} {
		encoded, _ := json.Marshal(name)
		cases = append(cases, refusal{licensesGet.ID, "open", `{"license":` + string(encoded) + `}`, ""},
			refusal{gitignoreTemplatesGet.ID, "open", `{"name":` + string(encoded) + `}`, ""},
			refusal{codesOfConductGet.ID, "open", `{"key":` + string(encoded) + `}`, ""})
	}
	cases = append(cases,
		// Too long in bytes, though within the schema's character bound.
		refusal{markdownRender.ID, "open", `{"text":"` + strings.Repeat("ä", markdownTextLimit/2+1) + `"}`, "bytes"},
		refusal{markdownRender.ID, "open", `{"text":"` + strings.Repeat("a", markdownTextLimit+1) + `"}`, ""},
		refusal{markdownRender.ID, "open", `{"text":"a\u0000b"}`, "control"},
		refusal{markdownRender.ID, "open", `{"text":"a","mode":"html"}`, ""},
		refusal{markdownRender.ID, "open", `{"text":"#1","context":"octo-org/example"}`, "only with mode gfm"},
		refusal{markdownRender.ID, "org", `{"text":"#1","mode":"gfm","context":"octo-org/example"}`, "outside the targets"},
		refusal{markdownRender.ID, "excluding", `{"text":"#1","mode":"gfm","context":"octo-org/secret"}`, "not available"},
		refusal{markdownRender.ID, "excluding", `{"text":"#1","mode":"gfm","context":"Octo-Org/Secret"}`, "not available"},
		refusal{markdownRender.ID, "open", `{"text":"#1","mode":"gfm","context":"octo-org"}`, ""},
		refusal{metaGet.ID, "open", `{"services":["hooks","hooks"]}`, ""},
		refusal{metaGet.ID, "open", `{"services":["ftp"]}`, ""},
		refusal{emojisList.ID, "open", `{"name":"Heart"}`, ""},
		refusal{emojisList.ID, "open", `{"limit":101}`, ""},
	)
	for _, tt := range cases {
		*reads = 0
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, tt.connection, tt.arguments, false)
		if !isInvalidRequest(err) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s(%.80s) on %s = %v, want an invalid request naming %q", tt.id, tt.arguments, tt.connection, err, tt.want)
		}
		if *reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s(%.80s) on %s reached the credential or GitHub", tt.id, tt.arguments, tt.connection)
		}
	}
	for _, name := range []string{".", ".."} {
		if validReferenceName(name) {
			t.Errorf("validReferenceName(%q) = true, want a dot segment refused", name)
		}
	}
	for _, name := range []string{"mit", "apache-2.0", "C++", "Objective-C", "contributor_covenant"} {
		if !validReferenceName(name) {
			t.Errorf("validReferenceName(%q) = false", name)
		}
	}
}

// An unknown name is not found, a gateway timeout is a timeout, each after exactly one request, and GitHub's
// own text never reaches the message.
func TestReferenceFailuresAreSentOnce(t *testing.T) {
	f, r, core, _ := serveReference(t)
	for _, tt := range []struct{ id, arguments string }{
		{licensesGet.ID, `{"license":"unknown"}`},
		{gitignoreTemplatesGet.ID, `{"name":"unknown"}`},
		{codesOfConductGet.ID, `{"key":"unknown"}`},
	} {
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, "open", tt.arguments, false)
		if classOf(err) != provider.ClassNotFound || strings.Contains(err.Error(), providerSecret) ||
			strings.Contains(err.Error(), "unknown") || len(f.recorded()) != before+1 {
			t.Errorf("%s unknown = %v after %d requests, want one not-found without provider text", tt.id, err,
				len(f.recorded())-before)
		}
	}
	r.gateway = true
	for _, tt := range []struct{ id, arguments string }{
		{rateLimitGet.ID, `{}`}, {metaGet.ID, `{}`}, {licensesList.ID, `{}`}, {licensesGet.ID, `{"license":"mit"}`},
		{gitignoreTemplatesList.ID, `{}`}, {gitignoreTemplatesGet.ID, `{"name":"Go"}`}, {codesOfConductList.ID, `{}`},
		{codesOfConductGet.ID, `{"key":"contributor_covenant"}`}, {emojisList.ID, `{}`},
		{markdownRender.ID, `{"text":"hi"}`},
	} {
		before := len(f.recorded())
		_, err := invoke(t, core, tt.id, "open", tt.arguments, false)
		if classOf(err) != provider.ClassTimeout || len(f.recorded()) != before+1 {
			t.Errorf("%s on a gateway timeout = %v after %d requests, want one timeout", tt.id, err,
				len(f.recorded())-before)
		}
	}
}

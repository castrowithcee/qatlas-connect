package github

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	compareRoute     = "GET dependency-graph/compare/main...feature%2Fup"
	snapshotRoute    = "POST dependency-graph/snapshots"
	snapshotSHA      = "ddc951f4b1293222421f2c8df679786153acf689"
	snapshotCommit   = "GET git/commits/" + snapshotSHA
	snapshotAnswerOK = `{"id":42,"created_at":"2026-11-01T10:00:00Z","result":"SUCCESS","message":"ok"}`
)

var dependencyTools = []string{dependencyReviewCompare.ID, dependencySnapshotsCreate.ID}

// serveDependencies answers the routes below the repository like serveSecurity. dropPost closes the
// connection of the snapshot POST without an answer.
func serveDependencies(t *testing.T, answers map[string]answerOf, dropPost bool) (*fakeGitHub, *application.Core, *int) {
	t.Helper()
	f := &fakeGitHub{}
	f.failure = func(w http.ResponseWriter, r *http.Request) bool {
		below, ok := strings.CutPrefix(r.URL.EscapedPath(), repositoryRoute)
		if !ok {
			return false
		}
		key := r.Method + " " + strings.TrimPrefix(below, "/")
		if dropPost && key == snapshotRoute {
			if connection, _, err := w.(http.Hijacker).Hijack(); err == nil {
				connection.Close()
			}
			return true
		}
		answer, ok := answers[key]
		if !ok {
			answer = answerOf{status: http.StatusNotFound, body: `{"message":"` + providerCanary + `"}`}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(answer.status)
		_, _ = w.Write([]byte(answer.body))
		return true
	}
	base := serve(t, f)
	reads := 0
	red := &redact.Redactor{}
	cfg := coreConfig(base)
	all := []config.Effect{config.EffectRead, config.EffectCreate}
	cfg.Connections["dep"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all, Tools: dependencyTools}
	cfg.Connections["unlisted"] = config.Connection{Service: "gh", Credential: "gh-reader", Target: repoTarget,
		Effects: all}
	cfg.Connections["excluded"] = config.Connection{Service: "gh", Credential: "gh-reader",
		Targets: []string{"repos/octo-org/*"}, Exclude: []string{repoTarget}, Effects: all, Tools: dependencyTools}
	return f, application.New(registry(t), cfg, resolver(red, &reads), red), &reads
}

func changeAnswer(index int) string {
	return fmt.Sprintf(`{"change_type":"added","manifest":"package-lock.json","ecosystem":"npm","name":"pkg-%d",`+
		`"version":"1.0.%d","package_url":"pkg:npm/pkg-%d@1.0.%d","license":"MIT",`+
		`"source_repository_url":"https://example.com/%d","vulnerabilities":[{"severity":"high",`+
		`"advisory_ghsa_id":"GHSA-xxxx-yyyy-zzzz","advisory_summary":"Prototype pollution",`+
		`"advisory_url":"https://github.com/advisories/GHSA-xxxx-yyyy-zzzz"}],"scope":"runtime"}`,
		index, index, index, index, index)
}

func TestDependencyReviewCompareReadsEveryDocumentedField(t *testing.T) {
	f, core, _ := serveDependencies(t, map[string]answerOf{compareRoute: {http.StatusOK,
		`[` + changeAnswer(1) + `,{"change_type":"removed","manifest":"go.mod","ecosystem":"go","name":"old",` +
			`"version":"0.1.0","package_url":null,"license":null,"source_repository_url":null,` +
			`"vulnerabilities":[],"scope":"unknown"}]`}}, false)
	result, err := invoke(t, core, dependencyReviewCompare.ID, "dep",
		`{"base":"main","head":"feature/up","name":"package-lock.json"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"total": float64(2), "changes": []any{
		map[string]any{"change_type": "added", "manifest": "package-lock.json", "ecosystem": "npm",
			"name": "pkg-1", "version": "1.0.1", "package_url": "pkg:npm/pkg-1@1.0.1", "license": "MIT",
			"source_repository_url": "https://example.com/1", "scope": "runtime",
			"vulnerabilities": []any{map[string]any{"severity": "high", "advisory_ghsa_id": "GHSA-xxxx-yyyy-zzzz",
				"advisory_summary": "Prototype pollution",
				"advisory_url":     "https://github.com/advisories/GHSA-xxxx-yyyy-zzzz"}}},
		map[string]any{"change_type": "removed", "manifest": "go.mod", "ecosystem": "go", "name": "old",
			"version": "0.1.0", "scope": "unknown"},
	}}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}
	requests := f.recorded()
	filter := url.Values{"name": {"package-lock.json"}}.Encode()
	if len(requests) != 1 || requests[0].method != http.MethodGet ||
		requests[0].path != repositoryRoute+"/dependency-graph/compare/main...feature/up" ||
		requests[0].query != filter {
		t.Errorf("requests = %+v, want one read with the manifest filter", requests)
	}
}

func TestDependencyReviewCompareCutsALargeAnswerAndItsTexts(t *testing.T) {
	var changes []string
	for i := 0; i < maxDependencyChanges+5; i++ {
		changes = append(changes, changeAnswer(i))
	}
	vulnerabilities := make([]string, maxDependencyVulnerabilities+3)
	for i := range vulnerabilities {
		vulnerabilities[i] = fmt.Sprintf(`{"severity":"low","advisory_ghsa_id":"GHSA-%d","advisory_summary":"s",`+
			`"advisory_url":"https://x/%d"}`, i, i)
	}
	changes[0] = `{"change_type":"added","manifest":"m","ecosystem":"npm","name":"` + strings.Repeat("n", 900) +
		`","version":"1","package_url":null,"license":"MIT\u0007","source_repository_url":null,` +
		`"vulnerabilities":[` + strings.Join(vulnerabilities, ",") + `],"scope":"runtime"}`
	_, core, _ := serveDependencies(t, map[string]answerOf{compareRoute: {http.StatusOK,
		`[` + strings.Join(changes, ",") + `]`}}, false)
	result, err := invoke(t, core, dependencyReviewCompare.ID, "dep", `{"base":"main","head":"feature/up"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	var got DependencyReview
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	first := got.Changes[0]
	if len(got.Changes) != maxDependencyChanges || got.Total != maxDependencyChanges+5 || !got.Truncated ||
		len(first.Name) != maxDependencyText || !first.Truncated || len(first.Vulnerabilities) != maxDependencyVulnerabilities ||
		!first.VulnerabilitiesTruncated || strings.ContainsRune(first.License, 7) {
		t.Errorf("total = %d, kept = %d, truncated = %t, first = %+v", got.Total, len(got.Changes), got.Truncated,
			first.Name[:5])
	}
	if got.Changes[1].Truncated || got.Changes[1].VulnerabilitiesTruncated {
		t.Errorf("an unbounded change is marked as cut: %+v", got.Changes[1])
	}
}

func TestDependencyReviewCompareRefusesInvalidArgumentsBeforeAnyIO(t *testing.T) {
	f, core, reads := serveDependencies(t, nil, false)
	for name, arguments := range map[string]string{
		"dots in a ref":        `{"base":"a..b","head":"main"}`,
		"a space in the head":  `{"base":"main","head":"a b"}`,
		"a missing head":       `{"base":"main"}`,
		"an empty base":        `{"base":"","head":"main"}`,
		"a traversing name":    `{"base":"main","head":"dev","name":"../package.json"}`,
		"a trailing slash":     `{"base":"main","head":"dev","name":"app/"}`,
		"an unknown argument":  `{"base":"main","head":"dev","url":"https://example.com"}`,
		"a fork head":          `{"base":"main","head":"other:dev"}`,
		"a control in a name":  `{"base":"main","head":"dev","name":"a\u0000b"}`,
		"another repository":   `{"repository":"octo-org/other","base":"main","head":"dev"}`,
		"a repository pattern": `{"repository":"octo-org/*","base":"main","head":"dev"}`,
	} {
		if _, err := invoke(t, core, dependencyReviewCompare.ID, "dep", arguments, false); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := invoke(t, core, dependencyReviewCompare.ID, "excluded",
		`{"repository":"octo-org/example","base":"main","head":"dev"}`, false); err == nil {
		t.Error("an excluded repository was accepted")
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(f.recorded()))
	}
}

func TestDependencyReviewCompareFailuresQuoteNoProviderText(t *testing.T) {
	for name, tt := range map[string]struct {
		answer answerOf
		class  provider.Class
		want   string
	}{
		"forbidden":     {answerOf{http.StatusForbidden, `{"message":"` + providerCanary + `"}`}, provider.ClassPermission, "Advanced Security"},
		"not found":     {answerOf{http.StatusNotFound, `{"message":"` + providerCanary + `"}`}, provider.ClassNotFound, "token"},
		"unprocessable": {answerOf{http.StatusUnprocessableEntity, `{"message":"` + providerCanary + `"}`}, provider.ClassProviderError, ""},
		"bad request":   {answerOf{http.StatusBadRequest, `{"message":"` + providerCanary + `"}`}, "", ""},
		"timeout":       {answerOf{http.StatusGatewayTimeout, `{"message":"` + providerCanary + `"}`}, provider.ClassTimeout, ""},
		"unreadable":    {answerOf{http.StatusOK, `[{"name":"` + providerCanary + `" broken`}, provider.ClassInvalidResponse, "invalid response"},
		"not a list":    {answerOf{http.StatusOK, `{"message":"` + providerCanary + `"}`}, provider.ClassInvalidResponse, "invalid response"},
		"no name":       {answerOf{http.StatusOK, `[{"change_type":"added"}]`}, provider.ClassInvalidResponse, "dependency change"},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveDependencies(t, map[string]answerOf{compareRoute: tt.answer}, false)
			_, err := invoke(t, core, dependencyReviewCompare.ID, "dep", `{"base":"main","head":"feature/up"}`, false)
			if err == nil || strings.Contains(err.Error(), providerCanary) ||
				(tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want class %q containing %q and no provider text", err, tt.class, tt.want)
			}
			if n := len(f.recorded()); n != 1 {
				t.Errorf("requests = %d, want one", n)
			}
		})
	}
}

// snapshotArgumentsJSON is a complete snapshot with every member the tool takes.
const snapshotArgumentsJSON = `{"version":0,"sha":"` + snapshotSHA + `","ref":"refs/heads/main",` +
	`"job":{"id":"5622a2b0","correlator":"ci_build","html_url":"https://example.com/build/1"},` +
	`"detector":{"name":"my-scanner","version":"1.0.0","url":"https://example.com/scanner"},` +
	`"scanned":"2026-11-01T10:00:00Z","metadata":{"team":"core","retries":2,"fast":true},` +
	`"manifests":{"package-lock.json":{"name":"package-lock.json","file":{"source_location":"/src/package-lock.json"},` +
	`"metadata":{"lockfile":3},"resolved":{"left-pad":{"package_url":"pkg:npm/left-pad@1.3.0",` +
	`"relationship":"direct","scope":"runtime","dependencies":["pkg:npm/dep@1.0.0"],"metadata":{"source":"npm"}},` +
	`"dep":{"package_url":"pkg:npm/dep@1.0.0","relationship":"indirect","scope":"development"}}}}}`

func commitAnswer() answerOf {
	return answerOf{http.StatusOK, `{"sha":"` + snapshotSHA + `","message":"` + providerCanary + `"}`}
}

func posts(f *fakeGitHub) []recorded {
	var out []recorded
	for _, request := range f.recorded() {
		if request.method == http.MethodPost {
			out = append(out, request)
		}
	}
	return out
}

func TestDependencySnapshotCreateReadsTheCommitThenSendsOnePost(t *testing.T) {
	f, core, _ := serveDependencies(t, map[string]answerOf{snapshotCommit: commitAnswer(),
		snapshotRoute: {http.StatusCreated, snapshotAnswerOK}}, false)
	result, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, true)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"id": float64(42), "created_at": "2026-11-01T10:00:00Z", "result": "SUCCESS", "message": "ok"}
	if got := decodeResult(t, result); !reflect.DeepEqual(got, want) {
		t.Errorf("result = %v, want %v", got, want)
	}
	requests := f.recorded()
	if len(requests) != 2 || requests[0].method != http.MethodGet ||
		requests[0].path != repositoryRoute+"/git/commits/"+snapshotSHA || requests[1].method != http.MethodPost ||
		requests[1].path != repositoryRoute+"/dependency-graph/snapshots" {
		t.Fatalf("requests = %+v, want the commit read and one POST", requests)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(snapshotArgumentsJSON), &sent); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(requests[1].body, sent) {
		t.Errorf("body = %v, want the snapshot as given %v", requests[1].body, sent)
	}
}

func TestDependencySnapshotCreateSendsAMinimalSnapshotWithALowerCaseSha(t *testing.T) {
	f, core, _ := serveDependencies(t, map[string]answerOf{snapshotCommit: commitAnswer(),
		snapshotRoute: {http.StatusCreated, `{"id":7,"created_at":"x","result":"ACCEPTED","message":"` +
			strings.Repeat("m", 700) + `"}`}}, false)
	result, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep",
		`{"version":1,"sha":"`+strings.ToUpper(snapshotSHA)+`","ref":"refs/tags/v1","job":{"id":"1","correlator":"c"},`+
			`"detector":{"name":"d","version":"1","url":"http://example.com"},"scanned":"2026-11-01T10:00:00+01:00"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	var got DependencySnapshotCreated
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatal(err)
	}
	if got.Result != "ACCEPTED" || len(got.Message) != maxDependencyText || !got.MessageTruncated {
		t.Errorf("result = %+v, want ACCEPTED with the message cut", got)
	}
	body := posts(f)[0].body
	if body["sha"] != snapshotSHA {
		t.Errorf("sha = %v, want the lower-case form", body["sha"])
	}
	if _, present := body["manifests"]; present {
		t.Errorf("an absent manifests member was sent: %v", body)
	}
}

func TestDependencySnapshotCreateRefusesASnapshotOfAForeignCommit(t *testing.T) {
	for name, answer := range map[string]answerOf{
		"not found":     {http.StatusNotFound, `{"message":"` + providerCanary + `"}`},
		"unprocessable": {http.StatusUnprocessableEntity, `{"message":"` + providerCanary + `"}`},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveDependencies(t, map[string]answerOf{snapshotCommit: answer,
				snapshotRoute: {http.StatusCreated, snapshotAnswerOK}}, false)
			_, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, true)
			if !isInvalidRequest(err) || !strings.Contains(err.Error(), "not a commit of this repository") ||
				strings.Contains(err.Error(), providerCanary) {
				t.Errorf("err = %v, want an invalid request without provider text", err)
			}
			if len(posts(f)) != 0 || len(f.recorded()) != 1 {
				t.Errorf("requests = %+v, want the commit read alone", f.recorded())
			}
		})
	}
}

func TestDependencySnapshotCreateRefusesAnAnswerForAnotherCommit(t *testing.T) {
	f, core, _ := serveDependencies(t, map[string]answerOf{snapshotCommit: {http.StatusOK,
		`{"sha":"` + strings.Repeat("a", 40) + `"}`}, snapshotRoute: {http.StatusCreated, snapshotAnswerOK}}, false)
	if _, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, true); err == nil {
		t.Error("a commit read that names another sha was accepted")
	}
	if len(posts(f)) != 0 {
		t.Errorf("a POST followed: %+v", f.recorded())
	}
}

// swap replaces one member of the snapshot JSON.
func swap(old, replacement string) string {
	if !strings.Contains(snapshotArgumentsJSON, old) {
		panic("snapshot has no " + old)
	}
	return strings.Replace(snapshotArgumentsJSON, old, replacement, 1)
}

func TestDependencySnapshotCreateRefusesOversizedAndInvalidSnapshotsBeforeAnyIO(t *testing.T) {
	manifests := func(count int) string {
		var parts []string
		for i := 0; i < count; i++ {
			parts = append(parts, fmt.Sprintf(`"m%d":{"name":"m%d"}`, i, i))
		}
		return `"manifests":{` + strings.Join(parts, ",") + `}`
	}
	packages := func(manifestCount, perManifest int) string {
		var parts []string
		for m := 0; m < manifestCount; m++ {
			var resolved []string
			for p := 0; p < perManifest; p++ {
				resolved = append(resolved, fmt.Sprintf(`"p%d":{"package_url":"pkg:npm/p%d"}`, p, p))
			}
			parts = append(parts, fmt.Sprintf(`"m%d":{"name":"m%d","resolved":{%s}}`, m, m, strings.Join(resolved, ",")))
		}
		return `"manifests":{` + strings.Join(parts, ",") + `}`
	}
	dependencies := make([]string, maxSnapshotDependencies+1)
	for i := range dependencies {
		dependencies[i] = fmt.Sprintf(`"pkg:npm/d%d"`, i)
	}
	head := `{"version":0,"sha":"` + snapshotSHA + `","ref":"refs/heads/main","job":{"id":"1","correlator":"c"},` +
		`"detector":{"name":"d","version":"1","url":"https://example.com"},"scanned":"2026-11-01T10:00:00Z",`
	f, core, reads := serveDependencies(t, map[string]answerOf{snapshotCommit: commitAnswer(),
		snapshotRoute: {http.StatusCreated, snapshotAnswerOK}}, false)
	for name, arguments := range map[string]string{
		"too many manifests":    head + manifests(maxSnapshotManifests+1) + `}`,
		"too many packages":     head + packages(2, maxSnapshotPackages/2+1) + `}`,
		"too many dependencies": head + `"manifests":{"m":{"name":"m","resolved":{"p":{"dependencies":[` + strings.Join(dependencies, ",") + `]}}}}}`,
		"too much body": head + `"manifests":{"m":{"name":"m","resolved":{` + func() string {
			var parts []string
			for i := 0; i < 12; i++ {
				parts = append(parts, fmt.Sprintf(`"p%d":{"dependencies":[%s]}`, i, strings.Repeat(`"`+strings.Repeat("x", 990)+`",`, 199)+`"y"`))
			}
			return strings.Join(parts, ",")
		}() + `}}}}`,
		"nine metadata keys": swap(`"metadata":{"team":"core","retries":2,"fast":true}`,
			`"metadata":{"a":1,"b":1,"c":1,"d":1,"e":1,"f":1,"g":1,"h":1,"i":1}`),
		"a nested metadata value": swap(`"retries":2`, `"retries":{"x":1}`),
		"a short sha":             swap(snapshotSHA, snapshotSHA[:39]),
		"a branch name as sha":    swap(snapshotSHA, strings.Repeat("g", 40)),
		"a ref without refs/":     swap(`refs/heads/main`, `main`),
		"a ref with dots":         swap(`refs/heads/main`, `refs/heads/a..b`),
		"a bad scanned time":      swap(`2026-11-01T10:00:00Z`, `yesterday`),
		"an address with login":   swap(`https://example.com/scanner`, `https://user:pw@example.com/scanner`),
		"a file address":          swap(`https://example.com/scanner`, `file:///etc/passwd`),
		"a job address with text": swap(`https://example.com/build/1`, "https://example.com/\\u0007"),
		"a package url":           swap(`pkg:npm/left-pad@1.3.0`, `npm:left-pad`),
		"an unknown relationship": swap(`"direct"`, `"transitive"`),
		"an unknown scope":        swap(`"runtime"`, `"test"`),
		"an unknown member":       swap(`"version":0,`, `"version":0,"extra":1,`),
		"a missing job":           swap(`"job":{"id":"5622a2b0","correlator":"ci_build","html_url":"https://example.com/build/1"},`, ``),
		"a missing correlator":    swap(`"correlator":"ci_build",`, ``),
		"a negative version":      swap(`"version":0`, `"version":-1`),
		"another repository":      swap(`"version":0,`, `"repository":"octo-org/other","version":0,`),
	} {
		if _, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", arguments, true); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := invoke(t, core, dependencySnapshotsCreate.ID, "excluded",
		swap(`"version":0,`, `"repository":"octo-org/example","version":0,`), true); err == nil {
		t.Error("an excluded repository was accepted")
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(f.recorded()))
	}
}

func TestDependencySnapshotCreateNeedsConfirmationAndTheToolsList(t *testing.T) {
	f, core, reads := serveDependencies(t, map[string]answerOf{snapshotCommit: commitAnswer(),
		snapshotRoute: {http.StatusCreated, snapshotAnswerOK}}, false)
	if _, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, false); err == nil {
		t.Error("an unconfirmed snapshot was accepted")
	}
	if _, err := invoke(t, core, dependencySnapshotsCreate.ID, "unlisted", snapshotArgumentsJSON, true); err == nil {
		t.Error("the snapshot ran on a connection without a tools list")
	}
	if *reads != 0 || len(f.recorded()) != 0 {
		t.Errorf("secret reads = %d, requests = %d; want none", *reads, len(f.recorded()))
	}
	descriptor, _, ok := registry(t).Lookup(dependencySnapshotsCreate.ID)
	if !ok || !descriptor.RequiresToolAllowList {
		t.Error("the snapshot tool does not require an allow-list")
	}
	if descriptor, _, ok := registry(t).Lookup(dependencyReviewCompare.ID); !ok || descriptor.RequiresToolAllowList {
		t.Error("the dependency review requires an allow-list")
	}
}

func TestDependencySnapshotCreateFailuresSendOnePostAndQuoteNoProviderText(t *testing.T) {
	canary := `{"message":"` + providerCanary + `"}`
	for name, tt := range map[string]struct {
		answer    answerOf
		drop      bool
		class     provider.Class
		want      string
		uncertain bool
	}{
		"forbidden":     {answer: answerOf{http.StatusForbidden, canary}, class: provider.ClassPermission, want: "Dependency graph: write"},
		"not found":     {answer: answerOf{http.StatusNotFound, canary}, class: provider.ClassNotFound},
		"unprocessable": {answer: answerOf{http.StatusUnprocessableEntity, canary}, want: "malformed"},
		"gateway":       {answer: answerOf{http.StatusGatewayTimeout, canary}, class: provider.ClassTimeout, uncertain: true},
		"server error":  {answer: answerOf{http.StatusBadGateway, canary}, uncertain: true},
		"unreadable":    {answer: answerOf{http.StatusCreated, `{"id":1,"result":"` + providerCanary + `" broken`}, class: provider.ClassInvalidResponse, uncertain: true},
		"unknown result": {answer: answerOf{http.StatusCreated, `{"id":1,"result":"` + providerCanary + `"}`}, class: provider.ClassInvalidResponse,
			uncertain: true},
		"no id":      {answer: answerOf{http.StatusCreated, `{"result":"SUCCESS"}`}, class: provider.ClassInvalidResponse, uncertain: true},
		"wrong code": {answer: answerOf{http.StatusOK, snapshotAnswerOK}, class: provider.ClassInvalidResponse, uncertain: true},
		"dropped":    {drop: true, uncertain: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, core, _ := serveDependencies(t, map[string]answerOf{snapshotCommit: commitAnswer(),
				snapshotRoute: tt.answer}, tt.drop)
			_, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, true)
			if err == nil || strings.Contains(err.Error(), providerCanary) ||
				strings.Contains(err.Error(), uncertain) != tt.uncertain ||
				(tt.class != "" && classOf(err) != tt.class) || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want class %q containing %q, uncertain %v", err, tt.class, tt.want, tt.uncertain)
			}
			if n := len(posts(f)); n != 1 {
				t.Errorf("POSTs = %d, want exactly one", n)
			}
		})
	}
}

func TestDependencySnapshotCreateReportsAnInvalidResultAsAnAnswer(t *testing.T) {
	_, core, _ := serveDependencies(t, map[string]answerOf{snapshotCommit: commitAnswer(),
		snapshotRoute: {http.StatusCreated, `{"id":9,"created_at":"x","result":"INVALID","message":"bad"}`}}, false)
	result, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeResult(t, result)["result"]; got != "INVALID" {
		t.Errorf("result = %v, want INVALID", got)
	}
}

func TestDependencySnapshotCommitReadFailuresSendNoPost(t *testing.T) {
	for name, status := range map[string]int{"forbidden": http.StatusForbidden, "server error": http.StatusBadGateway} {
		f, core, _ := serveDependencies(t, map[string]answerOf{
			snapshotCommit: {status, `{"message":"` + providerCanary + `"}`},
			snapshotRoute:  {http.StatusCreated, snapshotAnswerOK}}, false)
		_, err := invoke(t, core, dependencySnapshotsCreate.ID, "dep", snapshotArgumentsJSON, true)
		if err == nil || strings.Contains(err.Error(), providerCanary) {
			t.Errorf("%s: err = %v, want a failure without provider text", name, err)
		}
		if len(posts(f)) != 0 {
			t.Errorf("%s: a POST followed the failed commit read", name)
		}
	}
}

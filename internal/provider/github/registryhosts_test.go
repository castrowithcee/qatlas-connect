package github

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/application"
	"github.com/castrowithcee/qatlas-kit/redact"
)

func TestRegistryHostTargetValidation(t *testing.T) {
	metadata, _ := registry(t).ProviderMetadata(Provider)
	for name, tc := range map[string]struct {
		target string
		valid  bool
	}{
		"host":               {"registry-hosts/registry.example.com", true},
		"single label":       {"registry-hosts/registry", true},
		"with port":          {"registry-hosts/registry.example.com:8443", true},
		"upper case":         {"registry-hosts/Registry.Example.COM", true},
		"digits and hyphens": {"registry-hosts/r-1.eu-2.example.com", true},
		"path":               {"registry-hosts/registry.example.com/v2", false},
		"scheme":             {"registry-hosts/https://registry.example.com", false},
		"wildcard":           {"registry-hosts/*", false},
		"wildcard subdomain": {"registry-hosts/*.example.com", false},
		"empty":              {"registry-hosts/", false},
		"bare prefix":        {"registry-hosts", false},
		"space":              {"registry-hosts/registry example.com", false},
		"trailing space":     {"registry-hosts/registry.example.com ", true}, // trimmed like every GitHub target
		"leading dot":        {"registry-hosts/.example.com", false},
		"trailing dot":       {"registry-hosts/example.com.", false},
		"double dot":         {"registry-hosts/a..example.com", false},
		"leading hyphen":     {"registry-hosts/-a.example.com", false},
		"userinfo":           {"registry-hosts/user@registry.example.com", false},
		"empty port":         {"registry-hosts/registry.example.com:", false},
		"port zero":          {"registry-hosts/registry.example.com:0", false},
		"port too large":     {"registry-hosts/registry.example.com:65536", false},
		"port leading zero":  {"registry-hosts/registry.example.com:0443", false},
		"port letters":       {"registry-hosts/registry.example.com:https", false},
		"two ports":          {"registry-hosts/registry.example.com:1:2", false},
		"IPv4":               {"registry-hosts/192.0.2.1", false},
		"IPv4 with port":     {"registry-hosts/192.0.2.1:443", false},
		"IPv6":               {"registry-hosts/[2001:db8::1]", false},
		"IPv6 bare":          {"registry-hosts/2001:db8::1", false},
		"non-ASCII":          {"registry-hosts/rëgistry.example.com", false},
		"underscore":         {"registry-hosts/re_gistry.example.com", false},
	} {
		if got := metadata.Target.ValidTarget("registry-host", tc.target) &&
			metadata.Target.Validate(tc.target) == nil; got != tc.valid {
			t.Errorf("%s: %q valid = %t, want %t", name, tc.target, got, tc.valid)
		}
	}
	if _, err := parseAllowlist([]string{"registry-hosts/a.example.com", "registry-hosts/A.EXAMPLE.com"}); err == nil {
		t.Error("a host named twice without regard to case was accepted")
	}
	if _, err := parseAllowlistExcluding([]string{"users/alice"}, []string{"registry-hosts/a.example.com"}); err == nil {
		t.Error("a registry host was accepted as an exclusion")
	}
	if got := metadata.Target.CheckExcludes([]string{"registry-hosts/a.example.com"}, nil); len(got) != 1 {
		t.Errorf("exclusion findings = %+v, want the registry host refused", got)
	}
	if kind := metadata.Target.Kinds[len(metadata.Target.Kinds)-1]; kind.Name != "registry-host" || !kind.Unlisted ||
		len(kind.Parents) != 0 {
		t.Errorf("registry host kind = %+v", kind)
	}
}

func TestCheckRegistryURL(t *testing.T) {
	list, err := parseAllowlist([]string{"registry-hosts/Registry.Example.com", "registry-hosts/npm.example.com:8443"})
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		url string
		ok  bool
	}{
		"approved":            {"https://registry.example.com", true},
		"approved with path":  {"https://registry.example.com/v2/?a=b#c", true},
		"upper case":          {"HTTPS://REGISTRY.EXAMPLE.COM/x", true},
		"approved with port":  {"https://npm.example.com:8443/", true},
		"only a subdomain":    {"https://sub.registry.example.com", false},
		"parent domain":       {"https://example.com", false},
		"suffix lookalike":    {"https://registry.example.com.evil.test", false},
		"prefix lookalike":    {"https://evilregistry.example.com", false},
		"other port":          {"https://registry.example.com:8443", false},
		"default port given":  {"https://registry.example.com:443", false},
		"port not approved":   {"https://npm.example.com", false},
		"wrong port":          {"https://npm.example.com:9443", false},
		"http":                {"http://registry.example.com", false},
		"other scheme":        {"ftp://registry.example.com", false},
		"no scheme":           {"registry.example.com", false},
		"scheme relative":     {"//registry.example.com", false},
		"opaque":              {"https:registry.example.com", false},
		"no host":             {"https:///v2", false},
		"userinfo":            {"https://user@registry.example.com", false},
		"userinfo lookalike":  {"https://registry.example.com@evil.test", false},
		"backslash":           {`https://evil.test\@registry.example.com`, false},
		"empty port":          {"https://registry.example.com:", false},
		"space":               {"https://registry.example.com /x", false},
		"leading space":       {" https://registry.example.com", false},
		"newline":             {"https://registry.example.com\n", false},
		"trailing dot":        {"https://registry.example.com.", false},
		"IPv4":                {"https://192.0.2.1", false},
		"IPv6":                {"https://[2001:db8::1]/", false},
		"percent encoded dot": {"https://registry%2eexample.com", false},
		"empty":               {"", false},
	} {
		err := checkRegistryURL(list, tc.url)
		if (err == nil) != tc.ok {
			t.Errorf("%s: %q error = %v, want ok %t", name, tc.url, err, tc.ok)
		}
		if err != nil && (!isInvalidRequest(err) || strings.Contains(err.Error(), "example")) {
			t.Errorf("%s: the refusal is no invalid request or quotes the value: %v", name, err)
		}
	}
	// Without a registry host target nothing is approved, however open the list is.
	for _, targets := range [][]string{nil, {"orgs/octo", "repos/octo/app", "users/alice/projects/1"}} {
		empty, err := parseAllowlist(targets)
		if err != nil {
			t.Fatal(err)
		}
		if checkRegistryURL(empty, "https://registry.example.com") == nil {
			t.Errorf("targets %v approved a registry host", targets)
		}
	}
}

// A registry host reaches no GitHub object: no target kind of an existing tool takes it, and a list that holds
// it allows and lists nothing more than the same list without it.
func TestRegistryHostReachesNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		targets []string
		want    bool
	}{
		"alone":           {[]string{"registry-hosts/registry.example.com"}, false},
		"beside an owner": {[]string{"registry-hosts/registry.example.com", "orgs/octo"}, true},
	} {
		list, err := parseAllowlist(tc.targets)
		if err != nil {
			t.Fatal(err)
		}
		for _, probe := range []target{
			{kind: kindRepository, owner: "registry.example.com", repo: "x"},
			{kind: kindRepository, owner: "registry-hosts", repo: "registry.example.com"},
			{kind: kindOwner, scope: "orgs", owner: "registry.example.com"},
			{kind: kindOwner, scope: "users", owner: "registry-hosts"},
			{kind: kindProject, scope: "orgs", owner: "registry.example.com", number: 1},
		} {
			if list.allows(probe) {
				t.Errorf("%s: the list allows %+v", name, probe)
			}
		}
		if _, ok := list.only(kindRepository); ok {
			t.Errorf("%s: a registry host became the only repository", name)
		}
		if _, ok := list.only(kindProject); ok {
			t.Errorf("%s: a registry host became the only project", name)
		}
		if owner, ok := list.only(kindOwner); ok != tc.want || (ok && owner.owner != "octo") {
			t.Errorf("%s: only owner = %+v, %t", name, owner, ok)
		}
		if list.names(kindRepository) || list.names(kindProject) || list.names(kindOwner) != tc.want {
			t.Errorf("%s: the list names a kind through a registry host", name)
		}
		if list.ownerNames("registry.example.com") || list.ownerNames("registry-hosts") {
			t.Errorf("%s: a registry host names an owner", name)
		}
		if _, err := list.choose(kindRepository, "registry-hosts/registry.example.com"); err == nil {
			t.Errorf("%s: choose accepted a registry host as a repository", name)
		}
		if _, err := list.choose(kindProject, "registry-hosts/registry.example.com"); err == nil {
			t.Errorf("%s: choose accepted a registry host as a project", name)
		}
		if _, err := list.chooseOwner(kindOwner, "registry-hosts/registry.example.com"); err == nil {
			t.Errorf("%s: chooseOwner accepted a registry host as an owner", name)
		}
		if _, err := list.chooseOwner(kindRepository, "registry-hosts/registry.example.com"); err == nil {
			t.Errorf("%s: chooseOwner listed a registry host", name)
		}
	}
	alone, _ := parseAllowlist([]string{"registry-hosts/registry.example.com"})
	if _, err := searchScope(alone); err == nil {
		t.Error("a list of registry hosts alone scoped a search")
	}
	if err := accountWideAllowed(alone); err == nil {
		t.Error("a list of registry hosts alone allowed an account-wide read")
	}
	if alone.namesUser("") || alone.restrictsRepositories("octo") {
		t.Error("a registry host counted as a user or restricted repositories")
	}
	mixed, _ := parseAllowlist([]string{"registry-hosts/registry.example.com", "orgs/octo"})
	scope, err := searchScope(mixed)
	if err != nil || scope != "org:octo" {
		t.Errorf("search scope beside a registry host = %q, %v; want org:octo", scope, err)
	}
	if err := accountWideAllowed(mixed); err != nil {
		t.Errorf("an owner beside a registry host lost the account-wide read: %v", err)
	}
}

// A connection whose only target is a registry host serves no existing tool, neither one with a repository,
// a project, or an owner argument nor an invitation, and a refusal comes before any secret read or request.
func TestRegistryHostAloneServesNoExistingTool(t *testing.T) {
	f := &fakeGitHub{}
	base := serve(t, f)
	cfg := targetsConfig(base)
	cfg.Connections["hosts"] = cfg.Connections["open"]
	conn := cfg.Connections["hosts"]
	conn.Targets = []string{"registry-hosts/registry.example.com"}
	cfg.Connections["hosts"] = conn
	reads := 0
	red := &redact.Redactor{}
	core := application.New(registry(t), cfg, resolver(red, &reads), red)
	for name, call := range map[string][2]string{
		"issue":         {"github.issues.get", `{"number":1,"repository":"octo-org/example"}`},
		"issue by host": {"github.issues.get", `{"number":1,"repository":"registry-hosts/registry.example.com"}`},
		"projects":      {"github.projects.list", `{"owner":"orgs/octo-org"}`},
		"repositories":  {"github.repositories.list", `{"owner":"orgs/octo-org"}`},
		"owner host":    {"github.repositories.list", `{"owner":"registry-hosts/registry.example.com"}`},
		"search":        {"github.repositories.search", `{"query":"x"}`},
		"account":       {"github.accounts.me", `{}`},
	} {
		before := len(f.recorded())
		reads = 0
		if _, err := invoke(t, core, call[0], "hosts", call[1], false); !isInvalidRequest(err) {
			t.Errorf("%s: a connection with only a registry host target was not refused as an invalid request", name)
		}
		if reads != 0 || len(f.recorded()) != before {
			t.Errorf("%s: the refusal reached the credential or GitHub", name)
		}
	}
}

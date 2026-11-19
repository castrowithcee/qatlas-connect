package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/config"
)

// guardedMetadata has only a tool that no connection offers unless its tools list names it, so it declares no
// profile.
var guardedMetadata = config.ProviderMetadata{
	ID: "guarded", Name: "Guarded", DefaultEffects: []config.Effect{config.EffectRead},
	SupportedEffects: []config.Effect{config.EffectRead, config.EffectCreate},
	SecretRoles:      []config.SecretRole{{Name: "token", Description: "the token"}},
	Tools: []config.ToolMetadata{
		{ID: "guarded.items.send", Title: "Send", Effect: config.EffectCreate, RequiresToolAllowList: true},
	},
}

// The build page of a provider without a profile starts on the default access with no tool ticked.
func TestGuidedConnectionOfAListedOnlyProviderStartsWithoutTools(t *testing.T) {
	s, _, _, _ := newConnectionTestServer(t, nil)
	cookie, _ := coupleAndApprove(t, s, nil)

	build := s.request(t, http.MethodGet, "/connections/new?provider=guarded", s.addr, cookie, nil)
	if build.Code != http.StatusOK {
		t.Fatalf("build page status = %d, want 200, body: %s", build.Code, build.Body.String())
	}
	if body := build.Body.String(); !strings.Contains(body, `value="guarded.items.send"`) ||
		strings.Contains(body, `value="guarded.items.send" checked`) {
		t.Errorf("the build page must list the tool unticked:\n%s", body)
	}

	cfg := config.New(bookCatalog{})
	f := connectionForm{Provider: "guarded"}
	applyConnectionDefaults(cfg, &f)
	if f.Access != accessEffects || f.PermMode != "default" || len(f.Tools) != 0 || len(f.Perms) != 0 {
		t.Errorf("defaults = %+v, want the default effects access without tools", f)
	}
}

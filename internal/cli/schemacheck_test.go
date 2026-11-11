package cli

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/castrowithcee/qatlas-kit/jsonschema"
)

// Every input and output schema of every registered tool is one the kit checks completely.
func TestEveryRegisteredSchemaPassesTheKitCheck(t *testing.T) {
	registry := defaultRegistry()
	count := 0
	for _, provider := range registry.ProviderMetadataAll() {
		for _, d := range registry.Provider(provider.ID) {
			count++
			if err := jsonschema.CheckSchema(d.InputSchema); err != nil {
				t.Errorf("%s input schema: %v", d.ID, err)
			}
			if err := jsonschema.CheckSchema(d.OutputSchema); err != nil {
				t.Errorf("%s output schema: %v", d.ID, err)
			}
		}
	}
	if count == 0 {
		t.Fatal("the registry holds no tool")
	}
}

const matomoSchemaConfig = `version: 4
services:
  stats:
    provider: matomo
    base_url: https://stats.example.invalid
credentials:
  token:
    type: env
    values:
      token: MATOMO_SCHEMA_TOKEN
connections:
  stats:
    service: stats
    credential: token
    tools: [matomo.tracking.record-page-view]
    targets: [site/1]
defaults: {}
`

// A member name that breaks propertyNames is refused as an invalid request before any secret is resolved.
func TestMatomoPropertyNamesRefusalHappensBeforeProviderIO(t *testing.T) {
	path := writeConfig(t, matomoSchemaConfig)
	t.Setenv("QCONNECT_HOME", "")
	var reads atomic.Int32
	code, stdout, stderr := runTwentyCLI(t, &reads, `{"idSite":1,"url":"https://example.org/","dimensions":{"abc":"x"}}`,
		"invoke", "matomo.tracking.record-page-view", "--connection", "stats", "--config", path)
	if code != exitUsage || stdout != "" || !strings.Contains(stderr, "qconnect: invalid-request:") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if reads.Load() != 0 {
		t.Errorf("secret lookups = %d, want none", reads.Load())
	}
}

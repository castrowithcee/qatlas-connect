package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// unofferedKind is a kind no provider declares; every target browser refuses it.
const unofferedKind = "conformance-unoffered-kind"

// TestTargetBrowserListsEveryListedKind checks every shipped target browser against its metadata: a kind a
// surface offers a list of must not be answered the way the browser answers a kind it does not offer. The
// reads run under a cancelled context, so no request leaves the process; what counts is only that the
// browser takes the kind past its own check of the kind.
func TestTargetBrowserListsEveryListedKind(t *testing.T) {
	reg := sharedRegistry()
	browsers := 0
	for _, metadata := range reg.ProviderMetadataAll() {
		if !reg.HasTargetBrowser(metadata.ID) {
			continue
		}
		browsers++
		t.Run(metadata.ID, func(t *testing.T) {
			reportViolations(t, targetBrowserViolations(reg, metadata))
		})
	}
	if browsers == 0 {
		t.Fatal("the shipped registry has no target browser")
	}
}

// The check names a listed kind the browser refuses like an undeclared one, and only that kind: an unlisted
// kind may be refused, and a listed kind may fail for any other reason.
func TestTargetBrowserChecksDetectViolations(t *testing.T) {
	metadata := config.ProviderMetadata{ID: "sample", Name: "Sample", Target: config.TargetMetadata{
		Label: "item", Kinds: []config.TargetKind{
			{Name: "shelf", Forms: []string{"shelf/ID"}},
			{Name: "book", Forms: []string{"book/ID"}, Parents: []string{"shelf"}},
			{Name: "loose", Forms: []string{"loose/ID"}},
			{Name: "typed", Forms: []string{"typed/ID"}, Unlisted: true},
		}}}
	reg := capability.NewRegistry()
	if err := reg.RegisterProvider(metadata, nil); err != nil {
		t.Fatal(err)
	}
	refused := errors.New("the kind is not one this provider offers")
	list := func(_ context.Context, _ *config.Resolved, _ *secret.Resolver, _ *redact.Redactor,
		req config.TargetListRequest) (config.TargetPage, error) {
		switch {
		case req.Kind == "shelf" && req.Parent == "":
			return config.TargetPage{}, nil
		case req.Kind == "book" && req.Parent == "shelf/1":
			return config.TargetPage{}, errors.New("the request ended")
		}
		return config.TargetPage{}, refused
	}
	if err := reg.RegisterTargetBrowser(metadata.ID, capability.TargetBrowser{List: list,
		ResolveURL: func(context.Context, *config.Resolved, *secret.Resolver, *redact.Redactor, string, string) ([]config.TargetEntry, error) {
			return nil, nil
		}}); err != nil {
		t.Fatal(err)
	}
	violations := targetBrowserViolations(reg, metadata)
	if len(violations) != 1 || !strings.Contains(violations[0], `kind "loose"`) {
		t.Errorf("violations = %q, want one for kind loose", violations)
	}
}

// targetBrowserViolations lists the listed kinds of a provider that its browser refuses like a kind it does
// not offer, each read under the same parent as the refusal it is compared with.
func targetBrowserViolations(reg *capability.Registry, metadata config.ProviderMetadata) []string {
	var violations []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolved := browseConnection(metadata)
	secrets := secret.NewWith(func(string) string { return "conformance-value" }, nil, nil, &redact.Redactor{})
	read := func(kind, parent string) error {
		_, err := reg.ListTargets(ctx, resolved, secrets, &redact.Redactor{},
			config.TargetListRequest{Kind: kind, Parent: parent})
		return err
	}
	for _, kind := range metadata.Target.Kinds {
		if kind.Unlisted {
			continue
		}
		for _, parent := range browseParents(metadata.Target, kind) {
			refusal := read(unofferedKind, parent)
			if refusal == nil {
				violations = append(violations, fmt.Sprintf("provider %q lists the undeclared kind %q under %q",
					metadata.ID, unofferedKind, parent))
				continue
			}
			if err := read(kind.Name, parent); err != nil && err.Error() == refusal.Error() {
				violations = append(violations, fmt.Sprintf(
					"provider %q kind %q under %q is answered like a kind it does not offer (%v); mark it Unlisted or list it",
					metadata.ID, kind.Name, parent, err))
			}
		}
	}
	return violations
}

// browseConnection is a connection of the provider with every secret role set and its default address.
func browseConnection(metadata config.ProviderMetadata) *config.Resolved {
	values := map[string]string{}
	for _, role := range metadata.SecretRoles {
		values[role.Name] = "CONFORMANCE_" + strings.ToUpper(strings.ReplaceAll(role.Name, "-", "_"))
	}
	resolved := &config.Resolved{Name: "conformance", Provider: metadata.ID, Service: "conformance",
		Credential: "conformance", BaseURL: metadata.DefaultBaseURL,
		Secrets: config.Credential{Type: config.CredentialTypeEnv, Values: values}}
	if metadata.Target.Wildcard != "" {
		resolved.Targets = []string{metadata.Target.Wildcard}
	}
	return resolved
}

// browseParents are the parents a surface reads the kind under: none at the top level, and one entry of
// each parent kind, spelled in its first form with every placeholder filled.
func browseParents(target config.TargetMetadata, kind config.TargetKind) []string {
	var parents []string
	if len(kind.Parents) == 0 || kind.Top {
		parents = append(parents, "")
	}
	names := kind.Parents
	if kind.Nested {
		names = append(append([]string{}, names...), kind.Name)
	}
	for _, name := range names {
		parent, ok := target.KindByName(name)
		if !ok || len(parent.Forms) == 0 {
			continue
		}
		segments := strings.Split(parent.Forms[0], "/")
		for i, segment := range segments {
			if config.Placeholder(segment) {
				segments[i] = "1"
			}
		}
		parents = append(parents, strings.Join(segments, "/"))
	}
	return parents
}

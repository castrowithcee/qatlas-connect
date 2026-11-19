// Package googleroute implements access to the Google Route Optimization API with one credential that issues
// access tokens: a person's consent, a service account key, or the impersonation of a service account, all
// through the token core of internal/provider. A connection takes no secret of its own and no scope: the scopes
// follow from the tools it offers (see scopes.go).
//
// The package optimizes tours, which Google bills, synchronously or by starting a long-running operation that it
// never waits for, and reads the state of that operation. Every
// request goes to the one fixed origin of the API, with the access token in the Authorization header only,
// within a bounded size and time, and spaced per credential. An optimization is sent exactly once and never
// repeated. A connection may be bound to projects; the project of a request is checked against them before
// any secret is read. Everything Google returns is untrusted third-party content: strings and lists are
// bounded.
package googleroute

import (
	"context"
	"errors"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/googleapi"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.route-optimization"

// subject names Google in the messages of this provider.
const subject = "Google Route Optimization"

// probeLocation and probeOperation name the operation the connection test reads for each project target. The
// operation does not exist, so the read never touches a real one and costs nothing.
const (
	probeLocation  = "global"
	probeOperation = "qatlas-connection-test"
)

// readTools are the tools of the profile read.
var readTools = []string{operationsGet.ID}

// TestConnection obtains an access token and, for every project target, reads an operation that does not
// exist. A 404 NOT_FOUND passes: the API is enabled in the project and the credential may read operations
// there. Google does not document whether it checks the right before the existence of the operation, so
// whether a 404 proves the right is not verified. A connection without a project target proves the token only,
// because an operation name always holds a project. The test never sends an optimization.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	const op = "test connection"
	if err := checkConnection(resolved); err != nil {
		return testClass(err)
	}
	list, err := allowlistOf(resolved)
	if err != nil {
		return testClass(failure(op, "the targets of the connection are not usable"))
	}
	if secrets == nil {
		return testClass(failure(op, "no credential resolver was configured"))
	}
	probe := func(ctx context.Context, source *provider.TokenSource) (provider.Class, error) {
		c := withSource(resolved, source)
		for _, project := range list {
			name := operationName(project, probeLocation, probeOperation)
			if _, err := c.operation(ctx, op, name); err != nil {
				var failed *provider.Error
				if errors.As(err, &failed) && failed.Class == provider.ClassNotFound {
					continue
				}
				return "", err
			}
		}
		return provider.ClassOK, nil
	}
	return provider.TestTokenConnection(ctx, resolved, secrets, red, tokenOptions(), probe)
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// Register adds the provider metadata, its connection test, its consent endpoints, and its tools.
func Register(reg *capability.Registry) error {
	token := tokenMetadata
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Route Optimization", DefaultBaseURL: apiRoot,
		Description:    "Google Route Optimization API: optimizes vehicle tours, also as long-running operations, which Google bills, and reads the state of operations",
		DefaultEffects: []config.Effect{config.EffectRead},
		ValidateBaseURL: func(raw string) error {
			if !isAPIRoot(raw) {
				return errors.New("a Google Route Optimization service must use the official API origin " + apiRoot)
			}
			return nil
		},
		SecretRoles: config.TokenSecretRoles(),
		Token:       &token,
		Target:      targetMetadata,
		Profiles: []config.ToolProfile{{
			ID: "read", Title: "Read", Recommended: true,
			Description: "reads the state of a Route Optimization operation by its name; the only scope Google lists " +
				"for this method is the broad cloud-platform scope, so the token may change what its account may, " +
				"although no tool of this profile changes anything",
			Tools: readTools,
		}, {
			ID: "optimize", Title: "Optimize",
			Description: "optimizes tours, synchronously or as a long-running operation, which Google bills per " +
				"shipment, and reads operations; each optimization needs a person's confirmation, and the token " +
				"holds the broad cloud-platform scope",
			Tools: []string{toursOptimizeID, toursLongRunningID, operationsGetID},
		}},
	}, TestConnection); err != nil {
		return err
	}
	oauth := googleapi.OAuth
	oauth.Transport = transport
	if err := reg.RegisterOAuth(Provider, oauth); err != nil {
		return err
	}
	return reg.Register(Provider,
		capability.Operation{Descriptor: operationsGet, Handler: handleOperationsGet},
		capability.Operation{Descriptor: toursOptimize, Handler: handleToursOptimize,
			ConfirmationSummary: toursSummary},
		capability.Operation{Descriptor: toursLongRunning, Handler: handleToursLongRunning,
			ConfirmationSummary: toursLongRunningSummary})
}

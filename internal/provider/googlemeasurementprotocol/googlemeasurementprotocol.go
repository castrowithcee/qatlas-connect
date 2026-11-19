// Package googlemeasurementprotocol sends events to a Google Analytics 4 web data stream with the Measurement
// Protocol. A connection binds one data stream: its stream secret is the only secret role, and the measurement
// ID of the stream is an option of the service, which is not secret.
//
// The one tool is listed-only: a connection offers it only when its tools list names it, because sending
// changes the measurement data of the property and cannot be undone. The provider therefore declares no tool
// profile. The host is fixed, nothing of the URL is a caller's choice, and the URL, which carries the secret
// as a query parameter, never reaches a message, a log, or a result.
//
// Google answers every request with a 2xx status, also for events it drops, and the protocol offers no way to
// learn that. A result therefore says that events were sent and never that they were accepted. Because the
// server reports nothing, the limits and name rules of the protocol are checked before anything is sent.
package googlemeasurementprotocol

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// Provider is the provider name used in the configuration and as the operation namespace.
const Provider = "google.measurement-protocol"

// roleSecret is the single secret role: the Measurement Protocol API secret of one data stream.
const roleSecret = "api_secret"

// optionMeasurementID names the service option that holds the measurement ID of the data stream.
const optionMeasurementID = "measurement_id"

// measurementIDPattern is the form of a web stream's measurement ID: G- and upper-case letters and digits.
var measurementIDPattern = regexp.MustCompile(`^G-[A-Z0-9]{1,32}$`)

// measurementID returns the checked measurement ID of the connection. The message never quotes the value.
func measurementID(resolved *config.Resolved) (string, error) {
	id := strings.TrimSpace(resolved.Options[optionMeasurementID])
	if !measurementIDPattern.MatchString(id) {
		return "", failure("open", "the service option measurement_id must be the measurement ID of a web data "+
			"stream, G- followed by upper-case letters and digits")
	}
	return id, nil
}

// checkConnection refuses what no request may start from: no connection, a base URL other than the official
// origin, any target, and a missing or malformed measurement ID. It reads no secret.
func checkConnection(resolved *config.Resolved) (string, error) {
	switch {
	case resolved == nil:
		return "", failure("open", "no connection was selected")
	case !isAPIRoot(resolved.BaseURL):
		return "", failure("open", "a Google Measurement Protocol service must use the official origin "+apiRoot)
	case len(provider.TargetsOf(resolved)) > 0:
		return "", failure("open", "a Google Measurement Protocol connection takes no target")
	}
	return measurementID(resolved)
}

// TestConnection checks only locally: the form of the measurement ID and that the secret exists. It sends
// nothing, because /mp/collect always answers 2xx and would record real data, so a pass proves neither the
// secret nor the data stream.
func TestConnection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor) (provider.Class, error) {
	if _, err := checkConnection(resolved); err != nil {
		return testClass(err)
	}
	if secrets == nil {
		return provider.ClassProviderError, nil
	}
	value, err := secrets.Resolve(ctx, resolved.Credential, resolved.Secrets, roleSecret)
	if err != nil {
		return testClass(err)
	}
	if red != nil {
		red.Add(value.Secret)
	}
	if !validSecret(value.Secret) {
		return provider.ClassAuth, nil
	}
	return provider.ClassOK, nil
}

// testClass turns a provider failure into the class of a connection test; any other error is returned.
func testClass(err error) (provider.Class, error) {
	var failed *provider.Error
	if errors.As(err, &failed) {
		return failed.Class, nil
	}
	return "", err
}

// noTarget refuses every configured target: the reach of a connection is its one data stream.
func noTarget(string) error {
	return errors.New("a Google Measurement Protocol connection takes no target; leave the target empty")
}

// Register adds the provider metadata, its connection test, and its tool. There is no profile: every tool of
// the provider is listed-only, so a new connection starts without a preselection.
func Register(reg *capability.Registry) error {
	if err := reg.RegisterProvider(config.ProviderMetadata{
		ID: Provider, Name: "Google Measurement Protocol", DefaultBaseURL: apiRoot,
		Description:    "Sends events to a Google Analytics 4 web data stream with its stream secret; the tool is listed-only and Google never reports whether events were accepted",
		DefaultEffects: []config.Effect{config.EffectCreate},
		ValidateBaseURL: func(raw string) error {
			if !isAPIRoot(raw) {
				return errors.New("a Google Measurement Protocol service must use the official origin " + apiRoot)
			}
			return nil
		},
		SecretRoles: []config.SecretRole{{
			Name: roleSecret,
			Description: "Measurement Protocol API secret of one Google Analytics 4 web data stream; the measurement " +
				"ID of that stream goes in the service option " + optionMeasurementID,
		}},
		Target: config.TargetMetadata{
			Label:       "target",
			Description: "none: the connection reaches the one data stream of its measurement ID, so leave the target empty",
			Validate:    noTarget,
		},
	}, TestConnection); err != nil {
		return err
	}
	return reg.Register(Provider, capability.Operation{Descriptor: eventsSend, Handler: handleEventsSend})
}

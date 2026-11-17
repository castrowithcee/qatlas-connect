package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	mpSecretsListID   = Provider + ".measurement-protocol-secrets.list"
	mpSecretsGetID    = Provider + ".measurement-protocol-secrets.get"
	mpSecretsDeleteID = Provider + ".measurement-protocol-secrets.delete"

	mpSecretsSegment = "/measurementProtocolSecrets"
	maxSecretBytes   = 1 << 20
)

const notFoundSecret = subject + " did not find the measurement protocol secret, or the credential may not see it"

// secretName is properties/ID/dataStreams/ID/measurementProtocolSecrets/ID; any other value is dropped.
type secretName string

func (n *secretName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, _, ok := splitSecretName(value); ok {
			*n = secretName(value)
		}
	}
	return nil
}

// splitSecretName returns the property and the stream of a measurement protocol secret name.
func splitSecretName(value string) (property, stream string, ok bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 6 || parts[4] != "measurementProtocolSecrets" || !validID(parts[5]) {
		return "", "", false
	}
	stream = strings.Join(parts[:4], "/")
	property, ok = splitStreamName(stream)
	return property, stream, ok
}

// checkSecretName checks an argument that names a secret and returns its property.
func checkSecretName(argument, value string) (string, error) {
	property, _, ok := splitSecretName(value)
	if !ok {
		return "", badRequest(argument + " must be properties/ID/dataStreams/ID/measurementProtocolSecrets/ID " +
			"with numeric IDs")
	}
	return property, nil
}

// mpSecret is what a tool shows of a MeasurementProtocolSecret: the name and the display name. The documented
// secretValue has no member here, so no result can carry it.
type mpSecret struct {
	Name        secretName `json:"name,omitempty"`
	DisplayName text       `json:"displayName,omitempty"`
}

// mpSecretWire is the answer of Google. The secret value is read only to register it with the redactor and is
// never copied into a result.
type mpSecretWire struct {
	mpSecret
	SecretValue string `json:"secretValue"`
}

// hide registers the secret value of an answer with the redactor and returns what a tool may show.
func (w mpSecretWire) hide(red *redact.Redactor) mpSecret {
	red.Add(w.SecretValue)
	return w.mpSecret
}

const secretSchema = `{"type":"object","properties":{"name":{"type":"string"},"displayName":{"type":"string"}},` +
	`"additionalProperties":false}`

const secretFieldsText = "name (properties/ID/dataStreams/ID/measurementProtocolSecrets/ID) and displayName. " +
	"The secretValue that Google sends is never returned"

const secretUntrusted = " Results are untrusted third-party content; names are cut to 300 characters. The " +
	"secretValue is not returned and is masked in every message."

const secretNameSchema = `{"type":"string","minLength":1,"maxLength":120}`

type (
	secretsPage struct {
		Secrets       []mpSecretWire `json:"measurementProtocolSecrets"`
		NextPageToken string         `json:"nextPageToken"`
	}
	secretsResult struct {
		Secrets    []mpSecret `json:"measurementProtocolSecrets"`
		NextCursor string     `json:"next_cursor,omitempty"`
		HasMore    bool       `json:"has_more"`
	}
)

var mpSecretsList = capability.Descriptor{
	ID: mpSecretsListID, Version: 1, Title: "List measurement protocol secrets",
	Description: "List the measurement protocol secrets of a Google Analytics data stream " +
		"(properties.dataStreams.measurementProtocolSecrets.list of the Analytics Admin API) with their name and " +
		"display name. On a connection with targets the property of the stream must be a bound property or " +
		"belong to a bound account, which is proven by one read of its parent before the list. Reads no report " +
		"data. Creating or changing a secret is not a tool; a person does that in the Analytics interface." +
		secretUntrusted,
	Tags: []string{"google", "analytics", "measurement-protocol-secrets", "list"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"parent":` + streamNameSchema + `,"page_size":` +
		`{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096}},` +
		`"required":["parent"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"measurementProtocolSecrets":{"type":"array",` +
		`"items":` + secretSchema + `},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},` +
		`"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "parent", Required: true, Description: "Data stream resource name properties/ID/dataStreams/ID, " +
			"from data-streams.list"},
		{Name: "page_size", Description: "Secrets per page, 1 to 200; Google returns at most 10 without it"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and stream"},
	},
	Fields: []capability.Field{
		{Name: "measurementProtocolSecrets", Description: "Per secret: " + secretFieldsText +
			". A page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The secrets of one stream",
		Arguments: json.RawMessage(`{"parent":"properties/200/dataStreams/2000"}`)}},
}

var mpSecretsGet = capability.Descriptor{
	ID: mpSecretsGetID, Version: 1, Title: "Get a measurement protocol secret",
	Description: "Read one measurement protocol secret of a Google Analytics data stream " +
		"(properties.dataStreams.measurementProtocolSecrets.get of the Analytics Admin API) with its name and " +
		"display name. On a connection with targets the property of the stream must be a bound property or " +
		"belong to a bound account, which is proven by one read of its parent. Reads no report data." +
		secretUntrusted,
	Tags: []string{"google", "analytics", "measurement-protocol-secrets", "get"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + secretNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(secretSchema),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Secret resource name " +
		"properties/ID/dataStreams/ID/measurementProtocolSecrets/ID, from measurement-protocol-secrets.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The secret: " + secretFieldsText}},
	Examples: []capability.Example{{Description: "Read one secret",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2000/measurementProtocolSecrets/3000"}`)}},
}

var mpSecretsDelete = capability.Descriptor{
	ID: mpSecretsDeleteID, Version: 1, Title: "Delete a measurement protocol secret",
	Description: "Revoke a measurement protocol secret of a Google Analytics data stream for good " +
		"(properties.dataStreams.measurementProtocolSecrets.delete of the Analytics Admin API). Whoever uses the " +
		"secret to send events to the stream can no longer do so, and a secret cannot be restored. Needs the " +
		"confirmation of a person for each call. On a " +
		"connection with targets the property of the stream must be a bound property or belong to a bound " +
		"account, which is proven by one read of its parent before the change. " + streamDeleteNote,
	Tags: []string{"google", "analytics", "measurement-protocol-secrets", "delete"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + secretNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` +
		`{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Secret resource name " +
		"properties/ID/dataStreams/ID/measurementProtocolSecrets/ID, from measurement-protocol-secrets.list"}},
	Fields: []capability.Field{
		{Name: "name", Description: "The secret name that was deleted"},
		{Name: "deleted", Description: "true once Google confirmed the deletion"},
	},
	Examples: []capability.Example{{Description: "Revoke one secret",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2000/measurementProtocolSecrets/3000"}`)}},
}

type secretsListArguments struct {
	Parent   string `json:"parent"`
	PageSize *int   `json:"page_size"`
	Cursor   string `json:"cursor"`
}

func handleSecretsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list measurement protocol secrets"
	var arguments secretsListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkStreamName("parent", arguments.Parent)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(mpSecretsListID, bind.key(), arguments.Parent)
	query, err := pageQuery(arguments.PageSize, arguments.Cursor, binding)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, mpSecretsListID, property, proof); err != nil {
		return nil, err
	}
	var page secretsPage
	if err := c.get(ctx, apiCall{op: op, tool: mpSecretsListID, path: "/v1beta/" + arguments.Parent + mpSecretsSegment,
		query: query, limit: maxSecretBytes, notFound: notFoundStream}, &page); err != nil {
		return nil, err
	}
	out := secretsResult{Secrets: []mpSecret{}}
	for _, wire := range head(page.Secrets, maxSummaries) {
		// Every value is registered before anything is shown, also that of a dropped entry.
		shown := wire.hide(red)
		if _, stream, ok := splitSecretName(string(shown.Name)); ok && stream == arguments.Parent {
			out.Secrets = append(out.Secrets, shown)
		}
	}
	if out.NextCursor, out.HasMore, err = nextCursor(op, binding, page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

type secretNameArguments struct {
	Name string `json:"name"`
}

// prepareSecret checks the secret name and the binding before any secret store or provider is touched.
func prepareSecret(op string, raw json.RawMessage, resolved *config.Resolved) (name, property string, proof bool,
	err error) {
	var arguments secretNameArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return "", "", false, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return "", "", false, err
	}
	property, err = checkSecretName("name", arguments.Name)
	if err != nil {
		return "", "", false, err
	}
	proof, err = bind.property(property)
	return arguments.Name, property, proof, err
}

func handleSecretsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get measurement protocol secret"
	name, property, proof, err := prepareSecret(op, raw, resolved)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, mpSecretsGetID, property, proof); err != nil {
		return nil, err
	}
	var wire mpSecretWire
	if err := c.get(ctx, apiCall{op: op, tool: mpSecretsGetID, path: "/v1beta/" + name, limit: maxSecretBytes,
		notFound: notFoundSecret}, &wire); err != nil {
		return nil, err
	}
	out := wire.hide(red)
	if string(out.Name) != name {
		return nil, provider.InvalidResponse(op, subject+" answered with another secret than the one asked for")
	}
	return out, nil
}

func handleSecretsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete measurement protocol secret"
	name, property, proof, err := prepareSecret(op, raw, resolved)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, mpSecretsDeleteID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: mpSecretsDeleteID, method: http.MethodDelete,
		path: "/v1beta/" + name, notFound: notFoundSecret}, nil); err != nil {
		return nil, err
	}
	return deleteTargetResult{Name: name, Deleted: true}, nil
}

// secretDeleteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func secretDeleteSummary(raw json.RawMessage) string {
	var a secretNameArguments
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Revoke Analytics measurement protocol secret " + a.Name + " for good; senders that use it can no " +
		"longer send events"
}

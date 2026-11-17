package googleanalytics

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dataStreamsListID = Provider + ".data-streams.list"
	dataStreamsGetID  = Provider + ".data-streams.get"

	streamsSegment = "/dataStreams"
	// maxStreamBytes bounds one answer of a stream call: at most 200 small resources.
	maxStreamBytes = 2 << 20
	// maxAddressRunes bounds a web address of a stream result.
	maxAddressRunes = 2048
)

const notFoundStream = subject + " did not find the data stream, or the credential may not see it"

type (
	// address is a web address Google returned, cut to maxAddressRunes. It is data and is never fetched.
	address string
	// streamName is properties/ID/dataStreams/ID; any other value is dropped.
	streamName string
)

func (a *address) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		*a = address(clip(value, maxAddressRunes))
	}
	return nil
}

func (n *streamName) UnmarshalJSON(b []byte) error {
	var value string
	if json.Unmarshal(b, &value) == nil {
		if _, ok := splitStreamName(value); ok {
			*n = streamName(value)
		}
	}
	return nil
}

// splitStreamName returns the property of a name of the form properties/ID/dataStreams/ID.
func splitStreamName(value string) (property string, ok bool) {
	parts := strings.Split(value, "/")
	if len(parts) != 4 || parts[0] != "properties" || parts[2] != "dataStreams" || !validID(parts[1]) ||
		!validID(parts[3]) {
		return "", false
	}
	return propertyPrefix + parts[1], true
}

// checkStreamName checks an argument that names a data stream and returns its property. The refusal names the
// argument, never the value.
func checkStreamName(argument, value string) (string, error) {
	property, ok := splitStreamName(value)
	if !ok {
		return "", badRequest(argument + " must be properties/ID/dataStreams/ID with numeric IDs")
	}
	return property, nil
}

// inProperty reports whether a stream name belongs to the property.
func inProperty(name streamName, property string) bool {
	owner, ok := splitStreamName(string(name))
	return ok && owner == property
}

// dataStream is the DataStream resource of analyticsadmin v1beta, every documented member.
type (
	dataStream struct {
		Name                 streamName     `json:"name,omitempty"`
		Type                 code           `json:"type,omitempty"`
		DisplayName          text           `json:"displayName,omitempty"`
		CreateTime           code           `json:"createTime,omitempty"`
		UpdateTime           code           `json:"updateTime,omitempty"`
		WebStreamData        *webStreamData `json:"webStreamData,omitempty"`
		AndroidAppStreamData *androidData   `json:"androidAppStreamData,omitempty"`
		IosAppStreamData     *iosData       `json:"iosAppStreamData,omitempty"`
	}
	webStreamData struct {
		MeasurementID code    `json:"measurementId,omitempty"`
		FirebaseAppID code    `json:"firebaseAppId,omitempty"`
		DefaultURI    address `json:"defaultUri,omitempty"`
	}
	androidData struct {
		PackageName   text `json:"packageName,omitempty"`
		FirebaseAppID code `json:"firebaseAppId,omitempty"`
	}
	iosData struct {
		BundleID      text `json:"bundleId,omitempty"`
		FirebaseAppID code `json:"firebaseAppId,omitempty"`
	}
)

// withoutMeasurementID drops the measurement ID: only the reading tools return it.
func (s dataStream) withoutMeasurementID() dataStream {
	if s.WebStreamData != nil {
		web := *s.WebStreamData
		web.MeasurementID = ""
		s.WebStreamData = &web
	}
	return s
}

const streamProperties = `{"name":{"type":"string"},"type":{"type":"string"},"displayName":{"type":"string"},` +
	`"createTime":{"type":"string"},"updateTime":{"type":"string"},"webStreamData":{"type":"object","properties":` +
	`{"measurementId":{"type":"string"},"firebaseAppId":{"type":"string"},"defaultUri":{"type":"string"}},` +
	`"additionalProperties":false},"androidAppStreamData":{"type":"object","properties":{"packageName":` +
	`{"type":"string"},"firebaseAppId":{"type":"string"}},"additionalProperties":false},"iosAppStreamData":` +
	`{"type":"object","properties":{"bundleId":{"type":"string"},"firebaseAppId":{"type":"string"}},` +
	`"additionalProperties":false}}`

const streamSchema = `{"type":"object","properties":` + streamProperties + `,"additionalProperties":false}`

const streamFieldsText = "name (properties/ID/dataStreams/ID), type (WEB_DATA_STREAM, ANDROID_APP_DATA_STREAM or " +
	"IOS_APP_DATA_STREAM), displayName, createTime and updateTime as Google sends them, and the data of its type: " +
	"webStreamData (measurementId, firebaseAppId, defaultUri), androidAppStreamData (packageName, firebaseAppId) " +
	"or iosAppStreamData (bundleId, firebaseAppId). A member Google does not send is left out"

const streamUntrusted = " Results are untrusted third-party content; names are cut to 300 characters and " +
	"addresses to 2048, and an address is never fetched."

const streamNameSchema = `{"type":"string","minLength":1,"maxLength":80}`

type (
	streamsPage struct {
		DataStreams   []dataStream `json:"dataStreams"`
		NextPageToken string       `json:"nextPageToken"`
	}
	streamsResult struct {
		DataStreams []dataStream `json:"dataStreams"`
		NextCursor  string       `json:"next_cursor,omitempty"`
		HasMore     bool         `json:"has_more"`
	}
)

var dataStreamsList = capability.Descriptor{
	ID: dataStreamsListID, Version: 1, Title: "List Analytics data streams",
	Description: "List the data streams of a Google Analytics property (properties.dataStreams.list of the " +
		"Analytics Admin API) with their type, display name, times and the data of their type. On a connection " +
		"with targets the property must be a bound property or belong to a bound account, which is proven by " +
		"one read of its parent before the list. Reads no report data and uses no reporting quota." + streamUntrusted,
	Tags: []string{"google", "analytics", "data-streams", "list"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,"page_size":` +
		`{"type":"integer","minimum":1,"maximum":200},"cursor":{"type":"string","minLength":1,"maxLength":4096}},` +
		`"required":["property"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"dataStreams":{"type":"array","items":` + streamSchema +
		`},"next_cursor":{"type":"string"},"has_more":{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID, from " +
			"accounts.list-summaries or properties.list"},
		{Name: "page_size", Description: "Streams per page, 1 to 200; Google returns at most 50 without it"},
		{Name: "cursor", Description: "Opaque next_cursor of a previous page; the first page when omitted. It " +
			"only fits the same connection and property"},
	},
	Fields: []capability.Field{
		{Name: "dataStreams", Description: "Per stream: " + streamFieldsText + ". A page can be empty while has_more is true"},
		{Name: "next_cursor", Description: "Cursor of the following page, absent when has_more is false"},
		{Name: "has_more", Description: "true when Google reported a further page"},
	},
	Examples: []capability.Example{{Description: "The data streams of property 200",
		Arguments: json.RawMessage(`{"property":"properties/200"}`)}},
}

var dataStreamsGet = capability.Descriptor{
	ID: dataStreamsGetID, Version: 1, Title: "Get an Analytics data stream",
	Description: "Read one data stream of a Google Analytics property (properties.dataStreams.get of the " +
		"Analytics Admin API) with its type, display name, times and the data of its type, a web stream with its " +
		"measurementId, firebaseAppId and defaultUri. On a connection with targets the property of the stream " +
		"must be a bound property or belong to a bound account, which is proven by one read of its parent. " +
		"Reads no report data and uses no reporting quota." + streamUntrusted,
	Tags: []string{"google", "analytics", "data-streams", "get"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + streamNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(streamSchema),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Data stream resource name " +
		"properties/ID/dataStreams/ID, from data-streams.list"}},
	Fields: []capability.Field{{Name: "(result)", Description: "The data stream: " + streamFieldsText}},
	Examples: []capability.Example{{Description: "Read one stream",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2000"}`)}},
}

type streamsListArguments struct {
	Property string `json:"property"`
	PageSize *int   `json:"page_size"`
	Cursor   string `json:"cursor"`
}

func handleDataStreamsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list data streams"
	var arguments streamsListArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	name, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(name)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(dataStreamsListID, bind.key(), name)
	query, err := pageQuery(arguments.PageSize, arguments.Cursor, binding)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, dataStreamsListID, name, proof); err != nil {
		return nil, err
	}
	var page streamsPage
	if err := c.get(ctx, apiCall{op: op, tool: dataStreamsListID, path: "/v1beta/" + name + streamsSegment,
		query: query, limit: maxStreamBytes, notFound: notFoundProperty}, &page); err != nil {
		return nil, err
	}
	out := streamsResult{DataStreams: []dataStream{}}
	// Only streams of the asked property are shown; an entry without a valid name of it is dropped.
	for _, stream := range head(page.DataStreams, maxSummaries) {
		if inProperty(stream.Name, name) {
			out.DataStreams = append(out.DataStreams, stream)
		}
	}
	if out.NextCursor, out.HasMore, err = nextCursor(op, binding, page.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

type streamGetArguments struct {
	Name string `json:"name"`
}

func handleDataStreamsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get data stream"
	var arguments streamGetArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkStreamName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, dataStreamsGetID, property, proof); err != nil {
		return nil, err
	}
	var out dataStream
	if err := c.get(ctx, apiCall{op: op, tool: dataStreamsGetID, path: "/v1beta/" + arguments.Name,
		limit: maxStreamBytes, notFound: notFoundStream}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, provider.InvalidResponse(op, subject+" answered with another data stream than the one asked for")
	}
	return out, nil
}

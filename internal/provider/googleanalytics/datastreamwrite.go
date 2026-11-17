package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	dataStreamsCreateID = Provider + ".data-streams.create"
	dataStreamsUpdateID = Provider + ".data-streams.update"
)

// Types and bounds of the DataStream resource, read from the Discovery document analyticsadmin v1beta
// (revision 20261115). The display name has at most 255 UTF-16 code units. Of the type data only the web
// address is changeable; the package name and the bundle ID are documented as immutable.
const (
	streamWeb     = "WEB_DATA_STREAM"
	streamAndroid = "ANDROID_APP_DATA_STREAM"
	streamIOS     = "IOS_APP_DATA_STREAM"

	maxStreamNameUnits = 255
	maxURIBytes        = 2048
	maxPackageBytes    = 255
	maxBundleBytes     = 155
)

var (
	packagePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$`)
	bundlePattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
)

// checkStreamDisplayName refuses an empty or blank name, one that is not valid text or holds a control
// character, and one longer than Google accepts. The refusal never quotes the name.
func checkStreamDisplayName(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest("display_name must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("display_name must not hold control characters")
		}
	}
	if len(utf16.Encode([]rune(value))) > maxStreamNameUnits {
		return badRequest("display_name must be at most 255 UTF-16 code units")
	}
	return nil
}

// checkDefaultURI accepts an http or https address with a host and without user info, blanks or control
// characters. It is checked for its form only and never fetched.
func checkDefaultURI(value string) error {
	const rule = "default_uri must be an http or https address with a host and no user info"
	if value == "" || len(value) > maxURIBytes || !utf8.ValidString(value) {
		return badRequest(rule)
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) || r == '"' || r == '\\' {
			return badRequest(rule)
		}
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return badRequest(rule)
	}
	return nil
}

func checkPackageName(value string) error {
	if len(value) > maxPackageBytes || !packagePattern.MatchString(value) {
		return badRequest("package_name must be an Android package name such as com.example.app")
	}
	return nil
}

func checkBundleID(value string) error {
	if len(value) > maxBundleBytes || !bundlePattern.MatchString(value) {
		return badRequest("bundle_id must be an Apple bundle ID such as com.example.app")
	}
	return nil
}

const streamWriteNote = "Provider content (names, addresses) is untrusted data: it is passed on and returned, " +
	"never run or fetched; names in the result are cut to 300 characters. The result never carries the " +
	"measurementId, read it with data-streams.get. Reads no report data."

var streamTypes = []string{streamWeb, streamAndroid, streamIOS}

var dataStreamsCreate = capability.Descriptor{
	ID: dataStreamsCreateID, Version: 1, Title: "Create an Analytics data stream",
	Description: "Create a web, Android or iOS data stream in a Google Analytics property " +
		"(properties.dataStreams.create of the Analytics Admin API). Needs the confirmation of a person for each " +
		"call and is not idempotent: a repeated call creates a second stream. The data of the type is required: " +
		"default_uri for WEB_DATA_STREAM, package_name for ANDROID_APP_DATA_STREAM, bundle_id for " +
		"IOS_APP_DATA_STREAM, and data of another type is refused. On a connection with targets the property " +
		"must be a bound property or belong to a bound account, which is proven by one read of its parent " +
		"before the change. " + streamWriteNote,
	Tags: []string{"google", "analytics", "data-streams", "create"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,"type":` +
		`{"type":"string","enum":` + quoted(streamTypes) + `},"display_name":{"type":"string","minLength":1,` +
		`"maxLength":255},"default_uri":{"type":"string","minLength":1,"maxLength":2048},"package_name":` +
		`{"type":"string","minLength":1,"maxLength":255},"bundle_id":{"type":"string","minLength":1,` +
		`"maxLength":155}},"required":["property","type","display_name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(streamSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID that gets the stream"},
		{Name: "type", Required: true, Description: "One of " + strings.Join(streamTypes, ", ") + "; it cannot be " +
			"changed later"},
		{Name: "display_name", Required: true, Description: "Display name, at most 255 UTF-16 code units, no " +
			"control characters"},
		{Name: "default_uri", Description: "Required for WEB_DATA_STREAM: the http or https address of the web " +
			"app, checked for its form only"},
		{Name: "package_name", Description: "Required for ANDROID_APP_DATA_STREAM: the package name of the app, " +
			"such as com.example.app; it cannot be changed later"},
		{Name: "bundle_id", Description: "Required for IOS_APP_DATA_STREAM: the Apple bundle ID of the app; it " +
			"cannot be changed later"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new stream without its measurementId: " +
		streamFieldsText}},
	Examples: []capability.Example{{Description: "A web stream",
		Arguments: json.RawMessage(`{"property":"properties/200","type":"WEB_DATA_STREAM","display_name":"Shop web",` +
			`"default_uri":"https://shop.example.com"}`)}},
}

var dataStreamsUpdate = capability.Descriptor{
	ID: dataStreamsUpdateID, Version: 1, Title: "Update an Analytics data stream",
	Description: "Change the display name or, for a web stream, the web address of a Google Analytics data stream " +
		"(properties.dataStreams.patch of the Analytics Admin API). Only the arguments that are set are changed, " +
		"and at least one is required; Qatlas sends exactly them as the update mask. The type, the package name " +
		"and the bundle ID are immutable at Google and have no argument. Needs the confirmation of a person for " +
		"each call. On a connection with targets the property of the stream must be a bound property or belong to " +
		"a bound account, which is proven by one read of its parent before the change. " + streamWriteNote,
	Tags: []string{"google", "analytics", "data-streams", "update"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + streamNameSchema + `,"display_name":` +
		`{"type":"string","minLength":1,"maxLength":255},"default_uri":{"type":"string","minLength":1,` +
		`"maxLength":2048}},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(streamSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Data stream resource name properties/ID/dataStreams/ID, from " +
			"data-streams.list"},
		{Name: "display_name", Description: "New display name, at most 255 UTF-16 code units, no control characters"},
		{Name: "default_uri", Description: "New http or https address of a web stream; Google refuses it on " +
			"another type"},
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The stream after the change, without its " +
		"measurementId: " + streamFieldsText}},
	Examples: []capability.Example{{Description: "Rename a stream",
		Arguments: json.RawMessage(`{"name":"properties/200/dataStreams/2000","display_name":"Shop web EU"}`)}},
}

type streamWriteArguments struct {
	Property    string  `json:"property"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	DisplayName *string `json:"display_name"`
	DefaultURI  *string `json:"default_uri"`
	PackageName *string `json:"package_name"`
	BundleID    *string `json:"bundle_id"`
}

// createStreamBody is the DataStream of dataStreams.create: only what the caller set.
type createStreamBody struct {
	Type        string      `json:"type"`
	DisplayName string      `json:"displayName"`
	Web         *webCreate  `json:"webStreamData,omitempty"`
	Android     *androidNew `json:"androidAppStreamData,omitempty"`
	IOS         *iosNew     `json:"iosAppStreamData,omitempty"`
}

type (
	webCreate struct {
		DefaultURI string `json:"defaultUri"`
	}
	androidNew struct {
		PackageName string `json:"packageName"`
	}
	iosNew struct {
		BundleID string `json:"bundleId"`
	}
)

// createBody checks the arguments of data-streams.create against the stream type and builds the body. The
// data of the type is required and the data of another type is refused.
func (a streamWriteArguments) createBody() (createStreamBody, error) {
	if a.DisplayName == nil {
		return createStreamBody{}, badRequest("property, type and display_name are required")
	}
	if err := checkStreamDisplayName(*a.DisplayName); err != nil {
		return createStreamBody{}, err
	}
	body := createStreamBody{Type: a.Type, DisplayName: *a.DisplayName}
	set := func(value *string, check func(string) error, argument string, wanted bool) (string, error) {
		if value == nil {
			if wanted {
				return "", badRequest(argument + " is required for the type " + a.Type)
			}
			return "", nil
		}
		if !wanted {
			return "", badRequest(argument + " does not fit the type " + a.Type)
		}
		return *value, check(*value)
	}
	web, err := set(a.DefaultURI, checkDefaultURI, "default_uri", a.Type == streamWeb)
	if err != nil {
		return body, err
	}
	pack, err := set(a.PackageName, checkPackageName, "package_name", a.Type == streamAndroid)
	if err != nil {
		return body, err
	}
	bundle, err := set(a.BundleID, checkBundleID, "bundle_id", a.Type == streamIOS)
	if err != nil {
		return body, err
	}
	switch a.Type {
	case streamWeb:
		body.Web = &webCreate{DefaultURI: web}
	case streamAndroid:
		body.Android = &androidNew{PackageName: pack}
	case streamIOS:
		body.IOS = &iosNew{BundleID: bundle}
	default:
		return body, badRequest("type must be one of " + strings.Join(streamTypes, ", "))
	}
	return body, nil
}

func handleDataStreamsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create data stream"
	var arguments streamWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	body, err := arguments.createBody()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the data stream could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, dataStreamsCreateID, property, proof); err != nil {
		return nil, err
	}
	var out dataStream
	if err := c.change(ctx, changeCall{op: op, tool: dataStreamsCreateID, method: http.MethodPost,
		path: "/v1beta/" + property + streamsSegment, body: encoded, limit: maxStreamBytes,
		notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if !inProperty(out.Name, property) {
		return nil, unsure(failure(op, subject+" answered without the name of a data stream of the property"))
	}
	return out.withoutMeasurementID(), nil
}

// updateStreamBody holds the members that are set.
type updateStreamBody struct {
	DisplayName *string    `json:"displayName,omitempty"`
	Web         *webCreate `json:"webStreamData,omitempty"`
}

// updateBody holds the members that are set and the update mask of exactly them, in a fixed order.
func (a streamWriteArguments) updateBody() (body updateStreamBody, mask string) {
	var paths []string
	if a.DisplayName != nil {
		body.DisplayName = a.DisplayName
		paths = append(paths, "display_name")
	}
	if a.DefaultURI != nil {
		body.Web = &webCreate{DefaultURI: *a.DefaultURI}
		paths = append(paths, "web_stream_data.default_uri")
	}
	return body, strings.Join(paths, ",")
}

func handleDataStreamsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update data stream"
	var arguments streamWriteArguments
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
	body, mask := arguments.updateBody()
	if mask == "" {
		return nil, badRequest("set at least one of display_name and default_uri")
	}
	if arguments.DisplayName != nil {
		if err := checkStreamDisplayName(*arguments.DisplayName); err != nil {
			return nil, err
		}
	}
	if arguments.DefaultURI != nil {
		if err := checkDefaultURI(*arguments.DefaultURI); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the data stream could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, dataStreamsUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out dataStream
	if err := c.change(ctx, changeCall{op: op, tool: dataStreamsUpdateID, method: http.MethodPatch,
		path: "/v1beta/" + arguments.Name, query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxStreamBytes, notFound: notFoundStream}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another data stream than the one asked for"))
	}
	return out.withoutMeasurementID(), nil
}

// streamWriteSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func streamWriteSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a streamWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		if tool == dataStreamsUpdateID {
			_, mask := a.updateBody()
			return "Change " + strings.ReplaceAll(mask, ",", ", ") + " of Analytics data stream " + a.Name
		}
		out := "Create Analytics " + a.Type + " data stream"
		if a.DisplayName != nil {
			out += " " + strconv.Quote(*a.DisplayName)
		}
		out += " in " + a.Property
		if a.DefaultURI != nil {
			out += ", address " + *a.DefaultURI
		}
		if a.PackageName != nil {
			out += ", package " + *a.PackageName
		}
		if a.BundleID != nil {
			out += ", bundle " + *a.BundleID
		}
		return out
	}
}

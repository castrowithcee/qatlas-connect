package googletagmanager

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	destinationsListID = Provider + ".destinations.list"
	destinationsGetID  = Provider + ".destinations.get"
)

const (
	destinationPattern   = `^accounts/` + idPattern + `/containers/` + idPattern + `/destinations/` + idPattern + `$`
	formDestination      = "accounts/ACCOUNT/containers/CONTAINER/destinations/DESTINATION"
	maxDestinationPath   = 160
	destinationArgSchema = `{"type":"string","maxLength":160,"pattern":"` + destinationPattern + `","x-form":"` +
		formDestination + `"}`
)

var destinationPathRE = regexp.MustCompile(destinationPattern)

// destinationOf settles a destination path against the target of the connection through its container.
func (s scope) destinationOf(given string) (string, error) {
	container, _, found := strings.Cut(given, "/destinations/")
	if len(given) > maxDestinationPath || !destinationPathRE.MatchString(given) || !found {
		return "", invalidArgument("destination must be " + formDestination + " with decimal IDs")
	}
	if _, err := s.containerOf(container); err != nil {
		return "", invalidArgument("destination " + outsideTarget)
	}
	return given, nil
}

type destination struct {
	Path              idText   `json:"path,omitempty"`
	AccountID         idText   `json:"accountId,omitempty"`
	ContainerID       idText   `json:"containerId,omitempty"`
	DestinationID     idText   `json:"destinationId,omitempty"`
	DestinationLinkID idText   `json:"destinationLinkId,omitempty"`
	Name              nameText `json:"name,omitempty"`
	Fingerprint       idText   `json:"fingerprint,omitempty"`
	TagManagerURL     urlText  `json:"tagManagerUrl,omitempty"`
}

type (
	destinationsAnswer struct {
		Destination   []destination `json:"destination"`
		NextPageToken string        `json:"nextPageToken"`
	}
	destinationsResult struct {
		Destinations []destination `json:"destinations"`
		NextCursor   string        `json:"next_cursor,omitempty"`
	}
)

var destinationSchema = objectSchema(member("path", stringSchema), member("accountId", stringSchema),
	member("containerId", stringSchema), member("destinationId", stringSchema),
	member("destinationLinkId", stringSchema), member("name", stringSchema), member("fingerprint", stringSchema),
	member("tagManagerUrl", stringSchema))

var destinationFields = []capability.Field{
	{Name: "path", Description: formDestination},
	{Name: "accountId", Description: "Account ID"}, {Name: "containerId", Description: "Container ID"},
	{Name: "destinationId", Description: "Destination ID"},
	{Name: "destinationLinkId", Description: "ID of the link between the container and the destination"},
	{Name: "name", Description: "Display name"},
	{Name: "fingerprint", Description: "Fingerprint of the stored destination"},
	{Name: "tagManagerUrl", Description: "Link to the destination in the Tag Manager interface"},
}

var destinationsList = capability.Descriptor{
	ID: destinationsListID, Version: 1, Title: "List Tag Manager destinations",
	Description: "List the Google tag destinations linked to one Google Tag Manager container, a batch per call " +
		"with a cursor to read on. Linking a destination is not offered. " + untrustedNote,
	Tags: []string{"google", "tag-manager", "destinations"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, member("container", containerArgSchema), member("cursor", cursorSchema)),
	OutputSchema: json.RawMessage(listResultSchema("destinations", destinationSchema)),
	Arguments:    []capability.Argument{containerArgument, cursorArgument},
	Fields: []capability.Field{{Name: "destinations", Description: "Destinations with every member of " +
		"destinations.get"}, nextCursorField},
	Examples: []capability.Example{{Description: "Destinations of one container",
		Arguments: json.RawMessage(`{"container":"accounts/1000001/containers/2000002"}`)}},
}

var destinationsGet = capability.Descriptor{
	ID: destinationsGetID, Version: 1, Title: "Get a Tag Manager destination",
	Description: "Read one Google tag destination of a container. Google marks the method deprecated. " + untrustedNote,
	Tags:        []string{"google", "tag-manager", "destinations"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"destination"}, member("destination", destinationArgSchema)),
	OutputSchema: json.RawMessage(destinationSchema),
	Arguments: []capability.Argument{{Name: "destination", Required: true, Description: "Destination as " +
		formDestination + ", from destinations.list; it must lie inside the target of the connection"}},
	Fields: destinationFields,
	Examples: []capability.Example{{Description: "One destination",
		Arguments: json.RawMessage(`{"destination":"accounts/1000001/containers/2000002/destinations/4"}`)}},
}

func destinationDescriptors() []capability.Descriptor {
	return []capability.Descriptor{destinationsList, destinationsGet}
}

func handleDestinationsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("list destinations", destinationsListID, resolved, raw)
	if err != nil {
		return nil, err
	}
	parent, err := c.scope.containerOf(c.args.Container)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(c.tool, parent.String(), c.scope.binding())
	query, err := pageQuery(binding, c.args.Cursor, nil)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer destinationsAnswer
	if err := cl.get(ctx, c.op, c.tool, parent.String()+"/destinations", query, &answer); err != nil {
		return nil, err
	}
	if len(answer.Destination) > maxEntries {
		return nil, tooMany(c.op)
	}
	out := destinationsResult{Destinations: append([]destination{}, answer.Destination...)}
	if out.NextCursor, err = nextCursor(c.op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleDestinationsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("get destination", destinationsGetID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var in struct {
		Destination string `json:"destination"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	path, err := c.scope.destinationOf(in.Destination)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out destination
	if err := cl.get(ctx, c.op, c.tool, path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

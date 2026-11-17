package googletagmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	containersCreateID = Provider + ".containers.create"
	containersUpdateID = Provider + ".containers.update"
	containersLookupID = Provider + ".containers.lookup"
	containersDeleteID = Provider + ".containers.delete"
)

// usageContexts are the values Google documents for a container, without the unspecified placeholder.
var usageContexts = []string{"web", "android", "ios", "androidSdk5", "iosSdk5", "amp", "server"}

const (
	containerNameSchema = `{"type":"string","minLength":1}`
	containerTextSchema = `{"type":"string"}`
	containerListSchema = `{"type":"array","items":{"type":"string","minLength":1},"maxItems":1000}`
	containerUsageList  = `{"type":"array","minItems":1,"maxItems":7,"items":{"type":"string","enum":` +
		`["web","android","ios","androidSdk5","iosSdk5","amp","server"]}}`
	lookupIDSchema = `{"type":"string","minLength":3,"maxLength":60,"pattern":"^[A-Za-z0-9]+-[A-Za-z0-9]{1,40}$"}`
)

const containerEffect = "The change is made to the container itself, not in a workspace. "

const servingNote = "taggingServerUrls decides where a web container sends its measurement data; the change " +
	"takes effect only with a published container version, which no tool of this provider makes. "

const containerReplaceNote = "A container is replaced as a whole: send name, domainName, notes, and " +
	"taggingServerUrls as read with containers.get, with the changes made, because a member that is left out is " +
	"removed. usageContext cannot be changed and is kept as stored. A value that containers.get returned cut " +
	"(ending in …) is refused instead of written back. "

const previousURLsNote = "previous_tagging_server_urls is required: the taggingServerUrls as stored, an empty list " +
	"if there are none. It is only compared and never written: the confirmation shows it next to the new list, and " +
	"the container is read before anything is written, so a call whose previous list is not exactly the stored one " +
	"writes nothing. "

var containerSettingArguments = []capability.Argument{
	{Name: "name", Required: true, Description: "Display name"},
	{Name: "domainName", Description: "Domain names associated with the container"},
	{Name: "notes", Description: "Notes"},
	{Name: "taggingServerUrls", Description: "URLs of the server-side container; if there are several, all URL " +
		"paths must match"},
}

var containerFingerprintArgument = capability.Argument{Name: "fingerprint", Required: true, Description: "Fingerprint " +
	"of the container as last read, from containers.get or containers.list; Google refuses the change when the " +
	"container has changed since"}

func containerMembers() []string {
	return []string{member("name", containerNameSchema), member("domainName", containerListSchema),
		member("notes", containerTextSchema), member("taggingServerUrls", containerListSchema)}
}

var containersCreate = capability.Descriptor{
	ID: containersCreateID, Version: 1, Title: "Create a Tag Manager container",
	Description: "Create a container in a Google Tag Manager account. " + containerEffect + servingNote +
		"Not available to a connection bound to a container, because it reaches the whole account. " + untrustedNote +
		" Not idempotent: a repeated call creates a second container.",
	Tags: []string{"google", "tag-manager", "containers", "create"}, Provider: Provider,
	Risk: changeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"name", "usageContext"}, append([]string{member("account", accountArgSchema),
		member("usageContext", containerUsageList)}, containerMembers()...)...),
	OutputSchema: json.RawMessage(containerSchema),
	Arguments: append([]capability.Argument{accountArgument, {Name: "usageContext", Required: true,
		Description: "Usage contexts, at least one of " + strings.Join(usageContexts, ", ")}},
		containerSettingArguments...),
	Fields: containerFields,
	Examples: []capability.Example{{Description: "A web container",
		Arguments: json.RawMessage(`{"account":"accounts/1000001","name":"Example site","usageContext":["web"],` +
			`"domainName":["example.org"]}`)}},
}

var containersUpdate = capability.Descriptor{
	ID: containersUpdateID, Version: 1, Title: "Update a Tag Manager container",
	Description: "Replace the name, domains, notes, and server-side URLs of a container. " + containerReplaceNote +
		containerEffect + servingNote + previousURLsNote + untrustedNote +
		" The fingerprint is required, so a container changed in the meantime is not overwritten.",
	Tags: []string{"google", "tag-manager", "containers", "update"}, Provider: Provider,
	Risk: changeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"fingerprint", "name", "previous_tagging_server_urls"}, append([]string{member("container", containerArgSchema),
		member("fingerprint", fingerprintSchema), member("previous_tagging_server_urls", containerListSchema)},
		containerMembers()...)...),
	OutputSchema: json.RawMessage(containerSchema),
	Arguments: append(append([]capability.Argument{containerArgument, containerFingerprintArgument},
		containerSettingArguments...), capability.Argument{Name: "previous_tagging_server_urls",
		Required: true, Description: "taggingServerUrls as stored, from containers.get, or an empty list; only compared, " +
			"never written"}),
	Fields: containerFields,
	Examples: []capability.Example{{Description: "Rename a container",
		Arguments: json.RawMessage(`{"container":"accounts/1000001/containers/2000002","fingerprint":"1700000000000",` +
			`"name":"Renamed site","previous_tagging_server_urls":[]}`)}},
}

var containersLookup = capability.Descriptor{
	ID: containersLookupID, Version: 1, Title: "Look up a Tag Manager container by tag ID",
	Description: "Find the container that a tag ID such as GTM-XXXX or a destination ID such as AW-123456789 " +
		"belongs to; give exactly one of them. The container is answered only when it lies inside the target of " +
		"the connection, otherwise the answer is not-found, the same as for an ID no container has. " + untrustedNote,
	Tags: []string{"google", "tag-manager", "containers"}, Risk: readRisk, Provider: Provider,
	InputSchema:  inputSchema(nil, member("destination_id", lookupIDSchema), member("tag_id", lookupIDSchema)),
	OutputSchema: json.RawMessage(containerSchema),
	Arguments: []capability.Argument{
		{Name: "destination_id", Description: "Destination ID linked to a container, such as AW-123456789; give " +
			"either this or tag_id"},
		{Name: "tag_id", Description: "Tag ID of a container, such as GTM-123456789; give either this or destination_id"}},
	Fields: containerFields,
	Examples: []capability.Example{{Description: "The container of a tag ID",
		Arguments: json.RawMessage(`{"tag_id":"GTM-EXAMPLE1"}`)}},
}

var containersDelete = capability.Descriptor{
	ID: containersDeleteID, Version: 1, Title: "Delete a Tag Manager container",
	Description: "Delete a container of a Google Tag Manager account with its workspaces, tags, triggers, " +
		"variables, and versions. Treat it as irreversible: it is not established that a deleted container can be " +
		"brought back. A connection bound to a container can delete only that container; one bound to an account " +
		"or to nothing can delete any container it reaches." + listedOnly,
	RequiresToolAllowList: true,
	Tags:                  []string{"google", "tag-manager", "containers", "delete"}, Provider: Provider,
	Risk:         changeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema(nil, member("container", containerArgSchema)),
	OutputSchema: json.RawMessage(objectSchema(member("container", stringSchema), member("deleted", boolSchema))),
	Arguments:    []capability.Argument{containerArgument},
	Fields: []capability.Field{{Name: "container", Description: "The container path that was deleted"},
		{Name: "deleted", Description: "True once Google accepted the deletion"}},
	Examples: []capability.Example{{Description: "Delete one container",
		Arguments: json.RawMessage(`{"container":"accounts/1000001/containers/2000002"}`)}},
}

func containerDescriptors() []capability.Descriptor {
	return []capability.Descriptor{containersCreate, containersUpdate, containersLookup, containersDelete}
}

// containerBody holds the members of a container a caller sets. usageContext is sent on create and, on an
// update, carried over from the stored container.
type containerBody struct {
	Name              string   `json:"name"`
	UsageContext      []string `json:"usageContext,omitempty"`
	DomainName        []string `json:"domainName,omitempty"`
	Notes             string   `json:"notes,omitempty"`
	TaggingServerURLs []string `json:"taggingServerUrls,omitempty"`
}

type containerInput struct {
	containerBody
	Previous *[]string `json:"previous_tagging_server_urls"`
}

func (c call) containerInputOf(raw json.RawMessage) (containerInput, error) {
	var in containerInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if strings.TrimSpace(in.Name) == "" {
		return in, invalidArgument("name is required")
	}
	return in, nil
}

func (c call) encodeContainer(b containerBody) ([]byte, error) {
	body, err := json.Marshal(b)
	if err != nil {
		return nil, provider.Fail(c.op, "the container could not be encoded")
	}
	if len(body) > maxRequestBody {
		return nil, invalidArgument("the container is larger than " + strconv.Itoa(maxRequestBody) + " bytes")
	}
	return body, nil
}

// storedContainer is a container as Google stores it, uncut: update compares against it and carries a member over.
type storedContainer struct {
	UsageContext      []string `json:"usageContext"`
	DomainName        []string `json:"domainName"`
	Notes             string   `json:"notes"`
	Name              string   `json:"name"`
	TaggingServerURLs []string `json:"taggingServerUrls"`
}

func handleContainersCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("create container", containersCreateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	if c.scope.container != "" {
		return nil, invalidArgument("containers.create reaches the whole account and is not available to a " +
			"connection bound to a container")
	}
	parent, err := c.scope.accountOf(c.args.Account)
	if err != nil {
		return nil, err
	}
	in, err := c.containerInputOf(raw)
	if err != nil {
		return nil, err
	}
	if len(in.UsageContext) == 0 {
		return nil, invalidArgument("usageContext needs at least one entry")
	}
	if slices.ContainsFunc(in.UsageContext, func(v string) bool { return !slices.Contains(usageContexts, v) }) {
		return nil, invalidArgument("usageContext holds a value that is not one of " + strings.Join(usageContexts, ", "))
	}
	body, err := c.encodeContainer(in.containerBody)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out container
	r := request{method: http.MethodPost, path: parent.String() + "/containers", body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleContainersUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("update container", containersUpdateID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.containerOf(c.args.Container)
	if err != nil {
		return nil, err
	}
	query, err := c.needFingerprint()
	if err != nil {
		return nil, err
	}
	in, err := c.containerInputOf(raw)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	// The stored container is read first: nothing is written if it cannot be read, a changed tagging server
	// must match the list the confirmation showed, and a cut value must not overwrite the stored one.
	var stored storedContainer
	if err := cl.get(ctx, c.op, c.tool, path.String(), nil, &stored); err != nil {
		return nil, err
	}
	if err := checkContainerChange(in, stored); err != nil {
		return nil, err
	}
	in.UsageContext = stored.UsageContext
	body, err := c.encodeContainer(in.containerBody)
	if err != nil {
		return nil, err
	}
	var out container
	r := request{method: http.MethodPut, path: path.String(), query: query, body: body, change: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// checkContainerChange refuses an update whose previous taggingServerUrls are not the stored ones, or that
// sends back a value a read had cut.
func checkContainerChange(in containerInput, stored storedContainer) error {
	if in.Previous == nil || !slices.Equal(*in.Previous, stored.TaggingServerURLs) {
		return invalidArgument("previous_tagging_server_urls is not the stored taggingServerUrls: read the " +
			"container with containers.get, pass its taggingServerUrls as previous_tagging_server_urls (an empty " +
			"list if there are none), and confirm again; nothing was written")
	}
	cutText := func(given, was string, limit int) bool {
		return given != was && given == clipJSON(quoteJSON(was), limit)
	}
	cutList := func(given, was []string) bool {
		return !slices.Equal(given, was) && slices.Equal(given, clipList(quoteJSONList(was), maxURLRunes))
	}
	if cutText(in.Name, stored.Name, maxNameRunes) || cutText(in.Notes, stored.Notes, maxNoteRunes) ||
		cutList(in.DomainName, stored.DomainName) || cutList(in.TaggingServerURLs, stored.TaggingServerURLs) {
		return invalidArgument("a value is the cut form of the stored value (containers.get cuts long texts and " +
			"lists): send the complete value instead; nothing was written")
	}
	return nil
}

func quoteJSON(s string) []byte { b, _ := json.Marshal(s); return b }

func quoteJSONList(l []string) []byte { b, _ := json.Marshal(l); return b }

func notFound(op string) *provider.Error {
	return &provider.Error{Class: provider.ClassNotFound, Op: op,
		Message: "no container with this ID was found inside the target of this connection"}
}

func handleContainersLookup(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("look up container", containersLookupID, resolved, raw)
	if err != nil {
		return nil, err
	}
	var in struct {
		DestinationID string `json:"destination_id"`
		TagID         string `json:"tag_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, provider.Fail(c.op, "the validated arguments could not be read")
	}
	if (in.DestinationID == "") == (in.TagID == "") {
		return nil, invalidArgument("give exactly one of destination_id and tag_id")
	}
	query := url.Values{}
	if in.TagID != "" {
		query.Set("tagId", in.TagID)
	} else {
		query.Set("destinationId", in.DestinationID)
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var out container
	r := request{method: http.MethodGet, path: "accounts/containers:lookup", query: query, missing: true}
	if err := cl.send(ctx, c.op, c.tool, r, &out); err != nil {
		return nil, err
	}
	// A container outside the binding is answered like a missing one, so the lookup reveals nothing of it.
	found, ok := parsePath(string(out.Path), containerPath)
	if !ok || !c.scope.allows(found) {
		return nil, notFound(c.op)
	}
	return out, nil
}

func handleContainersDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	c, err := prepare("delete container", containersDeleteID, resolved, raw)
	if err != nil {
		return nil, err
	}
	path, err := c.scope.containerOf(c.args.Container)
	if err != nil {
		return nil, err
	}
	cl, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	r := request{method: http.MethodDelete, path: path.String(), change: true}
	if err := cl.send(ctx, c.op, c.tool, r, nil); err != nil {
		return nil, err
	}
	return deleteContainerResult{Container: path.String(), Deleted: true}, nil
}

type deleteContainerResult struct {
	Container string `json:"container"`
	Deleted   bool   `json:"deleted"`
}

// containerSummary phrases a confirmation from the arguments alone; the core neutralizes and bounds it.
func containerSummary(verb string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a struct {
			Account, Container, Name string
			UsageContext             []string
			TaggingServerURLs        []string  `json:"taggingServerUrls"`
			Previous                 *[]string `json:"previous_tagging_server_urls"`
		}
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		out := verb + " container"
		if a.Name != "" {
			out += " " + strconv.Quote(a.Name)
		}
		switch {
		case a.Container != "":
			out += " " + a.Container
		case a.Account != "":
			out += " in " + a.Account
		}
		if len(a.UsageContext) > 0 {
			out += " for " + strings.Join(a.UsageContext, ", ")
		}
		switch {
		case a.Previous != nil:
			out += "; taggingServerUrls from " + urlsText(*a.Previous) + " to " + urlsText(a.TaggingServerURLs)
		case len(a.TaggingServerURLs) > 0:
			out += "; taggingServerUrls " + urlsText(a.TaggingServerURLs)
		}
		return out
	}
}

func urlsText(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	quoted := make([]string, len(list))
	for i, u := range list {
		quoted[i] = strconv.Quote(u)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// deleteContainerSummary names the container and says what goes with it.
func deleteContainerSummary(raw json.RawMessage) string {
	var a struct{ Container string }
	if json.Unmarshal(raw, &a) != nil {
		return ""
	}
	return "Delete container " + a.Container + " with all its workspaces, tags, triggers, variables, and versions; " +
		"treat it as irreversible"
}

package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The network configuration tools read and maintain the hosted compute network configurations of one
// organization and read the network settings resources they point at. Every tool names an organization as an
// owner target, checked against the targets and exclusions before a secret is read; the enterprise routes
// are not reachable. A configuration only refers to an Azure network settings resource that already exists:
// no tool creates a network resource, and no identifier of a subnet or a subscription is output beyond the
// subnet_id member GitHub documents. Names, identifiers, and regions come from the organization and are
// untrusted data.
//
// GitHub's description of these routes lists no error response, so it gives no answer of its own for an
// organization whose plan does not offer hosted compute networking. Like the audit log, every tool therefore
// ends a 403 or 404 as the state unavailable with a fixed reason that names the possible causes; every other
// failure stays an error.

const (
	networkConfigurationSensitivity = "github-network-configuration"

	// networkIDMax bounds an identifier of a configuration or a network settings resource. GitHub documents
	// the identifiers as plain strings; the pattern keeps one a single path segment.
	networkIDMax   = 128
	networkNameMax = 100

	networkUnavailableState  = "unavailable"
	networkUnavailableReason = "GitHub did not give the hosted compute network configurations of this " +
		"organization: the plan may not offer them (GitHub documents the feature for Enterprise Cloud), the " +
		"token may lack the network configurations right, the account behind the token may not be an owner of " +
		"the organization, or the organization or the addressed identifier may not exist"
	networkUnavailableChangeReason = networkUnavailableReason + "; nothing was changed"

	networkRunnerGroupNote = "Runner groups that use this network configuration may be affected by the change"

	networkRejected = "GitHub rejected the request: the name may be in use or break its naming rules, or a " +
		"network settings identifier may be unknown or already used by another configuration, so nothing was changed"
	networkConflict = "GitHub refused the change of the network configuration in its current state, so nothing " +
		"was changed"
)

var (
	networkIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	networkNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

var networkComputeServices = []string{"none", "actions"}

var networkReadRisk = capability.Risk{
	Effect:          capability.EffectRead,
	Idempotency:     capability.IdempotencySafe,
	Confirmation:    capability.ConfirmationNone,
	OpenWorld:       true,
	DataSensitivity: networkConfigurationSensitivity,
}

// ---- Schemas and fields

const (
	networkIDSchema   = `{"type":"string","minLength":1,"maxLength":128,"pattern":"^[A-Za-z0-9_-]{1,128}$"}`
	networkNameSchema = `{"type":"string","minLength":1,"maxLength":100,"pattern":"^[A-Za-z0-9._-]{1,100}$"}`

	// networkSettingsIDsSchema holds exactly one identifier, as GitHub documents the list.
	networkSettingsIDsSchema = `{"type":"array","minItems":1,"maxItems":1,"items":` + networkIDSchema + `}`
	// networkFailoverIDsSchema holds at most one identifier; an empty list clears the failover resource.
	networkFailoverIDsSchema = `{"type":"array","minItems":0,"maxItems":1,"items":` + networkIDSchema + `}`

	networkComputeServiceSchema = `{"type":"string","enum":["none","actions"]}`

	networkStateProperties = `"state":{"type":"string"},"reason":{"type":"string"}`

	networkConfigurationItemProperties = `"id":{"type":"string"},"name":{"type":"string"},` +
		`"compute_service":{"type":"string"},"network_settings_ids":{"type":"array","items":{"type":"string"}},` +
		`"network_settings_ids_truncated":{"type":"boolean"},` +
		`"failover_network_settings_ids":{"type":"array","items":{"type":"string"}},` +
		`"failover_network_settings_ids_truncated":{"type":"boolean"},"failover_network_enabled":{"type":"boolean"},` +
		`"created_on":{"type":"string"}`

	networkConfigurationProperties = networkConfigurationItemProperties + `,` + networkStateProperties

	networkSettingsProperties = `"id":{"type":"string"},"network_configuration_id":{"type":"string"},` +
		`"name":{"type":"string"},"subnet_id":{"type":"string"},"region":{"type":"string"},` + networkStateProperties
)

var networkStateFields = []capability.Field{
	{Name: "state", Description: "unavailable when GitHub refused the request with 403 or 404; then no other " +
		"member is given; absent otherwise"},
	{Name: "reason", Description: "Fixed reason that accompanies state unavailable; it names the possible causes " +
		"and never quotes GitHub"},
}

// networkConfigurationItemFields documents every member of GitHub's network configuration object. A member
// GitHub leaves out or sends as null is absent.
var networkConfigurationItemFields = []capability.Field{
	{Name: "id", Description: "Identifier of the configuration, as the other tools take it; untrusted data"},
	{Name: "name", Description: "Name of the configuration; untrusted data"},
	{Name: "compute_service", Description: "none, actions, or codespaces: the hosted compute service the " +
		"configuration supports"},
	{Name: "network_settings_ids", Description: "Identifiers of the network settings resources of the " +
		"configuration, as github.network-settings.get takes them; at most 100; untrusted data"},
	{Name: "network_settings_ids_truncated", Description: "True when more identifiers existed than are listed"},
	{Name: "failover_network_settings_ids", Description: "Identifiers of the failover network settings resources; " +
		"at most 100; untrusted data"},
	{Name: "failover_network_settings_ids_truncated", Description: "True when more identifiers existed than are " +
		"listed"},
	{Name: "failover_network_enabled", Description: "Whether the failover network resource is enabled"},
	{Name: "created_on", Description: "Creation time in ISO 8601 format"},
}

var networkConfigurationFields = append(append([]capability.Field{}, networkConfigurationItemFields...),
	networkStateFields...)

// ---- Descriptors

var networkConfigurationsList = capability.Descriptor{
	ID:      Provider + ".network-configurations.list",
	Version: 1,
	Title:   "List the hosted compute network configurations of a GitHub organization",
	Description: "List one bounded batch of the hosted compute network configurations of an organization an " +
		"explicit connection allows, in the order GitHub returns them, each with every member GitHub documents; " +
		"names and identifiers are untrusted data; needs an organization owner and a plan that offers hosted " +
		"compute networking, otherwise the state is unavailable",
	Tags:        []string{"github", "network-configurations", "hosted-compute", "organization-admin", "list"},
	Risk:        networkReadRisk,
	Provider:    Provider,
	InputSchema: inputSchema(pagingKeys),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"network_configurations":{"type":"array",` +
		`"items":{"type":"object","properties":{` + networkConfigurationProperties + `},"additionalProperties":false}},` +
		`"total_count":{"type":"integer"},` + networkStateProperties + `,"next_cursor":{"type":"string"},` +
		`"has_more":{"type":"boolean"}},"required":["has_more"],"additionalProperties":false}`),
	Arguments: pagingArguments,
	Fields: append(append([]capability.Field{
		{Name: "network_configurations", Description: "Configurations of the organization with the members below; " +
			"a member GitHub leaves out or sends as null is absent"},
		{Name: "total_count", Description: "Number of configurations GitHub reports"},
	}, networkConfigurationItemFields...), append(networkStateFields, pagingFields...)...),
	Examples: []capability.Example{{
		Description: "List the network configurations of an organization",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org"}`),
	}},
}

var networkConfigurationsGet = capability.Descriptor{
	ID:      Provider + ".network-configurations.get",
	Version: 1,
	Title:   "Get a hosted compute network configuration of a GitHub organization",
	Description: "Read one hosted compute network configuration of an organization an explicit connection allows, " +
		"with every member GitHub documents; name and identifiers are untrusted data; needs an organization " +
		"owner and a plan that offers hosted compute networking, otherwise the state is unavailable",
	Tags:        []string{"github", "network-configurations", "hosted-compute", "organization-admin", "get"},
	Risk:        networkReadRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"network_configuration_id":`+networkIDSchema, "network_configuration_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + networkConfigurationProperties +
		`},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "network_configuration_id", Description: "Identifier of the " +
		"configuration, as github.network-configurations.list reports it: letters, digits, hyphen, and " +
		"underscore, at most 128 characters", Required: true}},
	Fields: networkConfigurationFields,
	Examples: []capability.Example{{
		Description: "Read one network configuration",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","network_configuration_id":"123456789ABCDEF"}`),
	}},
}

var networkSettingsGet = capability.Descriptor{
	ID:      Provider + ".network-settings.get",
	Version: 1,
	Title:   "Get a hosted compute network settings resource of a GitHub organization",
	Description: "Read one hosted compute network settings resource of an organization an explicit connection " +
		"allows: its name, the subnet, and the region, as GitHub documents them; every text is untrusted data; " +
		"needs an organization owner and a plan that offers hosted compute networking, otherwise the state is " +
		"unavailable",
	Tags:        []string{"github", "network-configurations", "hosted-compute", "organization-admin", "get"},
	Risk:        networkReadRisk,
	Provider:    Provider,
	InputSchema: inputSchema(`"network_settings_id":`+networkIDSchema, "network_settings_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + networkSettingsProperties +
		`},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "network_settings_id", Description: "Identifier of the network " +
		"settings resource, as the network_settings_ids of github.network-configurations.get report it: " +
		"letters, digits, hyphen, and underscore, at most 128 characters", Required: true}},
	Fields: append([]capability.Field{
		{Name: "id", Description: "Identifier of the network settings resource; untrusted data"},
		{Name: "network_configuration_id", Description: "Identifier of the network configuration that uses this " +
			"resource; untrusted data"},
		{Name: "name", Description: "Name of the network settings resource; untrusted data"},
		{Name: "subnet_id", Description: "The subnet the resource is configured for, as GitHub reports it; " +
			"untrusted data"},
		{Name: "region", Description: "Location of the subnet; untrusted data"},
	}, networkStateFields...),
	Examples: []capability.Example{{
		Description: "Read one network settings resource",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","network_settings_id":"220F78DACB92BBFBC5E6F22DE1CCF52309D"}`),
	}},
}

const networkChangeProperties = `"compute_service":` + networkComputeServiceSchema + `,"network_settings_ids":` +
	networkSettingsIDsSchema + `,"failover_network_settings_ids":` + networkFailoverIDsSchema +
	`,"failover_network_enabled":{"type":"boolean"}`

func networkChangeArguments(nameRequired bool) []capability.Argument {
	return []capability.Argument{
		{Name: "name", Description: "Name of the configuration, 1 to 100 characters of letters, digits, '.', '-', " +
			"and '_'", Required: nameRequired},
		{Name: "compute_service", Description: "none or actions: the hosted compute service the configuration " +
			"supports"},
		{Name: "network_settings_ids", Description: "List of exactly one identifier of an existing network " +
			"settings resource; the tool creates no network resource", Required: nameRequired},
		{Name: "failover_network_settings_ids", Description: "List of at most one identifier of an existing " +
			"failover network settings resource; an empty list clears it"},
		{Name: "failover_network_enabled", Description: "Whether the failover network resource is enabled"},
	}
}

var networkConfigurationsCreate = capability.Descriptor{
	ID:      Provider + ".network-configurations.create",
	Version: 1,
	Title:   "Create a hosted compute network configuration in a GitHub organization",
	Description: "Create one hosted compute network configuration in an organization an explicit connection " +
		"allows, pointing at an existing network settings resource; no network resource is created; not " +
		"idempotent, since a repeated call is refused for the taken name or, under another name, creates a " +
		"second configuration; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "network-configurations", "hosted-compute", "organization-admin", "create"},
	Risk:                  guardedRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent, networkConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"name":`+networkNameSchema+`,`+networkChangeProperties, "name", "network_settings_ids"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + networkConfigurationProperties +
		`},"additionalProperties":false}`),
	Arguments: networkChangeArguments(true),
	Fields:    networkConfigurationFields,
	Examples: []capability.Example{{
		Description: "Create a configuration for Actions runners",
		Arguments: json.RawMessage(`{"owner":"orgs/octo-org","name":"my-network-configuration",` +
			`"compute_service":"actions","network_settings_ids":["23456789ABDCEF1"]}`),
	}},
}

var networkConfigurationsUpdate = capability.Descriptor{
	ID:      Provider + ".network-configurations.update",
	Version: 1,
	Title:   "Update a hosted compute network configuration of a GitHub organization",
	Description: "Change settings of one hosted compute network configuration of an organization an explicit " +
		"connection allows; only the given settings change; runner groups that use the configuration may be " +
		"affected, which the result says; idempotent, since sending the same settings again leaves GitHub in the " +
		"same state; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "network-configurations", "hosted-compute", "organization-admin", "update"},
	Risk:                  guardedRisk(capability.EffectUpdate, capability.IdempotencyIdempotent, networkConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema: inputSchema(`"network_configuration_id":`+networkIDSchema+`,"name":`+networkNameSchema+`,`+
		networkChangeProperties, "network_configuration_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + networkConfigurationProperties +
		`,"note":{"type":"string"}},"additionalProperties":false}`),
	Arguments: append([]capability.Argument{{Name: "network_configuration_id", Description: "Identifier of the " +
		"configuration, as github.network-configurations.list reports it", Required: true}},
		networkChangeArguments(false)...),
	Fields: append(append([]capability.Field{}, networkConfigurationFields...), capability.Field{Name: "note",
		Description: "Fixed note that runner groups using the configuration may be affected by the " +
			"change; absent with state unavailable"}),
	Examples: []capability.Example{{
		Description: "Rename a configuration",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","network_configuration_id":"123456789ABCDEF","name":"renamed"}`),
	}},
}

var networkConfigurationsDelete = capability.Descriptor{
	ID:      Provider + ".network-configurations.delete",
	Version: 1,
	Title:   "Delete a hosted compute network configuration of a GitHub organization",
	Description: "Delete one hosted compute network configuration of an organization an explicit connection " +
		"allows; runner groups that use the configuration may be affected, which the result says; the network " +
		"settings resource itself is not deleted; offered only where a connection's tools list names it",
	Tags:                  []string{"github", "network-configurations", "hosted-compute", "organization-admin", "delete"},
	Risk:                  guardedRisk(capability.EffectDelete, capability.IdempotencyUnknown, networkConfigurationSensitivity),
	Provider:              Provider,
	RequiresToolAllowList: true,
	InputSchema:           inputSchema(`"network_configuration_id":`+networkIDSchema, "network_configuration_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"deleted":{"type":"boolean"},` +
		`"note":{"type":"string"},` + networkStateProperties + `},"required":["deleted"],"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "network_configuration_id", Description: "Identifier of the " +
		"configuration to delete, as github.network-configurations.list reports it", Required: true}},
	Fields: append([]capability.Field{
		{Name: "deleted", Description: "True once GitHub deleted the configuration"},
		{Name: "note", Description: "Fixed note that runner groups using the configuration may be affected by the " +
			"deletion; absent with state unavailable"},
	}, networkStateFields...),
	Examples: []capability.Example{{
		Description: "Delete a configuration",
		Arguments:   json.RawMessage(`{"owner":"orgs/octo-org","network_configuration_id":"123456789ABCDEF"}`),
	}},
}

// networkConfigurationToolIDs are the six network configuration tools; every one names an organization as its
// owner target, and the three that change something are offered only where a connection's tools list names
// them.
var networkConfigurationToolIDs = []string{networkConfigurationsList.ID, networkConfigurationsGet.ID,
	networkSettingsGet.ID, networkConfigurationsCreate.ID, networkConfigurationsUpdate.ID,
	networkConfigurationsDelete.ID}

// ---- Results

// NetworkConfiguration is GitHub's hosted compute network configuration with every member it documents, or the
// state unavailable. The text members are untrusted data.
type NetworkConfiguration struct {
	ID                                  string   `json:"id,omitempty"`
	Name                                string   `json:"name,omitempty"`
	ComputeService                      *string  `json:"compute_service,omitempty"`
	NetworkSettingsIDs                  []string `json:"network_settings_ids,omitempty"`
	NetworkSettingsIDsTruncated         bool     `json:"network_settings_ids_truncated,omitempty"`
	FailoverNetworkSettingsIDs          []string `json:"failover_network_settings_ids,omitempty"`
	FailoverNetworkSettingsIDsTruncated bool     `json:"failover_network_settings_ids_truncated,omitempty"`
	FailoverNetworkEnabled              *bool    `json:"failover_network_enabled,omitempty"`
	CreatedOn                           *string  `json:"created_on,omitempty"`
	State                               string   `json:"state,omitempty"`
	Reason                              string   `json:"reason,omitempty"`
}

// NetworkConfigurationList is one batch of the network configurations of an organization, or the state
// unavailable.
type NetworkConfigurationList struct {
	Configurations []NetworkConfiguration `json:"network_configurations,omitempty"`
	TotalCount     *int64                 `json:"total_count,omitempty"`
	State          string                 `json:"state,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	NextCursor     string                 `json:"next_cursor,omitempty"`
	HasMore        bool                   `json:"has_more"`
}

// NetworkConfigurationUpdated is the configuration after an update with the fixed note about runner groups.
type NetworkConfigurationUpdated struct {
	NetworkConfiguration
	Note string `json:"note,omitempty"`
}

// NetworkConfigurationDeleted is the answer to a delete, or the state unavailable.
type NetworkConfigurationDeleted struct {
	Deleted bool   `json:"deleted"`
	Note    string `json:"note,omitempty"`
	State   string `json:"state,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

// NetworkSettings is GitHub's hosted compute network settings resource, or the state unavailable.
type NetworkSettings struct {
	ID                     string  `json:"id,omitempty"`
	NetworkConfigurationID *string `json:"network_configuration_id,omitempty"`
	Name                   string  `json:"name,omitempty"`
	SubnetID               *string `json:"subnet_id,omitempty"`
	Region                 *string `json:"region,omitempty"`
	State                  string  `json:"state,omitempty"`
	Reason                 string  `json:"reason,omitempty"`
}

// ---- Raw answers

type networkConfigurationRaw struct {
	ID                         *string  `json:"id"`
	Name                       *string  `json:"name"`
	ComputeService             *string  `json:"compute_service"`
	NetworkSettingsIDs         []string `json:"network_settings_ids"`
	FailoverNetworkSettingsIDs []string `json:"failover_network_settings_ids"`
	FailoverNetworkEnabled     *bool    `json:"failover_network_enabled"`
	CreatedOn                  *string  `json:"created_on"`
}

// networkIDs cleans a list of identifiers: an empty entry is dropped and the list is cut at the shared bound.
func networkIDs(ids []string) ([]string, bool) {
	var out []string
	for _, id := range ids {
		if id == "" {
			continue
		}
		if len(out) == projectionListLimit {
			return out, true
		}
		text := id
		out = append(out, *nameText(&text))
	}
	return out, false
}

func (r *networkConfigurationRaw) project(op string) (*NetworkConfiguration, error) {
	id, name := filled(r.ID), filled(r.Name)
	if id == nil || name == nil {
		return nil, invalidEntry(op, "a network configuration")
	}
	out := &NetworkConfiguration{ID: *nameText(id), Name: *nameText(name), ComputeService: shortText(r.ComputeService),
		FailoverNetworkEnabled: r.FailoverNetworkEnabled, CreatedOn: shortText(r.CreatedOn)}
	out.NetworkSettingsIDs, out.NetworkSettingsIDsTruncated = networkIDs(r.NetworkSettingsIDs)
	out.FailoverNetworkSettingsIDs, out.FailoverNetworkSettingsIDsTruncated = networkIDs(r.FailoverNetworkSettingsIDs)
	return out, nil
}

// ---- Arguments and checks

// networkArguments holds the arguments of the six tools; the input schema of each admits only its own. The
// pointers tell a setting left out from one sent, and the body is exactly what was checked.
type networkArguments struct {
	NetworkConfigurationID string `json:"network_configuration_id"`
	NetworkSettingsID      string `json:"network_settings_id"`
	Limit                  int    `json:"limit"`
	Cursor                 string `json:"cursor"`

	networkBody

	page, perPage int
	binding       []byte
}

// networkBody is the request body of create and update.
type networkBody struct {
	Name                       *string   `json:"name,omitempty"`
	ComputeService             *string   `json:"compute_service,omitempty"`
	NetworkSettingsIDs         *[]string `json:"network_settings_ids,omitempty"`
	FailoverNetworkSettingsIDs *[]string `json:"failover_network_settings_ids,omitempty"`
	FailoverNetworkEnabled     *bool     `json:"failover_network_enabled,omitempty"`
}

func checkNetworkID(name, value string) error {
	if !networkIDPattern.MatchString(value) {
		return invalidRequest(name + " must be an identifier of 1 to " + strconv.Itoa(networkIDMax) +
			" letters, digits, hyphens, or underscores")
	}
	return nil
}

func checkNetworkConfigurationID(a *networkArguments, _ target) error {
	return checkNetworkID("network_configuration_id", a.NetworkConfigurationID)
}

func checkNetworkSettingsID(a *networkArguments, _ target) error {
	return checkNetworkID("network_settings_id", a.NetworkSettingsID)
}

func checkNetworkConfigurationsList(a *networkArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	a.binding = fingerprint("network-configurations", strings.ToLower(bound.String()))
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

// check validates every value of a body before any request. Values are matched exactly, as GitHub documents
// them.
func (b *networkBody) check() error {
	if b.Name != nil && !networkNamePattern.MatchString(*b.Name) {
		return invalidRequest("name must be 1 to " + strconv.Itoa(networkNameMax) +
			" characters of letters, digits, '.', '-', and '_'")
	}
	if b.ComputeService != nil && !containsExact(networkComputeServices, *b.ComputeService) {
		return invalidRequest("compute_service must be none or actions")
	}
	if b.NetworkSettingsIDs != nil {
		if len(*b.NetworkSettingsIDs) != 1 {
			return invalidRequest("network_settings_ids must list exactly one identifier")
		}
		if err := checkNetworkID("network_settings_ids", (*b.NetworkSettingsIDs)[0]); err != nil {
			return err
		}
	}
	if b.FailoverNetworkSettingsIDs != nil {
		if len(*b.FailoverNetworkSettingsIDs) > 1 {
			return invalidRequest("failover_network_settings_ids must list at most one identifier")
		}
		for _, id := range *b.FailoverNetworkSettingsIDs {
			if err := checkNetworkID("failover_network_settings_ids", id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (b *networkBody) empty() bool {
	return b.Name == nil && b.ComputeService == nil && b.NetworkSettingsIDs == nil &&
		b.FailoverNetworkSettingsIDs == nil && b.FailoverNetworkEnabled == nil
}

func checkNetworkConfigurationsCreate(a *networkArguments, _ target) error {
	if a.NetworkConfigurationID != "" || a.NetworkSettingsID != "" {
		return invalidRequest("create takes no identifier of a configuration")
	}
	if a.Name == nil || a.NetworkSettingsIDs == nil {
		return invalidRequest("name and network_settings_ids are required")
	}
	return a.check()
}

func checkNetworkConfigurationsUpdate(a *networkArguments, bound target) error {
	if err := checkNetworkConfigurationID(a, bound); err != nil {
		return err
	}
	if a.empty() {
		return invalidRequest("at least one setting must be given")
	}
	return a.check()
}

// more reports whether the list continues after the page just read and returns the cursor of the next one.
// A short page ends the list whatever the reported total says.
func (a *networkArguments) more(got int, total int64) (bool, string) {
	if got < a.perPage || int64(a.page*a.perPage) >= total || a.page >= maxPage {
		return false, ""
	}
	return true, encodeCursor(a.binding, strconv.Itoa(a.page+1)+":"+strconv.Itoa(a.perPage))
}

func networkConfigurationOperations() []capability.Operation {
	bind := func(descriptor capability.Descriptor, check func(*networkArguments, target) error,
		call func(context.Context, *Client, *networkArguments) (any, error)) capability.Operation {
		return capability.Operation{Descriptor: descriptor, Handler: networkHandler(descriptor.ID, check, call)}
	}
	return []capability.Operation{
		bind(networkConfigurationsList, checkNetworkConfigurationsList,
			func(ctx context.Context, c *Client, a *networkArguments) (any, error) {
				return c.listNetworkConfigurations(ctx, a)
			}),
		bind(networkConfigurationsGet, checkNetworkConfigurationID,
			func(ctx context.Context, c *Client, a *networkArguments) (any, error) {
				return c.getNetworkConfiguration(ctx, a)
			}),
		bind(networkSettingsGet, checkNetworkSettingsID,
			func(ctx context.Context, c *Client, a *networkArguments) (any, error) {
				return c.getNetworkSettings(ctx, a)
			}),
		bind(networkConfigurationsCreate, checkNetworkConfigurationsCreate,
			func(ctx context.Context, c *Client, a *networkArguments) (any, error) {
				return c.createNetworkConfiguration(ctx, a)
			}),
		bind(networkConfigurationsUpdate, checkNetworkConfigurationsUpdate,
			func(ctx context.Context, c *Client, a *networkArguments) (any, error) {
				return c.updateNetworkConfiguration(ctx, a)
			}),
		bind(networkConfigurationsDelete, checkNetworkConfigurationID,
			func(ctx context.Context, c *Client, a *networkArguments) (any, error) {
				return c.deleteNetworkConfiguration(ctx, a)
			}),
	}
}

// networkHandler decodes and checks the arguments and the organization before a credential is resolved, so a
// refused request never becomes a provider call or a secret read. The owner must be an organization the targets
// name as an owner and no exclusion covers.
func networkHandler(id string, check func(*networkArguments, target) error,
	call func(context.Context, *Client, *networkArguments) (any, error)) capability.Handler {
	return func(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
		raw json.RawMessage) (any, error) {
		var arguments networkArguments
		if err := json.Unmarshal(raw, &arguments); err != nil {
			return nil, unreadable(id)
		}
		bound, err := selectOrganization(resolved, raw)
		if err != nil {
			return nil, err
		}
		if err := check(&arguments, bound); err != nil {
			return nil, err
		}
		client, err := openAt(ctx, resolved, secrets, red, bound)
		if err != nil {
			return nil, err
		}
		return bound.locate(call(ctx, client, &arguments))
	}
}

// ---- Requests

func (c *Client) networkConfigurationPath(id string) string {
	path := "settings/network-configurations"
	if id != "" {
		path += "/" + url.PathEscape(id)
	}
	return c.orgPath(path)
}

// networkRefused reports a refusal GitHub answers with 403 or 404: a plan without the feature, a missing
// right, a user who is no owner, or an unknown organization or identifier.
func networkRefused(err error) bool {
	var failure *provider.Error
	return errors.As(err, &failure) && (failure.Class == provider.ClassPermission || failure.Class == provider.ClassNotFound)
}

// networkChangeFailure renames the refusals of a change to what they mean for the request; GitHub's text
// stays apart, in Original.
func networkChangeFailure(err error) error {
	var failure *provider.Error
	if !errors.As(err, &failure) || failure.Original == nil {
		return err
	}
	message := ""
	switch failure.Original.Status {
	case http.StatusConflict:
		message = networkConflict
	case http.StatusUnprocessableEntity:
		message = networkRejected
	}
	if message == "" {
		return err
	}
	renamed := *failure
	renamed.Message = message
	return &renamed
}

func (c *Client) listNetworkConfigurations(ctx context.Context, a *networkArguments) (*NetworkConfigurationList, error) {
	const op = "list network configurations"
	var raw struct {
		TotalCount            *int64                    `json:"total_count"`
		NetworkConfigurations []networkConfigurationRaw `json:"network_configurations"`
	}
	query := url.Values{"per_page": {strconv.Itoa(a.perPage)}, "page": {strconv.Itoa(a.page)}}
	if _, err := c.restPage(ctx, op, c.networkConfigurationPath(""), query, &raw); err != nil {
		if networkRefused(err) {
			return &NetworkConfigurationList{State: networkUnavailableState, Reason: networkUnavailableReason}, nil
		}
		return nil, err
	}
	if raw.TotalCount == nil || *raw.TotalCount < 0 {
		return nil, invalidResponse(op, false)
	}
	result := &NetworkConfigurationList{TotalCount: raw.TotalCount,
		Configurations: make([]NetworkConfiguration, 0, len(raw.NetworkConfigurations))}
	for i := range raw.NetworkConfigurations {
		configuration, err := raw.NetworkConfigurations[i].project(op)
		if err != nil {
			return nil, err
		}
		result.Configurations = append(result.Configurations, *configuration)
	}
	result.HasMore, result.NextCursor = a.more(len(raw.NetworkConfigurations), *raw.TotalCount)
	return result, nil
}

func (c *Client) getNetworkConfiguration(ctx context.Context, a *networkArguments) (*NetworkConfiguration, error) {
	const op = "get network configuration"
	var raw networkConfigurationRaw
	if err := c.rest(ctx, op, c.networkConfigurationPath(a.NetworkConfigurationID), &raw); err != nil {
		if networkRefused(err) {
			return &NetworkConfiguration{State: networkUnavailableState, Reason: networkUnavailableReason}, nil
		}
		return nil, err
	}
	return raw.project(op)
}

func (c *Client) getNetworkSettings(ctx context.Context, a *networkArguments) (*NetworkSettings, error) {
	const op = "get network settings"
	var raw struct {
		ID                     *string `json:"id"`
		NetworkConfigurationID *string `json:"network_configuration_id"`
		Name                   *string `json:"name"`
		SubnetID               *string `json:"subnet_id"`
		Region                 *string `json:"region"`
	}
	path := c.orgPath("settings/network-settings/" + url.PathEscape(a.NetworkSettingsID))
	if err := c.rest(ctx, op, path, &raw); err != nil {
		if networkRefused(err) {
			return &NetworkSettings{State: networkUnavailableState, Reason: networkUnavailableReason}, nil
		}
		return nil, err
	}
	id, name := filled(raw.ID), filled(raw.Name)
	if id == nil || name == nil {
		return nil, invalidEntry(op, "a network settings resource")
	}
	return &NetworkSettings{ID: *nameText(id), Name: *nameText(name),
		NetworkConfigurationID: shortText(raw.NetworkConfigurationID), SubnetID: shortText(raw.SubnetID),
		Region: shortText(raw.Region)}, nil
}

func (c *Client) createNetworkConfiguration(ctx context.Context, a *networkArguments) (*NetworkConfiguration, error) {
	const op = "create network configuration"
	var raw networkConfigurationRaw
	if err := c.restChange(ctx, op, http.MethodPost, c.networkConfigurationPath(""), &a.networkBody, &raw); err != nil {
		if networkRefused(err) {
			return &NetworkConfiguration{State: networkUnavailableState, Reason: networkUnavailableChangeReason}, nil
		}
		return nil, networkChangeFailure(err)
	}
	return raw.project(op)
}

func (c *Client) updateNetworkConfiguration(ctx context.Context, a *networkArguments) (*NetworkConfigurationUpdated, error) {
	const op = "update network configuration"
	var raw networkConfigurationRaw
	if err := c.restChange(ctx, op, http.MethodPatch, c.networkConfigurationPath(a.NetworkConfigurationID),
		&a.networkBody, &raw); err != nil {
		if networkRefused(err) {
			return &NetworkConfigurationUpdated{NetworkConfiguration: NetworkConfiguration{
				State: networkUnavailableState, Reason: networkUnavailableChangeReason}}, nil
		}
		return nil, networkChangeFailure(err)
	}
	configuration, err := raw.project(op)
	if err != nil {
		return nil, err
	}
	return &NetworkConfigurationUpdated{NetworkConfiguration: *configuration, Note: networkRunnerGroupNote}, nil
}

func (c *Client) deleteNetworkConfiguration(ctx context.Context, a *networkArguments) (*NetworkConfigurationDeleted, error) {
	const op = "delete network configuration"
	if err := c.restChange(ctx, op, http.MethodDelete, c.networkConfigurationPath(a.NetworkConfigurationID),
		struct{}{}, nil); err != nil {
		if networkRefused(err) {
			return &NetworkConfigurationDeleted{State: networkUnavailableState, Reason: networkUnavailableChangeReason}, nil
		}
		return nil, networkChangeFailure(err)
	}
	return &NetworkConfigurationDeleted{Deleted: true, Note: networkRunnerGroupNote}, nil
}

// networkSubject names what a path below an organization's settings addresses: the network configurations,
// one configuration, or one network settings resource. It names an identifier only when it matches the
// pattern the input schema allows.
func networkSubject(rest []string) string {
	if len(rest) == 0 {
		return ""
	}
	one := func(what string) string {
		if len(rest) >= 2 && networkIDPattern.MatchString(rest[1]) {
			return what + " " + rest[1]
		}
		return ""
	}
	switch rest[0] {
	case "network-configurations":
		if len(rest) == 1 || rest[1] == "" {
			return "the network configurations"
		}
		if what := one("network configuration"); what != "" {
			return what
		}
		return "the network configurations"
	case "network-settings":
		if what := one("network settings"); what != "" {
			return what
		}
		return "the network settings"
	}
	return ""
}

// networkConfigurationChangeIDs are the three tools that change a network configuration.
var networkConfigurationChangeIDs = []string{networkConfigurationsCreate.ID, networkConfigurationsUpdate.ID,
	networkConfigurationsDelete.ID}

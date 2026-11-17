package googlecloud

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
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
	quotaInfosListID        = Provider + ".quota-infos.list"
	quotaInfosGetID         = Provider + ".quota-infos.get"
	quotaPreferencesListID  = Provider + ".quota-preferences.list"
	quotaPreferencesGetID   = Provider + ".quota-preferences.get"
	quotaPreferenceDataKind = "cloud-quota-contacts"
)

// The location of Cloud Quotas is always global for a project; no tool takes it.
const quotaLocation = "/locations/global"

var (
	quotaIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	// quotaTimePattern is the timestamp form of Google's examples for creation_time, with an optional Z.
	quotaTimePattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z?$`)
	quotaOrderFields = []string{"quota_id", "service", "create_time"}
	quotaOrigins     = []string{"CLOUD_CONSOLE", "AUTO_ADJUSTER"}
)

// quotaPreferenceRisk differs from readRisk in its sensitivity only: a preference may hold the email address
// of a person and a free-text justification.
var quotaPreferenceRisk = func() capability.Risk {
	r := readRisk
	r.DataSensitivity = quotaPreferenceDataKind
	return r
}()

// --- wire and result types ---

type quotaDetailsWire struct {
	Value       string `json:"value"`
	RolloutInfo *struct {
		OngoingRollout bool `json:"ongoingRollout"`
	} `json:"rolloutInfo"`
}

type dimensionsInfoWire struct {
	Details             *quotaDetailsWire `json:"details"`
	ApplicableLocations []string          `json:"applicableLocations"`
	Dimensions          map[string]string `json:"dimensions"`
}

type quotaInfoWire struct {
	Name                     string               `json:"name"`
	QuotaID                  string               `json:"quotaId"`
	Metric                   string               `json:"metric"`
	Service                  string               `json:"service"`
	IsPrecise                bool                 `json:"isPrecise"`
	RefreshInterval          string               `json:"refreshInterval"`
	ContainerType            string               `json:"containerType"`
	Dimensions               []string             `json:"dimensions"`
	MetricDisplayName        string               `json:"metricDisplayName"`
	QuotaDisplayName         string               `json:"quotaDisplayName"`
	MetricUnit               string               `json:"metricUnit"`
	IsFixed                  bool                 `json:"isFixed"`
	DimensionsInfos          []dimensionsInfoWire `json:"dimensionsInfos"`
	IsConcurrent             bool                 `json:"isConcurrent"`
	ServiceRequestQuotaURI   string               `json:"serviceRequestQuotaUri"`
	QuotaIncreaseEligibility *struct {
		IsEligible          bool   `json:"isEligible"`
		IneligibilityReason string `json:"ineligibilityReason"`
	} `json:"quotaIncreaseEligibility"`
}

type quotaDetailsResult struct {
	Value          string `json:"value,omitempty"`
	OngoingRollout *bool  `json:"ongoingRollout,omitempty"`
}

type dimensionsInfoResult struct {
	Details             *quotaDetailsResult `json:"details,omitempty"`
	ApplicableLocations []string            `json:"applicableLocations,omitempty"`
	Dimensions          map[string]string   `json:"dimensions,omitempty"`
}

type quotaEligibilityResult struct {
	IsEligible          bool   `json:"isEligible"`
	IneligibilityReason string `json:"ineligibilityReason,omitempty"`
}

type quotaInfoResult struct {
	Name                     string                  `json:"name"`
	QuotaID                  string                  `json:"quotaId,omitempty"`
	Metric                   string                  `json:"metric,omitempty"`
	Service                  string                  `json:"service,omitempty"`
	IsPrecise                bool                    `json:"isPrecise,omitempty"`
	RefreshInterval          string                  `json:"refreshInterval,omitempty"`
	ContainerType            string                  `json:"containerType,omitempty"`
	Dimensions               []string                `json:"dimensions,omitempty"`
	MetricDisplayName        string                  `json:"metricDisplayName,omitempty"`
	QuotaDisplayName         string                  `json:"quotaDisplayName,omitempty"`
	MetricUnit               string                  `json:"metricUnit,omitempty"`
	QuotaIncreaseEligibility *quotaEligibilityResult `json:"quotaIncreaseEligibility,omitempty"`
	IsFixed                  bool                    `json:"isFixed,omitempty"`
	DimensionsInfos          []dimensionsInfoResult  `json:"dimensionsInfos,omitempty"`
	IsConcurrent             bool                    `json:"isConcurrent,omitempty"`
	ServiceRequestQuotaURI   string                  `json:"serviceRequestQuotaUri,omitempty"`
}

func (q quotaInfoWire) result() quotaInfoResult {
	out := quotaInfoResult{Name: boundedText(q.Name, maxText), QuotaID: boundedText(q.QuotaID, maxNameText),
		Metric: boundedText(q.Metric, maxNameText), Service: boundedText(q.Service, maxNameText),
		IsPrecise: q.IsPrecise, RefreshInterval: boundedText(q.RefreshInterval, maxNameText),
		ContainerType: boundedText(q.ContainerType, maxNameText), Dimensions: boundedList(q.Dimensions),
		MetricDisplayName: boundedText(q.MetricDisplayName, maxNameText),
		QuotaDisplayName:  boundedText(q.QuotaDisplayName, maxNameText),
		MetricUnit:        boundedText(q.MetricUnit, maxNameText), IsFixed: q.IsFixed, IsConcurrent: q.IsConcurrent,
		ServiceRequestQuotaURI: boundedText(q.ServiceRequestQuotaURI, maxText)}
	if e := q.QuotaIncreaseEligibility; e != nil {
		out.QuotaIncreaseEligibility = &quotaEligibilityResult{IsEligible: e.IsEligible,
			IneligibilityReason: boundedText(e.IneligibilityReason, maxNameText)}
	}
	for _, info := range q.DimensionsInfos[:min(len(q.DimensionsInfos), maxListItems)] {
		item := dimensionsInfoResult{ApplicableLocations: boundedList(info.ApplicableLocations),
			Dimensions: boundedMap(info.Dimensions)}
		if d := info.Details; d != nil {
			item.Details = &quotaDetailsResult{Value: boundedText(d.Value, maxNameText)}
			if d.RolloutInfo != nil {
				item.Details.OngoingRollout = &d.RolloutInfo.OngoingRollout
			}
		}
		out.DimensionsInfos = append(out.DimensionsInfos, item)
	}
	return out
}

type quotaConfigWire struct {
	PreferredValue string            `json:"preferredValue"`
	StateDetail    string            `json:"stateDetail"`
	GrantedValue   string            `json:"grantedValue"`
	TraceID        string            `json:"traceId"`
	RequestOrigin  string            `json:"requestOrigin"`
	Annotations    map[string]string `json:"annotations"`
}

type quotaConfigResult struct {
	PreferredValue string            `json:"preferredValue,omitempty"`
	StateDetail    string            `json:"stateDetail,omitempty"`
	GrantedValue   string            `json:"grantedValue,omitempty"`
	TraceID        string            `json:"traceId,omitempty"`
	RequestOrigin  string            `json:"requestOrigin,omitempty"`
	Annotations    map[string]string `json:"annotations,omitempty"`
}

type quotaPreferenceWire struct {
	Name          string            `json:"name"`
	Dimensions    map[string]string `json:"dimensions"`
	QuotaConfig   *quotaConfigWire  `json:"quotaConfig"`
	Etag          string            `json:"etag"`
	CreateTime    string            `json:"createTime"`
	UpdateTime    string            `json:"updateTime"`
	Service       string            `json:"service"`
	QuotaID       string            `json:"quotaId"`
	Reconciling   bool              `json:"reconciling"`
	Justification string            `json:"justification"`
	ContactEmail  string            `json:"contactEmail"`
}

type quotaPreferenceResult struct {
	Name          string             `json:"name"`
	Dimensions    map[string]string  `json:"dimensions,omitempty"`
	QuotaConfig   *quotaConfigResult `json:"quotaConfig,omitempty"`
	Etag          string             `json:"etag,omitempty"`
	CreateTime    string             `json:"createTime,omitempty"`
	UpdateTime    string             `json:"updateTime,omitempty"`
	Service       string             `json:"service,omitempty"`
	QuotaID       string             `json:"quotaId,omitempty"`
	Reconciling   bool               `json:"reconciling,omitempty"`
	Justification string             `json:"justification,omitempty"`
	ContactEmail  string             `json:"contactEmail,omitempty"`
}

func (p quotaPreferenceWire) result() quotaPreferenceResult {
	out := quotaPreferenceResult{Name: boundedText(p.Name, maxText), Dimensions: boundedMap(p.Dimensions),
		Etag: boundedText(p.Etag, maxNameText), CreateTime: boundedText(p.CreateTime, maxNameText),
		UpdateTime: boundedText(p.UpdateTime, maxNameText), Service: boundedText(p.Service, maxNameText),
		QuotaID: boundedText(p.QuotaID, maxNameText), Reconciling: p.Reconciling,
		Justification: boundedText(p.Justification, maxText), ContactEmail: boundedText(p.ContactEmail, maxNameText)}
	if c := p.QuotaConfig; c != nil {
		out.QuotaConfig = &quotaConfigResult{PreferredValue: boundedText(c.PreferredValue, maxNameText),
			StateDetail: boundedText(c.StateDetail, maxText), GrantedValue: boundedText(c.GrantedValue, maxNameText),
			TraceID: boundedText(c.TraceID, maxNameText), RequestOrigin: boundedText(c.RequestOrigin, maxNameText),
			Annotations: boundedMap(c.Annotations)}
	}
	return out
}

// --- row checks ---

// quotaInfoRowValid checks the name of a row against the service asked: the project segment is not compared,
// because Google names a project by its number.
func quotaInfoRowValid(name, service string) bool {
	rest, ok := strings.CutPrefix(name, prefixProject)
	if !ok || len(name) > maxNameText {
		return false
	}
	_, after, found := strings.Cut(rest, quotaLocation+"/services/"+service+"/quotaInfos/")
	project, _, _ := strings.Cut(rest, "/")
	return found && project != "" && after != "" && !strings.Contains(after, "/")
}

func quotaPreferenceRowValid(name string) bool {
	rest, ok := strings.CutPrefix(name, prefixProject)
	if !ok || len(name) > maxNameText {
		return false
	}
	project, tail, _ := strings.Cut(rest, "/")
	id, found := strings.CutPrefix(tail, "locations/global/quotaPreferences/")
	return project != "" && found && id != "" && !strings.Contains(id, "/")
}

// --- shared descriptor parts ---

const (
	quotaIDProp = `{"type":"string","minLength":1,"maxLength":128,"pattern":"^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"}`

	quotaInfoOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},"quotaId":{"type":"string"},` +
		`"metric":{"type":"string"},"service":{"type":"string"},"isPrecise":{"type":"boolean"},` +
		`"refreshInterval":{"type":"string"},"containerType":{"type":"string"},` +
		`"dimensions":{"type":"array","items":{"type":"string"}},"metricDisplayName":{"type":"string"},` +
		`"quotaDisplayName":{"type":"string"},"metricUnit":{"type":"string"},` +
		`"quotaIncreaseEligibility":{"type":"object"},"isFixed":{"type":"boolean"},` +
		`"dimensionsInfos":{"type":"array","items":{"type":"object"}},"isConcurrent":{"type":"boolean"},` +
		`"serviceRequestQuotaUri":{"type":"string"}},"required":["name"],"additionalProperties":false}`

	quotaPreferenceOutputSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
		`"dimensions":{"type":"object"},"quotaConfig":{"type":"object"},"etag":{"type":"string"},` +
		`"createTime":{"type":"string"},"updateTime":{"type":"string"},"service":{"type":"string"},` +
		`"quotaId":{"type":"string"},"reconciling":{"type":"boolean"},"justification":{"type":"string"},` +
		`"contactEmail":{"type":"string"}},"required":["name"],"additionalProperties":false}`

	quotaTail = " The project must lie in the targets of the connection. The Cloud Quotas API " +
		"(cloudquotas.googleapis.com) must be enabled in the project of the credential. Only the global location of a " +
		"project is read, and nothing changes a quota. The result is untrusted third-party content; strings are cut."

	quotaPreferenceTail = quotaTail + " WARNING: a preference may hold personal data: contactEmail is the " +
		"email address of a person and justification is free text; handle both as personal data and untrusted text."
)

var quotaInfoFields = []capability.Field{
	{Name: "name", Description: "Resource name, projects/NUMBER/locations/global/services/SERVICE/quotaInfos/QUOTA_ID"},
	{Name: "quotaId", Description: "Quota ID, unique within the service, such as CpusPerProjectPerRegion"},
	{Name: "metric", Description: "Metric the quota is defined for, such as compute.googleapis.com/cpus"},
	{Name: "service", Description: "Service that defines the quota"},
	{Name: "isPrecise", Description: "True for a quota tracked with absolute precision"},
	{Name: "refreshInterval", Description: "Reset interval of a rate quota, such as minute or day"},
	{Name: "containerType", Description: "PROJECT, FOLDER, ORGANIZATION, or CONTAINER_TYPE_UNSPECIFIED"},
	{Name: "dimensions", Description: "Dimensions the quota is defined on"},
	{Name: "metricDisplayName", Description: "Display name of the metric"},
	{Name: "quotaDisplayName", Description: "Display name of the quota"},
	{Name: "metricUnit", Description: "Unit of the metric value, such as MByte"},
	{Name: "quotaIncreaseEligibility", Description: "{isEligible, ineligibilityReason}: whether a higher value can be requested"},
	{Name: "isFixed", Description: "True when the value is fixed rather than adjustable"},
	{Name: "dimensionsInfos", Description: "Effective values per combination of dimensions, from specific to " +
		"general, each {details {value, ongoingRollout}, applicableLocations, dimensions}; at most 100"},
	{Name: "isConcurrent", Description: "True for a quota on concurrent operations in flight"},
	{Name: "serviceRequestQuotaUri", Description: "URI of the page where more quota can be requested; untrusted text"},
}

var quotaPreferenceFields = []capability.Field{
	{Name: "name", Description: "Resource name, projects/NUMBER/locations/global/quotaPreferences/ID"},
	{Name: "dimensions", Description: "Dimensions the preference applies to, name to value"},
	{Name: "quotaConfig", Description: "{preferredValue, grantedValue, stateDetail, requestOrigin, traceId, " +
		"annotations}: the preferred and the granted value (int64 as text, -1 means unlimited); annotations are " +
		"untrusted text"},
	{Name: "etag", Description: "Current etag of the preference"},
	{Name: "createTime", Description: "Creation time (RFC 3339)"},
	{Name: "updateTime", Description: "Update time (RFC 3339)"},
	{Name: "service", Description: "Service the preference applies to"},
	{Name: "quotaId", Description: "Quota the preference applies to"},
	{Name: "reconciling", Description: "True while the preference awaits approval and fulfillment"},
	{Name: "justification", Description: "Reason given for the preference; free text of a person, personal data, cut"},
	{Name: "contactEmail", Description: "Email address to contact the requester; personal data, cut. Google " +
		"documents it as input only, so it is usually absent"},
}

var (
	quotaServiceArgument = capability.Argument{Name: "service", Required: true,
		Description: "Service name such as compute.googleapis.com; never a projects/ name or a path"}
	quotaIDArgument = capability.Argument{Name: "quota_id", Required: true,
		Description: "Quota ID such as CpusPerProjectPerRegion, as quota-infos.list returns it; never a path"}
	preferenceIDArgument = capability.Argument{Name: "preference_id", Required: true,
		Description: "ID of the preference, the last segment of its name; never a resource name or a path"}
)

// --- quota-infos.list ---

var quotaInfosList = capability.Descriptor{
	ID: quotaInfosListID, Version: 1, Title: "List the quotas of a Google API in a Cloud project",
	Description: "List the quotas that one service (Google API) defines for a project from Cloud Quotas, with " +
		"their effective values per dimension." + quotaTail,
	Tags: []string{"google", "cloud", "quotas"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"service":` +
		serviceNameProp + `,` + pageSizeProp + `,` + cursorProp +
		`},"required":["project_id","service"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("quotaInfos", quotaInfoOutputSchema),
	Arguments:    []capability.Argument{serviceProjectArgument, quotaServiceArgument, pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{{Name: "quotaInfos",
		Description: "Quotas of the page, each as quota-infos.get returns it"}}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the quotas of Compute Engine",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","service":"compute.googleapis.com"}`),
	}},
}

type quotaInfosAnswer struct {
	QuotaInfos    []json.RawMessage `json:"quotaInfos"`
	NextPageToken string            `json:"nextPageToken"`
}

func handleQuotaInfosList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list quota infos"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Service   string `json:"service"`
		PageSize  *int   `json:"page_size"`
		Cursor    string `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !validServiceName(arguments.Service) {
		return nil, badRequest("service must be a service name such as compute.googleapis.com")
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	binding := provider.CursorBinding(quotaInfosListID, provider.TargetsOf(resolved),
		arguments.ProjectID+"|"+arguments.Service)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := serviceClient(ctx, op, quotaInfosListID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var answer quotaInfosAnswer
	path := "/v1/" + prefixProject + arguments.ProjectID + quotaLocation + "/services/" + arguments.Service + "/quotaInfos"
	if err := c.get(ctx, op, quotaInfosListID, serviceQuotas, path, query, &answer); err != nil {
		return nil, err
	}
	if len(answer.QuotaInfos) > 2*maxPageSize || len(answer.NextPageToken) > maxPageTokenBytes {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]quotaInfoResult, 0, len(answer.QuotaInfos))
	for _, item := range answer.QuotaInfos {
		var row quotaInfoWire
		if err := json.Unmarshal(item, &row); err != nil || !quotaInfoRowValid(row.Name, arguments.Service) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row.result())
	}
	out := map[string]any{"quotaInfos": rows, "has_more": answer.NextPageToken != ""}
	if answer.NextPageToken != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, answer.NextPageToken)
	}
	return out, nil
}

// --- quota-infos.get ---

var quotaInfosGet = capability.Descriptor{
	ID: quotaInfosGetID, Version: 1, Title: "Get one quota of a Google API in a Cloud project",
	Description: "Read one quota of a service in a project from Cloud Quotas, by the service name and the quota ID." +
		quotaTail,
	Tags: []string{"google", "cloud", "quotas"}, Risk: readRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"service":` +
		serviceNameProp + `,"quota_id":` + quotaIDProp +
		`},"required":["project_id","service","quota_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(quotaInfoOutputSchema),
	Arguments:    []capability.Argument{serviceProjectArgument, quotaServiceArgument, quotaIDArgument},
	Fields:       quotaInfoFields,
	Examples: []capability.Example{{
		Description: "Read one quota",
		Arguments: json.RawMessage(`{"project_id":"my-project-123","service":"compute.googleapis.com",` +
			`"quota_id":"CPUS-per-project-region"}`),
	}},
}

func handleQuotaInfosGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get quota info"
	var arguments struct {
		ProjectID string `json:"project_id"`
		Service   string `json:"service"`
		QuotaID   string `json:"quota_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !validServiceName(arguments.Service) {
		return nil, badRequest("service must be a service name such as compute.googleapis.com")
	}
	if !quotaIDPattern.MatchString(arguments.QuotaID) {
		return nil, badRequest("quota_id must be a quota ID such as CpusPerProjectPerRegion, never a path")
	}
	c, err := serviceClient(ctx, op, quotaInfosGetID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := "/v1/" + prefixProject + arguments.ProjectID + quotaLocation + "/services/" + arguments.Service +
		"/quotaInfos/" + arguments.QuotaID
	var row quotaInfoWire
	if err := c.get(ctx, op, quotaInfosGetID, serviceQuotas, path, nil, &row); err != nil {
		return nil, err
	}
	if !quotaInfoRowValid(row.Name, arguments.Service) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row.result(), nil
}

// --- quota-preferences.list ---

// preferenceFilter builds the filter of quotaPreferences.list from its structured arguments. Google documents
// the fields reconciling, request_type, and creation_time with = and the comparison operators, joined by AND
// or OR; no argument can add another field, operator, or clause.
type preferenceFilter struct {
	Reconciling   *bool  `json:"reconciling"`
	RequestType   string `json:"request_type"`
	CreatedAfter  string `json:"created_after"`
	CreatedBefore string `json:"created_before"`
	Combine       string `json:"combine"`
}

func (f preferenceFilter) build() (string, error) {
	var clauses []string
	if f.Reconciling != nil {
		clauses = append(clauses, "reconciling="+strconv.FormatBool(*f.Reconciling))
	}
	if f.RequestType != "" {
		if !slices.Contains(quotaOrigins, f.RequestType) {
			return "", badRequest("request_type must be CLOUD_CONSOLE or AUTO_ADJUSTER")
		}
		clauses = append(clauses, "request_type="+f.RequestType)
	}
	for _, t := range []struct{ op, value, argument string }{
		{">", f.CreatedAfter, "created_after"}, {"<", f.CreatedBefore, "created_before"}} {
		if t.value == "" {
			continue
		}
		if !quotaTimePattern.MatchString(t.value) {
			return "", badRequest(t.argument + " must be a time such as 2026-01-02T03:04:05Z")
		}
		clauses = append(clauses, "creation_time"+t.op+t.value)
	}
	joiner := " AND "
	switch f.Combine {
	case "", "AND":
	case "OR":
		joiner = " OR "
	default:
		return "", badRequest("combine must be AND or OR")
	}
	return strings.Join(clauses, joiner), nil
}

func orderBy(fields []string) (string, error) {
	if len(fields) > len(quotaOrderFields) {
		return "", badRequest("order_by takes at most " + strconv.Itoa(len(quotaOrderFields)) + " fields")
	}
	for i, f := range fields {
		if !slices.Contains(quotaOrderFields, f) {
			return "", badRequest("order_by takes quota_id, service, and create_time only")
		}
		if slices.Contains(fields[:i], f) {
			return "", badRequest("order_by must not repeat a field")
		}
	}
	return strings.Join(fields, ", "), nil
}

var quotaPreferencesList = capability.Descriptor{
	ID: quotaPreferencesListID, Version: 1, Title: "List the quota preferences of a Cloud project",
	Description: "List the quota preferences set for a project from Cloud Quotas, optionally filtered by state, " +
		"origin, and creation time and ordered by quota ID, service, or creation time." + quotaPreferenceTail,
	Tags: []string{"google", "cloud", "quotas"}, Risk: quotaPreferenceRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,` +
		`"reconciling":{"type":"boolean"},` +
		`"request_type":{"type":"string","enum":["CLOUD_CONSOLE","AUTO_ADJUSTER"]},` +
		`"created_after":{"type":"string","maxLength":40,"pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?Z?$"},` +
		`"created_before":{"type":"string","maxLength":40,"pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,9})?Z?$"},` +
		`"combine":{"type":"string","enum":["AND","OR"]},` +
		`"order_by":{"type":"array","maxItems":3,"uniqueItems":true,` +
		`"items":{"type":"string","enum":["quota_id","service","create_time"]}},` +
		pageSizeProp + `,` + cursorProp + `},"required":["project_id"],"additionalProperties":false}`),
	OutputSchema: listOutputSchema("quotaPreferences", quotaPreferenceOutputSchema),
	Arguments: []capability.Argument{serviceProjectArgument,
		{Name: "reconciling", Description: "True for preferences awaiting approval, false for settled ones"},
		{Name: "request_type", Description: "CLOUD_CONSOLE or AUTO_ADJUSTER: how the preference was requested"},
		{Name: "created_after", Description: "Only preferences created after this time, such as 2026-01-02T03:04:05Z"},
		{Name: "created_before", Description: "Only preferences created before this time"},
		{Name: "combine", Description: "AND (default) or OR: how the filter arguments combine"},
		{Name: "order_by", Description: "Fields to order by, from quota_id, service, create_time, in that " +
			"priority; Google orders by creation time without it"},
		pageSizeArgument, cursorArgument},
	Fields: append([]capability.Field{{Name: "quotaPreferences",
		Description: "Preferences of the page, each as quota-preferences.get returns it"}}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the preferences that still await approval",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","reconciling":true,"order_by":["service","quota_id"]}`),
	}},
}

type quotaPreferencesAnswer struct {
	QuotaPreferences []json.RawMessage `json:"quotaPreferences"`
	NextPageToken    string            `json:"nextPageToken"`
}

func handleQuotaPreferencesList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list quota preferences"
	var arguments struct {
		preferenceFilter
		ProjectID string   `json:"project_id"`
		OrderBy   []string `json:"order_by"`
		PageSize  *int     `json:"page_size"`
		Cursor    string   `json:"cursor"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	filter, err := arguments.build()
	if err != nil {
		return nil, err
	}
	order, err := orderBy(arguments.OrderBy)
	if err != nil {
		return nil, err
	}
	if arguments.PageSize != nil && (*arguments.PageSize < 1 || *arguments.PageSize > maxPageSize) {
		return nil, badRequest("page_size must be 1 to " + strconv.Itoa(maxPageSize))
	}
	// The cursor binds the tool, the targets, the project, the filter, and the order.
	binding := provider.CursorBinding(quotaPreferencesListID, provider.TargetsOf(resolved),
		arguments.ProjectID+"|"+filter+"|"+order)
	token := ""
	if arguments.Cursor != "" {
		var ok bool
		if token, ok = provider.DecodeCursor(binding, arguments.Cursor, maxCursorLength); !ok {
			return nil, badRequest("cursor is not a next_cursor of this list; start again without cursor")
		}
	}
	c, err := serviceClient(ctx, op, quotaPreferencesListID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	if filter != "" {
		query.Set("filter", filter)
	}
	if order != "" {
		query.Set("orderBy", order)
	}
	if arguments.PageSize != nil {
		query.Set("pageSize", strconv.Itoa(*arguments.PageSize))
	}
	if token != "" {
		query.Set("pageToken", token)
	}
	var answer quotaPreferencesAnswer
	path := "/v1/" + prefixProject + arguments.ProjectID + quotaLocation + "/quotaPreferences"
	if err := c.get(ctx, op, quotaPreferencesListID, serviceQuotas, path, query, &answer); err != nil {
		return nil, err
	}
	if len(answer.QuotaPreferences) > 2*maxPageSize || len(answer.NextPageToken) > maxPageTokenBytes {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	rows := make([]quotaPreferenceResult, 0, len(answer.QuotaPreferences))
	for _, item := range answer.QuotaPreferences {
		var row quotaPreferenceWire
		if err := json.Unmarshal(item, &row); err != nil || !quotaPreferenceRowValid(row.Name) {
			return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
		}
		rows = append(rows, row.result())
	}
	out := map[string]any{"quotaPreferences": rows, "has_more": answer.NextPageToken != ""}
	if answer.NextPageToken != "" {
		out["next_cursor"] = provider.EncodeCursor(binding, answer.NextPageToken)
	}
	return out, nil
}

// --- quota-preferences.get ---

var quotaPreferencesGet = capability.Descriptor{
	ID: quotaPreferencesGetID, Version: 1, Title: "Get one quota preference of a Cloud project",
	Description: "Read one quota preference of a project from Cloud Quotas, by its ID." + quotaPreferenceTail,
	Tags:        []string{"google", "cloud", "quotas"}, Risk: quotaPreferenceRisk, Provider: Provider,
	InputSchema: json.RawMessage(`{"type":"object","properties":{` + serviceProjectProp + `,"preference_id":` +
		quotaIDProp + `},"required":["project_id","preference_id"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(quotaPreferenceOutputSchema),
	Arguments:    []capability.Argument{serviceProjectArgument, preferenceIDArgument},
	Fields:       quotaPreferenceFields,
	Examples: []capability.Example{{
		Description: "Read one preference",
		Arguments:   json.RawMessage(`{"project_id":"my-project-123","preference_id":"my-config-for-us-east1"}`),
	}},
}

func handleQuotaPreferencesGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get quota preference"
	var arguments struct {
		ProjectID    string `json:"project_id"`
		PreferenceID string `json:"preference_id"`
	}
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	if !quotaIDPattern.MatchString(arguments.PreferenceID) {
		return nil, badRequest("preference_id must be the ID of a preference, never a resource name or a path")
	}
	c, err := serviceClient(ctx, op, quotaPreferencesGetID, arguments.ProjectID, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	path := "/v1/" + prefixProject + arguments.ProjectID + quotaLocation + "/quotaPreferences/" + arguments.PreferenceID
	var row quotaPreferenceWire
	if err := c.get(ctx, op, quotaPreferencesGetID, serviceQuotas, path, nil, &row); err != nil {
		return nil, err
	}
	if !quotaPreferenceRowValid(row.Name) {
		return nil, provider.InvalidResponse(op, subject+" returned an invalid response")
	}
	return row.result(), nil
}

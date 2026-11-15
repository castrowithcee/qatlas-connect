package github

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// github.code-scanning-alert-instances.list reads the instances of one code scanning alert,
// github.code-scanning-analyses.list and .get read the analyses a repository's code scanning uploaded, and
// github.secret-scanning-locations.list reads where a secret scanning alert found its secret. They change
// nothing and read below the chosen repository, so a number that is not the repository's answers not found. A
// location names a place, never the found text: it is decoded into a shape with no member for a secret, a
// snippet, or a comment, so nothing else GitHub sends can reach a result. Texts come from scanners and are
// untrusted data, cut and cleaned.
//
// Verified against https://docs.github.com/en/rest/code-scanning/code-scanning#list-instances-of-a-code-scanning-alert,
// https://docs.github.com/en/rest/code-scanning/code-scanning#list-code-scanning-analyses-for-a-repository,
// https://docs.github.com/en/rest/code-scanning/code-scanning#get-a-code-scanning-analysis-for-a-repository, and
// https://docs.github.com/en/rest/secret-scanning/secret-scanning#list-locations-for-a-secret-scanning-alert:
// the lists take per_page (at most 100) and page; the analysis routes answer 403 without GitHub Advanced
// Security and 404 for an unknown analysis.

const (
	analysisIDSchema    = `{"type":"integer","minimum":1,"maximum":9007199254740991}`
	analysisSarifSchema = `{"type":"string","minLength":1,"maxLength":100}`
)

var analysisSorts = []string{"created"}

var analysisIDArgument = capability.Argument{Name: "analysis_id", Description: "Analysis ID, as the list tool reports it",
	Required: true}

const codeScanningAnalysisProperties = `"id":{"type":"integer"},"ref":{"type":"string"},"commit_sha":{"type":"string"},` +
	`"analysis_key":{"type":"string"},"environment":{"type":"string"},"category":{"type":"string"},` +
	`"error":{"type":"string"},"created_at":{"type":"string"},"results_count":{"type":"integer"},` +
	`"rules_count":{"type":"integer"},"sarif_id":{"type":"string"},"deletable":{"type":"boolean"},` +
	`"warning":{"type":"string"},"truncated":{"type":"boolean"},"tool":` + codeScanningToolSchema +
	`,"links":{"type":"object","properties":{"url":{"type":"string"}},"additionalProperties":false}`

const codeScanningToolSchema = `{"type":"object","properties":{"name":{"type":"string"},` +
	`"version":{"type":"string"},"guid":{"type":"string"}},"additionalProperties":false}`

var codeScanningAnalysisFields = []capability.Field{
	{Name: "id", Description: "Analysis ID"},
	{Name: "ref", Description: "Git reference the analysis ran on"},
	{Name: "commit_sha", Description: "Commit the analysis ran on"},
	{Name: "analysis_key", Description: "Identifier of the workflow and job that produced the analysis"},
	{Name: "environment", Description: "Variable values of the environment of the analysis, such as the language"},
	{Name: "category", Description: "Category the analysis was uploaded under"},
	{Name: "error", Description: "Error the analysis reported; untrusted data cut at 500 characters (truncated says so)"},
	{Name: "created_at", Description: "When the analysis was uploaded"},
	{Name: "results_count", Description: "Number of results in the analysis"},
	{Name: "rules_count", Description: "Number of rules in the analysis"},
	{Name: "sarif_id", Description: "ID of the SARIF upload; analyses of one upload share it"},
	{Name: "deletable", Description: "Whether GitHub allows deleting the analysis; this tool never deletes"},
	{Name: "warning", Description: "Warning the analysis reported; untrusted data cut at 500 characters (truncated says so)"},
	{Name: "truncated", Description: "True when a text of the analysis was cut"},
	{Name: "tool", Description: "Analysis tool as an object: name, version, and guid; untrusted data"},
	{Name: "links", Description: "REST url of the analysis by its GitHub name, untrusted data, never fetched"},
}

var codeScanningAlertInstancesList = capability.Descriptor{
	ID:      Provider + ".code-scanning-alert-instances.list",
	Version: 1,
	Title:   "List the instances of a GitHub code scanning alert",
	Description: "List one bounded batch of the instances of one code scanning alert of a repository an explicit " +
		"connection allows, optionally of one Git reference or pull request; texts are untrusted data",
	Tags:     []string{"github", "security", "code-scanning", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"alert_number":`+alertNumberSchema+`,"ref":`+refSchema+`,"pr":`+alertNumberSchema+
		`,`+pagingKeys, "alert_number"),
	OutputSchema: listOutput("instances", codeScanningInstanceProperties, `"additionalProperties":false`),
	Arguments: append([]capability.Argument{
		alertNumberArgument,
		{Name: "ref", Description: "Return only instances of this Git reference, a branch name or refs/heads/NAME"},
		{Name: "pr", Description: "Return only instances of the pull request with this number"},
	}, alertPagingArguments...),
	Fields: append([]capability.Field{
		{Name: "instances", Description: "Instances of the alert: ref, analysis_key, environment, category, state, " +
			"commit_sha, message (text and markdown, cut at 500 characters), location (path, start_line, end_line, " +
			"start_column, end_column), url (the web address), and classifications; untrusted data"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the instances of an alert on the default branch",
		Arguments:   json.RawMessage(`{"alert_number":42,"ref":"refs/heads/main","limit":10}`),
	}},
}

var codeScanningAnalysesList = capability.Descriptor{
	ID:      Provider + ".code-scanning-analyses.list",
	Version: 1,
	Title:   "List GitHub code scanning analyses",
	Description: "List one bounded, filtered batch of the code scanning analyses of a repository an explicit " +
		"connection allows, newest first unless direction says otherwise; texts are untrusted data",
	Tags:     []string{"github", "security", "code-scanning", "list"},
	Risk:     readRisk,
	Provider: Provider,
	InputSchema: inputSchema(`"tool_name":` + alertToolSchema + `,"tool_guid":` + alertGUIDSchema + `,"ref":` +
		refSchema + `,"pr":` + alertNumberSchema + `,"sarif_id":` + analysisSarifSchema + `,"sort":` +
		enumSchema(analysisSorts) + `,"direction":` + enumSchema(codeScanningDirections) + `,` + pagingKeys),
	OutputSchema: listOutput("analyses", codeScanningAnalysisProperties, `"required":["id"],"additionalProperties":false`),
	Arguments: append([]capability.Argument{
		{Name: "tool_name", Description: "Return only analyses of this code scanning tool, such as CodeQL"},
		{Name: "tool_guid", Description: "Return only analyses of the tool with this GUID, at most 256 characters; " +
			"not together with tool_name"},
		{Name: "ref", Description: "Return only analyses of this Git reference, a branch name or refs/heads/NAME"},
		{Name: "pr", Description: "Return only analyses of the pull request with this number"},
		{Name: "sarif_id", Description: "Return only analyses of the SARIF upload with this ID"},
		{Name: "sort", Description: "Sort by " + strings.Join(analysisSorts, " or ") + "; created when omitted"},
		{Name: "direction", Description: "Sort direction, " + strings.Join(codeScanningDirections, " or ") +
			"; desc when omitted"},
	}, alertPagingArguments...),
	Fields: append([]capability.Field{
		{Name: "analyses", Description: "Analyses as the get tool reads them; texts are untrusted data cut at 500 " +
			"characters (truncated says so)"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List the analyses of the default branch",
		Arguments:   json.RawMessage(`{"ref":"refs/heads/main","limit":10}`),
	}},
}

var codeScanningAnalysesGet = capability.Descriptor{
	ID:           Provider + ".code-scanning-analyses.get",
	Version:      1,
	Title:        "Get a GitHub code scanning analysis",
	Description:  "Read one code scanning analysis of a repository a connection allows",
	Tags:         []string{"github", "security", "code-scanning", "get"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"analysis_id":`+analysisIDSchema, "analysis_id"),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{` + codeScanningAnalysisProperties + `},"required":["id"],"additionalProperties":false}`),
	Arguments:    []capability.Argument{analysisIDArgument},
	Fields:       codeScanningAnalysisFields,
	Examples:     []capability.Example{{Description: "Read one analysis", Arguments: json.RawMessage(`{"analysis_id":201}`)}},
}

var secretScanningLocationsList = capability.Descriptor{
	ID:      Provider + ".secret-scanning-locations.list",
	Version: 1,
	Title:   "List the locations of a GitHub secret scanning alert",
	Description: "List one bounded batch of the places one secret scanning alert of a repository an explicit " +
		"connection allows found its secret in; a location names where, never the found text",
	Tags:         []string{"github", "security", "secret-scanning", "list"},
	Risk:         readRisk,
	Provider:     Provider,
	InputSchema:  inputSchema(`"alert_number":`+alertNumberSchema+`,`+pagingKeys, "alert_number"),
	OutputSchema: listOutput("locations", secretLocationEntryProperties, `"additionalProperties":false`),
	Arguments:    append([]capability.Argument{alertNumberArgument}, alertPagingArguments...),
	Fields: append([]capability.Field{
		{Name: "locations", Description: "Locations: type (commit, wiki_commit, issue or pull request title, body, " +
			"or comment, discussion title, body, or comment, and so on), path, start_line, end_line, start_column, " +
			"end_column, blob_sha, commit_sha, and links (the REST and the other *_url addresses of the location); " +
			"never the found text; untrusted data, a cut path says so in path_truncated"},
	}, pagingFields...),
	Examples: []capability.Example{{
		Description: "List where a secret was found",
		Arguments:   json.RawMessage(`{"alert_number":3,"limit":10}`),
	}},
}

var secretLocationEntryProperties = `"type":{"type":"string"},` + secretLocationMembers + `,"links":` + secretLocationLinksSchema

var secretLocationLinksSchema = linksSchema(secretLocationLinkKeys)

func securityAlertDetailOperations() []capability.Operation {
	return []capability.Operation{
		bindAlert(codeScanningAlertInstancesList, checkInstancesList,
			func(ctx context.Context, c *Client, a *alertArguments) (any, error) {
				return c.listCodeScanningInstances(ctx, a)
			}),
		bindAlert(codeScanningAnalysesList, checkAnalysesList,
			func(ctx context.Context, c *Client, a *alertArguments) (any, error) {
				return c.listCodeScanningAnalyses(ctx, a)
			}),
		bindAlert(codeScanningAnalysesGet, checkAnalysisID,
			func(ctx context.Context, c *Client, a *alertArguments) (any, error) {
				return c.getCodeScanningAnalysis(ctx, a)
			}),
		bindAlert(secretScanningLocationsList, checkLocationsList,
			func(ctx context.Context, c *Client, a *alertArguments) (any, error) {
				return c.listSecretScanningLocations(ctx, a)
			}),
	}
}

func checkAnalysisID(a *alertArguments, _ target) error {
	if a.AnalysisID < 1 {
		return invalidRequest("analysis_id must be a positive analysis ID")
	}
	return nil
}

// checkRefAndPR checks the filters the instance and analysis lists share.
func checkRefAndPR(a *alertArguments) error {
	if a.Ref != "" && !validRef(a.Ref) {
		return invalidRequest("ref must be a branch name or Git reference")
	}
	if a.PR != nil && *a.PR < 1 {
		return invalidRequest("pr must be a positive pull request number")
	}
	return nil
}

func checkInstancesList(a *alertArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	if err := checkAlertNumber(a, bound); err != nil {
		return err
	}
	if err := checkRefAndPR(a); err != nil {
		return err
	}
	a.binding = fingerprint("security-alerts", "code-scanning-instances", bound.String(), a.AlertNumber, a.Ref, a.PR)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkAnalysesList(a *alertArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	if a.ToolName != "" && (len(a.ToolName) > 100 || strings.ContainsAny(a.ToolName, "\x00\r\n")) {
		return invalidRequest("tool_name must be the name of a code scanning tool")
	}
	if a.ToolGUID != "" {
		if utf8.RuneCountInString(a.ToolGUID) > 256 || strings.IndexFunc(a.ToolGUID, unicode.IsControl) >= 0 {
			return invalidRequest("tool_guid must be the GUID of a code scanning tool, at most 256 characters")
		}
		if a.ToolName != "" {
			return invalidRequest("tool_name and tool_guid cannot be combined; name the tool by one of them")
		}
	}
	if err := checkRefAndPR(a); err != nil {
		return err
	}
	if a.SarifID != "" && (utf8.RuneCountInString(a.SarifID) > 100 || strings.IndexFunc(a.SarifID, unicode.IsControl) >= 0) {
		return invalidRequest("sarif_id must be the ID of a SARIF upload")
	}
	if a.Sort != "" && !containsFold(analysisSorts, a.Sort) {
		return invalidRequest("sort must be one of " + strings.Join(analysisSorts, ", "))
	}
	if a.Direction != "" && !containsFold(codeScanningDirections, a.Direction) {
		return invalidRequest("direction must be one of " + strings.Join(codeScanningDirections, ", "))
	}
	a.binding = fingerprint("security-alerts", "code-scanning-analyses", bound.String(), a.ToolName, a.ToolGUID,
		a.Ref, a.PR, a.SarifID, strings.ToLower(a.Sort), strings.ToLower(a.Direction))
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

func checkLocationsList(a *alertArguments, bound target) error {
	limit, err := normalizeLimit(a.Limit)
	if err != nil {
		return err
	}
	if err := checkAlertNumber(a, bound); err != nil {
		return err
	}
	a.binding = fingerprint("security-alerts", "secret-scanning-locations", bound.String(), a.AlertNumber)
	a.page, a.perPage, err = pageOf(a.binding, a.Cursor, limit)
	return err
}

// CodeScanningInstanceList is one batch of the instances of a code scanning alert.
type CodeScanningInstanceList struct {
	Instances  []CodeScanningInstance `json:"instances"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	HasMore    bool                   `json:"has_more"`
}

func (c *Client) listCodeScanningInstances(ctx context.Context, a *alertArguments) (*CodeScanningInstanceList, error) {
	const op = "list code scanning alert instances"
	query := a.query()
	setQuery(query, "ref", a.Ref)
	if a.PR != nil {
		query.Set("pr", strconv.FormatInt(*a.PR, 10))
	}
	var raw []codeScanningInstanceRaw
	hasNext, err := c.restPage(ctx, op, c.alertPath("code-scanning", a.AlertNumber)+"/instances", query, &raw)
	if err != nil {
		return nil, actionsFailure(err, codeScanningReadPermission)
	}
	result := &CodeScanningInstanceList{Instances: make([]CodeScanningInstance, 0, len(raw))}
	for _, entry := range raw {
		result.Instances = append(result.Instances, entry.view())
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// CodeScanningAnalysis is one code scanning analysis. GitHub names no web address for it; its REST url sits
// in Links. Texts are untrusted data.
type CodeScanningAnalysis struct {
	ID           int64             `json:"id"`
	Ref          string            `json:"ref,omitempty"`
	CommitSHA    string            `json:"commit_sha,omitempty"`
	AnalysisKey  string            `json:"analysis_key,omitempty"`
	Environment  string            `json:"environment,omitempty"`
	Category     string            `json:"category,omitempty"`
	Error        string            `json:"error,omitempty"`
	CreatedAt    string            `json:"created_at,omitempty"`
	ResultsCount int               `json:"results_count,omitempty"`
	RulesCount   int               `json:"rules_count,omitempty"`
	SarifID      string            `json:"sarif_id,omitempty"`
	Deletable    bool              `json:"deletable,omitempty"`
	Warning      string            `json:"warning,omitempty"`
	Truncated    bool              `json:"truncated,omitempty"`
	Tool         *CodeScanningTool `json:"tool,omitempty"`
	Links        Links             `json:"links,omitempty"`
}

// CodeScanningAnalysisList is one batch of code scanning analyses.
type CodeScanningAnalysisList struct {
	Analyses   []CodeScanningAnalysis `json:"analyses"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	HasMore    bool                   `json:"has_more"`
}

type codeScanningAnalysisJSON struct {
	ID           int64  `json:"id"`
	Ref          string `json:"ref"`
	CommitSHA    string `json:"commit_sha"`
	AnalysisKey  string `json:"analysis_key"`
	Environment  string `json:"environment"`
	Category     string `json:"category"`
	Error        string `json:"error"`
	CreatedAt    string `json:"created_at"`
	ResultsCount int    `json:"results_count"`
	RulesCount   int    `json:"rules_count"`
	SarifID      string `json:"sarif_id"`
	Deletable    bool   `json:"deletable"`
	Warning      string `json:"warning"`
	Tool         struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		GUID    string `json:"guid"`
	} `json:"tool"`
	links Links
}

func (j *codeScanningAnalysisJSON) UnmarshalJSON(data []byte) (err error) {
	type plain codeScanningAnalysisJSON
	if err = json.Unmarshal(data, (*plain)(j)); err != nil {
		return err
	}
	j.links, err = readLinks(data, []string{"url"})
	return err
}

func (j codeScanningAnalysisJSON) view() CodeScanningAnalysis {
	v := CodeScanningAnalysis{ID: j.ID, Ref: *nameText(&j.Ref), CommitSHA: *nameText(&j.CommitSHA),
		AnalysisKey: *longText(&j.AnalysisKey), Environment: *longText(&j.Environment), Category: *longText(&j.Category),
		CreatedAt: j.CreatedAt, ResultsCount: j.ResultsCount, RulesCount: j.RulesCount, SarifID: *nameText(&j.SarifID),
		Deletable: j.Deletable, Links: j.links}
	var errCut, warnCut bool
	v.Error, errCut = clipClean(j.Error, alertTextLimit)
	v.Warning, warnCut = clipClean(j.Warning, alertTextLimit)
	v.Truncated = errCut || warnCut
	tool := CodeScanningTool{Name: *nameText(&j.Tool.Name), Version: *nameText(&j.Tool.Version),
		GUID: *nameText(&j.Tool.GUID)}
	if tool != (CodeScanningTool{}) {
		v.Tool = &tool
	}
	return v
}

func (c *Client) listCodeScanningAnalyses(ctx context.Context, a *alertArguments) (*CodeScanningAnalysisList, error) {
	const op = "list code scanning analyses"
	query := a.query()
	setQuery(query, "tool_name", a.ToolName, "tool_guid", a.ToolGUID, "ref", a.Ref, "sarif_id", a.SarifID,
		"sort", strings.ToLower(a.Sort), "direction", strings.ToLower(a.Direction))
	if a.PR != nil {
		query.Set("pr", strconv.FormatInt(*a.PR, 10))
	}
	var raw []codeScanningAnalysisJSON
	hasNext, err := c.restPage(ctx, op, c.repoPath("code-scanning/analyses"), query, &raw)
	if err != nil {
		return nil, actionsFailure(err, codeScanningReadPermission)
	}
	result := &CodeScanningAnalysisList{Analyses: make([]CodeScanningAnalysis, 0, len(raw))}
	for _, entry := range raw {
		if entry.ID < 1 {
			return nil, invalidEntry(op, "an analysis")
		}
		result.Analyses = append(result.Analyses, entry.view())
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

// getCodeScanningAnalysis reads the analysis below the bound repository and checks the ID GitHub answers, so
// an analysis of another repository is never reported.
func (c *Client) getCodeScanningAnalysis(ctx context.Context, a *alertArguments) (*CodeScanningAnalysis, error) {
	const op = "get code scanning analysis"
	var raw codeScanningAnalysisJSON
	path := c.repoPath("code-scanning/analyses/" + strconv.FormatInt(a.AnalysisID, 10))
	if err := c.rest(ctx, op, path, &raw); err != nil {
		return nil, actionsFailure(err, codeScanningReadPermission)
	}
	if raw.ID != a.AnalysisID {
		return nil, invalidResponse(op, false)
	}
	view := raw.view()
	return &view, nil
}

// SecretScanningLocationEntry is one place a secret scanning alert found its secret. It names where, never what.
type SecretScanningLocationEntry struct {
	Type string `json:"type,omitempty"`
	SecretScanningLocation
}

// SecretScanningLocationList is one batch of the locations of a secret scanning alert.
type SecretScanningLocationList struct {
	Locations  []SecretScanningLocationEntry `json:"locations"`
	NextCursor string                        `json:"next_cursor,omitempty"`
	HasMore    bool                          `json:"has_more"`
}

// secretLocationEntryRaw names the two members read from a location entry; the details are decoded into the
// shape that has no member for a secret or a snippet.
type secretLocationEntryRaw struct {
	Type    json.RawMessage `json:"type"`
	Details json.RawMessage `json:"details"`
}

func (c *Client) listSecretScanningLocations(ctx context.Context, a *alertArguments) (*SecretScanningLocationList, error) {
	const op = "list secret scanning locations"
	var raw []secretLocationEntryRaw
	hasNext, err := c.restPage(ctx, op, c.alertPath("secret-scanning", a.AlertNumber)+"/locations", a.query(), &raw)
	if err != nil {
		return nil, actionsFailure(err, secretScanningReadPermission)
	}
	result := &SecretScanningLocationList{Locations: make([]SecretScanningLocationEntry, 0, len(raw))}
	for _, entry := range raw {
		// A member of an unexpected shape only drops itself.
		var kind *string
		if json.Unmarshal(entry.Type, &kind) != nil {
			kind = nil
		}
		item := SecretScanningLocationEntry{Type: shortString(kind)}
		var details secretLocationRaw
		if json.Unmarshal(entry.Details, &details) == nil {
			item.SecretScanningLocation, _ = details.view()
		}
		result.Locations = append(result.Locations, item)
	}
	result.HasMore, result.NextCursor = morePage(a.binding, a.page, a.perPage, hasNext)
	return result, nil
}

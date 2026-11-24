package googlesearchconsole

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	urlInspectionID   = Provider + ".url-inspection.inspect"
	urlInspectionPath = "/v1/urlInspection/index:inspect"
	// maxInspectList cuts every list of an inspection (sitemaps, referring addresses, issues, rich result
	// groups and items); a cut sets truncated.
	maxInspectList  = 50
	maxLanguageLen  = 35
	notFoundInspect = "Google did not find the property for this credential, or refused the address; check " +
		"site_url, inspection_url and that the account has access to the property"
)

// languagePattern is the shape of a BCP-47 tag as the discovery document names it ("en-US", "de-CH").
var languagePattern = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$`)

// The inspection as the discovery document documents it (revision 20261115). The deprecated mobile usability
// result is left out: the decoder skips it and nothing holds it.
type (
	inspectIssue struct {
		Severity     short `json:"severity,omitempty"`
		IssueMessage text  `json:"issueMessage,omitempty"`
	}
	indexStatus struct {
		Verdict         short  `json:"verdict,omitempty"`
		CoverageState   short  `json:"coverageState,omitempty"`
		RobotsTxtState  short  `json:"robotsTxtState,omitempty"`
		IndexingState   short  `json:"indexingState,omitempty"`
		PageFetchState  short  `json:"pageFetchState,omitempty"`
		CrawledAs       short  `json:"crawledAs,omitempty"`
		LastCrawlTime   short  `json:"lastCrawlTime,omitempty"`
		GoogleCanonical text   `json:"googleCanonical,omitempty"`
		UserCanonical   text   `json:"userCanonical,omitempty"`
		Sitemap         []text `json:"sitemap,omitempty"`
		ReferringURLs   []text `json:"referringUrls,omitempty"`
	}
	ampStatus struct {
		Verdict               short          `json:"verdict,omitempty"`
		AmpIndexStatusVerdict short          `json:"ampIndexStatusVerdict,omitempty"`
		IndexingState         short          `json:"indexingState,omitempty"`
		RobotsTxtState        short          `json:"robotsTxtState,omitempty"`
		PageFetchState        short          `json:"pageFetchState,omitempty"`
		LastCrawlTime         short          `json:"lastCrawlTime,omitempty"`
		AmpURL                text           `json:"ampUrl,omitempty"`
		Issues                []inspectIssue `json:"issues,omitempty"`
	}
	richItem struct {
		Name   text           `json:"name,omitempty"`
		Issues []inspectIssue `json:"issues,omitempty"`
	}
	detectedItems struct {
		RichResultType short      `json:"richResultType,omitempty"`
		Items          []richItem `json:"items,omitempty"`
	}
	richStatus struct {
		Verdict       short           `json:"verdict,omitempty"`
		DetectedItems []detectedItems `json:"detectedItems,omitempty"`
	}
	inspection struct {
		InspectionResultLink text         `json:"inspectionResultLink,omitempty"`
		IndexStatusResult    *indexStatus `json:"indexStatusResult,omitempty"`
		AmpResult            *ampStatus   `json:"ampResult,omitempty"`
		RichResultsResult    *richStatus  `json:"richResultsResult,omitempty"`
	}
	inspectionAnswer struct {
		InspectionResult inspection `json:"inspectionResult"`
	}
	inspectionResult struct {
		InspectionResult inspection `json:"inspectionResult"`
		Truncated        bool       `json:"truncated,omitempty"`
	}
)

// clip cuts list to maxInspectList entries and records the cut.
func clip[T any](list []T, cut *bool) []T {
	if len(list) > maxInspectList {
		*cut = true
		return list[:maxInspectList]
	}
	return list
}

// bound cuts every list of the inspection and reports whether any was cut.
func (i *inspection) bound() (cut bool) {
	if s := i.IndexStatusResult; s != nil {
		s.Sitemap, s.ReferringURLs = clip(s.Sitemap, &cut), clip(s.ReferringURLs, &cut)
	}
	if a := i.AmpResult; a != nil {
		a.Issues = clip(a.Issues, &cut)
	}
	if r := i.RichResultsResult; r != nil {
		r.DetectedItems = clip(r.DetectedItems, &cut)
		for g := range r.DetectedItems {
			group := &r.DetectedItems[g]
			group.Items = clip(group.Items, &cut)
			for n := range group.Items {
				group.Items[n].Issues = clip(group.Items[n].Issues, &cut)
			}
		}
	}
	return cut
}

const (
	issueSchema = `{"type":"object","properties":{"severity":{"type":"string"},"issueMessage":{"type":"string"}},` +
		`"additionalProperties":false}`
	stringList = `{"type":"array","items":{"type":"string"}}`
)

var (
	urlInspectionInput = `{"type":"object","properties":{` + siteURLSchema +
		`,"inspection_url":{"type":"string","minLength":8,"maxLength":2048},"language_code":{"type":"string",` +
		`"minLength":2,"maxLength":` + strconv.Itoa(maxLanguageLen) + `,"pattern":"^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8}){0,3}$"}},` +
		`"required":["inspection_url"],"additionalProperties":false}`
	urlInspectionOutput = `{"type":"object","properties":{"inspectionResult":{"type":"object","properties":{` +
		`"inspectionResultLink":{"type":"string"},"indexStatusResult":{"type":"object","properties":{` +
		`"verdict":{"type":"string"},"coverageState":{"type":"string"},"robotsTxtState":{"type":"string"},` +
		`"indexingState":{"type":"string"},"pageFetchState":{"type":"string"},"crawledAs":{"type":"string"},` +
		`"lastCrawlTime":{"type":"string"},"googleCanonical":{"type":"string"},"userCanonical":{"type":"string"},` +
		`"sitemap":` + stringList + `,"referringUrls":` + stringList + `},"additionalProperties":false},` +
		`"ampResult":{"type":"object","properties":{"verdict":{"type":"string"},"ampIndexStatusVerdict":{"type":"string"},` +
		`"indexingState":{"type":"string"},"robotsTxtState":{"type":"string"},"pageFetchState":{"type":"string"},` +
		`"lastCrawlTime":{"type":"string"},"ampUrl":{"type":"string"},"issues":{"type":"array","items":` + issueSchema +
		`}},"additionalProperties":false},"richResultsResult":{"type":"object","properties":{"verdict":{"type":"string"},` +
		`"detectedItems":{"type":"array","items":{"type":"object","properties":{"richResultType":{"type":"string"},` +
		`"items":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"},"issues":` +
		`{"type":"array","items":` + issueSchema + `}},"additionalProperties":false}}},"additionalProperties":false}}},` +
		`"additionalProperties":false}},"additionalProperties":false},"truncated":{"type":"boolean"}},` +
		`"required":["inspectionResult"],"additionalProperties":false}`
)

// inspectionRisk is the risk of the inspection: a safe read although it is sent as POST. It only reads the
// index state; it asks for no indexing and Google does not fetch the address for it.
var inspectionRisk = capability.Risk{
	Effect:       capability.EffectRead,
	Idempotency:  capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone,
	OpenWorld:    true,
	DataSensitivity: "index state of one address of the property: canonical addresses, crawl time and crawler, " +
		"sitemaps and the addresses that link to it, and issues found on the address",
}

var urlInspectionInspect = capability.Descriptor{
	ID: urlInspectionID, Version: 1, Title: "Inspect one URL in the Google index",
	Description: "Read the index state Google holds for one URL of a Search Console property: verdict and " +
		"coverage, robots.txt and noindex state, fetch state, the canonical addresses declared and chosen, last " +
		"crawl and crawler, sitemaps and referring addresses, plus the AMP and rich results findings when Google " +
		"has them. It only reads: it requests no indexing, Google does not crawl the URL for it, and Qatlas never " +
		"fetches it. The URL must lie under the property. Google allows 2,000 inspections per day and 600 per " +
		"minute for one property; Qatlas counts its own calls per credential and property in this process, " +
		"refuses the 2,001st of a day (Pacific Time), and spaces them by 100 ms. Google's own count decides " +
		"and a 429 is never overridden. " + untrustedNote,
	Tags: []string{"google", "search-console", "url-inspection"}, Risk: inspectionRisk, Provider: Provider,
	InputSchema:  json.RawMessage(urlInspectionInput),
	OutputSchema: json.RawMessage(urlInspectionOutput),
	Arguments: []capability.Argument{siteArgument,
		{Name: "inspection_url", Required: true, Description: "Absolute http or https URL to inspect, at most 2048 " +
			"characters. It must lie under the property: start with the URL prefix, or have the domain of a domain " +
			"property as its host or a subdomain of it"},
		{Name: "language_code", Description: "BCP-47 language of Google's issue messages such as en-US or de-CH " +
			"(default en-US)"}},
	Fields: []capability.Field{
		{Name: "inspectionResult", Description: "inspectionResultLink (the report in Search Console), " +
			"indexStatusResult, ampResult and richResultsResult; ampResult is absent for a URL that is not AMP and " +
			"richResultsResult when Google found no rich results. Enumerations keep Google's spelling"},
		{Name: "truncated", Description: "true when a list of the result was cut at 50 entries"}},
	Examples: []capability.Example{{Description: "Index state of one URL of a domain property",
		Arguments: json.RawMessage(`{"site_url":"sc-domain:example.com","inspection_url":"https://www.example.com/blog/post"}`)}},
}

type urlInspectionArguments struct {
	SiteURL       string `json:"site_url"`
	InspectionURL string `json:"inspection_url"`
	LanguageCode  string `json:"language_code"`
}

// normalizeAddress lowercases scheme and host, the parts of an address that are not case-sensitive.
func normalizeAddress(parsed *url.URL) string {
	rest := parsed.EscapedPath()
	if parsed.RawQuery != "" || parsed.ForceQuery {
		rest += "?" + parsed.RawQuery
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host) + rest
}

// checkUnderProperty checks that address lies under property: for a URL prefix by prefix, for a domain
// property by a host that is the domain or a subdomain of it. It never contacts the address.
func checkUnderProperty(address, property string) error {
	parsed, err := url.Parse(address)
	if err != nil {
		return badRequest("inspection_url must be an absolute http or https address with a host")
	}
	outside := badRequest("inspection_url lies outside the property")
	if domain, ok := strings.CutPrefix(property, domainPrefix); ok {
		host, domain := strings.ToLower(parsed.Hostname()), strings.ToLower(domain)
		if host != domain && !strings.HasSuffix(host, "."+domain) {
			return outside
		}
		return nil
	}
	prefix, err := url.Parse(property)
	if err != nil || !strings.HasPrefix(normalizeAddress(parsed), normalizeAddress(prefix)) {
		return outside
	}
	return nil
}

func handleURLInspection(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver, red *redact.Redactor,
	raw json.RawMessage) (any, error) {
	const op = "url inspection"
	var arguments urlInspectionArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	siteURL, err := siteOf(resolved, arguments.SiteURL)
	if err != nil {
		return nil, err
	}
	if err := checkAddress("inspection_url", arguments.InspectionURL); err != nil {
		return nil, err
	}
	if err := checkUnderProperty(arguments.InspectionURL, siteURL); err != nil {
		return nil, err
	}
	if lang := arguments.LanguageCode; lang != "" && (len(lang) > maxLanguageLen || !languagePattern.MatchString(lang)) {
		return nil, badRequest("language_code must be a BCP-47 tag such as en-US or de-CH")
	}
	body := map[string]string{"inspectionUrl": arguments.InspectionURL, "siteUrl": siteURL}
	if arguments.LanguageCode != "" {
		body["languageCode"] = arguments.LanguageCode
	}
	encoded, _ := json.Marshal(body)

	if err := checkConnection(resolved, secrets); err != nil {
		return nil, err
	}
	dayKey := credentialKey(resolved) + "\x00" + siteURL
	if err := countInspection(op, dayKey); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer inspectionAnswer
	if err := c.send(ctx, op, urlInspectionID, http.MethodPost, apiRoot+urlInspectionPath, encoded,
		inspectionLimiters.For(c.key+"\x00"+siteURL), notFoundInspect, maxBytes, false, &answer); err != nil {
		return nil, err
	}
	result := inspectionResult{InspectionResult: answer.InspectionResult}
	result.Truncated = result.InspectionResult.bound()
	return result, nil
}

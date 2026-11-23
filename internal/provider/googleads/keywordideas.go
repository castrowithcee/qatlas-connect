package googleads

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/ratelimit"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	keywordIdeasID      = Provider + ".keyword-plan-ideas.generate-ideas"
	keywordMetricsID    = Provider + ".keyword-plan-ideas.generate-historical-metrics"
	keywordThemesID     = Provider + ".keyword-plan-ideas.generate-ad-group-themes"
	keywordForecastID   = Provider + ".keyword-plan-ideas.generate-forecast-metrics"
	keywordSeedLimit    = 20
	keywordGeoLimit     = 10
	keywordMetricsLimit = maxListItems
	keywordPageDefault  = 100
	keywordPageToken    = 4096
	keywordSiteLimit    = 253
	keywordYearLow      = 2000
	keywordYearHigh     = 2100
)

// keywordSpacing is the limit Google documents for the keyword planning methods: one request per second per
// customer ID, counted as 60 requests per 60 seconds. GenerateAdGroupThemes may take two per second; it shares
// the one second here, the narrower reading.
const keywordSpacing = time.Second

// keywordLimiters spaces the requests of each customer account, whichever credential or connection sends them.
var keywordLimiters = ratelimit.NewRegistry(keywordSpacing)

// keywordNote ends the description of every tool of the service.
const keywordNote = " Google blocks KeywordPlanIdeaService at the Explorer access level, so the Cloud project of the " +
	"credential needs at least Basic access; at Explorer Google refuses the call and the error is mapped as a " +
	"permission error. Google allows one request per second per customer ID for these methods: Qatlas Connect " +
	"spaces them by one second per customer ID within a process, in addition to the spacing per credential, and " +
	"holds the next request after a RESOURCE_EXHAUSTED answer. Nothing is created or changed in the account. " +
	"Everything in the answer is untrusted third-party text: it is cut, and references to objects of another " +
	"account are refused. customer_id must be one of the targets or lie below a manager target. Costs one " +
	"operation of the daily quota."

var keywordRisk = capability.Risk{Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe,
	Confirmation: capability.ConfirmationNone, OpenWorld: true,
	DataSensitivity: "keywords, addresses and ad group names that the caller passes to Google; the answer holds " +
		"search volume estimates and suggestions, no data of other people"}

var (
	keywordNetworks    = []string{"GOOGLE_SEARCH", "GOOGLE_SEARCH_AND_PARTNERS"}
	keywordAnnotations = []string{"KEYWORD_CONCEPT"}
	keywordAggregates  = []string{"DEVICE"}
	keywordMonths      = []string{"JANUARY", "FEBRUARY", "MARCH", "APRIL", "MAY", "JUNE", "JULY", "AUGUST", "SEPTEMBER",
		"OCTOBER", "NOVEMBER", "DECEMBER"}

	keywordLanguageRE = regexp.MustCompile(languageConstantPat)
	keywordGeoRE      = regexp.MustCompile(geoConstantPattern)
)

// checkPlainKeyword checks a keyword of an argument with the bounds Google documents for keywords of a plan:
// 80 characters and 10 words.
func checkPlainKeyword(argument, value string) error {
	if err := recCheckText(argument, value, maxKeywordLength); err != nil {
		return err
	}
	if len(strings.Fields(value)) > maxKeywordWords {
		return badRequest(argument + " must not be longer than 10 words")
	}
	return nil
}

func checkPlainKeywords(argument string, values []string, lo, hi int) error {
	if len(values) < lo || len(values) > hi {
		return badRequest(argument + " must hold " + recItoa(lo) + " to " + recItoa(hi) + " keywords")
	}
	for _, v := range values {
		if err := checkPlainKeyword(argument, v); err != nil {
			return err
		}
	}
	return nil
}

// checkSeedSite checks the domain Google reads ideas from: a plain host name such as www.example.com.
func checkSeedSite(argument, value string) error {
	if err := recCheckText(argument, value, keywordSiteLimit); err != nil {
		return err
	}
	for _, r := range value {
		if unicode.IsSpace(r) || strings.ContainsRune(`/\?#@:{}'"`, r) {
			return badRequest(argument + " must be a plain host name such as www.example.com, without a scheme, " +
				"path or user info")
		}
	}
	return nil
}

// --- shared targeting arguments of ideas and historical metrics ---

type (
	yearMonthArgument struct {
		Year  int    `json:"year"`
		Month string `json:"month"`
	}
	metricsOptionsArgument struct {
		YearMonthRange *struct {
			Start yearMonthArgument `json:"start"`
			End   yearMonthArgument `json:"end"`
		} `json:"year_month_range"`
		IncludeAverageCPC bool `json:"include_average_cpc"`
	}
	keywordTargeting struct {
		Language                 string                  `json:"language"`
		GeoTargetConstants       []string                `json:"geo_target_constants"`
		IncludeAdultKeywords     bool                    `json:"include_adult_keywords"`
		KeywordPlanNetwork       string                  `json:"keyword_plan_network"`
		AggregateMetricTypes     []string                `json:"aggregate_metric_types"`
		HistoricalMetricsOptions *metricsOptionsArgument `json:"historical_metrics_options"`
	}
)

func (t keywordTargeting) check() error {
	if t.Language != "" && !keywordLanguageRE.MatchString(t.Language) {
		return badRequest("language must be a language constant, languageConstants/ID")
	}
	if len(t.GeoTargetConstants) > keywordGeoLimit {
		return badRequest("geo_target_constants takes at most 10 locations")
	}
	for i, g := range t.GeoTargetConstants {
		if !keywordGeoRE.MatchString(g) || slices.Contains(t.GeoTargetConstants[:i], g) {
			return badRequest("geo_target_constants must be location constants, geoTargetConstants/ID, each once")
		}
	}
	if t.KeywordPlanNetwork != "" && !slices.Contains(keywordNetworks, t.KeywordPlanNetwork) {
		return badRequest("keyword_plan_network must be GOOGLE_SEARCH or GOOGLE_SEARCH_AND_PARTNERS")
	}
	for i, a := range t.AggregateMetricTypes {
		if !slices.Contains(keywordAggregates, a) || slices.Contains(t.AggregateMetricTypes[:i], a) {
			return badRequest("aggregate_metric_types may name DEVICE only, once")
		}
	}
	if o := t.HistoricalMetricsOptions; o != nil && o.YearMonthRange != nil {
		r := o.YearMonthRange
		for _, ym := range []yearMonthArgument{r.Start, r.End} {
			if ym.Year < keywordYearLow || ym.Year > keywordYearHigh || !slices.Contains(keywordMonths, ym.Month) {
				return badRequest("historical_metrics_options.year_month_range needs a year of 2000 to 2100 and a " +
					"month name, JANUARY to DECEMBER, for start and end")
			}
		}
		if r.Start.Year*12+slices.Index(keywordMonths, r.Start.Month) > r.End.Year*12+slices.Index(keywordMonths, r.End.Month) {
			return badRequest("historical_metrics_options.year_month_range must not end before it starts")
		}
	}
	return nil
}

// fill adds the targeting members to the request body.
func (t keywordTargeting) fill(body map[string]any) {
	if t.Language != "" {
		body["language"] = t.Language
	}
	if len(t.GeoTargetConstants) > 0 {
		body["geoTargetConstants"] = t.GeoTargetConstants
	}
	if t.IncludeAdultKeywords {
		body["includeAdultKeywords"] = true
	}
	if t.KeywordPlanNetwork != "" {
		body["keywordPlanNetwork"] = t.KeywordPlanNetwork
	}
	if len(t.AggregateMetricTypes) > 0 {
		body["aggregateMetrics"] = map[string]any{"aggregateMetricTypes": t.AggregateMetricTypes}
	}
	if o := t.HistoricalMetricsOptions; o != nil {
		options := map[string]any{}
		if r := o.YearMonthRange; r != nil {
			options["yearMonthRange"] = map[string]any{
				"start": map[string]any{"year": r.Start.Year, "month": r.Start.Month},
				"end":   map[string]any{"year": r.End.Year, "month": r.End.Month}}
		}
		if o.IncludeAverageCPC {
			options["includeAverageCpc"] = true
		}
		body["historicalMetricsOptions"] = options
	}
}

var targetingProperties = schema{
	"language":               schema{"type": "string", "pattern": languageConstantPat},
	"geo_target_constants":   schema{"type": "array", "maxItems": keywordGeoLimit, "uniqueItems": true, "items": schema{"type": "string", "pattern": geoConstantPattern}},
	"include_adult_keywords": booleanSchema,
	"keyword_plan_network":   recEnumSchema(keywordNetworks),
	"aggregate_metric_types": schema{"type": "array", "maxItems": 1, "uniqueItems": true, "items": recEnumSchema(keywordAggregates)},
	"historical_metrics_options": object(schema{
		"year_month_range":    object(schema{"start": yearMonthSchema(), "end": yearMonthSchema()}, "start", "end"),
		"include_average_cpc": booleanSchema}),
}

func yearMonthSchema() schema {
	return object(schema{"year": schema{"type": "integer", "minimum": keywordYearLow, "maximum": keywordYearHigh},
		"month": recEnumSchema(keywordMonths)}, "year", "month")
}

// withTargeting returns the input properties of a tool: its own and the shared targeting ones.
func withTargeting(own schema) schema {
	out := schema{}
	for k, v := range targetingProperties {
		out[k] = v
	}
	for k, v := range own {
		out[k] = v
	}
	return out
}

var targetingArguments = []capability.Argument{
	{Name: "language", Description: "Language constant to target, languageConstants/ID such as languageConstants/1000; " +
		"a keyword counts when it belongs to this language; left out, all keywords count"},
	{Name: "geo_target_constants", Description: "Up to 10 location constants, geoTargetConstants/ID such as " +
		"geoTargetConstants/2276; left out, all locations"},
	{Name: "include_adult_keywords", Description: "true includes adult keywords; false or left out leaves them out"},
	{Name: "keyword_plan_network", Description: "GOOGLE_SEARCH or GOOGLE_SEARCH_AND_PARTNERS; left out, Google uses " +
		"Google Search and its partners"},
	{Name: "aggregate_metric_types", Description: "DEVICE adds the search counts per device to the answer"},
	{Name: "historical_metrics_options", Description: "year_month_range with start and end, each a year (2000 to 2100) " +
		"and a month name, both inclusive; left out, the past 12 months. Google offers search metrics for the past " +
		"four years and judges the range. include_average_cpc true adds the average CPC, which Google keeps for legacy support"},
}

// --- shared answer ---

type (
	monthlyVolumeWire struct {
		Year            *wireInt `json:"year"`
		Month           string   `json:"month"`
		MonthlySearches *wireInt `json:"monthlySearches"`
	}
	metricsWire struct {
		AvgMonthlySearches     *wireInt            `json:"avgMonthlySearches"`
		MonthlySearchVolumes   []monthlyVolumeWire `json:"monthlySearchVolumes"`
		Competition            string              `json:"competition"`
		CompetitionIndex       *wireInt            `json:"competitionIndex"`
		LowTopOfPageBidMicros  *wireInt            `json:"lowTopOfPageBidMicros"`
		HighTopOfPageBidMicros *wireInt            `json:"highTopOfPageBidMicros"`
		AverageCPCMicros       *wireInt            `json:"averageCpcMicros"`
	}
	aggregateWire struct {
		DeviceSearches []struct {
			Device      string   `json:"device"`
			SearchCount *wireInt `json:"searchCount"`
		} `json:"deviceSearches"`
	}

	monthlyVolume struct {
		Year            string `json:"year,omitempty"`
		Month           string `json:"month,omitempty"`
		MonthlySearches string `json:"monthly_searches,omitempty"`
	}
	keywordMetrics struct {
		AvgMonthlySearches     string          `json:"avg_monthly_searches,omitempty"`
		MonthlySearchVolumes   []monthlyVolume `json:"monthly_search_volumes,omitempty"`
		Competition            string          `json:"competition,omitempty"`
		CompetitionIndex       string          `json:"competition_index,omitempty"`
		LowTopOfPageBidMicros  string          `json:"low_top_of_page_bid_micros,omitempty"`
		HighTopOfPageBidMicros string          `json:"high_top_of_page_bid_micros,omitempty"`
		AverageCPCMicros       string          `json:"average_cpc_micros,omitempty"`
	}
	deviceSearches struct {
		Device      string `json:"device,omitempty"`
		SearchCount string `json:"search_count,omitempty"`
	}
	aggregateResult struct {
		DeviceSearches []deviceSearches `json:"device_searches,omitempty"`
	}
)

// wireDecimal is an int64 of an answer as the decimal string the results carry; "" for a member left out.
func wireDecimal(v *wireInt) string {
	if v == nil {
		return ""
	}
	return string(*v)
}

func metricsOf(w *metricsWire) *keywordMetrics {
	if w == nil {
		return nil
	}
	m := &keywordMetrics{AvgMonthlySearches: wireDecimal(w.AvgMonthlySearches), Competition: code(w.Competition),
		CompetitionIndex: wireDecimal(w.CompetitionIndex), LowTopOfPageBidMicros: wireDecimal(w.LowTopOfPageBidMicros),
		HighTopOfPageBidMicros: wireDecimal(w.HighTopOfPageBidMicros), AverageCPCMicros: wireDecimal(w.AverageCPCMicros)}
	for _, v := range w.MonthlySearchVolumes[:min(len(w.MonthlySearchVolumes), maxListItems)] {
		m.MonthlySearchVolumes = append(m.MonthlySearchVolumes, monthlyVolume{Year: wireDecimal(v.Year),
			Month: code(v.Month), MonthlySearches: wireDecimal(v.MonthlySearches)})
	}
	return m
}

func aggregateOf(w *aggregateWire) *aggregateResult {
	if w == nil || len(w.DeviceSearches) == 0 {
		return nil
	}
	out := &aggregateResult{}
	for _, d := range w.DeviceSearches[:min(len(w.DeviceSearches), maxListItems)] {
		out.DeviceSearches = append(out.DeviceSearches, deviceSearches{Device: code(d.Device),
			SearchCount: wireDecimal(d.SearchCount)})
	}
	return out
}

var (
	metricsOutputSchema = object(schema{"avg_monthly_searches": stringSchema,
		"monthly_search_volumes": arrayOf(object(schema{"year": stringSchema, "month": stringSchema,
			"monthly_searches": stringSchema})),
		"competition": stringSchema, "competition_index": stringSchema, "low_top_of_page_bid_micros": stringSchema,
		"high_top_of_page_bid_micros": stringSchema, "average_cpc_micros": stringSchema})
	aggregateOutputSchema = object(schema{"device_searches": arrayOf(object(schema{"device": stringSchema,
		"search_count": stringSchema}))})
)

const metricsFieldText = "the metrics with the member names of the reference: avg_monthly_searches (the average of the " +
	"past 12 months), monthly_search_volumes (year, month, monthly_searches; a month without a volume has none), " +
	"competition (LOW, MEDIUM or HIGH), competition_index (0 to 100), low_top_of_page_bid_micros (20th percentile), " +
	"high_top_of_page_bid_micros (80th percentile) and average_cpc_micros, all numbers as decimal strings"

// keywordCall sends one keyword planning request of the checked account: the account of the call is checked
// against the targets before the secret is read, and the request is spaced per customer ID.
func keywordCall(env callEnv, customerID, method string, body map[string]any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return failure(env.op, "the request body could not be encoded")
	}
	c, err := env.settle(customerID, false)
	if err != nil {
		return err
	}
	return c.read(env.ctx, request{op: env.op, tool: env.tool, method: http.MethodPost,
		path: "/" + customerName(customerID) + ":" + method, body: raw, onAccount: true, judgesInput: true,
		account: keywordLimiters.For(customerID)}, out)
}

// answerRef checks a reference of an answer: empty, or an object of the collection that belongs to the account of
// the call.
func answerRef(op, customerID, collection, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	id, ok := cutOwn(customerID, collection, value)
	if !ok || !idPattern.MatchString(id) {
		return "", provider.InvalidResponse(op, subject+" returned a reference to an object of another account than "+
			"the one asked for")
	}
	return value, nil
}

// --- generate-ideas ---

type ideasArguments struct {
	CustomerID         string   `json:"customer_id"`
	SeedKeywords       []string `json:"seed_keywords"`
	SeedURL            string   `json:"seed_url"`
	SeedSite           string   `json:"seed_site"`
	KeywordAnnotations []string `json:"keyword_annotations"`
	PageToken          string   `json:"page_token"`
	PageSize           *int     `json:"page_size"`
	keywordTargeting
}

func checkPageToken(value string) error {
	if value == "" {
		return nil
	}
	if err := recCheckText("page_token", value, keywordPageToken); err != nil {
		return err
	}
	if strings.ContainsFunc(value, unicode.IsSpace) {
		return badRequest("page_token must be the token of the previous answer")
	}
	return nil
}

func (a ideasArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	switch {
	case a.SeedSite != "" && (len(a.SeedKeywords) > 0 || a.SeedURL != ""):
		return badRequest("seed_site stands alone: Google takes a site, a url, keywords, or keywords with a url")
	case a.SeedSite == "" && len(a.SeedKeywords) == 0 && a.SeedURL == "":
		return badRequest("one seed is needed: seed_keywords, seed_url, both, or seed_site")
	}
	if len(a.SeedKeywords) > 0 {
		if err := checkPlainKeywords("seed_keywords", a.SeedKeywords, 1, keywordSeedLimit); err != nil {
			return err
		}
	}
	if a.SeedURL != "" {
		if err := recCheckSeedURL("seed_url", a.SeedURL); err != nil {
			return err
		}
	}
	if a.SeedSite != "" {
		if err := checkSeedSite("seed_site", a.SeedSite); err != nil {
			return err
		}
	}
	for i, k := range a.KeywordAnnotations {
		if !slices.Contains(keywordAnnotations, k) || slices.Contains(a.KeywordAnnotations[:i], k) {
			return badRequest("keyword_annotations may name KEYWORD_CONCEPT only, once")
		}
	}
	if a.PageSize != nil && (*a.PageSize < 1 || *a.PageSize > keywordMetricsLimit) {
		return badRequest("page_size must be 1 to 500")
	}
	if err := checkPageToken(a.PageToken); err != nil {
		return err
	}
	return a.keywordTargeting.check()
}

func (a ideasArguments) body() map[string]any {
	body := map[string]any{"pageSize": keywordPageDefault}
	if a.PageSize != nil {
		body["pageSize"] = *a.PageSize
	}
	switch {
	case a.SeedSite != "":
		body["siteSeed"] = map[string]any{"site": a.SeedSite}
	case a.SeedURL != "" && len(a.SeedKeywords) > 0:
		body["keywordAndUrlSeed"] = map[string]any{"url": a.SeedURL, "keywords": a.SeedKeywords}
	case a.SeedURL != "":
		body["urlSeed"] = map[string]any{"url": a.SeedURL}
	default:
		body["keywordSeed"] = map[string]any{"keywords": a.SeedKeywords}
	}
	if len(a.KeywordAnnotations) > 0 {
		body["keywordAnnotation"] = a.KeywordAnnotations
	}
	if a.PageToken != "" {
		body["pageToken"] = a.PageToken
	}
	a.keywordTargeting.fill(body)
	return body
}

type (
	ideasAnswer struct {
		Results []struct {
			Text               string       `json:"text"`
			KeywordIdeaMetrics *metricsWire `json:"keywordIdeaMetrics"`
			KeywordAnnotations *struct {
				Concepts []struct {
					Name         string `json:"name"`
					ConceptGroup *struct {
						Name string `json:"name"`
						Type string `json:"type"`
					} `json:"conceptGroup"`
				} `json:"concepts"`
			} `json:"keywordAnnotations"`
			CloseVariants []string `json:"closeVariants"`
		} `json:"results"`
		AggregateMetricResults *aggregateWire `json:"aggregateMetricResults"`
		NextPageToken          string         `json:"nextPageToken"`
		TotalSize              *wireInt       `json:"totalSize"`
	}
	ideaConcept struct {
		Name             string `json:"name,omitempty"`
		ConceptGroupName string `json:"concept_group_name,omitempty"`
		ConceptGroupType string `json:"concept_group_type,omitempty"`
	}
	ideaItem struct {
		Text               string          `json:"text,omitempty"`
		KeywordIdeaMetrics *keywordMetrics `json:"keyword_idea_metrics,omitempty"`
		KeywordAnnotations []ideaConcept   `json:"keyword_annotations,omitempty"`
		CloseVariants      []string        `json:"close_variants,omitempty"`
	}
	ideasResult struct {
		CustomerID             string           `json:"customer_id"`
		Results                []ideaItem       `json:"results"`
		AggregateMetricResults *aggregateResult `json:"aggregate_metric_results,omitempty"`
		NextPageToken          string           `json:"next_page_token,omitempty"`
		TotalSize              string           `json:"total_size,omitempty"`
		Truncated              bool             `json:"truncated,omitempty"`
	}
)

var ideaOutputSchema = object(schema{"text": stringSchema, "keyword_idea_metrics": metricsOutputSchema,
	"keyword_annotations": arrayOf(object(schema{"name": stringSchema, "concept_group_name": stringSchema,
		"concept_group_type": stringSchema})),
	"close_variants": arrayOf(stringSchema)})

var keywordIdeas = capability.Descriptor{
	ID: keywordIdeasID, Version: 1, Title: "Generate keyword ideas with Google Ads",
	Description: "Ask Google Ads for keyword ideas from a seed (KeywordPlanIdeaService.GenerateKeywordIdeas): keywords " +
		"(1 to 20), a page address, both, or a site. Qatlas Connect never fetches the address or the site; it only " +
		"passes the text to Google, which reads the page. Each idea carries the historical search metrics of its " +
		"text, which may be the canonical form of several keywords, and optionally concept annotations. The " +
		"answer is paged: page_size is 1 to 500 and 100 when left out, and next_page_token goes into page_token " +
		"of the next call with every other argument unchanged. Google may answer more rows than asked; rows " +
		"beyond 500 are cut and the answer says truncated." + keywordNote,
	Tags: []string{"google", "ads", "keywords", "ideas"}, Provider: Provider, Risk: keywordRisk,
	InputSchema: encoded(object(withTargeting(schema{
		"customer_id":         customerIDArgument,
		"seed_keywords":       recListOf(recTextSchema(maxKeywordLength), keywordSeedLimit),
		"seed_url":            recTextSchema(maxURLArgument),
		"seed_site":           recTextSchema(keywordSiteLimit),
		"keyword_annotations": schema{"type": "array", "maxItems": 1, "uniqueItems": true, "items": recEnumSchema(keywordAnnotations)},
		"page_token":          schema{"type": "string", "minLength": 1, "maxLength": keywordPageToken},
		"page_size":           schema{"type": "integer", "minimum": 1, "maximum": keywordMetricsLimit},
	}), "customer_id")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema, "results": arrayOf(ideaOutputSchema),
		"aggregate_metric_results": aggregateOutputSchema, "next_page_token": stringSchema, "total_size": stringSchema,
		"truncated": booleanSchema}, "customer_id", "results")),
	Arguments: append([]capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "seed_keywords", Description: "1 to 20 keywords of at most 80 characters and 10 words; alone or with seed_url"},
		{Name: "seed_url", Description: "A page such as www.example.com/cars that Google reads, without a scheme or " +
			"with https and without user info; alone or with seed_keywords"},
		{Name: "seed_site", Description: "A domain such as www.example.com; if the account does not own the site Google " +
			"returns public information only. Cannot be combined with the other seeds"},
		{Name: "keyword_annotations", Description: "KEYWORD_CONCEPT adds the concepts of each idea"},
		{Name: "page_token", Description: "The next_page_token of the previous answer; every other argument stays the same"},
		{Name: "page_size", Description: "Ideas per page, 1 to 500, 100 when left out. Google caps pages at 10000 and may " +
			"return fewer; fewer than asked is not the last page"},
	}, targetingArguments...),
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "results", Description: "Per idea: text, keyword_idea_metrics (" + metricsFieldText + "), " +
			"keyword_annotations (name, concept_group_name, concept_group_type BRAND, OTHER_BRANDS or NON_BRAND) " +
			"when requested, and close_variants of the requested keywords that were combined into it"},
		{Name: "aggregate_metric_results", Description: "device_searches with device (MOBILE, TABLET or DESKTOP) and " +
			"search_count for all ideas, when aggregate_metric_types asks for DEVICE"},
		{Name: "next_page_token", Description: "Token of the next page; absent on the last page"},
		{Name: "total_size", Description: "Total number of ideas Google has, as a decimal string"},
		{Name: "truncated", Description: "true when Google answered more than 500 ideas on the page and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Ideas for two keywords in Germany",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","seed_keywords":["garden chairs","patio furniture"],` +
			`"language":"languageConstants/1001","geo_target_constants":["geoTargetConstants/2276"],"page_size":50}`)}},
}

func handleKeywordIdeas(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate keyword ideas"
	var args ideasArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, keywordIdeasID)
	if err != nil {
		return nil, err
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	var answer ideasAnswer
	if err := keywordCall(env, args.CustomerID, "generateKeywordIdeas", args.body(), &answer); err != nil {
		return nil, err
	}
	if len(answer.NextPageToken) > keywordPageToken {
		return nil, provider.InvalidResponse(op, subject+" returned a page token longer than Qatlas Connect passes on")
	}
	out := ideasResult{CustomerID: args.CustomerID, Results: []ideaItem{}, NextPageToken: answer.NextPageToken,
		TotalSize: wireDecimal(answer.TotalSize), AggregateMetricResults: aggregateOf(answer.AggregateMetricResults),
		Truncated: len(answer.Results) > maxListItems}
	for _, r := range answer.Results[:min(len(answer.Results), maxListItems)] {
		item := ideaItem{Text: boundedText(r.Text, maxText), KeywordIdeaMetrics: metricsOf(r.KeywordIdeaMetrics),
			CloseVariants: boundedList(r.CloseVariants, maxText)}
		if r.KeywordAnnotations != nil {
			for _, c := range r.KeywordAnnotations.Concepts[:min(len(r.KeywordAnnotations.Concepts), maxListItems)] {
				concept := ideaConcept{Name: boundedText(c.Name, maxText)}
				if c.ConceptGroup != nil {
					concept.ConceptGroupName = boundedText(c.ConceptGroup.Name, maxText)
					concept.ConceptGroupType = code(c.ConceptGroup.Type)
				}
				item.KeywordAnnotations = append(item.KeywordAnnotations, concept)
			}
		}
		out.Results = append(out.Results, item)
	}
	return out, nil
}

// --- generate-historical-metrics ---

type metricsArguments struct {
	CustomerID string   `json:"customer_id"`
	Keywords   []string `json:"keywords"`
	keywordTargeting
}

// check bounds the keywords by what the answer can hold: Google takes 10000, but a result list is cut at 500
// entries, so a larger list could lose results without a way to read them.
func (a metricsArguments) check() error {
	if !customerIDPattern.MatchString(a.CustomerID) {
		return badRequest("customer_id must be the ten-digit customer ID without hyphens")
	}
	if err := checkPlainKeywords("keywords", a.Keywords, 1, keywordMetricsLimit); err != nil {
		return err
	}
	return a.keywordTargeting.check()
}

func (a metricsArguments) body() map[string]any {
	body := map[string]any{"keywords": a.Keywords}
	a.keywordTargeting.fill(body)
	return body
}

type (
	metricsAnswer struct {
		Results []struct {
			Text          string       `json:"text"`
			CloseVariants []string     `json:"closeVariants"`
			KeywordMetric *metricsWire `json:"keywordMetrics"`
		} `json:"results"`
		AggregateMetricResults *aggregateWire `json:"aggregateMetricResults"`
	}
	metricsItem struct {
		Text           string          `json:"text,omitempty"`
		CloseVariants  []string        `json:"close_variants,omitempty"`
		KeywordMetrics *keywordMetrics `json:"keyword_metrics,omitempty"`
	}
	metricsResult struct {
		CustomerID             string           `json:"customer_id"`
		Results                []metricsItem    `json:"results"`
		AggregateMetricResults *aggregateResult `json:"aggregate_metric_results,omitempty"`
		Truncated              bool             `json:"truncated,omitempty"`
	}
)

var keywordMetricsTool = capability.Descriptor{
	ID: keywordMetricsID, Version: 1, Title: "Get historical search metrics for keywords with Google Ads",
	Description: "Ask Google Ads for the historical search metrics of keywords " +
		"(KeywordPlanIdeaService.GenerateKeywordHistoricalMetrics). Google merges near-identical keywords: of " +
		"\"car\" and \"cars\" only one text comes back, and its close_variants name the merged ones. Google takes up to " +
		"10000 keywords; Qatlas Connect takes 500 per call, the most a result list holds, so no result is lost." +
		keywordNote,
	Tags: []string{"google", "ads", "keywords", "metrics"}, Provider: Provider, Risk: keywordRisk,
	InputSchema: encoded(object(withTargeting(schema{
		"customer_id": customerIDArgument,
		"keywords":    recListOf(recTextSchema(maxKeywordLength), keywordMetricsLimit),
	}), "customer_id", "keywords")),
	OutputSchema: encoded(object(schema{"customer_id": stringSchema,
		"results": arrayOf(object(schema{"text": stringSchema, "close_variants": arrayOf(stringSchema),
			"keyword_metrics": metricsOutputSchema})),
		"aggregate_metric_results": aggregateOutputSchema, "truncated": booleanSchema}, "customer_id", "results")),
	Arguments: append([]capability.Argument{
		{Name: "customer_id", Required: true, Description: "Ten-digit customer ID of the account without hyphens; a " +
			"target of the connection or an account below a manager target"},
		{Name: "keywords", Required: true, Description: "1 to 500 keywords of at most 80 characters and 10 words"},
	}, targetingArguments...),
	Fields: []capability.Field{
		{Name: "customer_id", Description: "The account of the call"},
		{Name: "results", Description: "Per keyword text: text, close_variants and keyword_metrics (" + metricsFieldText + ")"},
		{Name: "aggregate_metric_results", Description: "device_searches with device (MOBILE, TABLET or DESKTOP) and " +
			"search_count for all keywords, when aggregate_metric_types asks for DEVICE"},
		{Name: "truncated", Description: "true when Google answered more than 500 results and the rest was cut"},
	},
	Examples: []capability.Example{{Description: "Metrics for two keywords over a range of months",
		Arguments: json.RawMessage(`{"customer_id":"1234567890","keywords":["garden chairs","patio furniture"],` +
			`"geo_target_constants":["geoTargetConstants/2276"],"historical_metrics_options":{"year_month_range":` +
			`{"start":{"year":2025,"month":"JANUARY"},"end":{"year":2025,"month":"JUNE"}}}}`)}},
}

func handleKeywordMetrics(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "generate keyword historical metrics"
	var args metricsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, failure(op, "the validated arguments could not be read")
	}
	env, err := handleEnv(ctx, resolved, secrets, red, op, keywordMetricsID)
	if err != nil {
		return nil, err
	}
	if err := args.check(); err != nil {
		return nil, err
	}
	var answer metricsAnswer
	if err := keywordCall(env, args.CustomerID, "generateKeywordHistoricalMetrics", args.body(), &answer); err != nil {
		return nil, err
	}
	out := metricsResult{CustomerID: args.CustomerID, Results: []metricsItem{},
		AggregateMetricResults: aggregateOf(answer.AggregateMetricResults), Truncated: len(answer.Results) > maxListItems}
	for _, r := range answer.Results[:min(len(answer.Results), maxListItems)] {
		out.Results = append(out.Results, metricsItem{Text: boundedText(r.Text, maxText),
			CloseVariants: boundedList(r.CloseVariants, maxText), KeywordMetrics: metricsOf(r.KeywordMetric)})
	}
	return out, nil
}

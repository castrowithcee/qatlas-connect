package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"math"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const googleLocationsSearchID = Provider + ".google-locations.search"

// maxGoogleLocations is the largest number of matches Google documents; the search has no continuation.
const maxGoogleLocations = 10

const maxAddressLines = 5

// The search request as the discovery document documents it: a text query or the details of a location,
// which must include its title. Only the details Google can match on are offered; hours, services, and the
// fields that only describe a profile you manage are not.
type (
	locationQuery struct {
		Title             string        `json:"title"`
		LanguageCode      string        `json:"languageCode,omitempty"`
		StorefrontAddress *addressQuery `json:"storefrontAddress,omitempty"`
		WebsiteURI        string        `json:"websiteUri,omitempty"`
		PhoneNumbers      *phoneQuery   `json:"phoneNumbers,omitempty"`
		Categories        *categoryRef  `json:"categories,omitempty"`
		Latlng            *latlngQuery  `json:"latlng,omitempty"`
	}
	addressQuery struct {
		RegionCode         string   `json:"regionCode,omitempty"`
		LanguageCode       string   `json:"languageCode,omitempty"`
		PostalCode         string   `json:"postalCode,omitempty"`
		AdministrativeArea string   `json:"administrativeArea,omitempty"`
		Locality           string   `json:"locality,omitempty"`
		Sublocality        string   `json:"sublocality,omitempty"`
		AddressLines       []string `json:"addressLines,omitempty"`
	}
	phoneQuery struct {
		PrimaryPhone string `json:"primaryPhone,omitempty"`
	}
	categoryRef struct {
		PrimaryCategory *categoryName `json:"primaryCategory,omitempty"`
	}
	categoryName struct {
		Name string `json:"name,omitempty"`
	}
	latlngQuery struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	googleLocationsArguments struct {
		Query    string         `json:"query"`
		Location *locationQuery `json:"location"`
		PageSize int            `json:"page_size"`
	}
	searchBody struct {
		Query    string         `json:"query,omitempty"`
		Location *locationQuery `json:"location,omitempty"`
		PageSize int            `json:"pageSize,omitempty"`
	}
)

// The answer: potential matches, best first. location is sparse; requestAdminRightsUri is present only for a
// location some user has already claimed.
type (
	googleLocation struct {
		Name                  short     `json:"name,omitempty"`
		RequestAdminRightsURI link      `json:"requestAdminRightsUri,omitempty"`
		Location              *location `json:"location,omitempty"`
	}
	googleLocationsAnswer struct {
		GoogleLocations []googleLocation `json:"googleLocations"`
	}
)

var (
	googleLocationSchema = objectSchema("name", stringSchema, "requestAdminRightsUri", stringSchema,
		"location", locationSchema)
	googleLocationsResultSchema = `{"type":"object","properties":{"googleLocations":` +
		arraySchema(googleLocationSchema) + `},"required":["googleLocations"],"additionalProperties":false}`
)

const (
	lineSchema = `{"type":"string","minLength":1,"maxLength":500}`
	// locationQuerySchema is the closed object of the details the search matches on; title is required.
	locationQuerySchema = `{"type":"object","properties":{` +
		`"title":` + lineSchema + `,"languageCode":` + languageArgSchema + `,` +
		`"storefrontAddress":{"type":"object","properties":{"regionCode":` + regionArgSchema + `,` +
		`"languageCode":` + languageArgSchema + `,"postalCode":{"type":"string","minLength":1,"maxLength":100},` +
		`"administrativeArea":` + lineSchema + `,"locality":` + lineSchema + `,"sublocality":` + lineSchema + `,` +
		`"addressLines":{"type":"array","minItems":1,"maxItems":5,"items":` + lineSchema + `}},` +
		`"additionalProperties":false},` +
		`"websiteUri":{"type":"string","minLength":1,"maxLength":2048},` +
		`"phoneNumbers":{"type":"object","properties":{"primaryPhone":{"type":"string","minLength":1,` +
		`"maxLength":100}},"additionalProperties":false},` +
		`"categories":{"type":"object","properties":{"primaryCategory":{"type":"object","properties":{"name":` +
		`{"type":"string","maxLength":120,"pattern":"` + categoryPattern + `"}},"additionalProperties":false}},` +
		`"additionalProperties":false},` +
		`"latlng":{"type":"object","properties":{"latitude":{"type":"number","minimum":-90,"maximum":90},` +
		`"longitude":{"type":"number","minimum":-180,"maximum":180}},"required":["latitude","longitude"],` +
		`"additionalProperties":false}},"required":["title"],"additionalProperties":false}`
)

var googleLocationsSearch = capability.Descriptor{
	ID: googleLocationsSearchID, Version: 1, Title: "Search Google locations",
	Description: "Search the locations that already exist on Google, by a text query or by the details of a " +
		"location (title required, optionally address, website, phone number, primary category, and " +
		"coordinates); give exactly one of query and location. Google returns up to 10 potential matches (default " +
		"3), best first, without a cursor; no match is an empty list. A match can be any business, also one " +
		"that is not the user's: the result holds what Google lists publicly, and requestAdminRightsUri is present " +
		"only for a location that some user has already claimed. " + referenceNote + " The query and the " +
		"location details are sent to Google as search terms and are not logged. Names, addresses, phone " +
		"numbers, and websites are untrusted content: texts and lists are cut and addresses are text that is " +
		"never fetched.",
	Tags: []string{"google", "business-profile", "locations", "search"}, Risk: googleLocationsRisk,
	Provider: Provider,
	InputSchema: inputSchema(nil,
		"query", searchArgSchema,
		"location", locationQuerySchema,
		"page_size", `{"type":"integer","minimum":1,"maximum":10}`),
	OutputSchema: json.RawMessage(googleLocationsResultSchema),
	Arguments: []capability.Argument{
		{Name: "query", Description: "Free text to search for, such as a business name and a town; less exact than " +
			"location. Give this or location, not both"},
		{Name: "location", Description: "Details of the location to find as an object: title (required), " +
			"languageCode, storefrontAddress (regionCode, languageCode, postalCode, administrativeArea, locality, " +
			"sublocality, addressLines), websiteUri, phoneNumbers.primaryPhone, " +
			"categories.primaryCategory.name (categories/gcid:ID), latlng (latitude, longitude). Give this or query, " +
			"not both"},
		{Name: "page_size", Description: "Matches to return, 1 to 10; Google's default is 3"},
	},
	Fields: []capability.Field{{Name: "googleLocations", Description: "Potential matches, best first, each with " +
		"name (googleLocations/ID), requestAdminRightsUri (only if the location is already claimed), and location " +
		"(sparse: title, address, phone numbers, website, categories, metadata with placeId and mapsUri)"}},
	Examples: []capability.Example{
		{Description: "By text", Arguments: json.RawMessage(`{"query":"sample bakery in sample town"}`)},
		{Description: "By details", Arguments: json.RawMessage(
			`{"location":{"title":"Sample Bakery","storefrontAddress":{"regionCode":"DE","locality":"Sample Town"}},` +
				`"page_size":5}`)},
	},
}

var googleLocationsRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "business data Google lists publicly, of any business and not of the user: names, postal " +
		"addresses, phone numbers, websites, categories, and whether someone has claimed the location; the " +
		"search terms are sent to Google and are not logged",
}

func checkOptional(name, text string, limit int) error {
	if text == "" {
		return nil
	}
	return checkText(name, text, limit)
}

// checkLocationQuery checks every detail of a location query before it is sent.
func checkLocationQuery(l *locationQuery) error {
	if err := checkText("location.title", l.Title, maxSearchRunes); err != nil {
		return err
	}
	if l.LanguageCode != "" {
		if err := checkLanguage("location.languageCode", l.LanguageCode); err != nil {
			return err
		}
	}
	if a := l.StorefrontAddress; a != nil {
		if err := checkRegion(a.RegionCode, false); err != nil {
			return err
		}
		if a.LanguageCode != "" {
			if err := checkLanguage("location.storefrontAddress.languageCode", a.LanguageCode); err != nil {
				return err
			}
		}
		for name, text := range map[string]string{"postalCode": a.PostalCode,
			"administrativeArea": a.AdministrativeArea, "locality": a.Locality, "sublocality": a.Sublocality} {
			if err := checkOptional("location.storefrontAddress."+name, text, maxSearchRunes); err != nil {
				return err
			}
		}
		if len(a.AddressLines) > maxAddressLines {
			return invalidArgument("location.storefrontAddress.addressLines holds at most 5 lines")
		}
		for _, line := range a.AddressLines {
			if err := checkText("location.storefrontAddress.addressLines", line, maxSearchRunes); err != nil {
				return err
			}
		}
	}
	if err := checkOptional("location.websiteUri", l.WebsiteURI, maxURLRunes); err != nil {
		return err
	}
	if l.PhoneNumbers != nil {
		if err := checkOptional("location.phoneNumbers.primaryPhone", l.PhoneNumbers.PrimaryPhone, 100); err != nil {
			return err
		}
	}
	if c := l.Categories; c != nil && c.PrimaryCategory != nil && c.PrimaryCategory.Name != "" &&
		(!categoryForm.MatchString(c.PrimaryCategory.Name)) {
		return invalidArgument("location.categories.primaryCategory.name must be categories/gcid:ID")
	}
	if g := l.Latlng; g != nil {
		if g.Latitude == nil || g.Longitude == nil || math.IsNaN(*g.Latitude) || math.IsNaN(*g.Longitude) ||
			math.Abs(*g.Latitude) > 90 || math.Abs(*g.Longitude) > 180 {
			return invalidArgument("location.latlng needs latitude between -90 and 90 and longitude between -180 and 180")
		}
	}
	return nil
}

func handleGoogleLocationsSearch(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "search Google locations"
	var args googleLocationsArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, provider.Fail(op, "the validated arguments could not be read")
	}
	switch {
	case args.Query == "" && args.Location == nil:
		return nil, invalidArgument("give query or location")
	case args.Query != "" && args.Location != nil:
		return nil, invalidArgument("give query or location, not both")
	case args.PageSize < 0 || args.PageSize > maxGoogleLocations:
		return nil, invalidArgument("page_size must be 1 to 10")
	}
	if args.Query != "" {
		if err := checkText("query", args.Query, maxSearchRunes); err != nil {
			return nil, err
		}
	} else if err := checkLocationQuery(args.Location); err != nil {
		return nil, err
	}
	if _, err := checkConnection(resolved); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	var answer googleLocationsAnswer
	body := searchBody{Query: args.Query, Location: args.Location, PageSize: args.PageSize}
	if err := c.postJSON(ctx, op, googleLocationsSearchID, request{root: informationRoot,
		path: "googleLocations:search", notFound: notFoundReference}, body, &answer); err != nil {
		return nil, err
	}
	if len(answer.GoogleLocations) > maxGoogleLocations {
		return nil, tooMany(op, maxGoogleLocations)
	}
	return googleLocationsAnswer{GoogleLocations: append([]googleLocation{}, answer.GoogleLocations...)}, nil
}

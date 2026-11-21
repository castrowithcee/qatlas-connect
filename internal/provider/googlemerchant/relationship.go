package googlemerchant

import "strings"

// The AccountRelationship resource of the accounts sub-API, shared by every tool that reads or changes
// relationships. Its text members come from Google or from the provider account and are untrusted, so the
// text types of shape.go cut them, and a member the discovery document does not list is never held.

// Well-known providers that are not accounts: their display names are Google's own.
const (
	providerPrefix = "providers/"
	providerAds    = providerPrefix + "GOOGLE_ADS"
	providerGBP    = providerPrefix + "GOOGLE_BUSINESS_PROFILE"
)

// accountRelationship is a connection between the account and a provider account. provider is an identifier;
// providerDisplayName is a display value of the provider account and is kept only where
// scope.showsProviderName allows it.
type accountRelationship struct {
	Name                idText   `json:"name,omitempty"`
	Provider            idText   `json:"provider,omitempty"`
	ProviderDisplayName nameText `json:"providerDisplayName,omitempty"`
	AccountIDAlias      nameText `json:"accountIdAlias,omitempty"`
}

// showsProviderName reports whether the display name of a linked provider account may be shown. Objects
// outside the binding give identifiers only, and whether an account lies inside is settled without any
// request: without targets the credential reaches everything, a provider account that a target names is
// inside, and Google Ads and Google Business Profile are no accounts. Any other provider is masked,
// including one that might be a sub-account of a bound advanced account, which only a read could tell.
func (s scope) showsProviderName(provider idText) bool {
	if !s.bound() || provider == providerAds || provider == providerGBP {
		return true
	}
	id, ok := strings.CutPrefix(string(provider), providerPrefix)
	return ok && validID(id) && s.reachOf(id) == reachNamed
}

// masked drops the display name of a provider the binding does not show.
func (r accountRelationship) masked(s scope) accountRelationship {
	if !s.showsProviderName(r.Provider) {
		r.ProviderDisplayName = ""
	}
	return r
}

type accountRelationshipsAnswer struct {
	AccountRelationships []accountRelationship `json:"accountRelationships"`
	NextPageToken        string                `json:"nextPageToken"`
}

// accountRelationshipsResult is the result of the list tool; a list is never null.
type accountRelationshipsResult struct {
	AccountRelationships []accountRelationship `json:"accountRelationships"`
	NextCursor           string                `json:"next_cursor,omitempty"`
}

const relationshipFieldsDescription = "name (accounts/ACCOUNT/relationships/RELATIONSHIP), provider " +
	"(providers/ID of the provider account, or providers/GOOGLE_ADS or providers/GOOGLE_BUSINESS_PROFILE), " +
	"providerDisplayName (shown only for a provider the connection's targets name, for Google Ads and Google " +
	"Business Profile, or on a connection without targets), and accountIdAlias"

var accountRelationshipSchema = objectSchema(member("name", stringSchema), member("provider", stringSchema),
	member("providerDisplayName", stringSchema), member("accountIdAlias", stringSchema))

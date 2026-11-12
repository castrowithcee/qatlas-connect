package gen

import "slices"

const (
	deferR53VPC      = "VPC association writes wait for their own tool group: a VPC of another account must be ruled out before a zone is associated or an authorization is removed"
	deferR53Features = "hosted zone feature changes wait for their own tool group"
)

// route53Table curates the route-53 model. Every reader is curated; the writes of the areas that are only read so far wait for their tool groups.
var route53Table = Table{
	Service: "route-53", Segment: "route53", Geltung: Global,
	// A record set list continues at a name and a type, and at an identifier when the records are weighted,
	// latency or failover sets that share a name.
	Pagers: []PagerSpec{
		{Operation: "ListResourceRecordSets", Truncated: "IsTruncated", PageSize: "MaxItems", StartArguments: true, Positions: []PositionSpec{
			{In: "StartRecordName", Out: "NextRecordName", Required: true},
			{In: "StartRecordType", Out: "NextRecordType", Required: true},
			{In: "StartRecordIdentifier", Out: "NextRecordIdentifier"}}},
		{Operation: "ListReusableDelegationSets", PageSize: "MaxItems", Positions: []PositionSpec{{In: "Marker", Out: "NextMarker"}}},
		{Operation: "ListTrafficPolicies", PageSize: "MaxItems", Positions: []PositionSpec{{In: "TrafficPolicyIdMarker", Out: "TrafficPolicyIdMarker"}}},
		{Operation: "ListTrafficPolicyVersions", PageSize: "MaxItems", Positions: []PositionSpec{{In: "TrafficPolicyVersionMarker", Out: "TrafficPolicyVersionMarker"}}},
		// The marker of an instance list is three members that Route 53 reports together; they only continue a list.
		{Operation: "ListTrafficPolicyInstances", PageSize: "MaxItems", Positions: []PositionSpec{
			{In: "HostedZoneIdMarker", Out: "HostedZoneIdMarker", Required: true},
			{In: "TrafficPolicyInstanceNameMarker", Out: "TrafficPolicyInstanceNameMarker", Required: true},
			{In: "TrafficPolicyInstanceTypeMarker", Out: "TrafficPolicyInstanceTypeMarker", Required: true}}},
		{Operation: "ListTrafficPolicyInstancesByHostedZone", PageSize: "MaxItems", Positions: []PositionSpec{
			{In: "TrafficPolicyInstanceNameMarker", Out: "TrafficPolicyInstanceNameMarker", Required: true},
			{In: "TrafficPolicyInstanceTypeMarker", Out: "TrafficPolicyInstanceTypeMarker", Required: true}}},
		{Operation: "ListTrafficPolicyInstancesByPolicy", PageSize: "MaxItems", Positions: []PositionSpec{
			{In: "HostedZoneIdMarker", Out: "HostedZoneIdMarker", Required: true},
			{In: "TrafficPolicyInstanceNameMarker", Out: "TrafficPolicyInstanceNameMarker", Required: true},
			{In: "TrafficPolicyInstanceTypeMarker", Out: "TrafficPolicyInstanceTypeMarker", Required: true}}},
		// A geolocation list continues at a continent, a country, or a country with a subdivision, whichever comes next, so no
		// member is always there; the first page has no start arguments.
		{Operation: "ListGeoLocations", PageSize: "MaxItems", Positions: []PositionSpec{
			{In: "StartContinentCode", Out: "NextContinentCode"}, {In: "StartCountryCode", Out: "NextCountryCode"},
			{In: "StartSubdivisionCode", Out: "NextSubdivisionCode"}}},
		// These two lists answer no truncation member; the end is a missing token.
		{Operation: "ListHostedZonesByVPC", PageSize: "MaxItems", TokenEnds: true, Positions: []PositionSpec{{In: "NextToken", Out: "NextToken"}}},
		{Operation: "ListVPCAssociationAuthorizations", PageSize: "MaxResults", TokenEnds: true, Positions: []PositionSpec{{In: "NextToken", Out: "NextToken"}}},
	},
	// Route 53 answers zone IDs as /hostedzone/Z..., and accepts both forms only in some places. The tools take both. The binding is per shape, and ResourceId also types the
	// delegation set arguments, so those take /hostedzone/ as well (stripped, then refused by Route 53 as an unknown set).
	IDPrefixes: []IDPrefix{{Shape: "ResourceId", Prefix: "/hostedzone/"}, {Shape: "ResourceId", Prefix: "/delegationset/", Operations: []string{"GetReusableDelegationSet", "GetReusableDelegationSetLimit", "DeleteReusableDelegationSet", "CreateHostedZone"}}, {Shape: "TagResourceId", Prefix: "/hostedzone/"},
		{Shape: "ChangeId", Prefix: "/change/"}},
	Operations: slices.Concat(route53ZoneTools, route53RecordTools, route53HealthTools, route53DNSSECTools, route53DelegationTools, route53CIDRTools, route53PolicyTools, route53InstanceTools, route53ReadTools, route53LoggingTools),
	Exclude: []Omission{
		{Operation: "CreateVPCAssociationAuthorization", Reason: "authorizing a VPC of another account to join a private zone lets that account resolve the zone's records; nothing checks the account, so it is not offered"},
	},
	Defer: []Omission{
		{Operation: "AssociateVPCWithHostedZone", Reason: deferR53VPC},
		{Operation: "DeleteVPCAssociationAuthorization", Reason: deferR53VPC},
		{Operation: "DisassociateVPCFromHostedZone", Reason: deferR53VPC},
		{Operation: "UpdateHostedZoneFeatures", Reason: deferR53Features},
	},
}

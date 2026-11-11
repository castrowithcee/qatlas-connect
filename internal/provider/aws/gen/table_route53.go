package gen

import "slices"

// deferRoute53 is the reason for every route-53 operation: the table is a raw scaffold, and the service package that
// curates the tools moves each operation it takes into operations or exclude.
const deferRoute53 = "not curated yet; the Route 53 service package decides effect, confirmation and zone target first"

// route53Table curates the route-53 model. The hosted zone tools are curated; the other operations wait for their tool groups.
var route53Table = Table{
	Service: "route-53", Segment: "route53", Geltung: Global,
	// A record set list continues at a name and a type, and at an identifier when the records are weighted,
	// latency or failover sets that share a name.
	Pagers: []PagerSpec{
		{Operation: "ListResourceRecordSets", Truncated: "IsTruncated", PageSize: "MaxItems", StartArguments: true, Positions: []PositionSpec{
			{In: "StartRecordName", Out: "NextRecordName", Required: true},
			{In: "StartRecordType", Out: "NextRecordType", Required: true},
			{In: "StartRecordIdentifier", Out: "NextRecordIdentifier"}}},
	},
	// Route 53 answers zone IDs as /hostedzone/Z..., and accepts both forms only in some places. The tools take both.
	IDPrefixes: []IDPrefix{{Shape: "ResourceId", Prefix: "/hostedzone/"}, {Shape: "TagResourceId", Prefix: "/hostedzone/"},
		{Shape: "ChangeId", Prefix: "/change/"}},
	Operations: slices.Concat(route53ZoneTools, route53RecordTools),
	Exclude: []Omission{
		{Operation: "CreateVPCAssociationAuthorization", Reason: "authorizing a VPC of another account to join a private zone lets that account resolve the zone's records; nothing checks the account, so it is not offered"},
	},
	Defer: []Omission{
		{Operation: "ActivateKeySigningKey", Reason: deferRoute53},
		{Operation: "AssociateVPCWithHostedZone", Reason: deferRoute53},
		{Operation: "ChangeCidrCollection", Reason: deferRoute53},
		{Operation: "CreateCidrCollection", Reason: deferRoute53},
		{Operation: "CreateHealthCheck", Reason: deferRoute53},
		{Operation: "CreateKeySigningKey", Reason: deferRoute53},
		{Operation: "CreateQueryLoggingConfig", Reason: deferRoute53},
		{Operation: "CreateReusableDelegationSet", Reason: deferRoute53},
		{Operation: "CreateTrafficPolicy", Reason: deferRoute53},
		{Operation: "CreateTrafficPolicyInstance", Reason: deferRoute53},
		{Operation: "CreateTrafficPolicyVersion", Reason: deferRoute53},
		{Operation: "DeactivateKeySigningKey", Reason: deferRoute53},
		{Operation: "DeleteCidrCollection", Reason: deferRoute53},
		{Operation: "DeleteHealthCheck", Reason: deferRoute53},
		{Operation: "DeleteKeySigningKey", Reason: deferRoute53},
		{Operation: "DeleteQueryLoggingConfig", Reason: deferRoute53},
		{Operation: "DeleteReusableDelegationSet", Reason: deferRoute53},
		{Operation: "DeleteTrafficPolicy", Reason: deferRoute53},
		{Operation: "DeleteTrafficPolicyInstance", Reason: deferRoute53},
		{Operation: "DeleteVPCAssociationAuthorization", Reason: deferRoute53},
		{Operation: "DisableHostedZoneDNSSEC", Reason: deferRoute53},
		{Operation: "DisassociateVPCFromHostedZone", Reason: deferRoute53},
		{Operation: "EnableHostedZoneDNSSEC", Reason: deferRoute53},
		{Operation: "GetAccountLimit", Reason: deferRoute53},
		{Operation: "GetCheckerIpRanges", Reason: deferRoute53},
		{Operation: "GetDNSSEC", Reason: deferRoute53},
		{Operation: "GetGeoLocation", Reason: deferRoute53},
		{Operation: "GetHealthCheck", Reason: deferRoute53},
		{Operation: "GetHealthCheckCount", Reason: deferRoute53},
		{Operation: "GetHealthCheckLastFailureReason", Reason: deferRoute53},
		{Operation: "GetHealthCheckStatus", Reason: deferRoute53},
		{Operation: "GetHostedZoneLimit", Reason: deferRoute53},
		{Operation: "GetQueryLoggingConfig", Reason: deferRoute53},
		{Operation: "GetReusableDelegationSet", Reason: deferRoute53},
		{Operation: "GetReusableDelegationSetLimit", Reason: deferRoute53},
		{Operation: "GetTrafficPolicy", Reason: deferRoute53},
		{Operation: "GetTrafficPolicyInstance", Reason: deferRoute53},
		{Operation: "GetTrafficPolicyInstanceCount", Reason: deferRoute53},
		{Operation: "ListCidrBlocks", Reason: deferRoute53},
		{Operation: "ListCidrCollections", Reason: deferRoute53},
		{Operation: "ListCidrLocations", Reason: deferRoute53},
		{Operation: "ListGeoLocations", Reason: deferRoute53},
		{Operation: "ListHealthChecks", Reason: deferRoute53},
		{Operation: "ListHostedZonesByVPC", Reason: deferRoute53},
		{Operation: "ListQueryLoggingConfigs", Reason: deferRoute53},
		{Operation: "ListReusableDelegationSets", Reason: deferRoute53},
		{Operation: "ListTagsForResources", Reason: deferRoute53},
		{Operation: "ListTrafficPolicies", Reason: deferRoute53},
		{Operation: "ListTrafficPolicyInstances", Reason: deferRoute53},
		{Operation: "ListTrafficPolicyInstancesByHostedZone", Reason: deferRoute53},
		{Operation: "ListTrafficPolicyInstancesByPolicy", Reason: deferRoute53},
		{Operation: "ListTrafficPolicyVersions", Reason: deferRoute53},
		{Operation: "ListVPCAssociationAuthorizations", Reason: deferRoute53},
		{Operation: "UpdateHealthCheck", Reason: deferRoute53},
		{Operation: "UpdateHostedZoneFeatures", Reason: deferRoute53},
		{Operation: "UpdateTrafficPolicyComment", Reason: deferRoute53},
		{Operation: "UpdateTrafficPolicyInstance", Reason: deferRoute53},
	},
}

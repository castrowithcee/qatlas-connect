package gen

import "slices"

const (
	descSESEndpointName = "Name of the multi-region endpoint, 1 to 64 letters, digits, hyphens, and underscores"

	sesMultiRegionCost = "Global endpoints are expected to cost more than single-region sending for the same volume of mail; " +
		"see the Amazon SES pricing page, the price is not retrieved by this tool."

	reasonSESEndpointRegion = "the model gives no bound on a region name; region names are short"
)

// sesMultiRegionTools are the SES v2 operations on multi-region endpoints, also called global endpoints. An
// endpoint spreads the sending of the account over the region of the call and one more region.
var sesMultiRegionTools = []Operation{
	sesDeliverRead(Operation{
		Operation: "ListMultiRegionEndpoints", Object: "multi-region-endpoint", Action: "list",
		Title:       "List SES multi-region endpoints",
		Description: "List the multi-region endpoints (global endpoints) that have the region of the call as primary region, with name, ID, status, regions, and timestamps. Names are untrusted. Needs ses:ListMultiRegionEndpoints.",
		Arguments:   map[string]Argument{},
		Fields:      map[string]Field{"MultiRegionEndpoints": {Description: "Endpoints of this page, each with endpoint_name, endpoint_id, status, regions, created_timestamp, and last_updated_timestamp"}},
		Examples:    []Example{{Description: "List the endpoints of a region", Arguments: `{"region":"eu-central-1"}`}},
	}, sesDeliverSensitivity, "multi-region"),
	sesDeliverRead(Operation{
		Operation: "GetMultiRegionEndpoint", Object: "multi-region-endpoint", Action: "get",
		Title:       "Get an SES multi-region endpoint",
		Description: "Return one multi-region endpoint (global endpoint) by name from its primary region: ID, status, routes with their regions, and timestamps. Needs ses:GetMultiRegionEndpoint.",
		Arguments:   map[string]Argument{"EndpointName": {Description: descSESEndpointName}},
		Fields: map[string]Field{
			"EndpointName":         {Description: "Name of the endpoint"},
			"EndpointId":           {Description: "ID of the endpoint, which SendEmail takes as endpoint_id"},
			"Routes":               {Description: "Regions the endpoint routes to, each with region"},
			"Status":               {Description: "CREATING, READY, FAILED, or DELETING"},
			"CreatedTimestamp":     {Description: "When the endpoint was created"},
			"LastUpdatedTimestamp": {Description: "When the endpoint was last changed"},
		},
		Examples: []Example{{Description: "Read an endpoint", Arguments: `{"region":"eu-central-1","endpoint_name":"my-endpoint"}`}},
	}, sesDeliverSensitivity, "multi-region"),
	sesDeliverWrite(Operation{
		Operation: "CreateMultiRegionEndpoint", Object: "multi-region-endpoint", Action: "create",
		Title: "Create an SES multi-region endpoint",
		Description: "Create a multi-region endpoint (global endpoint) with the region of the call as primary region and the one region " +
			"in details.routes_details as secondary region. SES then splits sending between the two regions and shifts traffic " +
			"away from an impaired one. Both regions must be regions the connection allows, else the call is refused before " +
			"any request. Identities, configuration sets, and sending limits are not copied by this tool and must match in both " +
			"regions. SES needs the permission to create a service-linked role. " + sesMultiRegionCost + " Needs " +
			"ses:CreateMultiRegionEndpoint, and ses:TagResource with tags. Not repeated after an unclear outcome: read the " +
			"endpoint before trying again. Needs confirmation; the confirmation concerns the endpoint named in endpoint_name " +
			"and the regions of the call and of details.routes_details.",
		Arguments: map[string]Argument{
			"EndpointName": {Description: descSESEndpointName},
			"Details":      {Description: "routes_details: exactly one entry with the region of the secondary region, which must be a region the connection allows"},
			"Tags":         {Description: descSESTags},
		},
		Fields: map[string]Field{"Status": {Description: "CREATING, READY, FAILED, or DELETING"}, "EndpointId": {Description: "ID of the endpoint, which SendEmail takes as endpoint_id"}},
		Limits: slices.Concat(sesTagLimits, []InputLimit{{Member: "Details.RoutesDetails", MaxItems: 1, Reason: "the API takes exactly one route"}, {Member: "Details.RoutesDetails.Region", MaxLen: 32, Reason: reasonSESEndpointRegion}}),
		Guards: []string{"ses-multi-region"},
		// The generated tests bind one region, so the sample names it as secondary region too.
		SampleArgs: `{"endpoint_name":"a","details":{"routes_details":[{"region":"eu-central-1"}]}}`,
		Examples:   []Example{{Description: "Create an endpoint over two regions", Arguments: `{"region":"eu-central-1","endpoint_name":"my-endpoint","details":{"routes_details":[{"region":"eu-west-1"}]}}`}},
	}, "create", "non-idempotent", "multi-region"),
	sesDeliverWrite(Operation{
		Operation: "DeleteMultiRegionEndpoint", Object: "multi-region-endpoint", Action: "delete",
		Title: "Delete an SES multi-region endpoint",
		Description: "Delete a multi-region endpoint (global endpoint) by name from its primary region. Mail sent with its endpoint ID " +
			"then fails. Needs ses:DeleteMultiRegionEndpoint. Not repeated after an unclear outcome. Needs confirmation; the " +
			"confirmation concerns the endpoint named in endpoint_name and the region of the call.",
		Arguments: map[string]Argument{"EndpointName": {Description: descSESEndpointName}},
		Fields:    map[string]Field{"Status": {Description: "CREATING, READY, FAILED, or DELETING"}},
		Examples:  []Example{{Description: "Delete an endpoint", Arguments: `{"region":"eu-central-1","endpoint_name":"my-endpoint"}`}},
	}, "delete", "non-idempotent", "multi-region"),
}

func init() {
	declareGuards(
		"ses-multi-region",
	)
}

package gen

import "slices"

const (
	descAccountID     = "12-digit ID of the account; only the account this connection is bound to is accepted, and it is the default"
	descAccountRegion = "Name of a region of the aws partition, such as af-south-1"
	accountRegionNote = "Changing the region does not change the region list of this connection: a regional tool uses a " +
		"region only when region/<name> is a target of the connection."
)

// accountRegionRead and accountRegionWrite fill what the region tools share. The service is global, so the
// region argument names the opted region and not where the call goes.
func accountRegionRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = "aws-account-configuration"
	op.Tags = slices.Concat([]string{"aws", "account", "region"}, tags)
	op.TargetCheck, op.TargetArgs = "account-argument", []string{"AccountId"}
	op.PolicyCheck = "none"
	return op
}

func accountRegionWrite(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = "update", "idempotent", true
	op.DataSensitivity = "aws-account-configuration"
	op.Tags = slices.Concat([]string{"aws", "account", "region"}, tags)
	op.TargetCheck, op.TargetArgs = "account-argument", []string{"AccountId"}
	op.PolicyCheck = "none"
	op.Guards = []string{"region-name"}
	op.SampleArgs = `{"region_name":"af-south-1"}`
	op.ResultHook = "region-allowlist"
	return op
}

// accountRegionTools are the account operations on the opt-in state of regions.
var accountRegionTools = []Operation{
	accountRegionRead(Operation{
		Operation: "ListRegions", Object: "region", Action: "list",
		Title: "List the regions of the account with their opt-in status",
		Description: "List the regions of the bound account with region_opt_status: ENABLED_BY_DEFAULT, ENABLED, ENABLING, " +
			"DISABLING, or DISABLED. Without a filter every region is listed. The status does not say which regions this " +
			"connection allows: that is the region list of the connection. Needs account:ListRegions.",
		Arguments: map[string]Argument{
			"AccountId":               {Description: descAccountID},
			"RegionOptStatusContains": {Description: "Only regions with one of these statuses: ENABLED, ENABLING, DISABLING, DISABLED, or ENABLED_BY_DEFAULT"},
		},
		Fields:      map[string]Field{"Regions": {Description: "Regions of this page, each with region_name and region_opt_status", MaxItems: 50}},
		PageSizeMax: 50,
		Limits:      []InputLimit{{Member: "RegionOptStatusContains", MaxItems: 5, Reason: "the model has five statuses"}},
		Examples:    []Example{{Description: "List the regions that are enabled or being enabled", Arguments: `{"region_opt_status_contains":["ENABLED","ENABLING"]}`}},
	}, "list"),
	accountRegionRead(Operation{
		Operation: "GetRegionOptStatus", Object: "region", Action: "get-opt-status",
		Title: "Get the opt-in status of a region",
		Description: "Return region_opt_status of one region of the bound account: ENABLED_BY_DEFAULT, ENABLED, ENABLING, " +
			"DISABLING, or DISABLED. Needs account:GetRegionOptStatus.",
		Arguments: map[string]Argument{
			"AccountId":  {Description: descAccountID},
			"RegionName": {Description: descAccountRegion},
		},
		Fields: map[string]Field{
			"RegionName":      {Description: "The region asked for", MaxLen: 50},
			"RegionOptStatus": {Description: "ENABLED_BY_DEFAULT, ENABLED, ENABLING, DISABLING, or DISABLED"},
		},
		Guards:     []string{"region-name"},
		SampleArgs: `{"region_name":"af-south-1"}`,
		Examples:   []Example{{Description: "Check whether a region is enabled", Arguments: `{"region_name":"af-south-1"}`}},
	}, "get"),
	accountRegionWrite(Operation{
		Operation: "EnableRegion", Object: "region", Action: "enable",
		Title: "Enable an opt-in region",
		Description: "Start the opt-in of one region for the bound account. The change takes a while: region_opt_status goes " +
			"through ENABLING to ENABLED; read it with get-opt-status. Enabling a region that is enabled does nothing. " +
			"AWS may charge for what you later create there. " + accountRegionNote + " Needs account:EnableRegion.",
		Arguments: map[string]Argument{
			"AccountId":  {Description: descAccountID},
			"RegionName": {Description: descAccountRegion},
		},
		Examples: []Example{{Description: "Opt in to a region", Arguments: `{"region_name":"af-south-1"}`}},
	}, "enable"),
	accountRegionWrite(Operation{
		Operation: "DisableRegion", Object: "region", Action: "disable",
		Title: "Disable an opt-in region",
		Description: "Start the opt-out of one region for the bound account. The change takes a while: region_opt_status goes " +
			"through DISABLING to DISABLED. Access to the region and its resources is lost; what exists there is not " +
			"removed by this tool, and AWS decides what happens to it. Disabling a region that is disabled does nothing. " +
			accountRegionNote + " The result says whether the region is in the region list of this connection. Needs account:DisableRegion.",
		Arguments: map[string]Argument{
			"AccountId":  {Description: descAccountID},
			"RegionName": {Description: descAccountRegion},
		},
		Examples: []Example{{Description: "Opt out of a region", Arguments: `{"region_name":"af-south-1"}`}},
	}, "disable"),
}

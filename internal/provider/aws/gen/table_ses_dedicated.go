package gen

import "slices"

const (
	sesDedicatedSensitivity = "aws-ses-dedicated-ips"

	sesDedicatedCost = "Dedicated IP addresses are charged by AWS per address and month on top of the sending price; " +
		"the price is on the SES pricing page and is not retrieved by this tool."

	descSESPoolName = "Name of the dedicated IP pool: 1 to 64 letters, digits, underscores, and hyphens"
	descSESDedIP    = "Dedicated IP address of the bound account in this region, IPv4 such as 192.0.2.1"
	descSESScaling  = "STANDARD (you manage the addresses of the pool) or MANAGED (SES adds and removes addresses as sending changes)"

	reasonSESPoolName = "the documented form of a pool name, which the model does not state"
	reasonSESIPLength = "the longest textual IP address; the model gives no bound"
)

var (
	sesPoolNameLimit = InputLimit{Member: "PoolName", MaxLen: 64, Reason: reasonSESPoolName}
	sesIPLimit       = InputLimit{Member: "Ip", MaxLen: 45, Reason: reasonSESIPLength}
)

// sesDedicatedRead and sesDedicatedWrite fill what the dedicated IP tools share. They act on the account of
// one region, so no target argument is checked beyond the region.
func sesDedicatedRead(op Operation, tags ...string) Operation {
	op.Effect, op.Idempotency = "read", "safe"
	op.DataSensitivity = sesDedicatedSensitivity
	op.Tags = slices.Concat([]string{"aws", "ses", "dedicated-ip"}, tags)
	op.TargetCheck, op.PolicyCheck = "none", "none"
	return op
}

func sesDedicatedWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op.Effect, op.Idempotency, op.Confirm = effect, idempotency, true
	op.DataSensitivity = sesDedicatedSensitivity
	op.Tags = slices.Concat([]string{"aws", "ses", "dedicated-ip"}, tags)
	op.TargetCheck, op.PolicyCheck = "none", "none"
	return op
}

// sesDedicatedTools are the SES v2 operations on dedicated IP addresses and their pools.
var sesDedicatedTools = []Operation{
	sesDedicatedRead(Operation{
		Operation: "GetDedicatedIps", Object: "dedicated-ip", Action: "list",
		Title: "List the SES dedicated IP addresses of a region",
		Description: "List the dedicated IP addresses of the account in one region, optionally of one pool, with pool, " +
			"warm-up status, and warm-up percentage. Needs ses:GetDedicatedIps.",
		Arguments:   map[string]Argument{"PoolName": {Description: "Only the addresses of this pool; all pools when left out"}},
		Fields:      map[string]Field{"DedicatedIps": {Description: "Addresses of this page, each with ip, warmup_status (IN_PROGRESS, DONE, or NOT_APPLICABLE), warmup_percentage, and pool_name"}},
		Limits:      []InputLimit{sesPoolNameLimit},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the addresses of one pool", Arguments: `{"region":"eu-central-1","pool_name":"marketing"}`}},
	}),
	sesDedicatedRead(Operation{
		Operation: "GetDedicatedIp", Object: "dedicated-ip", Action: "get",
		Title:       "Get an SES dedicated IP address",
		Description: "Return one dedicated IP address of the account with its pool, warm-up status, and warm-up percentage. Needs ses:GetDedicatedIp.",
		Arguments:   map[string]Argument{"Ip": {Description: descSESDedIP}},
		Fields:      map[string]Field{"DedicatedIp": {Description: "The address with ip, warmup_status (IN_PROGRESS, DONE, or NOT_APPLICABLE), warmup_percentage, and pool_name"}},
		Limits:      []InputLimit{sesIPLimit},
		Examples:    []Example{{Description: "Read one address", Arguments: `{"region":"eu-central-1","ip":"192.0.2.1"}`}},
	}),
	sesDedicatedRead(Operation{
		Operation: "ListDedicatedIpPools", Object: "dedicated-ip-pool", Action: "list",
		Title:       "List the SES dedicated IP pools of a region",
		Description: "List the names of the dedicated IP pools of the account in one region. Needs ses:ListDedicatedIpPools.",
		Arguments:   map[string]Argument{},
		Fields:      map[string]Field{"DedicatedIpPools": {Description: "Pool names of this page"}},
		PageSizeMax: 100,
		Examples:    []Example{{Description: "List the pools", Arguments: `{"region":"eu-central-1"}`}},
	}, "pool"),
	sesDedicatedRead(Operation{
		Operation: "GetDedicatedIpPool", Object: "dedicated-ip-pool", Action: "get",
		Title:       "Get an SES dedicated IP pool",
		Description: "Return one dedicated IP pool of the account with its scaling mode. Needs ses:GetDedicatedIpPool.",
		Arguments:   map[string]Argument{"PoolName": {Description: descSESPoolName}},
		Fields:      map[string]Field{"DedicatedIpPool": {Description: "The pool with pool_name and scaling_mode (STANDARD or MANAGED)"}},
		Limits:      []InputLimit{sesPoolNameLimit},
		Examples:    []Example{{Description: "Read one pool", Arguments: `{"region":"eu-central-1","pool_name":"marketing"}`}},
	}, "pool"),
	sesDedicatedWrite(Operation{
		Operation: "CreateDedicatedIpPool", Object: "dedicated-ip-pool", Action: "create",
		Title: "Create an SES dedicated IP pool",
		Description: "Create a dedicated IP pool in one region of the account, named and with optional tags and scaling mode. " +
			"A MANAGED pool lets SES add dedicated addresses, which are charged. " +
			sesDedicatedCost + " What is sent: the region, the pool name, the tags, and the scaling mode. Needs " +
			"ses:CreateDedicatedIpPool, and ses:TagResource with tags. Not repeated after an unclear outcome: read the " +
			"pool before trying again. " + sesWriteRule,
		Arguments: map[string]Argument{
			"PoolName":    {Description: descSESPoolName},
			"Tags":        {Description: descSESTags},
			"ScalingMode": {Description: descSESScaling + "; STANDARD when left out"},
		},
		Limits:   slices.Concat([]InputLimit{sesPoolNameLimit}, sesTagLimits),
		Examples: []Example{{Description: "Create a pool", Arguments: `{"region":"eu-central-1","pool_name":"marketing","scaling_mode":"STANDARD"}`}},
	}, "create", "non-idempotent", "pool", "cost"),
	sesDedicatedWrite(Operation{
		Operation: "DeleteDedicatedIpPool", Object: "dedicated-ip-pool", Action: "delete",
		Title: "Delete an SES dedicated IP pool",
		Description: "Delete a dedicated IP pool of the account in one region. Move its addresses to another pool first. Needs ses:DeleteDedicatedIpPool. " +
			"Not repeated after an unclear outcome. " + sesWriteRule,
		Arguments: map[string]Argument{"PoolName": {Description: descSESPoolName}},
		Limits:    []InputLimit{sesPoolNameLimit},
		Examples:  []Example{{Description: "Delete a pool", Arguments: `{"region":"eu-central-1","pool_name":"marketing"}`}},
	}, "delete", "non-idempotent", "pool"),
	sesDedicatedWrite(Operation{
		Operation: "PutDedicatedIpInPool", Object: "dedicated-ip", Action: "put-in-pool",
		Title: "Move an SES dedicated IP address to a pool",
		Description: "Move one dedicated IP address of the account to another pool in the same region. The address keeps " +
			"its sending reputation, so the mail of the new pool is sent from an address with the history of its " +
			"earlier use. What is sent: the region, the address, and the destination " +
			"pool. Needs ses:PutDedicatedIpInPool. " + sesWriteRule,
		Arguments: map[string]Argument{
			"Ip":                  {Description: descSESDedIP},
			"DestinationPoolName": {Description: "Pool the address moves to: 1 to 64 letters, digits, underscores, and hyphens"},
		},
		Limits:   []InputLimit{sesIPLimit, {Member: "DestinationPoolName", MaxLen: 64, Reason: reasonSESPoolName}},
		Examples: []Example{{Description: "Move an address", Arguments: `{"region":"eu-central-1","ip":"192.0.2.1","destination_pool_name":"marketing"}`}},
	}, "update", "idempotent", "pool", "reputation"),
	sesDedicatedWrite(Operation{
		Operation: "PutDedicatedIpWarmupAttributes", Object: "dedicated-ip", Action: "put-warmup-attributes",
		Title: "Set the warm-up percentage of an SES dedicated IP address",
		Description: "Set the warm-up percentage of one dedicated IP address: the share of mail SES sends over the address " +
			"while it warms up, the rest goes over shared addresses. 100 ends the warm-up. A value that is too high for " +
			"a new address can harm its reputation. What is sent: the region, the address, and the percentage. Needs " +
			"ses:PutDedicatedIpWarmupAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"Ip":               {Description: descSESDedIP},
			"WarmupPercentage": {Description: "Percentage of mail sent over the address, 0 to 100"},
		},
		Limits:   []InputLimit{sesIPLimit, {Member: "WarmupPercentage", MaxValue: 100, Reason: "the percentage is at most 100; the model gives no bound"}},
		Examples: []Example{{Description: "Set 25 percent", Arguments: `{"region":"eu-central-1","ip":"192.0.2.1","warmup_percentage":25}`}},
	}, "update", "idempotent", "warmup", "reputation"),
	sesDedicatedWrite(Operation{
		Operation: "PutDedicatedIpPoolScalingAttributes", Object: "dedicated-ip-pool", Action: "put-scaling-attributes",
		Title: "Set the scaling mode of an SES dedicated IP pool",
		Description: "Set the scaling mode of a dedicated IP pool: " + descSESScaling + ". Switching a STANDARD pool to " +
			"MANAGED lets SES add dedicated addresses, which are charged. " + sesDedicatedCost + " What is sent: the region, the pool, and the mode. Needs " +
			"ses:PutDedicatedIpPoolScalingAttributes. " + sesWriteRule,
		Arguments: map[string]Argument{
			"PoolName":    {Description: descSESPoolName},
			"ScalingMode": {Description: descSESScaling},
		},
		Limits:   []InputLimit{sesPoolNameLimit},
		Examples: []Example{{Description: "Let SES manage a pool", Arguments: `{"region":"eu-central-1","pool_name":"marketing","scaling_mode":"MANAGED"}`}},
	}, "update", "idempotent", "pool", "cost"),
}

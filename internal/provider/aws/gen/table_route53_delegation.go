package gen

const (
	// r53DelegationConfirm is the text the confirmation of a delegation set write has to carry.
	r53DelegationConfirm = " The confirmation names the tool, not the arguments: show the set ID (or the zone to copy) before confirming."
	r53NameServerStep    = " Next manual step: enter the returned name servers at the registrar of each domain whose zone uses the set " +
		"(this tool does not do that); until then the zones answer no queries."
)

// route53DelegationTools are the reusable delegation sets: fixed name servers that several hosted zones share.
var route53DelegationTools = []Operation{
	r53Write(Operation{
		Operation: "CreateReusableDelegationSet", Object: "delegation-set", Action: "create", Effect: "create", Idempotency: "non-idempotent",
		Title: "Create a Route 53 reusable delegation set",
		Description: "Create a reusable delegation set: one group of name servers that several hosted zones of the account can share, so a " +
			"domain keeps its name servers when its zone is replaced. With hosted_zone_id the set takes over the name servers of that existing " +
			"zone (the zone itself stays as it is); without it Route 53 picks the servers. " +
			"The result names the name servers (delegation_set.name_servers) and the set ID to give to aws.route53.hosted-zone.create as " +
			"delegation_set_id. A repeated token with another zone answers DelegationSetAlreadyCreated, a zone whose " +
			"set is already reusable DelegationSetAlreadyReusable, a set that cannot be made DelegationSetNotAvailable, an unknown zone " +
			"NoSuchHostedZone, and the account's quota LimitsExceeded. Needs route53:CreateReusableDelegationSet. " +
			"The call token is generated and returned as client_token." + r53NameServerStep + r53DelegationConfirm,
		Arguments: map[string]Argument{
			"HostedZoneId": {Description: "ID of an existing public hosted zone of the bound account whose name servers the set takes over, with or " +
				"without the /hostedzone/ prefix; left out for name servers Route 53 chooses"},
		},
		Fields: map[string]Field{
			"DelegationSet": {Description: "The new set: id (as Route 53 reports it, /delegationset/N...; the tools take it with or without the prefix), caller_reference, and name_servers to enter at the registrar"},
			"Location":      {Description: "Path of the new set, as Route 53 reports it"},
		},
		TokenMember: "CallerReference",
		Examples: []Example{
			{Description: "Create a set with name servers Route 53 chooses", Arguments: `{}`},
			{Description: "Take over the name servers of an existing zone", Arguments: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ"}`},
		},
	}, "delegation-set"),
	r53Write(Operation{
		Operation: "DeleteReusableDelegationSet", Object: "delegation-set", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Delete a Route 53 reusable delegation set",
		Description: "Delete a reusable delegation set. A set that hosted zones still use answers DelegationSetInUse and stays (find the zones " +
			"with aws.route53.hosted-zone.list and delegation_set_id), an unknown one NoSuchDelegationSet, one that is not reusable " +
			"DelegationSetNotReusable. A deleted set and its name servers cannot be restored. Needs route53:DeleteReusableDelegationSet." +
			r53DelegationConfirm,
		Arguments: map[string]Argument{"Id": {Description: descR53DelegationSet}},
		Examples:  []Example{{Description: "Delete an unused set", Arguments: `{"id":"N0123456789ABCDEFGHIJ"}`}},
	}, "delegation-set"),
}

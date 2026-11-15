package gen

// Operations of the Route 53 curation tables that name a guard.

func init() {
	expectGuarded(
		"AssociateVPCWithHostedZone",
		"ChangeCidrCollection",
		"ChangeResourceRecordSets",
		"CreateHealthCheck",
		"CreateHostedZone",
		"CreateKeySigningKey",
		"CreateQueryLoggingConfig",
		"CreateTrafficPolicy",
		"CreateTrafficPolicyInstance",
		"CreateTrafficPolicyVersion",
		"DeleteVPCAssociationAuthorization",
		"DisassociateVPCFromHostedZone",
		"ListHostedZonesByVPC",
		"TestDNSAnswer",
		"UpdateHealthCheck",
	)
}

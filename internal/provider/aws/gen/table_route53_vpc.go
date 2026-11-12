package gen

const (
	// r53VPCConfirm is the text the confirmation of every VPC association write has to carry. The confirmation names the tool, not the arguments.
	r53VPCConfirm = " The confirmation names the tool, not the arguments: show hosted_zone_id, vpc.vpc_id, and vpc.vpc_region to the user before confirming, " +
		"and say that the account of the VPC cannot be checked: a VPC ID carries no account."
	r53VPCArg = "VPC to use: vpc_region (a region the connection allows) and vpc_id (vpc-...); both are required together"
)

// route53VPCTools associate and release the VPCs of a private hosted zone, and withdraw an authorization. Granting an authorization is not offered.
var route53VPCTools = []Operation{
	r53Write(Operation{
		Operation: "AssociateVPCWithHostedZone", Object: "hosted-zone", Action: "associate-vpc", Effect: "update", Idempotency: "non-idempotent",
		Title: "Associate a VPC with a Route 53 private hosted zone",
		Description: "Make a private hosted zone resolvable in a VPC. vpc.vpc_region must be a region the connection allows and vpc.vpc_id a VPC ID " +
			"such as vpc-0123456789abcdef0; both are checked before any request. A VPC ID carries no account, so the tool cannot tell whether the VPC " +
			"belongs to the bound account: a VPC of another account is only accepted by Route 53 when its owner authorized it before " +
			"(NotAuthorizedException otherwise), and the tools never grant that authorization. Remaining risk: the zone's records become " +
			"resolvable in a VPC whose account is not known here. A public zone answers PublicZoneVPCAssociation, an unknown zone NoSuchHostedZone, " +
			"a VPC that already belongs to the zone ConflictingDomainExists, and a parallel change PriorRequestNotComplete. " +
			"Needs route53:AssociateVPCWithHostedZone, and ec2:DescribeVpcs for the VPC." + r53VPCConfirm + r53Change,
		Arguments: map[string]Argument{
			"HostedZoneId": {Description: descR53Zone},
			"VPC":          {Description: r53VPCArg},
			"Comment":      {Description: "Comment of the change, left out for none"},
		},
		Fields: map[string]Field{
			"ChangeInfo": {Description: "The change that associates the VPC: id, status, submitted_at, and comment"},
		},
		Guards:     []string{"route53-vpc-member"},
		SampleArgs: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ","vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}}`,
		Examples:   []Example{{Description: "Associate a VPC", Arguments: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ","vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}}`}},
	}, "hosted-zone"),
	r53Write(Operation{
		Operation: "DisassociateVPCFromHostedZone", Object: "hosted-zone", Action: "disassociate-vpc", Effect: "update", Idempotency: "non-idempotent",
		Title: "Release a VPC from a Route 53 private hosted zone",
		Description: "Stop the resolution of a private hosted zone in a VPC. vpc.vpc_region must be a region the connection allows and vpc.vpc_id " +
			"a VPC ID such as vpc-0123456789abcdef0; both are checked before any request, and the account of the VPC cannot be checked. " +
			"Names of the zone no longer resolve in that VPC as soon as the change is in sync. Route 53 refuses to release the last VPC of a " +
			"private zone (LastVPCAssociation) and answers VPCAssociationNotFound for a VPC that is not associated. " +
			"Needs route53:DisassociateVPCFromHostedZone, and ec2:DescribeVpcs for the VPC." + r53VPCConfirm + r53Change,
		Arguments: map[string]Argument{
			"HostedZoneId": {Description: descR53Zone},
			"VPC":          {Description: r53VPCArg},
			"Comment":      {Description: "Comment of the change, left out for none"},
		},
		Fields: map[string]Field{
			"ChangeInfo": {Description: "The change that releases the VPC: id, status, submitted_at, and comment"},
		},
		Guards:     []string{"route53-vpc-member"},
		SampleArgs: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ","vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}}`,
		Examples:   []Example{{Description: "Release a VPC", Arguments: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ","vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}}`}},
	}, "hosted-zone"),
	r53Write(Operation{
		Operation: "DeleteVPCAssociationAuthorization", Object: "vpc-association-authorization", Action: "delete", Effect: "delete", Idempotency: "non-idempotent",
		Title: "Withdraw the authorization of a VPC for a Route 53 private hosted zone",
		Description: "Withdraw the authorization that lets a VPC, also one of another account, associate with a private hosted zone. A VPC that " +
			"is already associated stays associated; release it with aws.route53.hosted-zone.disassociate-vpc. vpc.vpc_region must be a region " +
			"the connection allows and vpc.vpc_id a VPC ID such as vpc-0123456789abcdef0; both are checked before any request. An authorization " +
			"that does not exist answers VPCAssociationAuthorizationNotFound. Granting an authorization is not part of the tools. " +
			"Needs route53:DeleteVPCAssociationAuthorization." + r53VPCConfirm,
		Arguments: map[string]Argument{
			"HostedZoneId": {Description: descR53Zone},
			"VPC":          {Description: "VPC whose authorization is withdrawn: vpc_region (a region the connection allows) and vpc_id (vpc-...); both are required together"},
		},
		SampleArgs: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ","vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}}`,
		Guards:     []string{"route53-vpc-member"},
		Examples:   []Example{{Description: "Withdraw an authorization", Arguments: `{"hosted_zone_id":"Z0123456789ABCDEFGHIJ","vpc":{"vpc_region":"eu-central-1","vpc_id":"vpc-0123456789abcdef0"}}`}},
	}, "hosted-zone"),
}

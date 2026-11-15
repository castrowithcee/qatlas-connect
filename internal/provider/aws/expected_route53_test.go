package aws

// Tools of the Route 53 tables that name a guard.

func init() {
	expectGuarded(
		"aws.route53.cidr-collection.change",
		"aws.route53.dns-answer.test",
		"aws.route53.health-check.create",
		"aws.route53.health-check.update",
		"aws.route53.hosted-zone.associate-vpc",
		"aws.route53.hosted-zone.create",
		"aws.route53.hosted-zone.disassociate-vpc",
		"aws.route53.hosted-zone.list-by-vpc",
		"aws.route53.key-signing-key.create",
		"aws.route53.query-logging-config.create",
		"aws.route53.record-set.change",
		"aws.route53.traffic-policy-instance.create",
		"aws.route53.traffic-policy.create",
		"aws.route53.traffic-policy.create-version",
		"aws.route53.vpc-association-authorization.delete",
	)
}

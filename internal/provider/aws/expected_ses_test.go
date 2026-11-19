package aws

// Tools of the SES tables that name a guard or a readback.

func init() {
	expectGuarded(
		"aws.ses.configuration-set.create",
		"aws.ses.configuration-set.put-archiving-options",
		"aws.ses.configuration-set.put-tracking-options",
		"aws.ses.email-identity-certificate.associate",
		"aws.ses.email.send",
		"aws.ses.email.send-bulk",
		"aws.ses.export-job.create",
		"aws.ses.import-job.create",
		"aws.ses.multi-region-endpoint.create",
		"aws.ses.reputation-entity.update-policy",
	)
	expectReadback(
		"aws.ses.export-job.create",
		"aws.ses.import-job.create",
	)
	expectSummary(
		"aws.ses.account.put-dedicated-ip-warmup-attributes",
		"aws.ses.account.put-details",
		"aws.ses.account.put-sending-attributes",
		"aws.ses.account.put-suppression-attributes",
		"aws.ses.account.put-vdm-attributes",
	)
}

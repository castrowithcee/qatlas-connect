package gen

// Operations of the SES curation tables that name a guard or a readback.

func init() {
	expectGuarded(
		"AssociateEmailIdentityCertificate",
		"CreateConfigurationSet",
		"CreateExportJob",
		"CreateImportJob",
		"CreateMultiRegionEndpoint",
		"PutConfigurationSetArchivingOptions",
		"PutConfigurationSetTrackingOptions",
		"SendBulkEmail",
		"SendEmail",
		"UpdateReputationEntityPolicy",
	)
	expectReadback(
		"CreateExportJob",
		"CreateImportJob",
	)
}

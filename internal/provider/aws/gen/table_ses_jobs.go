package gen

import "slices"

const (
	// SES does not check that the S3 bucket of a job belongs to the caller, so the bucket is confirmed before
	// the call.
	sesJobBucketRule = "The bucket of s3_url must be a bucket of the bound account in a region the connection allows: " +
		"before the call it is checked with HeadBucket and the bound account as expected owner (this needs s3:ListBucket " +
		"on it). Another account, a bucket that cannot be checked (a missing permission, a missing bucket, a redirect " +
		"without a later success), a region that is not allowed, and any other failure refuse the call, and nothing is " +
		"sent to SES. The call names the bucket, the key, and the data format, which the confirmation shows. "

	sesJobUnclear = "Not repeated after an unclear outcome: list the jobs before trying again."

	sesJobsTag = "jobs"
)

// reasonSESJobFilter is the reason to keep the filter members of an export open: the caller sets them on purpose,
// and they go to SES only.
const reasonSESJobFilter = "the filter values the caller chooses for the export; they go to SES only and the tool marks the result untrusted"

// sesJobWriteTools start and cancel the import and export jobs of SES. An import reads a file from a bucket
// and an export writes its result to one, so every bucket a request names is confirmed to belong to the bound
// account first. The model gives ExportDestination an optional S3Url next to the required DataFormat: SES
// writes to a location of its own when it is left out, and the same check applies when it is set. Cancelling
// names no bucket.
var sesJobWriteTools = []Operation{
	sesJobWrite(Operation{
		Operation: "CreateImportJob", Object: "import-job", Action: "create",
		Title: "Start an SES import job",
		Description: "Start a job that reads a CSV or JSON file from S3 and puts its addresses into the suppression list " +
			"(import_destination.suppression_list_destination with the action PUT or DELETE) or into a contact list " +
			"(import_destination.contact_list_destination with the list name and the action PUT or DELETE). The file is " +
			"read by SES from import_data_source.s3_url (s3://BUCKET/KEY) and holds addresses of third parties; its content " +
			"is never returned by this tool. " + sesJobBucketRule + "Returns the job ID; follow the job with " +
			"aws.ses.import-job.get. Needs ses:CreateImportJob. " + sesJobUnclear + " " + sesWriteRule,
		Arguments: map[string]Argument{
			"ImportDestination": {Description: "Where the addresses go: suppression_list_destination with suppression_list_import_action, or contact_list_destination with contact_list_name and contact_list_import_action (PUT or DELETE)"},
			"ImportDataSource":  {Description: "s3_url of the file as s3://BUCKET/KEY and its data_format (CSV or JSON)"},
		},
		Fields:     map[string]Field{"JobId": {Description: "ID of the new job"}},
		Guards:     []string{"ses-job-url"},
		Readback:   "ses-job-bucket",
		SampleArgs: `{"import_destination":{"suppression_list_destination":{"suppression_list_import_action":"PUT"}},"import_data_source":{"s3_url":"s3://aaa/a","data_format":"CSV"}}`,
		Examples:   []Example{{Description: "Import a file of addresses into the suppression list", Arguments: `{"region":"eu-central-1","import_destination":{"suppression_list_destination":{"suppression_list_import_action":"PUT"}},"import_data_source":{"s3_url":"s3://my-bucket/addresses.csv","data_format":"CSV"}}`}},
	}, "create", "non-idempotent", "import"),
	sesJobWrite(Operation{
		Operation: "CreateExportJob", Object: "export-job", Action: "create",
		Title: "Start an SES export job",
		Description: "Start a job that exports metrics data or message insights of the account to a file, in CSV or JSON " +
			"(export_destination.data_format). export_data_source holds either metrics_data_source or " +
			"message_insights_data_source with a period and filters. The result can hold addresses and subject lines of " +
			"third parties. export_destination.s3_url is optional; when it is set, " + sesJobBucketRule + "Returns the job ID; " +
			"read the job with aws.ses.export-job.get, which names the location of the result. Needs ses:CreateExportJob. " +
			sesJobUnclear + " " + sesWriteRule,
		Arguments: map[string]Argument{
			"ExportDataSource":  {Description: "metrics_data_source (dimensions, namespace, metrics, start_date, end_date) or message_insights_data_source (start_date, end_date, include, exclude, max_results)"},
			"ExportDestination": {Description: "data_format (CSV or JSON) and optionally s3_url as s3://BUCKET/KEY"},
		},
		Fields: map[string]Field{"JobId": {Description: "ID of the new job"}},
		Release: []Release{
			{Member: "ExportDataSource.MessageInsightsDataSource.Include.FromEmailAddress", Reason: reasonSESJobFilter},
			{Member: "ExportDataSource.MessageInsightsDataSource.Include.Destination", Reason: reasonSESJobFilter},
			{Member: "ExportDataSource.MessageInsightsDataSource.Include.Subject", Reason: reasonSESJobFilter},
			{Member: "ExportDataSource.MessageInsightsDataSource.Exclude.FromEmailAddress", Reason: reasonSESJobFilter},
			{Member: "ExportDataSource.MessageInsightsDataSource.Exclude.Destination", Reason: reasonSESJobFilter},
			{Member: "ExportDataSource.MessageInsightsDataSource.Exclude.Subject", Reason: reasonSESJobFilter},
		},
		Guards:     []string{"ses-job-url"},
		Readback:   "ses-job-bucket",
		SampleArgs: `{"export_destination":{"data_format":"CSV","s3_url":"s3://aaa/a"}}`,
		Examples:   []Example{{Description: "Export the sends of a week to a bucket of the account", Arguments: `{"region":"eu-central-1","export_data_source":{"metrics_data_source":{"dimensions":{"EMAIL_IDENTITY":["example.com"]},"namespace":"VDM","metrics":[{"name":"SEND","aggregation":"VOLUME"}],"start_date":"2026-01-01T00:00:00Z","end_date":"2026-01-08T00:00:00Z"}},"export_destination":{"data_format":"CSV","s3_url":"s3://my-bucket/export.csv"}}`}},
	}, "create", "non-idempotent", "export"),
	sesJobWrite(Operation{
		Operation: "CancelExportJob", Object: "export-job", Action: "cancel",
		Title: "Cancel an SES export job",
		Description: "Cancel an export job that has not finished; a finished job cannot be cancelled. Names no bucket and " +
			"moves no data. Needs ses:CancelExportJob. " + sesWriteRule,
		Arguments: map[string]Argument{"JobId": {Description: descSESJobID}},
		Limits:    []InputLimit{{Member: "JobId", MaxLen: 128, Reason: "a job ID is far shorter; the model gives no bound"}},
	}, "update", "idempotent", "export"),
}

// sesJobWrite fills what the job write tools share.
func sesJobWrite(op Operation, effect, idempotency string, tags ...string) Operation {
	op = sesDeliverWrite(op, effect, idempotency, slices.Concat([]string{sesJobsTag}, tags)...)
	op.DataSensitivity = sesJobSensitivity
	if op.Readback != "" {
		op.ReadbackAnswer = reportHeadAnswer
	}
	return op
}

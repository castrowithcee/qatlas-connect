package googleanalytics

// The answer types of the Data API reports. Like the Admin types they bound what they hold while they are
// decoded: report values, names and messages are third-party content and are cut, never followed.

// Bounds of the lists of a report answer. Rows are bounded in reports.go.
const (
	maxHeaders      = 100
	maxCells        = 100
	maxTotalsRows   = 100
	maxMetaEntries  = 100
	maxRestrictions = 20
)

type (
	// quotaStatus is one bucket of the property quota. A member Google leaves out stays absent.
	quotaStatus struct {
		Remaining *int32 `json:"remaining,omitempty"`
		Consumed  *int32 `json:"consumed,omitempty"`
	}
	// propertyQuota is the quota state of a property, including the request that returned it.
	propertyQuota struct {
		TokensPerDay                          *quotaStatus `json:"tokensPerDay,omitempty"`
		TokensPerHour                         *quotaStatus `json:"tokensPerHour,omitempty"`
		ConcurrentRequests                    *quotaStatus `json:"concurrentRequests,omitempty"`
		ServerErrorsPerProjectPerHour         *quotaStatus `json:"serverErrorsPerProjectPerHour,omitempty"`
		PotentiallyThresholdedRequestsPerHour *quotaStatus `json:"potentiallyThresholdedRequestsPerHour,omitempty"`
		TokensPerProjectPerHour               *quotaStatus `json:"tokensPerProjectPerHour,omitempty"`
	}

	dimensionHeader struct {
		Name text `json:"name"`
	}
	metricHeader struct {
		Name text `json:"name"`
		Type code `json:"type,omitempty"`
	}
	dimensionValue struct {
		Value text `json:"value"`
	}
	metricValue struct {
		Value code `json:"value"`
	}
	reportRow struct {
		DimensionValues []dimensionValue `json:"dimensionValues,omitempty"`
		MetricValues    []metricValue    `json:"metricValues,omitempty"`
	}

	samplingMetadata struct {
		SamplesReadCount  code `json:"samplesReadCount,omitempty"`
		SamplingSpaceSize code `json:"samplingSpaceSize,omitempty"`
	}
	truncationRange struct {
		StartDate code `json:"startDate,omitempty"`
		EndDate   code `json:"endDate,omitempty"`
	}
	truncationReason struct {
		DataTruncationType       code              `json:"dataTruncationType,omitempty"`
		DataTruncationMessage    text              `json:"dataTruncationMessage,omitempty"`
		DataTruncationDateRanges []truncationRange `json:"dataTruncationDateRanges,omitempty"`
		DataTruncationDate       code              `json:"dataTruncationDate,omitempty"`
	}
	metricRestriction struct {
		MetricName            text   `json:"metricName,omitempty"`
		RestrictedMetricTypes []code `json:"restrictedMetricTypes,omitempty"`
	}
	schemaRestriction struct {
		ActiveMetricRestrictions []metricRestriction `json:"activeMetricRestrictions,omitempty"`
	}
	// reportMetadata carries what Google says about the quality of the data: sampling, thresholding, rolled-up
	// rows and the reason of an empty report. The members are returned as Google sends them.
	reportMetadata struct {
		SamplingMetadatas         []samplingMetadata `json:"samplingMetadatas,omitempty"`
		EmptyReason               text               `json:"emptyReason,omitempty"`
		SubjectToThresholding     bool               `json:"subjectToThresholding"`
		TimeZone                  text               `json:"timeZone,omitempty"`
		CurrencyCode              code               `json:"currencyCode,omitempty"`
		SchemaRestrictionResponse *schemaRestriction `json:"schemaRestrictionResponse,omitempty"`
		DataLossFromOtherRow      bool               `json:"dataLossFromOtherRow"`
		DataTruncationReasons     []truncationReason `json:"dataTruncationReasons,omitempty"`
	}

	// reportAnswer is the RunReportResponse of analyticsdata v1beta.
	reportAnswer struct {
		DimensionHeaders []dimensionHeader `json:"dimensionHeaders"`
		MetricHeaders    []metricHeader    `json:"metricHeaders"`
		Rows             []reportRow       `json:"rows"`
		Totals           []reportRow       `json:"totals,omitempty"`
		Maximums         []reportRow       `json:"maximums,omitempty"`
		Minimums         []reportRow       `json:"minimums,omitempty"`
		RowCount         int64             `json:"rowCount"`
		Metadata         *reportMetadata   `json:"metadata,omitempty"`
		PropertyQuota    *propertyQuota    `json:"propertyQuota,omitempty"`
		Kind             code              `json:"kind,omitempty"`
	}
)

// remaining is the remaining quota of a bucket, or -1 when it is not known.
func (s *quotaStatus) remaining() int64 {
	if s == nil || s.Remaining == nil {
		return -1
	}
	return int64(*s.Remaining)
}

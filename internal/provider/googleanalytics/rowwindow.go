package googleanalytics

import (
	"encoding/json"
	"strconv"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
)

// rowWindow resolves the start row and the row count of one page of a Data API method with offset and limit.
// The start row comes from the argument or from a cursor of this binding, never from both; a limit above what
// a page holds is cut and reported as capped.
func rowWindow(binding []byte, cursor string, offset, limit *int64) (start, count int64, capped bool, err error) {
	if cursor != "" && offset != nil {
		return 0, 0, false, badRequest("set offset or cursor, not both")
	}
	switch {
	case cursor != "":
		inner, ok := provider.DecodeCursor(binding, cursor, maxCursorBytes)
		parsed, perr := strconv.ParseInt(inner, 10, 32)
		if !ok || perr != nil || parsed < 0 {
			return 0, 0, false, badRequest("cursor is not a next_cursor of this tool on this connection and name; " +
				"start again without cursor")
		}
		start = parsed
	case offset != nil:
		start = *offset
	}
	count = defaultReportRows
	if limit != nil {
		count = *limit
	}
	if count > maxReportRows {
		count, capped = maxReportRows, true
	}
	return start, count, capped, nil
}

// windowBody is the request body of a method with only offset and limit; both go as strings, as the API
// documents int64.
func windowBody(offset, limit int64) ([]byte, error) {
	body := map[string]string{"limit": strconv.FormatInt(limit, 10)}
	if offset > 0 {
		body["offset"] = strconv.FormatInt(offset, 10)
	}
	return json.Marshal(body)
}

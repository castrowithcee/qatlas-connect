package aws

import (
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/provider/aws/awsquery"
)

// A csvReport tool gets a CSV table as a base64 blob, such as the IAM credential report. The blob is decoded
// and parsed here, only the known columns are returned, and the rows are paged by the tool itself. The report
// is data from AWS: nothing in it is executed, and the blob is never part of a result.

// csvColumn is one known column of a report; a Bool column holds true or false.
type csvColumn struct {
	Name           string
	Bool, Required bool
}

type csvReport struct {
	// Member is the key of the decoded blob in the answer; it never reaches a result.
	Member string
	// MaxBytes caps the base64 text, MaxRows the data rows, MaxField one field in bytes.
	MaxBytes, MaxRows, MaxField int
	DefaultPage, MaxPage        int
	Columns                     []csvColumn
}

// Names of the result members the CSV step adds.
const (
	csvRows    = "rows"
	csvDropped = "dropped_columns"
	csvTotal   = "total_rows"

	csvMaxColumns = 64
	csvMaxDropped = 32
)

// csvAbsent are the values a report uses for a field without a value; they are left out of a row.
var csvAbsent = map[string]bool{"": true, "N/A": true, "not_supported": true}

// csvColumnName limits what an unknown header may be before it is named in a result.
var csvColumnName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// csvPage is one request for rows: the first row, the page size, and the report the cursor belongs to.
type csvPage struct {
	offset, size int
	generated    string
	binding      []byte
}

// startCSV reads page_size and cursor of a CSV tool. The cursor is bound to the tool and its region, carries
// the row offset and the generation time of the report it continues, and is refused when altered.
func (s toolSpec) startCSV(region string, args map[string]any) (csvPage, error) {
	pg := csvPage{size: s.csv.DefaultPage}
	if v, set := args[awsquery.ArgPageSize]; set {
		n, ok := v.(json.Number)
		i, err := n.Int64()
		if !ok || err != nil || i < 1 || i > int64(s.csv.MaxPage) {
			return pg, &provider.InvalidRequestError{Message: "page_size must be between 1 and " + strconv.Itoa(s.csv.MaxPage)}
		}
		pg.size = int(i)
	}
	cursor, _ := args[awsquery.ArgCursor].(string)
	delete(args, awsquery.ArgPageSize)
	delete(args, awsquery.ArgCursor)
	bound := make(map[string]any, len(args)+1)
	for k, v := range args {
		bound[k] = v
	}
	bound[argRegion] = region
	pg.binding = awsquery.Binding(s.descriptor.ID, bound)
	if cursor != "" {
		refuse := &provider.InvalidRequestError{Message: "cursor is not a next_cursor of this report; start again without cursor"}
		inner, ok := provider.DecodeCursor(pg.binding, cursor, awsquery.MaxCursorLength)
		offset, generated, found := strings.Cut(inner, "|")
		n, err := strconv.Atoi(offset)
		if !ok || !found || err != nil || n < 1 || n > s.csv.MaxRows || generated == "" || len(generated) > 64 {
			return pg, refuse
		}
		pg.offset, pg.generated = n, generated
	}
	return pg, nil
}

// apply replaces the blob of the answer by the rows of the requested page and the members that page it.
func (c *csvReport) apply(op string, out map[string]any, pg csvPage) error {
	invalid := func() error { return provider.InvalidResponse(op, "AWS returned an invalid response") }
	text, _ := out[c.Member].(string)
	delete(out, c.Member)
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(text) > c.MaxBytes || !utf8.Valid(raw) {
		return invalid()
	}
	r := csv.NewReader(bytes.NewReader(raw))
	header, err := r.Read()
	if err != nil || len(header) > csvMaxColumns {
		return invalid()
	}
	known := make(map[string]csvColumn, len(c.Columns))
	for _, col := range c.Columns {
		known[col.Name] = col
	}
	// column[i] is the known column at header position i; the others are dropped and named.
	column := make([]*csvColumn, len(header))
	seen := map[string]bool{}
	dropped := []any{}
	for i, name := range header {
		if seen[name] {
			return invalid()
		}
		seen[name] = true
		if col, ok := known[name]; ok {
			column[i] = &col
		} else if csvColumnName.MatchString(name) && len(dropped) < csvMaxDropped {
			dropped = append(dropped, name)
		}
	}
	for _, col := range c.Columns {
		if col.Required && !seen[col.Name] {
			return invalid()
		}
	}
	var rows []any
	total := 0
	for {
		record, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return invalid()
		}
		if total++; total > c.MaxRows {
			return invalid()
		}
		row := map[string]any{}
		for i, value := range record {
			if len(value) > c.MaxField {
				return invalid()
			}
			col := column[i]
			if col == nil || csvAbsent[value] {
				continue
			}
			if col.Bool {
				switch value {
				case "true":
					row[col.Name] = true
				case "false":
					row[col.Name] = false
				default:
					return invalid()
				}
				continue
			}
			row[col.Name] = value
		}
		for _, col := range c.Columns {
			if _, set := row[col.Name]; col.Required && !set {
				return invalid()
			}
		}
		if total > pg.offset && len(rows) < pg.size {
			rows = append(rows, row)
		}
	}
	generated, _ := out["generated_time"].(string)
	if pg.generated != "" && pg.generated != generated {
		return &provider.InvalidRequestError{Message: "the report was generated again since this cursor was issued; start again without cursor"}
	}
	if pg.offset >= total && pg.offset > 0 {
		return &provider.InvalidRequestError{Message: "cursor is beyond the end of this report; start again without cursor"}
	}
	if rows == nil {
		rows = []any{}
	}
	out[csvRows], out[csvDropped], out[csvTotal] = rows, dropped, total
	end := pg.offset + len(rows)
	if end < total {
		out["next_cursor"] = provider.EncodeCursor(pg.binding, strconv.Itoa(end)+"|"+generated)
	}
	out["has_more"] = end < total
	return nil
}

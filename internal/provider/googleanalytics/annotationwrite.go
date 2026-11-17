package googleanalytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	annotationsCreateID = Provider + ".annotations.create"
	annotationsUpdateID = Provider + ".annotations.update"
	annotationsDeleteID = Provider + ".annotations.delete"
)

// Bounds of what a changing tool accepts. Google documents no length for a title or a description; Qatlas
// accepts what a result shows, so that nothing it writes is cut when read back.
const (
	maxTitleRunes = maxTextRunes
	dateLayout    = "2006-01-02"
)

// annotationColors are the colors Google documents, without COLOR_UNSPECIFIED and ORANGE, which Google marks as
// used for system-generated annotations only.
var annotationColors = []string{"PURPLE", "BROWN", "BLUE", "GREEN", "RED", "CYAN"}

// checkAnnotationTitle refuses a blank title, text that is not valid or holds a control character, and one
// longer than maxTitleRunes. The refusal never quotes the title.
func checkAnnotationTitle(value string) error {
	if strings.TrimSpace(value) == "" || !utf8.ValidString(value) {
		return badRequest("title must be non-blank text")
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return badRequest("title must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxTitleRunes {
		return badRequest("title must be at most 300 characters")
	}
	return nil
}

// checkAnnotationDescription accepts empty text, which clears the description on an update, and line breaks.
func checkAnnotationDescription(value string) error {
	if !utf8.ValidString(value) {
		return badRequest("description must be valid text")
	}
	for _, r := range value {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return badRequest("description must not hold control characters")
		}
	}
	if utf8.RuneCountInString(value) > maxNoteRunes {
		return badRequest("description must be at most 1000 characters")
	}
	return nil
}

func checkAnnotationColor(value string) error {
	if !slices.Contains(annotationColors, value) {
		return badRequest("color must be one of " + strings.Join(annotationColors, ", "))
	}
	return nil
}

// parseDay reads a calendar date YYYY-MM-DD with a year from 1 to 9999.
func parseDay(argument, value string) (time.Time, error) {
	day, err := time.Parse(dateLayout, value)
	if err != nil || day.Year() < 1 || len(value) != len(dateLayout) {
		return time.Time{}, badRequest(argument + " must be a calendar date YYYY-MM-DD")
	}
	return day, nil
}

type (
	// dateArgument is the range of annotation_date_range.
	dateArgument struct {
		StartDate string `json:"start_date"`
		EndDate   string `json:"end_date"`
	}
	annotationWriteArguments struct {
		Property            string        `json:"property"`
		Name                string        `json:"name"`
		Title               *string       `json:"title"`
		Description         *string       `json:"description"`
		Color               *string       `json:"color"`
		AnnotationDate      *string       `json:"annotation_date"`
		AnnotationDateRange *dateArgument `json:"annotation_date_range"`
	}
)

// Request bodies of google.type.Date and the annotation: only what the caller set. The pointers keep an empty
// description, which clears it on an update.
type (
	dateBody struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	}
	rangeBody struct {
		StartDate dateBody `json:"startDate"`
		EndDate   dateBody `json:"endDate"`
	}
	annotationBody struct {
		Title               *string    `json:"title,omitempty"`
		Description         *string    `json:"description,omitempty"`
		Color               *string    `json:"color,omitempty"`
		AnnotationDate      *dateBody  `json:"annotationDate,omitempty"`
		AnnotationDateRange *rangeBody `json:"annotationDateRange,omitempty"`
	}
)

func bodyOfDay(day time.Time) dateBody {
	return dateBody{Year: day.Year(), Month: int(day.Month()), Day: day.Day()}
}

// check validates the members that are set, which is what both changing tools share.
func (a annotationWriteArguments) check() error {
	if a.Title != nil {
		if err := checkAnnotationTitle(*a.Title); err != nil {
			return err
		}
	}
	if a.Description != nil {
		if err := checkAnnotationDescription(*a.Description); err != nil {
			return err
		}
	}
	if a.Color != nil {
		if err := checkAnnotationColor(*a.Color); err != nil {
			return err
		}
	}
	if a.AnnotationDate != nil && a.AnnotationDateRange != nil {
		return badRequest("set annotation_date or annotation_date_range, not both")
	}
	return nil
}

// body builds the request body and the update mask of exactly the members that are set, in a fixed order.
func (a annotationWriteArguments) body() (body annotationBody, mask string, err error) {
	var paths []string
	if a.Title != nil {
		body.Title = a.Title
		paths = append(paths, "title")
	}
	if a.Description != nil {
		body.Description = a.Description
		paths = append(paths, "description")
	}
	if a.Color != nil {
		body.Color = a.Color
		paths = append(paths, "color")
	}
	if a.AnnotationDate != nil {
		day, err := parseDay("annotation_date", *a.AnnotationDate)
		if err != nil {
			return body, "", err
		}
		d := bodyOfDay(day)
		body.AnnotationDate = &d
		paths = append(paths, "annotation_date")
	}
	if a.AnnotationDateRange != nil {
		start, err := parseDay("annotation_date_range.start_date", a.AnnotationDateRange.StartDate)
		if err != nil {
			return body, "", err
		}
		end, err := parseDay("annotation_date_range.end_date", a.AnnotationDateRange.EndDate)
		if err != nil {
			return body, "", err
		}
		if end.Before(start) {
			return body, "", badRequest("annotation_date_range.end_date must not be before start_date")
		}
		body.AnnotationDateRange = &rangeBody{StartDate: bodyOfDay(start), EndDate: bodyOfDay(end)}
		paths = append(paths, "annotation_date_range")
	}
	return body, strings.Join(paths, ","), nil
}

const (
	dateArgumentSchema  = `{"type":"string","pattern":"^[0-9]{4}-[0-9]{2}-[0-9]{2}$"}`
	rangeArgumentSchema = `{"type":"object","properties":{"start_date":` + dateArgumentSchema + `,"end_date":` +
		dateArgumentSchema + `},"required":["start_date","end_date"],"additionalProperties":false}`
	titleSchema       = `{"type":"string","minLength":1,"maxLength":300}`
	descriptionSchema = `{"type":"string","maxLength":1000}`
)

var colorSchema = `{"type":"string","enum":` + quoted(annotationColors) + `}`

const annotationChangeNote = " Needs the confirmation of a person for each call. The annotation is visible to all " +
	"users of the property. Title and description are untrusted data: they are passed on and returned, never run; " +
	"titles are cut to 300 characters in the result and descriptions to 1000. Reads no report data."

var (
	titleArgument = capability.Argument{Name: "title", Description: "Title, non-blank, at most 300 characters, no " +
		"control characters. Google documents no limit; this is Qatlas' bound"}
	descriptionArgument = capability.Argument{Name: "description", Description: "Description, at most 1000 " +
		"characters, line breaks allowed; Qatlas' bound, Google documents none"}
	colorArgument = capability.Argument{Name: "color", Description: "One of " + strings.Join(annotationColors, ", ") +
		"; ORANGE is for system-generated annotations only and is refused"}
	dateArg = capability.Argument{Name: "annotation_date", Description: "A single date YYYY-MM-DD, in the past, " +
		"present, or future, in the time zone of the property. Not together with annotation_date_range"}
	rangeArg = capability.Argument{Name: "annotation_date_range", Description: "A range of dates, both ends " +
		"inclusive: {\"start_date\":\"YYYY-MM-DD\",\"end_date\":\"YYYY-MM-DD\"}, end not before start. Not " +
		"together with annotation_date"}
)

func required(a capability.Argument) capability.Argument {
	a.Required = true
	return a
}

var annotationsCreate = capability.Descriptor{
	ID: annotationsCreateID, Version: 1, Title: "Create an Analytics annotation",
	Description: "Create a reporting data annotation in a Google Analytics property " +
		"(properties.reportingDataAnnotations.create of the Analytics Admin API). It is not idempotent: a repeated " +
		"call creates a second annotation. Exactly one of annotation_date and annotation_date_range is required." +
		annotationChangeNote + annotationTargetNote + alphaNote,
	Tags: []string{"google", "analytics", "annotations", "create"}, Provider: Provider,
	Risk: writeRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"property":` + propertyArgSchema + `,"title":` +
		titleSchema + `,"description":` + descriptionSchema + `,"color":` + colorSchema + `,"annotation_date":` +
		dateArgumentSchema + `,"annotation_date_range":` + rangeArgumentSchema + `},` +
		`"required":["property","title","color"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(annotationSchema),
	Arguments: []capability.Argument{
		{Name: "property", Required: true, Description: "Property resource name properties/ID that gets the annotation"},
		required(titleArgument), descriptionArgument, required(colorArgument), dateArg, rangeArg,
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The new annotation: " + annotationFieldsText}},
	Examples: []capability.Example{{Description: "An annotation for one day",
		Arguments: json.RawMessage(`{"property":"properties/200","title":"Site relaunch","color":"BLUE",` +
			`"annotation_date":"2026-11-20"}`)}, {Description: "An annotation for a range",
		Arguments: json.RawMessage(`{"property":"properties/200","title":"Holiday sale","color":"RED",` +
			`"annotation_date_range":{"start_date":"2026-12-24","end_date":"2026-12-31"}}`)}},
}

var annotationsUpdate = capability.Descriptor{
	ID: annotationsUpdateID, Version: 1, Title: "Update an Analytics annotation",
	Description: "Change the title, description, color, or date of a reporting data annotation " +
		"(properties.reportingDataAnnotations.patch of the Analytics Admin API). Only the arguments that are set are " +
		"changed, and at least one is required; Qatlas sends exactly them as the update mask. Setting a date or a " +
		"range replaces the previous one; an empty description clears it. Google refuses to change a " +
		"system-generated annotation." + annotationChangeNote + annotationTargetNote + alphaNote,
	Tags: []string{"google", "analytics", "annotations", "update"}, Provider: Provider,
	Risk: writeRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + annotationNameSchema + `,"title":` +
		titleSchema + `,"description":` + descriptionSchema + `,"color":` + colorSchema + `,"annotation_date":` +
		dateArgumentSchema + `,"annotation_date_range":` + rangeArgumentSchema + `},"required":["name"],` +
		`"additionalProperties":false}`),
	OutputSchema: json.RawMessage(annotationSchema),
	Arguments: []capability.Argument{
		{Name: "name", Required: true, Description: "Annotation resource name " +
			"properties/ID/reportingDataAnnotations/ID, from annotations.list"},
		titleArgument, descriptionArgument, colorArgument, dateArg, rangeArg,
	},
	Fields: []capability.Field{{Name: "(result)", Description: "The annotation after the change: " +
		annotationFieldsText}},
	Examples: []capability.Example{{Description: "Move an annotation to another day",
		Arguments: json.RawMessage(`{"name":"properties/200/reportingDataAnnotations/3000",` +
			`"annotation_date":"2026-11-21"}`)}},
}

var annotationsDelete = capability.Descriptor{
	ID: annotationsDeleteID, Version: 1, Title: "Delete an Analytics annotation",
	Description: "Delete a reporting data annotation of a Google Analytics property " +
		"(properties.reportingDataAnnotations.delete of the Analytics Admin API). The deletion is final and no tool " +
		"restores it. Google refuses to delete a system-generated annotation; the error class of that refusal is " +
		"not verified." + annotationChangeNote + annotationTargetNote + alphaNote,
	Tags: []string{"google", "analytics", "annotations", "delete"}, Provider: Provider,
	Risk: writeRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema: json.RawMessage(`{"type":"object","properties":{"name":` + annotationNameSchema +
		`},"required":["name"],"additionalProperties":false}`),
	OutputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"deleted":` +
		`{"type":"boolean"}},"additionalProperties":false}`),
	Arguments: []capability.Argument{{Name: "name", Required: true, Description: "Annotation resource name " +
		"properties/ID/reportingDataAnnotations/ID, from annotations.list"}},
	Fields: []capability.Field{{Name: "name", Description: "The name that was deleted"},
		{Name: "deleted", Description: "true once Google confirmed the deletion"}},
	Examples: []capability.Example{{Description: "Delete one annotation",
		Arguments: json.RawMessage(`{"name":"properties/200/reportingDataAnnotations/3000"}`)}},
}

func parseAnnotationArguments(op string, raw json.RawMessage) (annotationWriteArguments, error) {
	var arguments annotationWriteArguments
	if err := json.Unmarshal(raw, &arguments); err != nil {
		return arguments, failure(op, "the validated arguments could not be read")
	}
	return arguments, nil
}

func handleAnnotationsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create annotation"
	arguments, err := parseAnnotationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkPropertyName("property", arguments.Property)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	if arguments.Title == nil || arguments.Color == nil {
		return nil, badRequest("property, title, and color are required")
	}
	if (arguments.AnnotationDate == nil) == (arguments.AnnotationDateRange == nil) {
		return nil, badRequest("set exactly one of annotation_date and annotation_date_range")
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	body, _, err := arguments.body()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the annotation could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, annotationsCreateID, property, proof); err != nil {
		return nil, err
	}
	var out annotation
	if err := c.change(ctx, changeCall{op: op, tool: annotationsCreateID, method: http.MethodPost,
		path: alphaPath(property + annotationsSegment), body: encoded, limit: maxAnnotationBytes,
		notFound: notFoundProperty}, &out); err != nil {
		return nil, err
	}
	if !annotationIn(out.Name, property) {
		return nil, unsure(failure(op, subject+" answered without the name of an annotation of the property"))
	}
	return out, nil
}

func handleAnnotationsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "update annotation"
	arguments, err := parseAnnotationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkAnnotationName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	if err := arguments.check(); err != nil {
		return nil, err
	}
	body, mask, err := arguments.body()
	if err != nil {
		return nil, err
	}
	if mask == "" {
		return nil, badRequest("set at least one of title, description, color, annotation_date, and annotation_date_range")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, failure(op, "the annotation could not be encoded")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, annotationsUpdateID, property, proof); err != nil {
		return nil, err
	}
	var out annotation
	if err := c.change(ctx, changeCall{op: op, tool: annotationsUpdateID, method: http.MethodPatch,
		path: alphaPath(arguments.Name), query: url.Values{"updateMask": {mask}}, body: encoded,
		limit: maxAnnotationBytes, notFound: notFoundAnnotation}, &out); err != nil {
		return nil, err
	}
	if string(out.Name) != arguments.Name {
		return nil, unsure(failure(op, subject+" answered with another annotation than the one asked for"))
	}
	return out, nil
}

type annotationDeleted struct {
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

func handleAnnotationsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete annotation"
	arguments, err := parseAnnotationArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bind, err := bindingOf(resolved)
	if err != nil {
		return nil, err
	}
	property, err := checkAnnotationName("name", arguments.Name)
	if err != nil {
		return nil, err
	}
	proof, err := bind.property(property)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveProperty(ctx, op, annotationsDeleteID, property, proof); err != nil {
		return nil, err
	}
	if err := c.change(ctx, changeCall{op: op, tool: annotationsDeleteID, method: http.MethodDelete,
		path: alphaPath(arguments.Name), limit: maxAnnotationBytes, notFound: notFoundAnnotation}, nil); err != nil {
		return nil, err
	}
	return annotationDeleted{Name: arguments.Name, Deleted: true}, nil
}

// annotationSummary phrases what a confirmed call would do from its arguments alone. The arguments are
// untrusted data; the core cuts and neutralizes what this returns.
func annotationSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a annotationWriteArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		switch tool {
		case annotationsCreateID:
			out := "Create Analytics annotation"
			if a.Title != nil {
				out += " " + strconv.Quote(*a.Title)
			}
			out += " in " + a.Property
			if a.AnnotationDate != nil {
				out += " on " + *a.AnnotationDate
			}
			if a.AnnotationDateRange != nil {
				out += " from " + a.AnnotationDateRange.StartDate + " to " + a.AnnotationDateRange.EndDate
			}
			if a.Color != nil {
				out += ", color " + *a.Color
			}
			return out + "; visible to all users of the property"
		case annotationsUpdateID:
			_, mask, _ := a.body()
			if mask == "" {
				// An invalid date has no mask; the summary still names what was asked for.
				mask = "the given members"
			}
			return "Change " + strings.ReplaceAll(mask, ",", ", ") + " of Analytics annotation " + a.Name +
				"; visible to all users of the property"
		}
		return "Delete Analytics annotation " + a.Name + " for good; no tool restores it"
	}
}

package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The local post tools use the localPosts methods of the My Business API v4 as documented at
// https://developers.google.com/my-business/reference/rest/v4/accounts.locations.localPosts (the LocalPost
// resource with CallToAction, LocalPostEvent, TimeInterval, TimeOfDay, LocalPostOffer, and the enums, and the
// methods list, get, create, patch, delete) and, for the media of a post, the MediaItem resource at
// https://developers.google.com/my-business/reference/rest/v4/accounts.locations.media, read on 2026-11-18.
// v4 has no discovery document, so nothing here comes from one. The reference names no recurrence field, so
// none is offered. localPosts.reportInsights is a different tool group.
const (
	localPostsListID   = Provider + ".local-posts.list"
	localPostsGetID    = Provider + ".local-posts.get"
	localPostsCreateID = Provider + ".local-posts.create"
	localPostsUpdateID = Provider + ".local-posts.update"
	localPostsDeleteID = Provider + ".local-posts.delete"
)

// Bounds. The page size is documented (default 20, 1 to 100). The reference states no length for a summary,
// a title, a code, or the number of media items, so the tools rely on what the confirmation can show whole
// and keep the media list at 10, which is Qatlas's own bound.
const (
	maxLocalPostsPage = 100
	maxLocalPostMedia = 10

	formLocalPost = "accounts/ACCOUNT/locations/LOCATION/localPosts/POST"
	// The post ID is documented only as part of a name; the tool accepts the characters of a URL-safe token, so
	// no segment can be a path step.
	localPostIDPattern = `[A-Za-z0-9_-]{1,256}`
	localPostPattern   = `^accounts/` + idPattern + `/locations/` + idPattern + `/localPosts/` + localPostIDPattern + `$`
	maxLocalPostLen    = 350
	localPostSegment   = "/localPosts/"
)

var localPostForm = regexp.MustCompile(localPostPattern)

var (
	localPostTopicTypes = []string{"STANDARD", "EVENT", "OFFER", "ALERT"}
	// GET_OFFER is deprecated by Google (use the OFFER topic type) and the unspecified value names nothing.
	localPostActionTypes = []string{"BOOK", "ORDER", "SHOP", "LEARN_MORE", "SIGN_UP", "CALL"}
	// COVID_19 is the only alert type the reference lists; Google is disabling the creation of new ones.
	localPostAlertTypes = []string{"COVID_19"}
	// localPostFields are the fields of a post an update may name, in the spelling of the update mask. The
	// topic type is not among them: it is the context of a change, never changed.
	localPostFields = []string{"languageCode", "summary", "callToAction", "event", "media", "offer", "alertType"}
)

var (
	localPostArgSchema = `{"type":"string","maxLength":` + strconv.Itoa(maxLocalPostLen) + `,"pattern":"` +
		localPostPattern + `","x-form":"` + formLocalPost + `"}`
	localPostTopicSchema  = `{"type":"string","enum":` + mustJSON(localPostTopicTypes) + `}`
	localPostAlertSchema  = `{"type":"string","enum":` + mustJSON(localPostAlertTypes) + `}`
	localPostLangSchema   = `{"type":"string","minLength":2,"maxLength":35}`
	localPostTextSchema   = `{"type":"string","minLength":1,"maxLength":4096}`
	localPostURLSchema    = `{"type":"string","minLength":1,"maxLength":2000}`
	localPostPageSchema   = `{"type":"integer","minimum":1,"maximum":100}`
	localPostDateSchema   = localPostSchemaOf([]string{"year", "month", "day"}, "year", `{"type":"integer","minimum":1,"maximum":9999}`, "month", `{"type":"integer","minimum":1,"maximum":12}`, "day", `{"type":"integer","minimum":1,"maximum":31}`)
	localPostTimeSchema   = localPostSchemaOf([]string{"hours", "minutes"}, "hours", `{"type":"integer","minimum":0,"maximum":23}`, "minutes", `{"type":"integer","minimum":0,"maximum":59}`, "seconds", `{"type":"integer","minimum":0,"maximum":59}`, "nanos", `{"type":"integer","minimum":0,"maximum":999999999}`)
	localPostEventSchema  = localPostSchemaOf([]string{"title", "schedule"}, "title", localPostTextSchema, "schedule", localPostSchemaOf([]string{"start_date", "start_time", "end_date", "end_time"}, "start_date", localPostDateSchema, "start_time", localPostTimeSchema, "end_date", localPostDateSchema, "end_time", localPostTimeSchema))
	localPostActionSchema = localPostSchemaOf([]string{"action_type"}, "action_type", `{"type":"string","enum":`+mustJSON(localPostActionTypes)+`}`, "url", localPostURLSchema)
	localPostOfferSchema  = localPostSchemaOf(nil, "coupon_code", localPostTextSchema, "redeem_online_url", localPostURLSchema, "terms_conditions", localPostTextSchema)
	localPostMediaSchema  = `{"type":"array","minItems":1,"maxItems":` + strconv.Itoa(maxLocalPostMedia) + `,"items":` +
		localPostSchemaOf([]string{"source_url"}, "source_url", localPostURLSchema, "media_format", `{"type":"string","enum":["PHOTO"]}`) + `}`
	localPostMaskSchema = `{"type":"array","minItems":1,"maxItems":7,"uniqueItems":true,"items":{"type":"string","enum":` +
		mustJSON(localPostFields) + `}}`
)

// localPostSchemaOf is inputSchema as a string, for nested objects.
func localPostSchemaOf(required []string, pairs ...string) string {
	return string(inputSchema(required, pairs...))
}

// The LocalPost resource as documented. Everything in it is written by whoever manages the profile, or by
// Google for the output-only members: texts are cut and links are text that is never fetched. Only the media
// members that describe the photo are answered: the insights and the attribution of a media item (the name and
// photo of a contributor) are left out, and its location association does not apply to a post.
type (
	localPostAction struct {
		ActionType short `json:"actionType,omitempty"`
		URL        link  `json:"url,omitempty"`
	}
	localPostDate struct {
		Year  integer `json:"year,omitzero"`
		Month integer `json:"month,omitzero"`
		Day   integer `json:"day,omitzero"`
	}
	localPostTimeOfDay struct {
		Hours   integer `json:"hours,omitzero"`
		Minutes integer `json:"minutes,omitzero"`
		Seconds integer `json:"seconds,omitzero"`
		Nanos   integer `json:"nanos,omitzero"`
	}
	localPostInterval struct {
		StartDate *localPostDate      `json:"startDate,omitempty"`
		StartTime *localPostTimeOfDay `json:"startTime,omitempty"`
		EndDate   *localPostDate      `json:"endDate,omitempty"`
		EndTime   *localPostTimeOfDay `json:"endTime,omitempty"`
	}
	localPostEvent struct {
		Title    line               `json:"title,omitempty"`
		Schedule *localPostInterval `json:"schedule,omitempty"`
	}
	localPostOffer struct {
		CouponCode      line `json:"couponCode,omitempty"`
		RedeemOnlineURL link `json:"redeemOnlineUrl,omitempty"`
		TermsConditions long `json:"termsConditions,omitempty"`
	}
	localPostDimensions struct {
		WidthPixels  integer `json:"widthPixels,omitzero"`
		HeightPixels integer `json:"heightPixels,omitzero"`
	}
	localPostMediaItem struct {
		Name         line                 `json:"name,omitempty"`
		MediaFormat  short                `json:"mediaFormat,omitempty"`
		SourceURL    link                 `json:"sourceUrl,omitempty"`
		GoogleURL    link                 `json:"googleUrl,omitempty"`
		ThumbnailURL link                 `json:"thumbnailUrl,omitempty"`
		CreateTime   short                `json:"createTime,omitempty"`
		Dimensions   *localPostDimensions `json:"dimensions,omitempty"`
		Description  long                 `json:"description,omitempty"`
	}
	localPost struct {
		Name         line                     `json:"name,omitempty"`
		LanguageCode short                    `json:"languageCode,omitempty"`
		Summary      long                     `json:"summary,omitempty"`
		CallToAction *localPostAction         `json:"callToAction,omitempty"`
		CreateTime   short                    `json:"createTime,omitempty"`
		UpdateTime   short                    `json:"updateTime,omitempty"`
		Event        *localPostEvent          `json:"event,omitempty"`
		State        short                    `json:"state,omitempty"`
		Media        list[localPostMediaItem] `json:"media,omitempty"`
		SearchURL    link                     `json:"searchUrl,omitempty"`
		TopicType    short                    `json:"topicType,omitempty"`
		AlertType    short                    `json:"alertType,omitempty"`
		Offer        *localPostOffer          `json:"offer,omitempty"`
	}

	localPostsAnswer struct {
		LocalPosts    list[localPost] `json:"localPosts"`
		NextPageToken string          `json:"nextPageToken"`
	}
	localPostsResult struct {
		LocalPosts []localPost `json:"localPosts"`
		NextCursor string      `json:"next_cursor,omitempty"`
	}
	localPostDeleted struct {
		LocalPost string `json:"local_post"`
		Deleted   bool   `json:"deleted"`
	}
)

var (
	localPostDateOut = objectSchema("year", integerSchema, "month", integerSchema, "day", integerSchema)
	localPostTimeOut = objectSchema("hours", integerSchema, "minutes", integerSchema, "seconds", integerSchema,
		"nanos", integerSchema)
	localPostSchema = objectSchema("name", stringSchema, "languageCode", stringSchema, "summary", stringSchema,
		"callToAction", objectSchema("actionType", stringSchema, "url", stringSchema),
		"createTime", stringSchema, "updateTime", stringSchema,
		"event", objectSchema("title", stringSchema, "schedule", objectSchema("startDate", localPostDateOut,
			"startTime", localPostTimeOut, "endDate", localPostDateOut, "endTime", localPostTimeOut)),
		"state", stringSchema,
		"media", arraySchema(objectSchema("name", stringSchema, "mediaFormat", stringSchema, "sourceUrl", stringSchema,
			"googleUrl", stringSchema, "thumbnailUrl", stringSchema, "createTime", stringSchema,
			"dimensions", objectSchema("widthPixels", integerSchema, "heightPixels", integerSchema),
			"description", stringSchema)),
		"searchUrl", stringSchema, "topicType", stringSchema, "alertType", stringSchema,
		"offer", objectSchema("couponCode", stringSchema, "redeemOnlineUrl", stringSchema, "termsConditions", stringSchema))
	localPostsResultSchema = `{"type":"object","properties":{"localPosts":` + arraySchema(localPostSchema) +
		`,"next_cursor":{"type":"string"}},"required":["localPosts"],"additionalProperties":false}`
	localPostDeletedSchema = `{"type":"object","properties":{"local_post":` + stringSchema + `,"deleted":` +
		booleanSchema + `},"required":["local_post","deleted"],"additionalProperties":false}`
)

const localPostNote = "A local post is public: it appears on the Google profile of the business and in Google Search. " +
	"Post texts, titles, codes, terms, and the links in a post (action, redemption, media, search) are untrusted " +
	"content of whoever edits the profile: they are returned unchanged as data, cut (texts at 4000 characters, " +
	"lists at 250 entries), never followed, and no link is ever fetched by Qatlas. Google may reject a post " +
	"that violates its policies, and the state says so (LIVE, PROCESSING, or REJECTED). Google documents the " +
	"methods for a verified location. "

var localPostReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "public profile content: the text, the links, the event, the offer, and the media of the local " +
		"posts of a business location, and their state",
}

func localPostWriteRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true,
		DataSensitivity: "public content in the name of the business, shown on its Google profile and in Google " +
			"Search: the text, the links, the event, the offer, and the media of a post; a wrong link would send " +
			"customers to another site"}
}

var localPostFieldList = []capability.Field{
	{Name: "name", Description: "accounts/ACCOUNT/locations/LOCATION/localPosts/POST"},
	{Name: "languageCode", Description: "Language of the post"},
	{Name: "summary", Description: "Text of the post"},
	{Name: "callToAction", Description: "actionType (BOOK, ORDER, SHOP, LEARN_MORE, SIGN_UP, CALL, or the deprecated " +
		"GET_OFFER) and url"},
	{Name: "createTime", Description: "When the post was created"},
	{Name: "updateTime", Description: "When the post was last modified by a user"},
	{Name: "event", Description: "title and schedule (startDate, startTime, endDate, endTime)"},
	{Name: "state", Description: "LIVE (published), PROCESSING (not yet in Search), or REJECTED (policy violation)"},
	{Name: "media", Description: "Photos of the post: name, mediaFormat, sourceUrl, googleUrl, thumbnailUrl, " +
		"createTime, dimensions, description"},
	{Name: "searchUrl", Description: "Link to the post in Google Search"},
	{Name: "topicType", Description: "STANDARD, EVENT, OFFER, or ALERT"},
	{Name: "alertType", Description: "COVID_19, for an ALERT post"},
	{Name: "offer", Description: "couponCode, redeemOnlineUrl, termsConditions"},
}

var localPostConfirmNote = "The confirmation shows the location, the type, the whole text, every URL, and the media " +
	"sources; a post whose confirmation would not fit into the " + limitText(localPostsCreateID) + " the core keeps is refused before " +
	"anything is sent, since the confirmation would be cut. "

const localPostContentArguments = "language_code, summary, call_to_action (action_type BOOK, ORDER, SHOP, " +
	"LEARN_MORE, SIGN_UP, or CALL, and url unless CALL), event (title and schedule with start_date, start_time, " +
	"end_date, end_time), offer (coupon_code, redeem_online_url, terms_conditions), alert_type, and media " +
	"(source_url, https only)"

var localPostContentArgs = []capability.Argument{
	{Name: "language_code", Description: "Language of the post as a BCP-47 code, for example en or de"},
	{Name: "summary", Description: "Text of the post, plain text without control or invisible characters"},
	{Name: "call_to_action", Description: "action_type: BOOK, ORDER, SHOP, LEARN_MORE, SIGN_UP, or CALL; url: https " +
		"address, required unless CALL, and not allowed with CALL; refused for OFFER, where Google ignores it"},
	{Name: "event", Description: "title and schedule: start_date and end_date as year, month, day and start_time " +
		"and end_time as hours, minutes, and optionally seconds and nanos; all four are required; for EVENT and OFFER " +
		"posts"},
	{Name: "offer", Description: "coupon_code, redeem_online_url (https address), and terms_conditions, for OFFER " +
		"posts"},
	{Name: "alert_type", Description: "COVID_19, for ALERT posts, which Google supports only with summary and " +
		"call_to_action and is disabling for new posts"},
	{Name: "media", Description: "Up to 10 photos as source_url (https address Google fetches the photo from) and " +
		"optionally media_format PHOTO; not for ALERT"},
}

var localPostsList = capability.Descriptor{
	ID: localPostsListID, Version: 1, Title: "List Business Profile local posts",
	Description: "List the local posts of one location (Google My Business API v4), up to 100 per call with a cursor " +
		"to read on, each with its state and its search link. " + localPostNote + "On a connection bound to an " +
		"account the location is first proved to belong to it. " + untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "local-posts"}, Risk: localPostReadRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"location"},
		"location", legacyLocationArgSchema, "page_size", localPostPageSchema, "cursor", cursorSchema),
	OutputSchema: json.RawMessage(localPostsResultSchema),
	Arguments: []capability.Argument{
		{Name: "location", Required: true, Description: "Location as " + formLegacyLocation + "; on a connection " +
			"bound to an account the account must be that account, and on one bound to a location the location " +
			"must be that location"},
		{Name: "page_size", Description: "Posts per call, 1 to 100; Google's default is 20"},
		cursorArgument,
	},
	Fields: []capability.Field{{Name: "localPosts", Description: "Posts of the page"}, nextCursorField},
	Examples: []capability.Example{{Description: "Posts of a location", Arguments: json.RawMessage(
		`{"location":"accounts/1000001/locations/2000002"}`)}},
}

var localPostsGet = capability.Descriptor{
	ID: localPostsGetID, Version: 1, Title: "Get a Business Profile local post",
	Description: "Read one local post of a location (Google My Business API v4) with its text, links, event, " +
		"offer, media, and state. " + localPostNote + "On a connection bound to an account the location is first " +
		"proved to belong to it. " + untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "local-posts"}, Risk: localPostReadRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"local_post"}, "local_post", localPostArgSchema),
	OutputSchema: json.RawMessage(localPostSchema),
	Arguments: []capability.Argument{{Name: "local_post", Required: true, Description: "Post as listed: " +
		formLocalPost}},
	Fields: localPostFieldList,
	Examples: []capability.Example{{Description: "One post", Arguments: json.RawMessage(
		`{"local_post":"accounts/1000001/locations/2000002/localPosts/AbCd_123"}`)}},
}

var localPostsCreate = capability.Descriptor{
	ID: localPostsCreateID, Version: 1, Title: "Create a Business Profile local post",
	Description: "Publish a local post at one location (Google My Business API v4 accounts.locations.localPosts." +
		"create) of the type STANDARD, EVENT, OFFER, or ALERT. The post is public in the name of the business. " +
		"EVENT and OFFER posts need an event, an ALERT post an alert_type. " + localPostConfirmNote + "Creating is not " +
		"idempotent and Qatlas never sends it twice: after an error that says the outcome is unknown, read the " +
		"posts with local-posts.list before creating again. The result names the post by its path. Google counts " +
		"at most 10 changes per minute and profile. " + localPostNote + untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "local-posts", "create"}, Provider: Provider,
	Risk: localPostWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"location", "topic_type"},
		"location", legacyLocationArgSchema, "topic_type", localPostTopicSchema, "language_code", localPostLangSchema,
		"summary", localPostTextSchema, "call_to_action", localPostActionSchema, "event", localPostEventSchema,
		"offer", localPostOfferSchema, "alert_type", localPostAlertSchema, "media", localPostMediaSchema),
	OutputSchema: json.RawMessage(localPostSchema),
	Arguments: append([]capability.Argument{
		{Name: "location", Required: true, Description: "Location as " + formLegacyLocation},
		{Name: "topic_type", Required: true, Description: "STANDARD, EVENT, OFFER, or ALERT"},
	}, localPostContentArgs...),
	Fields: localPostFieldList,
	Examples: []capability.Example{{Description: "A standard post with a button", Arguments: json.RawMessage(
		`{"location":"accounts/1000001/locations/2000002","topic_type":"STANDARD","summary":"New menu.",` +
			`"call_to_action":{"action_type":"LEARN_MORE","url":"https://example.invalid/menu"}}`)}},
}

var localPostsUpdate = capability.Descriptor{
	ID: localPostsUpdateID, Version: 1, Title: "Change a Business Profile local post",
	Description: "Change fields of one local post (Google My Business API v4 accounts.locations.localPosts.patch). " +
		"update_mask is required and must name exactly the fields given with a value, at the top level (" +
		strings.Join(localPostFields, ", ") + "): a named field is replaced as a whole, a field in the mask without a " +
		"value would be cleared and a value outside it ignored, so both are refused, and no field is cleared. " +
		"topic_type is the type of the post as it is, needed for the checks and the confirmation, and never changed. " +
		localPostConfirmNote + "The change is public. Google counts at most 10 changes per minute and profile. " +
		localPostNote + untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "local-posts", "update"}, Provider: Provider,
	Risk: localPostWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"local_post", "topic_type", "update_mask"},
		"local_post", localPostArgSchema, "topic_type", localPostTopicSchema, "update_mask", localPostMaskSchema,
		"language_code", localPostLangSchema, "summary", localPostTextSchema, "call_to_action", localPostActionSchema,
		"event", localPostEventSchema, "offer", localPostOfferSchema, "alert_type", localPostAlertSchema,
		"media", localPostMediaSchema),
	OutputSchema: json.RawMessage(localPostSchema),
	Arguments: append([]capability.Argument{
		{Name: "local_post", Required: true, Description: "Post as listed: " + formLocalPost},
		{Name: "topic_type", Required: true, Description: "The type of the post as it is: STANDARD, EVENT, OFFER, or " +
			"ALERT; it is not changed"},
		{Name: "update_mask", Required: true, Description: "The fields to change, from " +
			strings.Join(localPostFields, ", ") + "; exactly those given with a value"},
	}, localPostContentArgs...),
	Fields: localPostFieldList,
	Examples: []capability.Example{{Description: "Change the text", Arguments: json.RawMessage(
		`{"local_post":"accounts/1000001/locations/2000002/localPosts/AbCd_123","topic_type":"STANDARD",` +
			`"update_mask":["summary"],"summary":"New menu from Monday."}`)}},
}

var localPostsDelete = capability.Descriptor{
	ID: localPostsDeleteID, Version: 1, Title: "Delete a Business Profile local post",
	Description: "Delete one local post (Google My Business API v4 accounts.locations.localPosts.delete). The post " +
		"disappears from the public profile and from Google Search and cannot be restored. Google counts at most 10 " +
		"changes per minute and profile. On a connection bound to an account the location is first proved to belong " +
		"to it." + listedOnlyNote,
	Tags: []string{"google", "business-profile", "local-posts", "delete"}, Provider: Provider,
	RequiresToolAllowList: true,
	Risk:                  localPostWriteRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:           inputSchema([]string{"local_post"}, "local_post", localPostArgSchema),
	OutputSchema:          json.RawMessage(localPostDeletedSchema),
	Arguments: []capability.Argument{{Name: "local_post", Required: true, Description: "Post to delete: " +
		formLocalPost}},
	Fields: []capability.Field{{Name: "local_post", Description: "The post that was deleted"},
		{Name: "deleted", Description: "true once Google confirmed the deletion"}},
	Examples: []capability.Example{{Description: "Delete a post", Arguments: json.RawMessage(
		`{"local_post":"accounts/1000001/locations/2000002/localPosts/AbCd_123"}`)}},
}

// Arguments of the local post tools. A pointer or a nil slice tells a member that was not given.
type (
	localPostActionArg struct {
		ActionType string  `json:"action_type"`
		URL        *string `json:"url"`
	}
	localPostDateArg struct {
		Year  int `json:"year"`
		Month int `json:"month"`
		Day   int `json:"day"`
	}
	localPostTimeArg struct {
		Hours   int `json:"hours"`
		Minutes int `json:"minutes"`
		Seconds int `json:"seconds"`
		Nanos   int `json:"nanos"`
	}
	localPostScheduleArg struct {
		StartDate *localPostDateArg `json:"start_date"`
		StartTime *localPostTimeArg `json:"start_time"`
		EndDate   *localPostDateArg `json:"end_date"`
		EndTime   *localPostTimeArg `json:"end_time"`
	}
	localPostEventArg struct {
		Title    string                `json:"title"`
		Schedule *localPostScheduleArg `json:"schedule"`
	}
	localPostOfferArg struct {
		CouponCode      *string `json:"coupon_code"`
		RedeemOnlineURL *string `json:"redeem_online_url"`
		TermsConditions *string `json:"terms_conditions"`
	}
	localPostMediaArg struct {
		SourceURL   string `json:"source_url"`
		MediaFormat string `json:"media_format"`
	}
	localPostArguments struct {
		Location     string              `json:"location"`
		LocalPost    string              `json:"local_post"`
		PageSize     int                 `json:"page_size"`
		Cursor       string              `json:"cursor"`
		TopicType    string              `json:"topic_type"`
		LanguageCode *string             `json:"language_code"`
		Summary      *string             `json:"summary"`
		CallToAction *localPostActionArg `json:"call_to_action"`
		Event        *localPostEventArg  `json:"event"`
		Offer        *localPostOfferArg  `json:"offer"`
		AlertType    *string             `json:"alert_type"`
		Media        []localPostMediaArg `json:"media"`
		UpdateMask   []string            `json:"update_mask"`
	}
)

func readLocalPostArguments(op string, raw json.RawMessage) (localPostArguments, error) {
	var args localPostArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, provider.Fail(op, "the validated arguments could not be read")
	}
	if args.PageSize < 0 || args.PageSize > maxLocalPostsPage {
		return args, invalidArgument("page_size must be 1 to 100")
	}
	return args, nil
}

// localPostOf checks the path of a post against its form and the target and returns its v4 location path.
func (s scope) localPostOf(given string) (string, error) {
	if len(given) > maxLocalPostLen || !localPostForm.MatchString(given) {
		return "", invalidArgument("local_post must be " + formLocalPost + " with decimal IDs")
	}
	location, _, _ := strings.Cut(given, localPostSegment)
	return location, s.legacyInTarget("local_post", location)
}

// localPostPlain refuses a text that is not plain text a person can read whole in the confirmation: invalid
// UTF-8 and control, invisible formatting, or separator characters other than line breaks and tabs.
func localPostPlain(name, text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return invalidArgument(name + " must not be empty")
	case !utf8.ValidString(text):
		return invalidArgument(name + " must be valid UTF-8")
	}
	for _, r := range text {
		if r != '\n' && r != '\t' && unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return invalidArgument(name + " must not contain control, invisible formatting, or separator " +
				"characters other than line breaks and tabs")
		}
	}
	return nil
}

// localPostURL checks an address of a post like the address of a place action link: https, plain ASCII, no user
// information. It is never fetched.
func localPostURL(name, address string) error {
	err := checkLinkURI(address)
	var invalid *provider.InvalidRequestError
	if errors.As(err, &invalid) {
		return invalidArgument(name + ": " + invalid.Message)
	}
	return err
}

func localPostDateOK(name string, d *localPostDateArg) error {
	if d == nil {
		return invalidArgument(name + " is required")
	}
	if d.Year < 1 || d.Year > 9999 || d.Month < 1 || d.Month > 12 || d.Day < 1 {
		return invalidArgument(name + " is not a valid date")
	}
	if t := time.Date(d.Year, time.Month(d.Month), d.Day, 0, 0, 0, 0, time.UTC); t.Day() != d.Day {
		return invalidArgument(name + " is not a valid date")
	}
	return nil
}

func localPostTimeOK(name string, v *localPostTimeArg) error {
	switch {
	case v == nil:
		return invalidArgument(name + " is required")
	case v.Hours < 0 || v.Hours > 23 || v.Minutes < 0 || v.Minutes > 59 || v.Seconds < 0 || v.Seconds > 59 ||
		v.Nanos < 0 || v.Nanos > 999999999:
		return invalidArgument(name + " must be hours 0 to 23, minutes 0 to 59, seconds 0 to 59, nanos 0 to 999999999")
	}
	return nil
}

func (d localPostDateArg) at(v localPostTimeArg) time.Time {
	return time.Date(d.Year, time.Month(d.Month), d.Day, v.Hours, v.Minutes, v.Seconds, v.Nanos, time.UTC)
}

func localPostEventOK(e *localPostEventArg) error {
	if err := localPostPlain("event.title", e.Title); err != nil {
		return err
	}
	s := e.Schedule
	if s == nil {
		return invalidArgument("event.schedule is required: Google needs all four of start_date, start_time, end_date, end_time")
	}
	for _, check := range []error{localPostDateOK("event.schedule.start_date", s.StartDate),
		localPostTimeOK("event.schedule.start_time", s.StartTime), localPostDateOK("event.schedule.end_date", s.EndDate),
		localPostTimeOK("event.schedule.end_time", s.EndTime)} {
		if check != nil {
			return check
		}
	}
	if s.EndDate.at(*s.EndTime).Before(s.StartDate.at(*s.StartTime)) {
		return invalidArgument("event.schedule ends before it starts")
	}
	return nil
}

func localPostOfferOK(o *localPostOfferArg) error {
	if o.CouponCode == nil && o.RedeemOnlineURL == nil && o.TermsConditions == nil {
		return invalidArgument("offer must name at least one of coupon_code, redeem_online_url, terms_conditions")
	}
	if o.CouponCode != nil {
		if err := localPostPlain("offer.coupon_code", *o.CouponCode); err != nil {
			return err
		}
	}
	if o.TermsConditions != nil {
		if err := localPostPlain("offer.terms_conditions", *o.TermsConditions); err != nil {
			return err
		}
	}
	if o.RedeemOnlineURL != nil {
		return localPostURL("offer.redeem_online_url", *o.RedeemOnlineURL)
	}
	return nil
}

// localPostGiven lists the updatable fields that carry a value, sorted.
func localPostGiven(a localPostArguments) []string {
	var given []string
	for field, set := range map[string]bool{"languageCode": a.LanguageCode != nil, "summary": a.Summary != nil,
		"callToAction": a.CallToAction != nil, "event": a.Event != nil, "media": len(a.Media) > 0,
		"offer": a.Offer != nil, "alertType": a.AlertType != nil} {
		if set {
			given = append(given, field)
		}
	}
	slices.Sort(given)
	return given
}

// checkLocalPost refuses before any I/O what Google documents as not valid for the type of the post, what is
// not plain text or a plain https address, and, for an update, a mask that does not name exactly the fields
// given. Google documents event as required for EVENT and OFFER, alertType for ALERT only, callToAction as
// ignored for OFFER, and an ALERT as supporting only summary and call to action; the tool refuses what Google
// would drop or reject instead of letting a person confirm a post that would look different.
func checkLocalPost(a localPostArguments, update bool) error {
	if !slices.Contains(localPostTopicTypes, a.TopicType) {
		return invalidArgument("topic_type must be one of " + strings.Join(localPostTopicTypes, ", "))
	}
	if a.Summary != nil {
		if err := localPostPlain("summary", *a.Summary); err != nil {
			return err
		}
	}
	if a.LanguageCode != nil && !languageCodeRe.MatchString(*a.LanguageCode) {
		return invalidArgument("language_code must be a language code such as en or de")
	}
	if a.AlertType != nil && !slices.Contains(localPostAlertTypes, *a.AlertType) {
		return invalidArgument("alert_type must be COVID_19")
	}
	forbidden := map[string]bool{}
	switch a.TopicType {
	case "STANDARD":
		forbidden = map[string]bool{"event": a.Event != nil, "offer": a.Offer != nil, "alert_type": a.AlertType != nil}
	case "EVENT":
		forbidden = map[string]bool{"offer": a.Offer != nil, "alert_type": a.AlertType != nil}
	case "OFFER":
		forbidden = map[string]bool{"alert_type": a.AlertType != nil, "call_to_action": a.CallToAction != nil}
	case "ALERT":
		forbidden = map[string]bool{"event": a.Event != nil, "offer": a.Offer != nil, "media": len(a.Media) > 0}
	}
	for _, field := range []string{"event", "offer", "alert_type", "call_to_action", "media"} {
		if forbidden[field] {
			return invalidArgument(field + " does not apply to a post of type " + a.TopicType)
		}
	}
	if a.CallToAction != nil {
		if err := localPostActionOK(a.CallToAction); err != nil {
			return err
		}
	}
	if a.Event != nil {
		if err := localPostEventOK(a.Event); err != nil {
			return err
		}
	}
	if a.Offer != nil {
		if err := localPostOfferOK(a.Offer); err != nil {
			return err
		}
	}
	if len(a.Media) > maxLocalPostMedia {
		return invalidArgument("media holds at most 10 items")
	}
	for _, m := range a.Media {
		if err := localPostURL("media.source_url", m.SourceURL); err != nil {
			return err
		}
		if m.MediaFormat != "" && m.MediaFormat != "PHOTO" {
			return invalidArgument("media_format must be PHOTO, the only format Google supports")
		}
	}
	if update {
		return localPostMaskOK(a)
	}
	switch {
	case a.Location == "":
		return invalidArgument("location is required: the confirmation names it")
	case len(a.UpdateMask) > 0:
		return invalidArgument("update_mask belongs to the update tool")
	case (a.TopicType == "EVENT" || a.TopicType == "OFFER") && a.Event == nil:
		return invalidArgument("event is required for a post of type " + a.TopicType)
	case a.TopicType == "ALERT" && a.AlertType == nil:
		return invalidArgument("alert_type is required for a post of type ALERT")
	case a.TopicType != "ALERT" && a.AlertType != nil:
		return invalidArgument("alert_type applies only to a post of type ALERT")
	}
	return nil
}

func localPostActionOK(c *localPostActionArg) error {
	switch {
	case !slices.Contains(localPostActionTypes, c.ActionType):
		return invalidArgument("call_to_action.action_type must be one of " + strings.Join(localPostActionTypes, ", "))
	case c.ActionType == "CALL" && c.URL != nil:
		return invalidArgument("call_to_action.url must be left out for CALL")
	case c.ActionType != "CALL" && c.URL == nil:
		return invalidArgument("call_to_action.url is required unless action_type is CALL")
	case c.URL != nil:
		return localPostURL("call_to_action.url", *c.URL)
	}
	return nil
}

func localPostMaskOK(a localPostArguments) error {
	if len(a.UpdateMask) == 0 {
		return invalidArgument("update_mask is required: name the fields to change")
	}
	mask := slices.Clone(a.UpdateMask)
	slices.Sort(mask)
	for i, field := range mask {
		switch {
		case !slices.Contains(localPostFields, field):
			return invalidArgument("update_mask names a field that cannot be changed")
		case i > 0 && mask[i-1] == field:
			return invalidArgument("update_mask names a field twice")
		}
	}
	if !slices.Equal(mask, localPostGiven(a)) {
		return invalidArgument("update_mask must name exactly the fields that are given with a value: a field " +
			"in the mask without a value would be cleared, a value outside the mask would be ignored")
	}
	return nil
}

func localPostClock(d *localPostDateArg, v *localPostTimeArg) string {
	out := fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d", d.Year, d.Month, d.Day, v.Hours, v.Minutes, v.Seconds)
	if v.Nanos != 0 {
		out += fmt.Sprintf(".%09d", v.Nanos)
	}
	return out
}

// localPostConfirmation phrases what a confirmed create or update would publish, from the arguments alone, with
// the location, the type, every text whole, every URL (the structured ones and those inside the texts), and the
// media sources. The core neutralizes and bounds it; the handlers refuse a post whose text would be cut.
func localPostConfirmation(tool string, a localPostArguments) string {
	var b strings.Builder
	if tool == localPostsCreateID {
		b.WriteString("Publish a public " + a.TopicType + " post at " + a.Location)
	} else {
		b.WriteString("Change the public " + a.TopicType + " post " + a.LocalPost + ": set")
	}
	var texts []string
	if a.LanguageCode != nil {
		b.WriteString("; language " + *a.LanguageCode)
	}
	if a.Summary != nil {
		b.WriteString("; text \"" + *a.Summary + "\"")
		texts = append(texts, *a.Summary)
	}
	if c := a.CallToAction; c != nil {
		b.WriteString("; button " + c.ActionType)
		if c.URL != nil {
			b.WriteString(" to " + *c.URL)
		}
	}
	if e := a.Event; e != nil {
		b.WriteString("; event \"" + e.Title + "\" " + localPostClock(e.Schedule.StartDate, e.Schedule.StartTime) +
			" to " + localPostClock(e.Schedule.EndDate, e.Schedule.EndTime))
		texts = append(texts, e.Title)
	}
	if o := a.Offer; o != nil {
		if o.CouponCode != nil {
			b.WriteString("; code \"" + *o.CouponCode + "\"")
			texts = append(texts, *o.CouponCode)
		}
		if o.RedeemOnlineURL != nil {
			b.WriteString("; redeem at " + *o.RedeemOnlineURL)
		}
		if o.TermsConditions != nil {
			b.WriteString("; terms \"" + *o.TermsConditions + "\"")
			texts = append(texts, *o.TermsConditions)
		}
	}
	if a.AlertType != nil {
		b.WriteString("; alert " + *a.AlertType)
	}
	if len(a.Media) > 0 {
		sources := make([]string, 0, len(a.Media))
		for _, m := range a.Media {
			sources = append(sources, m.SourceURL)
		}
		b.WriteString("; media from " + strings.Join(sources, ", "))
	}
	if urls := urlPattern.FindAllString(strings.Join(texts, "\n"), -1); len(urls) > 0 {
		b.WriteString("; URLs in the texts: " + strings.Join(urls, ", "))
	}
	return b.String()
}

var localPostTooLong = "the confirmation of this post would exceed " + limitText(localPostsCreateID) +
	", so it could not show the whole text, every URL, and every media source; shorten the texts or send fewer links"

func localPostDeleteConfirmation(a localPostArguments) string {
	return "Delete the public local post " + a.LocalPost + ", which disappears from the profile and from Google Search"
}

// localPostSummary phrases a confirmation. A call that the handler refuses says so instead of asking for a
// confirmation of what will not happen.
func localPostSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var a localPostArguments
		if json.Unmarshal(raw, &a) != nil {
			return ""
		}
		if tool == localPostsDeleteID {
			return localPostDeleteConfirmation(a)
		}
		if err := checkLocalPost(a, tool == localPostsUpdateID); err != nil {
			return "Refused before sending: " + err.Error()
		}
		text := localPostConfirmation(tool, a)
		if !fitsConfirmation(tool, text) {
			return "Refused before sending: " + localPostTooLong
		}
		return text
	}
}

// localPostBody builds the post of a create or update from the given members only; topicType is always sent.
func localPostBody(op string, a localPostArguments) ([]byte, error) {
	body := map[string]any{"topicType": a.TopicType}
	if a.LanguageCode != nil {
		body["languageCode"] = *a.LanguageCode
	}
	if a.Summary != nil {
		body["summary"] = *a.Summary
	}
	if c := a.CallToAction; c != nil {
		action := map[string]any{"actionType": c.ActionType}
		if c.URL != nil {
			action["url"] = *c.URL
		}
		body["callToAction"] = action
	}
	if e := a.Event; e != nil {
		date := func(d *localPostDateArg) map[string]int {
			return map[string]int{"year": d.Year, "month": d.Month, "day": d.Day}
		}
		clock := func(v *localPostTimeArg) map[string]int {
			return map[string]int{"hours": v.Hours, "minutes": v.Minutes, "seconds": v.Seconds, "nanos": v.Nanos}
		}
		body["event"] = map[string]any{"title": e.Title, "schedule": map[string]any{
			"startDate": date(e.Schedule.StartDate), "startTime": clock(e.Schedule.StartTime),
			"endDate": date(e.Schedule.EndDate), "endTime": clock(e.Schedule.EndTime)}}
	}
	if o := a.Offer; o != nil {
		offer := map[string]string{}
		if o.CouponCode != nil {
			offer["couponCode"] = *o.CouponCode
		}
		if o.RedeemOnlineURL != nil {
			offer["redeemOnlineUrl"] = *o.RedeemOnlineURL
		}
		if o.TermsConditions != nil {
			offer["termsConditions"] = *o.TermsConditions
		}
		body["offer"] = offer
	}
	if a.AlertType != nil {
		body["alertType"] = *a.AlertType
	}
	if len(a.Media) > 0 {
		items := make([]map[string]string, 0, len(a.Media))
		for _, m := range a.Media {
			item := map[string]string{"sourceUrl": m.SourceURL}
			if m.MediaFormat != "" {
				item["mediaFormat"] = m.MediaFormat
			}
			items = append(items, item)
		}
		body["media"] = items
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	return raw, nil
}

const notFoundLocalPost = "Google did not find the local post or its location for this credential; check the path " +
	"and that the consenting account manages the location, which must be verified"

func handleLocalPostsList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list local posts"
	args, err := readLocalPostArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.legacyLocationOf("location", args.Location)
	if err != nil {
		return nil, err
	}
	binding := provider.CursorBinding(localPostsListID, name, bound.binding())
	query := pageSizeQuery(url.Values{}, args.PageSize)
	if query, err = pageQuery(binding, args.Cursor, query); err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, name); err != nil {
		return nil, err
	}
	var answer localPostsAnswer
	if err := c.get(ctx, op, localPostsListID, request{root: legacyRoot, prefix: legacyPrefix,
		path: name + "/localPosts", query: query, notFound: notFoundLocalPost}, &answer); err != nil {
		return nil, err
	}
	if len(answer.LocalPosts) > maxLocalPostsPage {
		return nil, tooMany(op, maxLocalPostsPage)
	}
	out := localPostsResult{LocalPosts: append([]localPost{}, answer.LocalPosts...)}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLocalPostsGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get local post"
	args, err := readLocalPostArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	location, err := bound.localPostOf(args.LocalPost)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, location); err != nil {
		return nil, err
	}
	var one localPost
	if err := c.get(ctx, op, localPostsGetID, request{root: legacyRoot, prefix: legacyPrefix, path: args.LocalPost,
		notFound: notFoundLocalPost}, &one); err != nil {
		return nil, err
	}
	return one, nil
}

func handleLocalPostsCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "create local post"
	args, err := readLocalPostArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	name, err := bound.legacyLocationOf("location", args.Location)
	if err != nil {
		return nil, err
	}
	if err := checkLocalPost(args, false); err != nil {
		return nil, err
	}
	if !fitsConfirmation(localPostsCreateID, localPostConfirmation(localPostsCreateID, args)) {
		return nil, invalidArgument(localPostTooLong)
	}
	body, err := localPostBody(op, args)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, name); err != nil {
		return nil, err
	}
	_, locationID := splitLegacyLocation(name)
	var out localPost
	if err := c.post(ctx, op, localPostsCreateID, change{root: legacyRoot, prefix: legacyPrefix,
		path: name + "/localPosts", body: body, key: locationID, notFound: notFoundLocalPost}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLocalPostsUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change local post"
	args, err := readLocalPostArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	location, err := bound.localPostOf(args.LocalPost)
	if err != nil {
		return nil, err
	}
	if err := checkLocalPost(args, true); err != nil {
		return nil, err
	}
	if !fitsConfirmation(localPostsUpdateID, localPostConfirmation(localPostsUpdateID, args)) {
		return nil, invalidArgument(localPostTooLong)
	}
	body, err := localPostBody(op, args)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, location); err != nil {
		return nil, err
	}
	mask := slices.Clone(args.UpdateMask)
	slices.Sort(mask)
	_, locationID := splitLegacyLocation(location)
	var out localPost
	if err := c.patch(ctx, op, localPostsUpdateID, change{root: legacyRoot, prefix: legacyPrefix, path: args.LocalPost,
		query: url.Values{"updateMask": {strings.Join(mask, ",")}}, body: body, key: locationID,
		notFound: notFoundLocalPost}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleLocalPostsDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete local post"
	args, err := readLocalPostArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	location, err := bound.localPostOf(args.LocalPost)
	if err != nil {
		return nil, err
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, location); err != nil {
		return nil, err
	}
	_, locationID := splitLegacyLocation(location)
	var ignored map[string]any
	if err := c.remove(ctx, op, localPostsDeleteID, change{root: legacyRoot, prefix: legacyPrefix,
		path: args.LocalPost, key: locationID, notFound: notFoundLocalPost}, &ignored); err != nil {
		return nil, err
	}
	return localPostDeleted{LocalPost: args.LocalPost, Deleted: true}, nil
}

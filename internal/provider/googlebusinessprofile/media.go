package googlebusinessprofile

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
	"github.com/castrowithcee/qatlas-connect/internal/config"
	"github.com/castrowithcee/qatlas-connect/internal/provider"
	"github.com/castrowithcee/qatlas-connect/internal/secret"
	"github.com/castrowithcee/qatlas-kit/redact"
)

// The media tools use the media methods of the My Business API v4 as documented at
// https://developers.google.com/my-business/reference/rest/v4/accounts.locations.media (the MediaItem
// resource and the methods list, get, create, patch, delete) and its method pages, read on 2026-11-18. v4 has
// no discovery document, so nothing here comes from one. startUpload is not offered: it needs a local file,
// and dataRef, the field that carries its result, is input only and never sent.
const (
	mediaListID   = Provider + ".media.list"
	mediaGetID    = Provider + ".media.get"
	mediaCreateID = Provider + ".media.create"
	mediaUpdateID = Provider + ".media.update"
	mediaDeleteID = Provider + ".media.delete"
)

// Bounds. media.list documents at most 2500 items per page, but every list of this provider is cut at 250
// entries, so the tool offers at most 250 per call instead of cutting a page silently.
const (
	maxMediaPage     = 250
	maxMediaSource   = 2000
	maxMediaDescRune = 500
	maxMediaPriceLen = 128

	formMedia = "accounts/ACCOUNT/locations/LOCATION/media/MEDIA"
	// The media key is documented only as part of the name; the tool accepts the characters of a URL-safe
	// token, which also covers the fixed key profile, so no segment can be a path step.
	mediaKeyPattern = `[A-Za-z0-9_-]{1,256}`
	mediaPattern    = `^accounts/` + idPattern + `/locations/` + idPattern + `/media/` + mediaKeyPattern + `$`
	maxMediaLen     = 350

	mediaMaskCategory = "locationAssociation.category"
)

var mediaForm = regexp.MustCompile(mediaPattern)

// mediaCategories are the categories Google documents for a media item, without the unspecified one and the
// deprecated alias LOGO. A change may not set COVER or PROFILE.
var (
	mediaCategories       = []string{"COVER", "PROFILE", "EXTERIOR", "INTERIOR", "PRODUCT", "AT_WORK", "FOOD_AND_DRINK", "MENU", "COMMON_AREA", "ROOMS", "TEAMS", "ADDITIONAL"}
	mediaChangeCategories = mediaCategories[2:]
)

var (
	mediaArgSchema = `{"type":"string","maxLength":` + strconv.Itoa(maxMediaLen) + `,"pattern":"` + mediaPattern +
		`","x-form":"` + formMedia + `"}`
	mediaPageArg      = `{"type":"integer","minimum":1,"maximum":` + strconv.Itoa(maxMediaPage) + `}`
	mediaFormatSchema = `{"type":"string","enum":["PHOTO","VIDEO"]}`
	mediaSourceSchema = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxMediaSource) + `}`
	mediaDescSchema   = `{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxMediaDescRune) + `}`
	mediaAssocSchema  = `{"type":"object","properties":{"category":{"type":"string","enum":` + mustJSON(mediaCategories) +
		`},"price_list_item_id":{"type":"string","minLength":1,"maxLength":` + strconv.Itoa(maxMediaPriceLen) +
		`}},"minProperties":1,"maxProperties":1,"additionalProperties":false}`
	mediaChangeAssocSchema = `{"type":"object","properties":{"category":{"type":"string","enum":` +
		mustJSON(mediaChangeCategories) + `}},"required":["category"],"additionalProperties":false}`
	mediaMaskSchema = `{"type":"array","minItems":1,"maxItems":1,"items":{"type":"string","enum":["` +
		mediaMaskCategory + `"]}}`
)

// The MediaItem resource as documented. Descriptions and the names of contributors are third-party content:
// texts are cut and links are text that is never fetched. sourceUrl and dataRef are input members, which
// Google does not return for a created item, so they are not answered.
type (
	mediaAssociation struct {
		Category        short `json:"category,omitempty"`
		PriceListItemID line  `json:"priceListItemId,omitempty"`
	}
	mediaDimensions struct {
		WidthPixels  integer `json:"widthPixels,omitzero"`
		HeightPixels integer `json:"heightPixels,omitzero"`
	}
	mediaInsights struct {
		ViewCount short `json:"viewCount,omitempty"`
	}
	mediaAttribution struct {
		ProfileName     line `json:"profileName,omitempty"`
		ProfilePhotoURL link `json:"profilePhotoUrl,omitempty"`
		TakedownURL     link `json:"takedownUrl,omitempty"`
		ProfileURL      link `json:"profileUrl,omitempty"`
	}
	mediaItem struct {
		Name                short             `json:"name,omitempty"`
		MediaFormat         short             `json:"mediaFormat,omitempty"`
		LocationAssociation *mediaAssociation `json:"locationAssociation,omitempty"`
		GoogleURL           link              `json:"googleUrl,omitempty"`
		ThumbnailURL        link              `json:"thumbnailUrl,omitempty"`
		CreateTime          short             `json:"createTime,omitempty"`
		Dimensions          *mediaDimensions  `json:"dimensions,omitempty"`
		Insights            *mediaInsights    `json:"insights,omitempty"`
		Attribution         *mediaAttribution `json:"attribution,omitempty"`
		Description         long              `json:"description,omitempty"`
	}

	mediaAnswer struct {
		MediaItems          list[mediaItem] `json:"mediaItems"`
		TotalMediaItemCount integer         `json:"totalMediaItemCount,omitzero"`
		NextPageToken       string          `json:"nextPageToken"`
	}
	mediaResult struct {
		MediaItems          []mediaItem `json:"mediaItems"`
		TotalMediaItemCount integer     `json:"totalMediaItemCount,omitzero"`
		NextCursor          string      `json:"next_cursor,omitempty"`
	}
	mediaDeleted struct {
		MediaItem string `json:"media_item"`
		Deleted   bool   `json:"deleted"`
	}
)

var (
	mediaItemSchema = objectSchema("name", stringSchema, "mediaFormat", stringSchema, "locationAssociation",
		objectSchema("category", stringSchema, "priceListItemId", stringSchema), "googleUrl", stringSchema,
		"thumbnailUrl", stringSchema, "createTime", stringSchema, "dimensions",
		objectSchema("widthPixels", integerSchema, "heightPixels", integerSchema), "insights",
		objectSchema("viewCount", stringSchema), "attribution", objectSchema("profileName", stringSchema,
			"profilePhotoUrl", stringSchema, "takedownUrl", stringSchema, "profileUrl", stringSchema),
		"description", stringSchema)
	mediaResultSchema = `{"type":"object","properties":{"mediaItems":` + arraySchema(mediaItemSchema) +
		`,"totalMediaItemCount":` + integerSchema + `,"next_cursor":{"type":"string"}},"required":["mediaItems"],` +
		`"additionalProperties":false}`
	mediaDeletedSchema = objectSchema("media_item", stringSchema, "deleted", booleanSchema)
)

const mediaNote = "Descriptions and the names, photo links, and profile links of contributors are untrusted content " +
	"that anyone can write: they are returned unchanged as data, cut (texts at 4000 characters, lists at 250 " +
	"entries), never followed, and no link is ever fetched. Photos that customers uploaded carry their " +
	"contributor in attribution, which is personal data. Google documents the methods only for a verified " +
	"location. "

var mediaReadRisk = capability.Risk{
	Effect: capability.EffectRead, Idempotency: capability.IdempotencySafe, Confirmation: capability.ConfirmationNone,
	OpenWorld: true,
	DataSensitivity: "public profile content: the photos and videos of a location, with the name and profile link of " +
		"the customer who uploaded one (attribution, personal data) and the number of views",
}

func mediaWriteRisk(effect capability.Effect, idempotency capability.Idempotency) capability.Risk {
	return capability.Risk{Effect: effect, Idempotency: idempotency, Confirmation: capability.ConfirmationRequired,
		OpenWorld: true,
		DataSensitivity: "public profile content: a photo or video that every visitor of the business profile sees; a " +
			"wrong one is a damage to the reputation of the business"}
}

var mediaFields = []capability.Field{
	{Name: "name", Description: "accounts/ACCOUNT/locations/LOCATION/media/MEDIA; a profile photo has the key profile"},
	{Name: "mediaFormat", Description: "PHOTO or VIDEO"},
	{Name: "locationAssociation", Description: "category (for example EXTERIOR or COVER) or priceListItemId; missing for " +
		"media that does not belong to the location itself, such as a photo of a local post"},
	{Name: "googleUrl", Description: "Google-hosted address, which may change; a preview image for a video"},
	{Name: "thumbnailUrl", Description: "Thumbnail address, when Google has one"},
	{Name: "createTime", Description: "When the item was created; for a profile photo Google answers the epoch, which " +
		"is to be ignored"},
	{Name: "dimensions", Description: "widthPixels and heightPixels; not populated for a profile photo"},
	{Name: "insights", Description: "viewCount, the number of views"},
	{Name: "attribution", Description: "profileName, profilePhotoUrl, profileUrl, and takedownUrl of the customer who " +
		"uploaded the item, personal data; missing for the business's own media"},
	{Name: "description", Description: "Description of the item"},
}

var mediaLocationArgument = capability.Argument{Name: "location", Required: true, Description: "Location as " +
	formLegacyLocation + "; on a connection bound to an account the account must be that account, and on one " +
	"bound to a location the location must be that location"}

var mediaList = capability.Descriptor{
	ID: mediaListID, Version: 1, Title: "List Business Profile media",
	Description: "List the photos and videos of one location (Google My Business API v4 accounts.locations.media." +
		"list), up to 250 per call with a cursor to read on, with the total number of items of the location. " +
		mediaNote + "On a connection bound to an account the location is first proved to belong to it. " +
		untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "media"}, Risk: mediaReadRisk, Provider: Provider,
	InputSchema: inputSchema([]string{"location"}, "location", legacyLocationArgSchema, "page_size", mediaPageArg,
		"cursor", cursorSchema),
	OutputSchema: json.RawMessage(mediaResultSchema),
	Arguments: []capability.Argument{mediaLocationArgument,
		{Name: "page_size", Description: "Items per call, 1 to 250; Google's default is 100"}, cursorArgument},
	Fields: []capability.Field{{Name: "mediaItems", Description: "Items of the page"},
		{Name: "totalMediaItemCount", Description: "Number of all media items of the location"}, nextCursorField},
	Examples: []capability.Example{{Description: "First page", Arguments: json.RawMessage(
		`{"location":"accounts/1000001/locations/2000002"}`)}},
}

var mediaGet = capability.Descriptor{
	ID: mediaGetID, Version: 1, Title: "Get a Business Profile media item",
	Description: "Read one photo or video of a location (Google My Business API v4 accounts.locations.media.get). " +
		mediaNote + "On a connection bound to an account the location is first proved to belong to it. " +
		untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "media"}, Risk: mediaReadRisk, Provider: Provider,
	InputSchema:  inputSchema([]string{"media_item"}, "media_item", mediaArgSchema),
	OutputSchema: json.RawMessage(mediaItemSchema),
	Arguments: []capability.Argument{{Name: "media_item", Required: true, Description: "Media item as listed: " +
		formMedia}},
	Fields: mediaFields,
	Examples: []capability.Example{{Description: "One item", Arguments: json.RawMessage(
		`{"media_item":"accounts/1000001/locations/2000002/media/AbCd_123"}`)}},
}

var mediaCreate = capability.Descriptor{
	ID: mediaCreateID, Version: 1, Title: "Add a Business Profile photo or video by URL",
	Description: "Add a photo or video to one location from a public https address (Google My Business API v4 " +
		"accounts.locations.media.create). Google fetches the address itself, Qatlas never does, and no tool uploads a " +
		"file or reads a local one. The media is public on the profile at once. Photos other than PROFILE and COVER " +
		"need at least 250 pixels on the short edge and 10240 bytes. A COVER item takes no description. The " +
		"confirmation shows the location, the format, the category or price list item, the whole address, and the " +
		"description; a call whose confirmation would not fit into 500 characters is refused before anything is " +
		"sent, so use a shorter address or description. Google counts at most 10 changes per minute and profile. " +
		untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "media", "create"}, Provider: Provider,
	Risk: mediaWriteRisk(capability.EffectCreate, capability.IdempotencyNonIdempotent),
	InputSchema: inputSchema([]string{"location", "media_format", "source_url", "location_association"},
		"location", legacyLocationArgSchema, "media_format", mediaFormatSchema, "source_url", mediaSourceSchema,
		"location_association", mediaAssocSchema, "description", mediaDescSchema),
	OutputSchema: json.RawMessage(mediaItemSchema),
	Arguments: []capability.Argument{mediaLocationArgument,
		{Name: "media_format", Required: true, Description: "PHOTO or VIDEO; Google documents only photos for this version"},
		{Name: "source_url", Required: true, Description: "Public https address Google fetches the media from, ASCII " +
			"without user information, at most 2000 bytes"},
		{Name: "location_association", Required: true, Description: "Exactly one of category (COVER, PROFILE, EXTERIOR, " +
			"INTERIOR, PRODUCT, AT_WORK, FOOD_AND_DRINK, MENU, COMMON_AREA, ROOMS, TEAMS, ADDITIONAL) or " +
			"price_list_item_id (the ID of a price list item of the location)"},
		{Name: "description", Description: "Public description, plain text, at most 500 characters; not with COVER"}},
	Fields: mediaFields,
	Examples: []capability.Example{{Description: "Add an exterior photo", Arguments: json.RawMessage(
		`{"location":"accounts/1000001/locations/2000002","media_format":"PHOTO",` +
			`"source_url":"https://media.example.invalid/front.jpg","location_association":{"category":"EXTERIOR"}}`)}},
}

var mediaUpdate = capability.Descriptor{
	ID: mediaUpdateID, Version: 1, Title: "Change the category of a Business Profile media item",
	Description: "Change the category of one photo or video (Google My Business API v4 accounts.locations.media." +
		"patch). Google lets a change set only the category, and not to COVER or PROFILE; mediaFormat, source, and " +
		"description cannot be changed, so a different image is a new item. update_mask is required and names " +
		"exactly locationAssociation.category, because Google treats a missing mask as a full update. The " +
		"confirmation shows the item and the new category. Google counts at most 10 changes per minute and " +
		"profile. " + mediaNote + untrustedReviewsHint,
	Tags: []string{"google", "business-profile", "media", "update"}, Provider: Provider,
	Risk: mediaWriteRisk(capability.EffectUpdate, capability.IdempotencyIdempotent),
	InputSchema: inputSchema([]string{"media_item", "location_association", "update_mask"},
		"media_item", mediaArgSchema, "location_association", mediaChangeAssocSchema, "update_mask", mediaMaskSchema),
	OutputSchema: json.RawMessage(mediaItemSchema),
	Arguments: []capability.Argument{
		{Name: "media_item", Required: true, Description: "Media item as listed: " + formMedia},
		{Name: "location_association", Required: true, Description: "Object with the new category: EXTERIOR, INTERIOR, " +
			"PRODUCT, AT_WORK, FOOD_AND_DRINK, MENU, COMMON_AREA, ROOMS, TEAMS, or ADDITIONAL"},
		{Name: "update_mask", Required: true, Description: "List with exactly locationAssociation.category"}},
	Fields: mediaFields,
	Examples: []capability.Example{{Description: "Move an item to the interior photos", Arguments: json.RawMessage(
		`{"media_item":"accounts/1000001/locations/2000002/media/AbCd_123",` +
			`"location_association":{"category":"INTERIOR"},"update_mask":["locationAssociation.category"]}`)}},
}

var mediaDelete = capability.Descriptor{
	ID: mediaDeleteID, Version: 1, Title: "Delete a Business Profile media item",
	Description: "Delete one photo or video of a location (Google My Business API v4 accounts.locations.media." +
		"delete). The item disappears from the public profile, and Qatlas keeps no copy. Google counts at most 10 " +
		"changes per minute and profile. On a connection bound to an account the location is first proved to " +
		"belong to it.",
	Tags: []string{"google", "business-profile", "media", "delete"}, Provider: Provider,
	Risk:         mediaWriteRisk(capability.EffectDelete, capability.IdempotencyIdempotent),
	InputSchema:  inputSchema([]string{"media_item"}, "media_item", mediaArgSchema),
	OutputSchema: json.RawMessage(mediaDeletedSchema),
	Arguments: []capability.Argument{{Name: "media_item", Required: true, Description: "Media item to delete: " +
		formMedia}},
	Fields: []capability.Field{{Name: "media_item", Description: "The item that was deleted"},
		{Name: "deleted", Description: "true once Google confirmed the deletion"}},
	Examples: []capability.Example{{Description: "Remove an item", Arguments: json.RawMessage(
		`{"media_item":"accounts/1000001/locations/2000002/media/AbCd_123"}`)}},
}

type mediaAssociationArgument struct {
	Category        string `json:"category"`
	PriceListItemID string `json:"price_list_item_id"`
}

type mediaArguments struct {
	Location            string                    `json:"location"`
	MediaItem           string                    `json:"media_item"`
	PageSize            int                       `json:"page_size"`
	Cursor              string                    `json:"cursor"`
	MediaFormat         string                    `json:"media_format"`
	SourceURL           string                    `json:"source_url"`
	Description         string                    `json:"description"`
	LocationAssociation *mediaAssociationArgument `json:"location_association"`
	UpdateMask          []string                  `json:"update_mask"`
}

func readMediaArguments(op string, raw json.RawMessage) (mediaArguments, error) {
	var args mediaArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, provider.Fail(op, "the validated arguments could not be read")
	}
	if args.PageSize < 0 || args.PageSize > maxMediaPage {
		return args, invalidArgument("page_size must be 1 to 250")
	}
	return args, nil
}

// mediaOf checks the path of a media item against its form and the target and returns its v4 location path.
func (s scope) mediaOf(given string) (string, error) {
	if len(given) > maxMediaLen || !mediaForm.MatchString(given) {
		return "", invalidArgument("media_item must be " + formMedia + " with decimal IDs")
	}
	location, _, _ := strings.Cut(given, "/media/")
	return location, s.legacyInTarget("media_item", location)
}

const notFoundMedia = "Google did not find the media item or its location for this credential; check the path and " +
	"that the consenting account manages the location, which must be verified"

// mediaCheckCreate refuses before any I/O what Google documents as invalid or what the confirmation could not
// show whole.
func mediaCheckCreate(args mediaArguments) error {
	if _, err := mediaSourceURL(args.SourceURL); err != nil {
		return err
	}
	assoc := args.LocationAssociation
	switch {
	case args.MediaFormat != "PHOTO" && args.MediaFormat != "VIDEO":
		return invalidArgument("media_format must be PHOTO or VIDEO")
	case assoc == nil || (assoc.Category == "") == (assoc.PriceListItemID == ""):
		return invalidArgument("location_association needs exactly one of category and price_list_item_id")
	case assoc.Category != "" && !slices.Contains(mediaCategories, assoc.Category):
		return invalidArgument("location_association.category is not a category of a media item")
	case assoc.Category == "COVER" && args.Description != "":
		return invalidArgument("description is not accepted for a COVER item")
	}
	if assoc.PriceListItemID != "" {
		for _, r := range assoc.PriceListItemID {
			if r > unicode.MaxASCII || !unicode.IsPrint(r) || r == ' ' {
				return invalidArgument("location_association.price_list_item_id must be printable ASCII without spaces")
			}
		}
	}
	if args.Description != "" {
		if err := mediaCheckDescription(args.Description); err != nil {
			return err
		}
	}
	if confirmationRunes(mediaCreateSummary(args)) > maxConfirmRunes {
		return invalidArgument(mediaTooLong)
	}
	return nil
}

const mediaTooLong = "the confirmation of this change would exceed 500 characters, so it could not show the whole " +
	"address and description; use a shorter address or description"

// mediaSourceURL accepts only a plain https address. The address is never fetched, so the checks only keep it
// from carrying anything a person could misread in the confirmation.
func mediaSourceURL(given string) (string, error) {
	if given == "" || len(given) > maxMediaSource {
		return "", invalidArgument("source_url must be 1 to 2000 bytes")
	}
	if err := checkLinkURI(given); err != nil {
		return "", invalidArgument("source_url " + strings.TrimPrefix(err.Error(), "uri "))
	}
	return given, nil
}

func mediaCheckDescription(text string) error {
	switch {
	case !utf8.ValidString(text):
		return invalidArgument("description must be valid UTF-8")
	case strings.TrimSpace(text) == "":
		return invalidArgument("description must not be empty")
	case utf8.RuneCountInString(text) > maxMediaDescRune:
		return invalidArgument("description must be at most 500 characters")
	}
	for _, r := range text {
		if unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp) {
			return invalidArgument("description must not contain control, invisible formatting, or separator characters")
		}
	}
	return nil
}

// mediaCheckUpdate refuses before any I/O a change Google does not document, or a mask that does not name
// exactly what is sent.
func mediaCheckUpdate(args mediaArguments) error {
	switch {
	case len(args.UpdateMask) != 1 || args.UpdateMask[0] != mediaMaskCategory:
		return invalidArgument("update_mask is required and must be exactly [\"" + mediaMaskCategory + "\"]")
	case args.LocationAssociation == nil || args.LocationAssociation.PriceListItemID != "":
		return invalidArgument("location_association must hold only category: Google lets a change set only the category")
	case !slices.Contains(mediaChangeCategories, args.LocationAssociation.Category):
		return invalidArgument("category must not be empty, COVER, or PROFILE: Google refuses to change to them")
	}
	return nil
}

func mediaCreateSummary(a mediaArguments) string {
	out := "Add a public " + a.MediaFormat + " to the profile at " + a.Location + ", fetched by Google from " + a.SourceURL
	if assoc := a.LocationAssociation; assoc != nil {
		if assoc.Category != "" {
			out += "; category " + assoc.Category
		} else {
			out += "; price list item " + assoc.PriceListItemID
		}
	}
	if a.Description != "" {
		out += "; description \"" + a.Description + "\""
	}
	return out
}

func mediaUpdateSummary(a mediaArguments) string {
	category := ""
	if a.LocationAssociation != nil {
		category = a.LocationAssociation.Category
	}
	return "Change the category of the public media item " + a.MediaItem + " to " + category
}

// mediaSummary phrases a confirmation from the arguments alone. A call that the handler refuses says so
// instead of asking for a confirmation of what will not happen.
func mediaSummary(tool string) capability.SummaryFunc {
	return func(raw json.RawMessage) string {
		var args mediaArguments
		if json.Unmarshal(raw, &args) != nil {
			return ""
		}
		switch tool {
		case mediaCreateID:
			if err := mediaCheckCreate(args); err != nil {
				return "Refused before sending: " + err.Error()
			}
			return mediaCreateSummary(args)
		case mediaUpdateID:
			if err := mediaCheckUpdate(args); err != nil {
				return "Refused before sending: " + err.Error()
			}
			if confirmationRunes(mediaUpdateSummary(args)) > maxConfirmRunes {
				return "Refused before sending: " + mediaTooLong
			}
			return mediaUpdateSummary(args)
		}
		return "Delete the public media item " + args.MediaItem + " from the profile; it disappears for every visitor"
	}
}

func handleMediaList(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "list media"
	args, err := readMediaArguments(op, raw)
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
	binding := provider.CursorBinding(mediaListID, name, bound.binding())
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
	var answer mediaAnswer
	if err := c.get(ctx, op, mediaListID, request{root: legacyRoot, prefix: legacyPrefix, path: name + "/media",
		query: query, notFound: notFoundMedia}, &answer); err != nil {
		return nil, err
	}
	if len(answer.MediaItems) > maxMediaPage {
		return nil, tooMany(op, maxMediaPage)
	}
	out := mediaResult{MediaItems: append([]mediaItem{}, answer.MediaItems...),
		TotalMediaItemCount: answer.TotalMediaItemCount}
	if out.NextCursor, err = nextCursor(op, binding, answer.NextPageToken); err != nil {
		return nil, err
	}
	return out, nil
}

func handleMediaGet(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "get media item"
	args, err := readMediaArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	location, err := bound.mediaOf(args.MediaItem)
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
	var one mediaItem
	if err := c.get(ctx, op, mediaGetID, request{root: legacyRoot, prefix: legacyPrefix, path: args.MediaItem,
		notFound: notFoundMedia}, &one); err != nil {
		return nil, err
	}
	return one, nil
}

func handleMediaCreate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "add media item"
	args, err := readMediaArguments(op, raw)
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
	if err := mediaCheckCreate(args); err != nil {
		return nil, err
	}
	association := map[string]string{}
	if args.LocationAssociation.Category != "" {
		association["category"] = args.LocationAssociation.Category
	} else {
		association["priceListItemId"] = args.LocationAssociation.PriceListItemID
	}
	body := map[string]any{"mediaFormat": args.MediaFormat, "sourceUrl": args.SourceURL,
		"locationAssociation": association}
	if args.Description != "" {
		body["description"] = args.Description
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, name); err != nil {
		return nil, err
	}
	_, locationID := splitLegacyLocation(name)
	var out mediaItem
	if err := c.post(ctx, op, mediaCreateID, change{root: legacyRoot, prefix: legacyPrefix, path: name + "/media",
		body: encoded, key: locationID, notFound: notFoundMedia}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleMediaUpdate(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "change media item"
	args, err := readMediaArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	location, err := bound.mediaOf(args.MediaItem)
	if err != nil {
		return nil, err
	}
	if err := mediaCheckUpdate(args); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(map[string]any{"locationAssociation": map[string]string{
		"category": args.LocationAssociation.Category}})
	if err != nil {
		return nil, provider.Fail(op, "the request body could not be built")
	}
	c, err := open(ctx, resolved, secrets, red)
	if err != nil {
		return nil, err
	}
	if err := c.proveOne(ctx, op, bound, location); err != nil {
		return nil, err
	}
	_, locationID := splitLegacyLocation(location)
	var out mediaItem
	if err := c.patch(ctx, op, mediaUpdateID, change{root: legacyRoot, prefix: legacyPrefix, path: args.MediaItem,
		query: url.Values{"updateMask": {mediaMaskCategory}}, body: encoded, key: locationID,
		notFound: notFoundMedia}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func handleMediaDelete(ctx context.Context, resolved *config.Resolved, secrets *secret.Resolver,
	red *redact.Redactor, raw json.RawMessage) (any, error) {
	const op = "delete media item"
	args, err := readMediaArguments(op, raw)
	if err != nil {
		return nil, err
	}
	bound, err := checkConnection(resolved)
	if err != nil {
		return nil, err
	}
	location, err := bound.mediaOf(args.MediaItem)
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
	if err := c.remove(ctx, op, mediaDeleteID, change{root: legacyRoot, prefix: legacyPrefix, path: args.MediaItem,
		key: locationID, notFound: notFoundMedia}, &ignored); err != nil {
		return nil, err
	}
	return mediaDeleted{MediaItem: args.MediaItem, Deleted: true}, nil
}

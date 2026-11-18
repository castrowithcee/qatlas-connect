package googlebusinessprofile

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/castrowithcee/qatlas-connect/internal/capability"
)

// The confirmation of a tool that publishes public text shows that text whole, so the tool declares a bound
// that holds Google's longest value with its context. The core cuts nothing a tool stays within, and a call
// whose confirmation would exceed the bound is refused instead of shown cut.
const (
	// defaultConfirmRunes is the bound of a tool that declares none.
	defaultConfirmRunes = 500

	// A reply is at most 4096 bytes. Every line break is shown as two characters and every address is
	// repeated once, so the worst case is about 8192 characters plus the context.
	replyConfirmRunes = 9216
	// A post text of 1500 characters, every address in it repeated, an address of the button, and the media.
	localPostConfirmRunes = 6144
	// The title and the field names of a create.
	locationCreateConfirmRunes = 2048
	// A description of 4000 characters, as old and as new value, with every quote escaped.
	locationUpdateConfirmRunes = capability.MaxConfirmationSummaryRunes
	// An address of 2048 characters as old and as new value, with the other attributes of the call.
	attributesConfirmRunes = 8192
	lodgingConfirmRunes    = 4096
	// An address of 2000 bytes and a description of 500 characters.
	mediaCreateConfirmRunes = 3072
	// An address of 2000 bytes with the link name.
	placeActionConfirmRunes = 3072
)

// confirmLimits holds the declared bound of each tool. It is the one place both the registration and the
// check of a call read, so they cannot differ. A tool that is not listed keeps the default.
var confirmLimits = map[string]int{
	reviewsUpdateReplyID:     replyConfirmRunes,
	localPostsCreateID:       localPostConfirmRunes,
	localPostsUpdateID:       localPostConfirmRunes,
	locationsCreateID:        locationCreateConfirmRunes,
	locationsUpdateID:        locationUpdateConfirmRunes,
	attributesUpdateID:       attributesConfirmRunes,
	lodgingUpdateID:          lodgingConfirmRunes,
	mediaCreateID:            mediaCreateConfirmRunes,
	placeActionLinksCreateID: placeActionConfirmRunes,
	placeActionLinksUpdateID: placeActionConfirmRunes,
}

// confirmLimit is the bound of the confirmation of a tool, in characters.
func confirmLimit(id string) int {
	if n := confirmLimits[id]; n > 0 {
		return n
	}
	return defaultConfirmRunes
}

// limited declares the bound of the tool on its registration.
func limited(op capability.Operation) capability.Operation {
	op.ConfirmationSummaryRunes = confirmLimits[op.Descriptor.ID]
	return op
}

// fitsConfirmation reports whether the core would show the text of a tool whole.
func fitsConfirmation(id, text string) bool {
	return confirmationRunes(text) <= confirmLimit(id)
}

// limitText names the bound of a tool for a description or a refusal.
func limitText(id string) string {
	return strconv.Itoa(confirmLimit(id)) + " characters"
}

// confirmationRunes counts the characters the core keeps of a text. The core writes a line break, return, or
// tab as two characters and any other control, format, or separator character as an escape of six, or ten
// above the basic plane, and replaces invalid UTF-8 by one character.
func confirmationRunes(text string) int {
	n := 0
	for _, r := range strings.ToValidUTF8(text, "�") {
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			n += 2
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			if r > 0xFFFF {
				n += 10
			} else {
				n += 6
			}
		default:
			n++
		}
	}
	return n
}

package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/colorprofile"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// Lip Gloss renders every colour at full depth: the profile picks the palette, and shown steps whatever else a
// frame holds down to it.

// profile is what Run found the terminal to show; tests set it.
var profile = kittui.ProfileNone

// noColour reports whether the current profile shows no colour.
func noColour() bool { return currentProfile().Colorless() }

func currentProfile() kittui.ColorProfile { return profile }

func setProfile(p kittui.ColorProfile) { profile = p }

// terminalCharset is the character set the terminal draws, read from the environment on every call, so a
// change of the environment shows at once.
func terminalCharset() kittui.CharsetMode { return kittui.DetectCharsetMode(os.Environ()) }

// toColorprofile is the profile of the colour library that Bubble Tea and shown step a frame down with. Mono
// keeps bold, faint and reverse; None drops them as well, as a terminal that shows nothing does.
func toColorprofile(p kittui.ColorProfile) colorprofile.Profile {
	switch p {
	case kittui.ProfileTrueColor:
		return colorprofile.TrueColor
	case kittui.ProfileANSI256:
		return colorprofile.ANSI256
	case kittui.ProfileANSI16:
		return colorprofile.ANSI
	case kittui.ProfileMono:
		return colorprofile.ASCII
	}
	return colorprofile.NoTTY
}

// shown is a frame as the terminal of the current profile is sent it, so what render returns is what the
// terminal shows, symbols outside ASCII included where the terminal draws no UTF-8. Bubble Tea steps the
// frame down to the same profile, which leaves it unchanged.
func shown(frame string) string {
	frame = asciiOnly(strings.ReplaceAll(frame, cursorMark, ""))
	if profile == kittui.ProfileTrueColor {
		return frame
	}
	var b strings.Builder
	w := colorprofile.Writer{Forward: &b, Profile: toColorprofile(profile)}
	_, _ = w.WriteString(frame)
	return b.String()
}

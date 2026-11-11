package tui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	kittui "github.com/castrowithcee/qatlas-kit/tui"
)

// lip is the only place that turns a palette colour into a Lip Gloss colour.
func lip(c kittui.Color) color.Color { return lipgloss.Color(string(c)) }

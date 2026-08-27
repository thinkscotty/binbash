package handlers

import "strings"

// ColorTag is one selectable color in the item form's swatch row.
//
// Slug is what gets stored in the database and written to CSV backups; Label
// is what a human reads. The rendered color itself lives in style.css as a
// .swatch-<slug> / .dot-<slug> rule pair, which means the palette can be
// retuned later without migrating a single stored row -- and, just as
// importantly, satisfies binbash's strict CSP, which blocks the inline style
// attribute an on-the-fly hex would need.
type ColorTag struct {
	Slug  string
	Label string
}

// ColorTagConfig is the color-tag feature's runtime configuration, mirroring
// the config file's [color_tags] section.
type ColorTagConfig struct {
	Enabled bool
	Label   string
}

// colorTags is the fixed vocabulary offered in the picker, in the order the
// swatches appear. Deliberately short and fixed: these are the colors cheap
// storage bags actually come in, and a closed set keeps the stored value
// validatable and the CSS finite.
var colorTags = []ColorTag{
	{Slug: "white", Label: "White"},
	{Slug: "black", Label: "Black"},
	{Slug: "red", Label: "Red"},
	{Slug: "orange", Label: "Orange"},
	{Slug: "yellow", Label: "Yellow"},
	{Slug: "green", Label: "Green"},
	{Slug: "lightblue", Label: "Light blue"},
	{Slug: "darkblue", Label: "Dark blue"},
	{Slug: "purple", Label: "Purple"},
	{Slug: "pink", Label: "Pink"},
}

// normalizeColor returns s as a known color slug, or "" for anything it
// doesn't recognize -- including the empty string, which is how "no color" is
// stored.
//
// Unknown input degrades to "no color" rather than raising an error. The
// picker can only ever submit a value from the list above, so an unrecognized
// slug means a hand-crafted POST or a hand-edited CSV; treating it as absent
// costs nothing, can't corrupt anything, and keeps an error path off the fast
// item-entry form.
func normalizeColor(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, c := range colorTags {
		if c.Slug == s {
			return c.Slug
		}
	}
	return ""
}

// ColorLabel returns the human-readable name for a color slug ("lightblue"
// becomes "Light blue"), or "" if the slug is empty or unknown. It backs
// Item.ColorName, which templates use for the color dot's screen-reader text.
func ColorLabel(slug string) string {
	for _, c := range colorTags {
		if c.Slug == slug {
			return c.Label
		}
	}
	return ""
}

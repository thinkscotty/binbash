package handlers

import "testing"

func TestTitleCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"all lowercase", "winter coats", "Winter Coats"},
		{"single word", "hammer", "Hammer"},
		{"already title case", "Winter Coats", "Winter Coats"},
		{"empty", "", ""},

		// The escape hatch: any word carrying an uppercase letter is left as typed.
		{"camel case brand", "iPhone charger", "iPhone Charger"},
		{"acronym", "USB c cables", "USB C Cables"},
		{"all caps", "LEGO bricks", "LEGO Bricks"},
		{"mid-name acronym", "spare HDMI cable", "Spare HDMI Cable"},
		{"internal capital", "McCoy mugs", "McCoy Mugs"},

		// Minor words stay lowercase in the middle, but not at either end.
		{"minor word mid-name", "box of nails", "Box of Nails"},
		{"minor word first", "the good china", "The Good China"},
		{"minor word last", "things to sort by", "Things to Sort By"},
		{"multiple minor words", "nuts and bolts in a jar", "Nuts and Bolts in a Jar"},

		// Segment and punctuation handling.
		{"hyphenated", "t-shirt pile", "T-Shirt Pile"},
		{"hyphenated brand", "wi-fi router", "Wi-Fi Router"},
		{"slash separated", "on/off switches", "On/Off Switches"},
		{"leading punctuation", "(spare) parts", "(Spare) Parts"},
		{"letter after digit", "2x4 offcuts", "2x4 Offcuts"},
		{"apostrophe", "kid's toys", "Kid's Toys"},

		// Whitespace is preserved exactly as typed.
		{"double space", "winter  coats", "Winter  Coats"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := titleCase(tt.in); got != tt.want {
				t.Errorf("titleCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestTitleCaseIdempotent guards the property that matters for stored data:
// re-saving an item that was already normalized must not drift its name. Every
// output of titleCase is a valid input that maps to itself.
func TestTitleCaseIdempotent(t *testing.T) {
	inputs := []string{
		"winter coats", "iPhone charger", "box of nails", "t-shirt pile",
		"(spare) parts", "2x4 offcuts", "USB c cables", "things to sort by",
	}

	for _, in := range inputs {
		once := titleCase(in)
		if twice := titleCase(once); twice != once {
			t.Errorf("titleCase not idempotent for %q: %q -> %q", in, once, twice)
		}
	}
}

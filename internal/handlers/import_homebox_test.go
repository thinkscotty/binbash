package handlers

import "testing"

func TestDeriveBinName(t *testing.T) {
	tests := []struct {
		name     string
		location string
		want     string
	}{
		{"leaf bin", "Garage / Bin 01 - Dogs", "Bin 01 - Dogs"},
		{"single nesting", "Upstairs / Electronics Bin", "Electronics Bin"},
		{
			"deepest bin wins over parent bin",
			"Upstairs / Electronics Bin / Passive Electronics Mini Bin",
			"Passive Electronics Mini Bin",
		},
		{"bare location with no bin is skipped", "Garage", ""},
		{"bin word not at leaf", "Garage / Bin 05 / Left Tray", "Bin 05"},
		{"bin anywhere in leaf segment", "Garage / Small Bin - Scotty's Miscellaneous", "Small Bin - Scotty's Miscellaneous"},
		{"case insensitive", "garage / storage BIN", "storage BIN"},
		{"plural bins", "Garage / Storage Bins", "Storage Bins"},
		{"substring is not a match (cabinet)", "Kitchen / Cabinet", ""},
		{"substring is not a match (binder)", "Office / Binder Shelf", ""},
		{"whitespace is trimmed", "  Garage  /  Bin 7  ", "Bin 7"},
		{"empty location", "", ""},
		{"top-level bin", "Bin 20", "Bin 20"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deriveBinName(tt.location); got != tt.want {
				t.Errorf("deriveBinName(%q) = %q, want %q", tt.location, got, tt.want)
			}
		})
	}
}

package handlers

import "testing"

// TestExistingBinNameIgnoresCase pins the guarantee that bin names are unique
// regardless of case. It used to be implied by title-casing every new bin name;
// now that edits deliberately skip normalization, the check has to hold it up
// on its own, or a bin could be renamed into a case-variant of another and make
// CSV import's find-or-create-by-name ambiguous.
func TestExistingBinNameIgnoresCase(t *testing.T) {
	database := newTestDB(t)
	h := &Handlers{DB: database}

	if _, err := database.Exec(`INSERT INTO bins (id, name) VALUES (1, 'Garage Shelf A')`); err != nil {
		t.Fatalf("seed bin: %v", err)
	}

	tests := []struct {
		name      string
		candidate string
		excludeID int64
		want      string
	}{
		{"exact match", "Garage Shelf A", 0, "Garage Shelf A"},
		{"all lowercase", "garage shelf a", 0, "Garage Shelf A"},
		{"all uppercase", "GARAGE SHELF A", 0, "Garage Shelf A"},
		{"mixed case", "gArAgE sHeLf A", 0, "Garage Shelf A"},
		{"different name is free", "Winter Storage", 0, ""},
		// A bin never collides with itself, so re-saving it — or recasing it,
		// which is the whole point of skipping normalization on edit — is allowed.
		{"bin excludes itself", "garage shelf a", 1, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.existingBinName(tt.candidate, tt.excludeID)
			if err != nil {
				t.Fatalf("existingBinName(%q, %d): %v", tt.candidate, tt.excludeID, err)
			}
			if got != tt.want {
				t.Errorf("existingBinName(%q, %d) = %q, want %q", tt.candidate, tt.excludeID, got, tt.want)
			}
		})
	}
}

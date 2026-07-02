package handlers

import (
	"fmt"
	"unicode/utf8"
)

// Field length limits keep oversized input from bloating the database (which
// doubles as the backup artifact) or breaking page layout. Lengths are counted
// in runes, so a multi-byte character such as an emoji or an accented letter
// counts as a single character rather than by its byte length.
const (
	maxNameLen        = 200
	maxCategoryLen    = 100
	maxDescriptionLen = 2000
	maxKeywordsLen    = 500
	maxSearchLen      = 200
	minPasswordLen    = 8
	maxPasswordLen    = 72 // bcrypt's hard limit, in bytes
)

// tooLong reports whether s exceeds max runes.
func tooLong(s string, max int) bool {
	return utf8.RuneCountInString(s) > max
}

// truncateRunes returns s clipped to at most max runes.
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// validateBin returns a human-friendly error message for invalid bin input, or
// "" if the input is acceptable. Callers are expected to pass already-trimmed
// values.
func validateBin(name, category, description string) string {
	switch {
	case name == "":
		return "Bin name is required"
	case tooLong(name, maxNameLen):
		return fmt.Sprintf("Bin name is too long (max %d characters)", maxNameLen)
	case tooLong(category, maxCategoryLen):
		return fmt.Sprintf("Category is too long (max %d characters)", maxCategoryLen)
	case tooLong(description, maxDescriptionLen):
		return fmt.Sprintf("Description is too long (max %d characters)", maxDescriptionLen)
	}
	return ""
}

// validateItem returns a human-friendly error message for invalid item input,
// or "" if the input is acceptable. binErr carries any error from parsing the
// submitted bin id. Callers are expected to pass already-trimmed values.
func validateItem(name, description, keywords string, binID int64, binErr error) string {
	switch {
	case name == "":
		return "Item name is required"
	case binErr != nil || binID == 0:
		return "Bin is required"
	case tooLong(name, maxNameLen):
		return fmt.Sprintf("Item name is too long (max %d characters)", maxNameLen)
	case tooLong(description, maxDescriptionLen):
		return fmt.Sprintf("Description is too long (max %d characters)", maxDescriptionLen)
	case tooLong(keywords, maxKeywordsLen):
		return fmt.Sprintf("Keywords are too long (max %d characters)", maxKeywordsLen)
	}
	return ""
}

// validateImportRow returns a human-friendly error message for a single CSV
// import row, or "" if the row is acceptable. Callers are expected to pass
// already-trimmed values.
func validateImportRow(itemName, itemDescription, keywords, binName, binCategory, binDescription string) string {
	switch {
	case itemName == "":
		return "item_name is required"
	case binName == "":
		return "bin_name is required"
	case tooLong(itemName, maxNameLen):
		return fmt.Sprintf("item_name is too long (max %d characters)", maxNameLen)
	case tooLong(binName, maxNameLen):
		return fmt.Sprintf("bin_name is too long (max %d characters)", maxNameLen)
	case tooLong(itemDescription, maxDescriptionLen):
		return fmt.Sprintf("item_description is too long (max %d characters)", maxDescriptionLen)
	case tooLong(binDescription, maxDescriptionLen):
		return fmt.Sprintf("bin_description is too long (max %d characters)", maxDescriptionLen)
	case tooLong(keywords, maxKeywordsLen):
		return fmt.Sprintf("keywords are too long (max %d characters)", maxKeywordsLen)
	case tooLong(binCategory, maxCategoryLen):
		return fmt.Sprintf("bin_category is too long (max %d characters)", maxCategoryLen)
	}
	return ""
}

// validatePasswordChange returns a human-friendly error message for a
// password-change submission, or "" if the input is acceptable. currentOK
// reports whether the submitted current password matched.
func validatePasswordChange(currentOK bool, newPassword, confirm string) string {
	switch {
	case !currentOK:
		return "Current password is incorrect"
	case newPassword == "":
		return "New password is required"
	case len(newPassword) < minPasswordLen:
		return fmt.Sprintf("New password must be at least %d characters", minPasswordLen)
	case len(newPassword) > maxPasswordLen:
		return fmt.Sprintf("New password is too long (max %d characters)", maxPasswordLen)
	case newPassword != confirm:
		return "New password and confirmation don't match"
	}
	return ""
}

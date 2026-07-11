package handlers

import (
	"strings"
	"unicode"
)

// minorWords stay lowercase when they fall in the middle of a name, the way
// conventional title case writes "Box of Nails" rather than "Box Of Nails".
// The list is deliberately short: every word added here is a word the user can
// no longer capitalize mid-name by typing it lowercase, so it only holds
// connectives that would look wrong capitalized in an inventory name.
var minorWords = map[string]bool{
	"a": true, "an": true, "and": true, "as": true, "at": true, "but": true,
	"by": true, "for": true, "from": true, "in": true, "nor": true, "of": true,
	"on": true, "or": true, "the": true, "to": true, "via": true, "vs": true,
	"with": true,
}

// titleCase normalizes a name for display: words typed in all lowercase get
// their first letter capitalized, so a user can enter "winter coats" at speed
// and store "Winter Coats".
//
// A word that already contains an uppercase letter is passed through untouched.
// That is the deliberate escape hatch: typing "iPhone", "USB", or "LEGO" keeps
// exactly that casing, so the shortcut never fights a user who has taken the
// trouble to capitalize something a particular way. The only casing this cannot
// express is a brand that is deliberately all-lowercase.
//
// Whitespace is preserved as typed; only casing changes.
func titleCase(s string) string {
	total := len(strings.Fields(s))
	if total == 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))

	word := 0
	start := -1
	for i, r := range s {
		if unicode.IsSpace(r) {
			if start >= 0 {
				b.WriteString(titleCaseWord(s[start:i], word == 0, word == total-1))
				word++
				start = -1
			}
			b.WriteRune(r)
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		b.WriteString(titleCaseWord(s[start:], word == 0, word == total-1))
	}

	return b.String()
}

// titleCaseWord capitalizes a single word of a name. isFirst and isLast mark
// the outer words, which stay capitalized even when they're minor words —
// "The Box", not "the Box".
func titleCaseWord(word string, isFirst, isLast bool) string {
	if hasUpper(word) {
		return word
	}
	if !isFirst && !isLast && minorWords[trimToLetters(word)] {
		return word
	}
	return capitalizeSegments(word)
}

// capitalizeSegments uppercases the first letter of the word and of each
// hyphen- or slash-separated segment within it, so "t-shirt" becomes "T-Shirt".
//
// A letter is only capitalized when nothing alphanumeric precedes it in its
// segment. That keeps leading punctuation transparent ("(spare)" -> "(Spare)")
// while leaving letters that trail a digit alone ("2x4" stays "2x4" rather than
// becoming "2X4"). An apostrophe is not a segment break, because "don't" must
// not become "Don'T"; a user who wants "O'Brien" can simply type it that way.
func capitalizeSegments(word string) string {
	var b strings.Builder
	b.Grow(len(word))

	atSegmentStart := true
	for _, r := range word {
		switch {
		case r == '-' || r == '/':
			atSegmentStart = true
			b.WriteRune(r)
		case atSegmentStart && unicode.IsLetter(r):
			atSegmentStart = false
			b.WriteRune(unicode.ToUpper(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			atSegmentStart = false
			b.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}

	return b.String()
}

// hasUpper reports whether s contains at least one uppercase letter.
func hasUpper(s string) bool {
	return strings.IndexFunc(s, unicode.IsUpper) >= 0
}

// trimToLetters strips surrounding non-letters so a minor word still matches
// when it carries punctuation, as the "of" in "(box of nails)" does.
func trimToLetters(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return !unicode.IsLetter(r) })
}

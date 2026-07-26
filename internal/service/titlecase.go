package service

import "unicode"

// titleWords upper-cases the first letter of each word and leaves the rest of
// each word untouched.
//
// This is a local replacement for the deprecated strings.Title. It is a
// deliberate copy of that function's behaviour — including its isSeparator
// word-boundary rule — so the strings we render (country names, provider
// slugs, flight statuses) are byte-for-byte what they were before.
//
// Note this is NOT equivalent to golang.org/x/text/cases.Title, which also
// lower-cases the tail of each word: cases.Title("JFK LAX") is "Jfk Lax",
// whereas this (and the old strings.Title) yields "JFK LAX". Callers that
// want the normalising behaviour should lower-case the input first, as
// formatCountryName does.
func titleWords(s string) string {
	prev := ' '
	out := []rune(s)
	for i, r := range out {
		if isSeparator(prev) {
			out[i] = unicode.ToTitle(r)
		}
		prev = r
	}
	return string(out)
}

// isSeparator reports whether r is a word separator, matching the unexported
// helper the standard library's strings.Title used.
func isSeparator(r rune) bool {
	// ASCII alphanumerics and underscore are not separators.
	if r <= 0x7F {
		switch {
		case '0' <= r && r <= '9':
			return false
		case 'a' <= r && r <= 'z':
			return false
		case 'A' <= r && r <= 'Z':
			return false
		case r == '_':
			return false
		}
		return true
	}
	// Letters and digits are not separators.
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return false
	}
	// Otherwise, only spaces separate.
	return unicode.IsSpace(r)
}

package service

import "testing"

// OpenStreetMap tags are world-editable, and Place.Website is bound straight
// into an anchor href by the front end. A `javascript:` value there is a live
// script on a public page, so the scheme filter is load-bearing rather than
// cosmetic.
func TestSafeExternalURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"https passes", "https://example.com/cafe", "https://example.com/cafe"},
		{"http passes", "http://example.com", "http://example.com"},
		{"query and port preserved", "https://example.com:8443/a?b=c", "https://example.com:8443/a?b=c"},
		{"surrounding space trimmed", "  https://example.com  ", "https://example.com"},

		{"javascript scheme rejected", "javascript:alert(1)", ""},
		{"mixed-case javascript rejected", "JaVaScRiPt:alert(1)", ""},
		{"data scheme rejected", "data:text/html,<script>alert(1)</script>", ""},
		{"vbscript scheme rejected", "vbscript:msgbox(1)", ""},
		{"file scheme rejected", "file:///etc/passwd", ""},
		{"scheme-less rejected", "example.com", ""},
		{"relative rejected", "/explore", ""},
		{"empty rejected", "", ""},
		{"whitespace-only rejected", "   ", ""},
		{"no host rejected", "https://", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := safeExternalURL(tc.in); got != tc.want {
				t.Errorf("safeExternalURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

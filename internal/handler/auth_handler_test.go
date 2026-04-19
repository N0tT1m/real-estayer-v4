package handler

import "testing"

func TestSanitizeRedirect(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"safe internal", "/dashboard", "/dashboard"},
		{"safe internal with query", "/dashboard?tab=trips", "/dashboard?tab=trips"},
		{"absolute http blocked", "http://evil.example.com/x", ""},
		{"absolute https blocked", "https://evil.example.com/x", ""},
		{"protocol relative blocked", "//evil.example.com/x", ""},
		{"backslash escape blocked", "/\\evil.example.com/x", ""},
		{"missing leading slash blocked", "dashboard", ""},
		{"scheme-only blocked", "javascript:alert(1)", ""},
		{"data scheme blocked", "data:text/html,<script>alert(1)</script>", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeRedirect(c.in)
			if got != c.want {
				t.Errorf("sanitizeRedirect(%q) = %q; want %q", c.in, got, c.want)
			}
		})
	}
}

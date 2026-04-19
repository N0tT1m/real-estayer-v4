package mailer

import (
	"strings"
	"testing"
)

func TestConfigured(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"empty", Config{}, false},
		{"missing password", Config{Host: "smtp.example.com", Username: "u", FromAddress: "f@example.com"}, false},
		{"missing from", Config{Host: "smtp.example.com", Username: "u", Password: "p"}, false},
		{"fully set", Config{Host: "smtp.example.com", Username: "u", Password: "p", FromAddress: "f@example.com"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := New(c.cfg)
			if got := m.Configured(); got != c.want {
				t.Errorf("Configured() = %v; want %v", got, c.want)
			}
		})
	}
}

func TestSendReturnsNotConfiguredWhenBlank(t *testing.T) {
	m := New(Config{})
	err := m.Send(Message{To: []string{"a@example.com"}, Subject: "x", Text: "y"})
	if err != ErrNotConfigured {
		t.Fatalf("expected ErrNotConfigured; got %v", err)
	}
}

func TestRenderMessageIncludesPlainAndHTML(t *testing.T) {
	cfg := Config{FromAddress: "from@example.com", FromName: "Real-Estayer"}
	msg := Message{
		To:      []string{"a@example.com", "b@example.com"},
		Subject: "Reset your password",
		Text:    "Visit this link.",
		HTML:    "<p>Visit this <a href=\"#\">link</a>.</p>",
	}
	out := renderMessage(cfg, msg)
	checks := []string{
		"From: Real-Estayer <from@example.com>",
		"To: a@example.com, b@example.com",
		"Subject: Reset your password",
		"multipart/alternative",
		"text/plain",
		"text/html",
		"Visit this link.",
		"<p>Visit this",
	}
	for _, s := range checks {
		if !strings.Contains(out, s) {
			t.Errorf("rendered message missing %q\nfull:\n%s", s, out)
		}
	}
}

func TestRenderMessagePlainOnly(t *testing.T) {
	out := renderMessage(Config{FromAddress: "f@example.com"}, Message{
		To:      []string{"a@example.com"},
		Subject: "Hi",
		Text:    "Body",
	})
	if strings.Contains(out, "multipart/") {
		t.Errorf("plain-only message should not be multipart: %s", out)
	}
	if !strings.Contains(out, "Content-Type: text/plain") {
		t.Errorf("plain message should have text/plain header: %s", out)
	}
}

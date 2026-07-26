// Package mailer sends outbound transactional email. Intentionally minimal —
// net/smtp only, with STARTTLS on standard submission ports and implicit TLS
// on 465. The Mailer is safe to share across goroutines; dial/send happens
// per-call.
//
// When SMTP isn't configured the Mailer returns a sentinel error that the
// caller can log-and-continue on so local/dev environments don't block on
// missing credentials.
package mailer

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured signals that SMTP is not configured (host/user/password
// missing). Callers should treat this as a no-op rather than a hard failure,
// but still surface it in logs so it's visible during development.
var ErrNotConfigured = errors.New("mailer: SMTP not configured")

// Config is the subset of settings the mailer actually needs.
type Config struct {
	Host        string
	Port        int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	// ImplicitTLS forces SMTPS (port 465 behaviour). When false the mailer
	// upgrades with STARTTLS.
	ImplicitTLS bool
	// InsecureSkipVerify disables TLS verification. Off by default — if you
	// need this, fix your certificate chain instead.
	InsecureSkipVerify bool
}

// Mailer sends email over SMTP.
type Mailer struct {
	cfg Config
}

// New returns a Mailer. Host/Username/Password missing is not an error at
// construction time — the individual Send call returns ErrNotConfigured.
func New(cfg Config) *Mailer {
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	return &Mailer{cfg: cfg}
}

// Configured reports whether enough SMTP settings are set to actually send.
func (m *Mailer) Configured() bool {
	c := m.cfg
	return c.Host != "" && c.Username != "" && c.Password != "" && c.FromAddress != ""
}

// Message is a single transactional email.
type Message struct {
	To      []string
	Subject string
	Text    string // plain-text body
	HTML    string // optional HTML body
}

// Send delivers the message. When SMTP isn't configured returns
// ErrNotConfigured. Connection timeouts are capped at 20s.
func (m *Mailer) Send(msg Message) error {
	if !m.Configured() {
		return ErrNotConfigured
	}
	if len(msg.To) == 0 {
		return errors.New("mailer: at least one recipient required")
	}

	// JoinHostPort rather than "%s:%d" so a literal IPv6 host is bracketed
	// ("::1" -> "[::1]:587"). Hostnames and IPv4 addresses are unaffected.
	addr := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	tlsConfig := &tls.Config{
		ServerName: m.cfg.Host,
		// #nosec G402 -- opt-in via SMTP_INSECURE_SKIP_VERIFY for self-signed
		// relays; defaults to false.
		InsecureSkipVerify: m.cfg.InsecureSkipVerify, //nolint:gosec // opt-in
	}

	dialer := &net.Dialer{Timeout: 20 * time.Second}

	var conn net.Conn
	var err error
	if m.cfg.ImplicitTLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mailer: dial %s: %w", addr, err)
	}

	client, err := smtp.NewClient(conn, m.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("mailer: smtp client: %w", err)
	}
	defer func() { _ = client.Quit() }()

	if !m.cfg.ImplicitTLS {
		// STARTTLS upgrade; required on submission port 587.
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return fmt.Errorf("mailer: STARTTLS: %w", err)
			}
		}
	}

	if ok, _ := client.Extension("AUTH"); ok {
		auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("mailer: auth: %w", err)
		}
	}

	if err := client.Mail(m.cfg.FromAddress); err != nil {
		return fmt.Errorf("mailer: MAIL FROM: %w", err)
	}
	for _, to := range msg.To {
		if err := client.Rcpt(to); err != nil {
			return fmt.Errorf("mailer: RCPT TO %s: %w", to, err)
		}
	}

	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("mailer: DATA: %w", err)
	}
	if _, err := writer.Write([]byte(renderMessage(m.cfg, msg))); err != nil {
		_ = writer.Close()
		return fmt.Errorf("mailer: write body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("mailer: close body: %w", err)
	}
	return nil
}

// renderMessage builds an RFC-822 message with a multipart/alternative body
// when HTML is provided. The output is deliberately simple — no attachments,
// no inline images; callers that need those can add a richer path later.
func renderMessage(cfg Config, msg Message) string {
	from := cfg.FromAddress
	if cfg.FromName != "" {
		from = fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromAddress)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", from)
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(msg.To, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", msg.Subject)
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	fmt.Fprint(&b, "MIME-Version: 1.0\r\n")

	if msg.HTML != "" {
		boundary := "re-boundary-" + fmt.Sprintf("%d", time.Now().UnixNano())
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprint(&b, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
		fmt.Fprint(&b, "Content-Transfer-Encoding: 8bit\r\n\r\n")
		fmt.Fprint(&b, msg.Text)
		fmt.Fprint(&b, "\r\n")

		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprint(&b, "Content-Type: text/html; charset=\"utf-8\"\r\n")
		fmt.Fprint(&b, "Content-Transfer-Encoding: 8bit\r\n\r\n")
		fmt.Fprint(&b, msg.HTML)
		fmt.Fprint(&b, "\r\n")

		fmt.Fprintf(&b, "--%s--\r\n", boundary)
	} else {
		fmt.Fprint(&b, "Content-Type: text/plain; charset=\"utf-8\"\r\n")
		fmt.Fprint(&b, "Content-Transfer-Encoding: 8bit\r\n\r\n")
		fmt.Fprint(&b, msg.Text)
	}
	return b.String()
}

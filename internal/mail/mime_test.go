package mail

import (
	"strings"
	"testing"
)

func TestParseMessagePlainText(t *testing.T) {
	raw := strings.Join([]string{
		"From: Acme Cloud <billing@acme.example>",
		"To: me@example.com",
		"Subject: Your Acme Cloud receipt",
		"Date: Wed, 15 Jan 2026 10:30:00 +0000",
		"Message-ID: <receipt-1@acme.example>",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Acme Cloud Pro",
		"$9.99 per month",
	}, "\r\n")

	parsed, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Subject != "Your Acme Cloud receipt" {
		t.Fatalf("subject = %q", parsed.Subject)
	}
	if !strings.Contains(parsed.From, "Acme Cloud") || !strings.Contains(parsed.From, "billing@acme.example") {
		t.Fatalf("from = %q", parsed.From)
	}
	if parsed.MessageID != "receipt-1@acme.example" {
		t.Fatalf("message id = %q", parsed.MessageID)
	}
	if parsed.Date.IsZero() {
		t.Fatal("date was not parsed")
	}
	if !strings.Contains(parsed.Body, "$9.99 per month") {
		t.Fatalf("body = %q", parsed.Body)
	}
}

func TestParseMessagePrefersPlainTextAndSkipsAttachments(t *testing.T) {
	raw := strings.Join([]string{
		"From: Acme Cloud <billing@acme.example>",
		"Subject: Invoice",
		"Date: Wed, 15 Jan 2026 10:30:00 +0000",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=outer",
		"",
		"--outer",
		"Content-Type: multipart/alternative; boundary=inner",
		"",
		"--inner",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"Plain text body",
		"--inner",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><body><p>HTML body</p></body></html>",
		"--inner--",
		"--outer",
		"Content-Type: application/pdf; name=invoice.pdf",
		"Content-Disposition: attachment; filename=invoice.pdf",
		"",
		"not really a pdf",
		"--outer--",
	}, "\r\n")

	parsed, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Body != "Plain text body" {
		t.Fatalf("body = %q, want the text/plain part", parsed.Body)
	}
}

func TestParseMessageFallsBackToHTML(t *testing.T) {
	raw := strings.Join([]string{
		"From: Acme Cloud <billing@acme.example>",
		"Subject: Receipt",
		"Content-Type: text/html; charset=utf-8",
		"",
		"<html><head><style>p{color:red}</style></head><body><p>Acme &amp; Co</p><p>$5.00</p></body></html>",
	}, "\r\n")

	parsed, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !strings.Contains(parsed.Body, "Acme & Co") || !strings.Contains(parsed.Body, "$5.00") {
		t.Fatalf("body = %q", parsed.Body)
	}
	if strings.Contains(parsed.Body, "color:red") || strings.Contains(parsed.Body, "<p>") {
		t.Fatalf("HTML was not stripped: %q", parsed.Body)
	}
}

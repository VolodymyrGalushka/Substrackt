package mail

import (
	"errors"
	"strings"
	"testing"
)

func TestTLSConfigUsesAddressHost(t *testing.T) {
	cfg := Config{Address: "imap.example.com:993"}
	tlsCfg := tlsConfig(cfg)
	if tlsCfg.ServerName != "imap.example.com" {
		t.Fatalf("ServerName = %q", tlsCfg.ServerName)
	}
	if tlsCfg.InsecureSkipVerify {
		t.Fatal("certificate verification must not be disabled")
	}
}

func TestTLSConfigUsesOverride(t *testing.T) {
	cfg := Config{Address: "192.0.2.10:993", TLSHost: "mail.example.com"}
	if got := tlsConfig(cfg).ServerName; got != "mail.example.com" {
		t.Fatalf("ServerName = %q", got)
	}
}

func TestXOAuth2InitialResponse(t *testing.T) {
	client := xoauth2Client{username: "me@example.com", token: "token-123"}
	mechanism, response, err := client.Start()
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if mechanism != "XOAUTH2" {
		t.Fatalf("mechanism = %q", mechanism)
	}
	want := "user=me@example.com\x01auth=Bearer token-123\x01\x01"
	if string(response) != want {
		t.Fatalf("response = %q, want %q", response, want)
	}
}

func TestLoginUnsupported(t *testing.T) {
	unsupported := []string{
		"unknown command",
		"LOGIN not supported",
		"login disabled",
		"[ALERT] login is disabled",
		"bad command",
	}
	for _, message := range unsupported {
		if !loginUnsupported(errors.New(message)) {
			t.Fatalf("%q should trigger a SASL fallback", message)
		}
	}
	for _, message := range []string{"authentication failed", "invalid credentials", "timeout"} {
		if loginUnsupported(errors.New(message)) {
			t.Fatalf("%q must not trigger a credential retry", message)
		}
	}
}

func TestDescribeAuthErrorMentionsAppPasswords(t *testing.T) {
	err := describeAuthError(Config{Username: "me@yahoo.com"}, errors.New("NO [AUTHENTICATIONFAILED] Invalid credentials"))
	message := err.Error()
	if !strings.Contains(message, "app password") || !strings.Contains(message, "two-factor") {
		t.Fatalf("error = %q, want an app-password hint", message)
	}
	if strings.Contains(message, "secret") {
		t.Fatalf("error leaked a credential: %q", message)
	}
}

func TestDescribeDialErrorMentionsTLS(t *testing.T) {
	err := describeDialError(Config{Address: "imap.example.com:993"}, "tls", errors.New("tls: failed to verify certificate: x509: certificate signed by unknown authority"))
	if !strings.Contains(err.Error(), "TLS connection") || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("error = %q", err)
	}
}

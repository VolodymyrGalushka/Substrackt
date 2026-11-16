package domain

import (
	"strings"
	"testing"
)

func TestEmailSourceConfigDefaults(t *testing.T) {
	cfg := EmailSourceConfig{Address: "imap.example.com:993", Username: "me@example.com"}
	cfg.Normalize()
	if cfg.Security != SecurityTLS {
		t.Fatalf("security = %q, want tls", cfg.Security)
	}
	if cfg.Auth != AuthAuto {
		t.Fatalf("auth = %q, want auto", cfg.Auth)
	}
	if cfg.Mailbox != "INBOX" || cfg.InitialDays != 30 || cfg.MaxMessages != 50 {
		t.Fatalf("defaults = %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestEmailSourceConfigLegacyInsecureMapsToNone(t *testing.T) {
	cfg := EmailSourceConfig{Address: "127.0.0.1:143", Username: "me", Insecure: true}
	cfg.Normalize()
	if cfg.Security != SecurityNone {
		t.Fatalf("security = %q, want none", cfg.Security)
	}
	if !cfg.Insecure {
		t.Fatal("deprecated Insecure flag should stay in sync")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate local plain TCP: %v", err)
	}
}

func TestEmailSourceConfigRejectsPlainTextForPublicHosts(t *testing.T) {
	cfg := EmailSourceConfig{
		Address: "imap.example.com:143", Username: "me", Mailbox: "INBOX",
		Security: SecurityNone, Auth: AuthAuto, InitialDays: 30, MaxMessages: 50,
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "local or private") {
		t.Fatalf("validate = %v, want a local/private rejection", err)
	}
}

func TestEmailSourceConfigValidatesModes(t *testing.T) {
	base := EmailSourceConfig{
		Address: "imap.example.com:993", Username: "me", Mailbox: "INBOX",
		InitialDays: 30, MaxMessages: 50,
	}

	starttls := base
	starttls.Security = SecurityStartTLS
	starttls.Auth = AuthPlain
	starttls.TLSHost = "mail.example.com"
	if err := starttls.Validate(); err != nil {
		t.Fatalf("starttls validate: %v", err)
	}

	xoauth := base
	xoauth.Security = SecurityTLS
	xoauth.Auth = AuthXOAuth2
	if err := xoauth.Validate(); err != nil {
		t.Fatalf("xoauth2 validate: %v", err)
	}

	badSecurity := base
	badSecurity.Security = "ssl3"
	if err := badSecurity.Validate(); err == nil {
		t.Fatal("expected an invalid encryption mode to be rejected")
	}

	badAuth := base
	badAuth.Auth = "magic"
	if err := badAuth.Validate(); err == nil {
		t.Fatal("expected an invalid authentication mode to be rejected")
	}

	plainOAuth := base
	plainOAuth.Address = "127.0.0.1:143"
	plainOAuth.Security = SecurityNone
	plainOAuth.Auth = AuthXOAuth2
	if err := plainOAuth.Validate(); err == nil {
		t.Fatal("expected OAuth2 over plain TCP to be rejected")
	}
}

func TestMailHostIsLocal(t *testing.T) {
	local := []string{"localhost", "127.0.0.1", "::1", "192.168.1.10", "10.0.0.5", "mail.local", "imap.internal", "nas"}
	for _, host := range local {
		if !mailHostIsLocal(host) {
			t.Fatalf("%q should be local", host)
		}
	}
	public := []string{"imap.example.com", "8.8.8.8", ""}
	for _, host := range public {
		if mailHostIsLocal(host) {
			t.Fatalf("%q should not be local", host)
		}
	}
}

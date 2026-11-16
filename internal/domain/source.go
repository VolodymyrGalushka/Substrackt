package domain

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// SourceKind is the ingestion channel a source belongs to.
type SourceKind string

const (
	// SourceEmail reads subscription mail from an IMAP mailbox.
	SourceEmail SourceKind = "email"
)

func (k SourceKind) Valid() bool { return k == SourceEmail }

// DefaultSourceScheduleMinutes is how often an enabled source is polled.
const DefaultSourceScheduleMinutes = 15

// IMAP encryption modes.
const (
	// SecurityTLS uses implicit TLS (normally port 993).
	SecurityTLS = "tls"
	// SecurityStartTLS upgrades a plaintext connection with STARTTLS (normally
	// port 143).
	SecurityStartTLS = "starttls"
	// SecurityNone sends credentials in clear text. It is only allowed for
	// local/private servers and exists for development and home labs.
	SecurityNone = "none"
)

// IMAP authentication modes. Password-based modes use the password field; the
// OAuth2 mode treats it as an access token.
const (
	// AuthAuto picks LOGIN unless the server disables it, then SASL PLAIN/LOGIN.
	AuthAuto = "auto"
	// AuthLogin sends the IMAP LOGIN command.
	AuthLogin = "login"
	// AuthPlain uses SASL PLAIN.
	AuthPlain = "plain"
	// AuthXOAuth2 uses SASL XOAUTH2 with the password field as an access token.
	AuthXOAuth2 = "xoauth2"
)

// EmailSourceConfig is the typed configuration of an IMAP source. The password
// is never stored here: only a secret reference lives on the Source.
type EmailSourceConfig struct {
	Address  string // host:port, e.g. imap.example.com:993
	Username string
	Mailbox  string
	// Security is tls, starttls or none. Empty means tls.
	Security string
	// Auth is auto, login, plain or xoauth2. Empty means auto.
	Auth string
	// TLSHost overrides the certificate name when Address uses an IP or an
	// internal alias. Empty means the host part of Address.
	TLSHost string
	// InitialDays bounds the first sync when there is no cursor yet.
	InitialDays int
	// MaxMessages caps how many messages one sync fetches.
	MaxMessages int
	// Insecure is deprecated. It is retained so configurations saved before
	// Security existed still map to SecurityNone.
	Insecure bool
}

// Normalize trims and applies defaults to the email configuration.
func (c *EmailSourceConfig) Normalize() {
	c.Address = strings.TrimSpace(c.Address)
	c.Username = strings.TrimSpace(c.Username)
	c.Mailbox = strings.TrimSpace(c.Mailbox)
	c.TLSHost = strings.TrimSpace(c.TLSHost)
	if c.Mailbox == "" {
		c.Mailbox = "INBOX"
	}

	c.Security = strings.ToLower(strings.TrimSpace(c.Security))
	switch c.Security {
	case "":
		if c.Insecure {
			c.Security = SecurityNone
		} else {
			c.Security = SecurityTLS
		}
	case "ssl", "implicit", "imaps":
		c.Security = SecurityTLS
	}
	// Keep the deprecated flag in sync for older callers reading it.
	c.Insecure = c.Security == SecurityNone

	c.Auth = strings.ToLower(strings.TrimSpace(c.Auth))
	if c.Auth == "" {
		c.Auth = AuthAuto
	}

	if c.InitialDays <= 0 {
		c.InitialDays = 30
	}
	if c.MaxMessages <= 0 {
		c.MaxMessages = 50
	}
}

// Validate checks the configuration before it is saved.
func (c EmailSourceConfig) Validate() error {
	if c.Address == "" {
		return fmt.Errorf("IMAP address is required")
	}
	host, portRaw, err := net.SplitHostPort(c.Address)
	if err != nil || strings.TrimSpace(host) == "" {
		return fmt.Errorf("IMAP address must be host:port (for example imap.example.com:993)")
	}
	port, err := strconv.Atoi(portRaw)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("IMAP port %q is not valid", portRaw)
	}
	switch c.Security {
	case SecurityTLS, SecurityStartTLS:
	case SecurityNone:
		if !mailHostIsLocal(host) {
			return fmt.Errorf("plain TCP is only allowed for local or private mail servers; use TLS or STARTTLS")
		}
	default:
		return fmt.Errorf("unsupported encryption mode %q", c.Security)
	}
	switch c.Auth {
	case AuthAuto, AuthLogin, AuthPlain:
	case AuthXOAuth2:
		if c.Security == SecurityNone {
			return fmt.Errorf("OAuth2 tokens must be sent over TLS or STARTTLS")
		}
	default:
		return fmt.Errorf("unsupported authentication mode %q", c.Auth)
	}
	if c.Username == "" {
		return fmt.Errorf("IMAP username is required")
	}
	if c.Mailbox == "" {
		return fmt.Errorf("mailbox is required")
	}
	if c.InitialDays < 1 || c.InitialDays > 3650 {
		return fmt.Errorf("initial sync window must be between 1 and 3650 days")
	}
	if c.MaxMessages < 1 || c.MaxMessages > 500 {
		return fmt.Errorf("messages per sync must be between 1 and 500")
	}
	return nil
}

// mailHostIsLocal reports whether plaintext IMAP is acceptable for host. It
// mirrors the LLM guard: loopback, private/link-local IPs and local service
// names only.
func mailHostIsLocal(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" {
		return false
	}
	switch host {
	case "localhost", "host.docker.internal", "gateway.docker.internal":
		return true
	}
	if strings.HasSuffix(host, ".localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified()
	}
	for _, suffix := range []string{".local", ".internal", ".home.arpa", ".lan", ".localdomain"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return !strings.Contains(host, ".")
}

// Source is a configured ingestion channel. Its credentials live in the secret
// store under SecretRef and are never part of the stored config.
type Source struct {
	ID              string
	Kind            SourceKind
	Name            string
	Config          EmailSourceConfig
	SecretRef       string
	Enabled         bool
	ScheduleMinutes int
	LastSyncAt      time.Time
	LastError       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Normalize trims and applies defaults.
func (s *Source) Normalize() {
	s.Config.Normalize()
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		s.Name = s.Config.Username
	}
	if s.ScheduleMinutes <= 0 {
		s.ScheduleMinutes = DefaultSourceScheduleMinutes
	}
}

// Validate checks a source and its typed configuration.
func (s Source) Validate() error {
	if !s.Kind.Valid() {
		return fmt.Errorf("unsupported source kind %q", s.Kind)
	}
	if s.Name == "" {
		return fmt.Errorf("source name is required")
	}
	if s.ScheduleMinutes < 5 || s.ScheduleMinutes > 10080 {
		return fmt.Errorf("sync schedule must be between 5 and 10080 minutes")
	}
	switch s.Kind {
	case SourceEmail:
		return s.Config.Validate()
	default:
		return fmt.Errorf("unsupported source kind %q", s.Kind)
	}
}

// MailboxCursor is the last processed IMAP UID for one mailbox. UIDValidity
// guards against UIDs being reused after a server-side mailbox reset.
type MailboxCursor struct {
	SourceID    string
	Mailbox     string
	UIDValidity uint32
	LastUID     uint32
	UpdatedAt   time.Time
}

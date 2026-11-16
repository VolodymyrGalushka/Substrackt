// Package mail wraps the IMAP client used to read subscription mailboxes.
//
// It is a thin layer over github.com/emersion/go-imap/v2 so the rest of the app
// does not depend on the client API directly.
package mail

import (
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"
)

// Config describes how to connect to an IMAP server.
type Config struct {
	Address  string // host:port, e.g. imap.example.com:993
	Username string
	Password string
	// Security is "tls" (implicit TLS), "starttls" or "none". Empty means TLS.
	Security string
	// Auth is "auto", "login", "plain" or "xoauth2". Empty means auto.
	Auth string
	// TLSHost overrides the certificate hostname. Empty uses Address's host.
	TLSHost string
}

// MailboxInfo is the state of a selected mailbox.
type MailboxInfo struct {
	NumMessages uint32
	UIDNext     uint32
	UIDValidity uint32
}

// Message is one email returned by a sync.
type Message struct {
	UID       uint32
	Subject   string
	From      string
	MessageID string
	Date      time.Time
	Text      string
}

// Client is an authenticated IMAP connection.
type Client struct {
	c *imapclient.Client
}

// Dial connects to the server and logs in. The returned client must be closed
// by the caller.
func Dial(cfg Config) (*Client, error) {
	security := strings.ToLower(strings.TrimSpace(cfg.Security))
	if security == "" {
		security = "tls"
	}
	options := &imapclient.Options{TLSConfig: tlsConfig(cfg)}

	var (
		c   *imapclient.Client
		err error
	)
	switch security {
	case "none":
		c, err = imapclient.DialInsecure(cfg.Address, options)
	case "starttls":
		c, err = imapclient.DialStartTLS(cfg.Address, options)
	case "tls":
		c, err = imapclient.DialTLS(cfg.Address, options)
	default:
		return nil, fmt.Errorf("unsupported encryption mode %q", cfg.Security)
	}
	if err != nil {
		return nil, describeDialError(cfg, security, err)
	}

	if err := authenticate(c, cfg); err != nil {
		_ = c.Close()
		return nil, describeAuthError(cfg, err)
	}
	// Yahoo and some other servers expect an ID command before the first
	// SELECT. Send it when advertised; failure is not fatal.
	if c.Caps().Has(imap.CapID) {
		_, _ = c.ID(&imap.IDData{Name: "Substrackt", Version: "1.0"}).Wait()
	}
	return &Client{c: c}, nil
}

func tlsConfig(cfg Config) *tls.Config {
	host := strings.TrimSpace(cfg.TLSHost)
	if host == "" {
		if h, _, err := net.SplitHostPort(cfg.Address); err == nil {
			host = h
		}
	}
	return &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	}
}

func authenticate(c *imapclient.Client, cfg Config) error {
	mode := strings.ToLower(strings.TrimSpace(cfg.Auth))
	if mode == "" {
		mode = "auto"
	}
	switch mode {
	case "login":
		return c.Login(cfg.Username, cfg.Password).Wait()
	case "plain":
		return c.Authenticate(sasl.NewPlainClient("", cfg.Username, cfg.Password))
	case "xoauth2":
		return c.Authenticate(xoauth2Client{username: cfg.Username, token: cfg.Password})
	case "auto":
	default:
		return fmt.Errorf("unsupported authentication mode %q", cfg.Auth)
	}

	// Auto: prefer IMAP LOGIN, but honour servers that advertise LOGINDISABLED
	// (normally before STARTTLS) and fall back when LOGIN is not implemented.
	if c.Caps().Has(imap.CapLoginDisabled) {
		plainErr := authenticateSASL(c, cfg, "PLAIN")
		if plainErr == nil {
			return nil
		}
		if c.Caps().Has(imap.AuthCap("LOGIN")) {
			if loginErr := authenticateSASL(c, cfg, "LOGIN"); loginErr == nil {
				return nil
			}
		}
		return plainErr
	}
	err := c.Login(cfg.Username, cfg.Password).Wait()
	if err == nil {
		return nil
	}
	if loginUnsupported(err) {
		if plainErr := authenticateSASL(c, cfg, "PLAIN"); plainErr == nil {
			return nil
		}
		if loginErr := authenticateSASL(c, cfg, "LOGIN"); loginErr == nil {
			return nil
		}
	}
	return err
}

func authenticateSASL(c *imapclient.Client, cfg Config, mechanism string) error {
	switch mechanism {
	case "LOGIN":
		return c.Authenticate(sasl.NewLoginClient(cfg.Username, cfg.Password))
	default:
		return c.Authenticate(sasl.NewPlainClient("", cfg.Username, cfg.Password))
	}
}

// loginUnsupported reports whether an IMAP LOGIN failure means the command is
// unavailable rather than the credentials being rejected. Falling back after a
// credential rejection can trigger provider lockouts.
func loginUnsupported(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"unknown command", "not supported", "unsupported", "login disabled",
		"login is disabled", "logindisabled", "bad command", "invalid command",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// xoauth2Client implements the XOAUTH2 SASL mechanism used by Yahoo, Gmail and
// Microsoft. The password field carries a short-lived OAuth2 access token.
type xoauth2Client struct {
	username string
	token    string
}

func (c xoauth2Client) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + c.username + "\x01auth=Bearer " + c.token + "\x01\x01"), nil
}

func (c xoauth2Client) Next([]byte) ([]byte, error) {
	// On failure the server sends a base64 JSON error. An empty response lets
	// the exchange terminate so the caller sees the server's error.
	return []byte{}, nil
}

func describeDialError(cfg Config, security string, err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "certificate"), strings.Contains(msg, "x509"), strings.Contains(msg, "tls:"):
		return fmt.Errorf("TLS connection to %s failed: %w; check the encryption mode, port and certificate hostname (TLS hostname override is %q)", cfg.Address, err, cfg.TLSHost)
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "connection refused"), strings.Contains(msg, "network is unreachable"):
		return fmt.Errorf("cannot reach %s using %s: %w; check the server address, port and encryption mode", cfg.Address, securityLabel(security), err)
	}
	return fmt.Errorf("connect to %s using %s: %w", cfg.Address, securityLabel(security), err)
}

func describeAuthError(cfg Config, err error) error {
	msg := strings.ToLower(err.Error())
	if strings.EqualFold(cfg.Auth, "xoauth2") {
		return fmt.Errorf("OAuth2 authentication failed: the access token may be expired or missing the mail scope; paste a fresh token: %w", err)
	}
	switch {
	case strings.Contains(msg, "authenticationfailed"), strings.Contains(msg, "authentication failed"),
		strings.Contains(msg, "invalid credentials"), strings.Contains(msg, "invalid username or password"),
		strings.Contains(msg, "login failed"), strings.Contains(msg, "authorization failed"):
		return fmt.Errorf("login as %s was rejected: check the username and password. If the provider uses two-factor authentication, create an app password (Yahoo, Gmail, iCloud) instead of using your account password: %w", cfg.Username, err)
	case strings.Contains(msg, "xoauth2"), strings.Contains(msg, "oauth"), strings.Contains(msg, "bearer"):
		return fmt.Errorf("OAuth2 authentication failed: the access token may be expired or missing the mail scope; paste a fresh token: %w", err)
	case strings.Contains(msg, "logindisabled"), strings.Contains(msg, "login disabled"):
		return fmt.Errorf("the server requires SASL authentication; choose PLAIN or OAuth2 as the authentication method: %w", err)
	}
	return fmt.Errorf("login as %s: %w", cfg.Username, err)
}

func securityLabel(security string) string {
	switch security {
	case "none":
		return "plain TCP"
	case "starttls":
		return "STARTTLS"
	default:
		return "TLS"
	}
}

// Mailboxes lists mailbox names matching pattern. Use "*" for all mailboxes.
func (c *Client) Mailboxes(pattern string) ([]string, error) {
	items, err := c.c.List("", pattern, nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}

	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Mailbox)
	}
	return names, nil
}

// Select selects a mailbox for reading and returns its message count.
func (c *Client) Select(mailbox string) (uint32, error) {
	info, err := c.SelectInfo(mailbox)
	if err != nil {
		return 0, err
	}
	return info.NumMessages, nil
}

// SelectInfo selects a mailbox and returns its UID state.
func (c *Client) SelectInfo(mailbox string) (MailboxInfo, error) {
	data, err := c.c.Select(mailbox, nil).Wait()
	if err != nil {
		return MailboxInfo{}, fmt.Errorf("select %s: %w", mailbox, err)
	}
	return MailboxInfo{
		NumMessages: data.NumMessages,
		UIDNext:     uint32(data.UIDNext),
		UIDValidity: data.UIDValidity,
	}, nil
}

// FetchSince returns up to limit messages newer than lastUID. lastUID is the
// last processed UID; when it is zero the first sync is bounded by since.
func (c *Client) FetchSince(mailbox string, lastUID uint32, since time.Time, limit int) (MailboxInfo, []Message, error) {
	if limit <= 0 {
		limit = 50
	}
	info, err := c.SelectInfo(mailbox)
	if err != nil {
		return MailboxInfo{}, nil, err
	}

	criteria := &imap.SearchCriteria{}
	if lastUID > 0 {
		set := imap.UIDSet{}
		set.AddRange(imap.UID(lastUID+1), 0) // 0 means "*"
		criteria.UID = []imap.UIDSet{set}
	} else if !since.IsZero() {
		criteria.Since = since
	}

	found, err := c.c.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return info, nil, fmt.Errorf("search %s: %w", mailbox, err)
	}
	uids := found.AllUIDs()
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) > limit {
		uids = uids[:limit]
	}
	if len(uids) == 0 {
		return info, nil, nil
	}

	fetch := c.c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{{Peek: true}},
	})
	buffers, err := fetch.Collect()
	if err != nil {
		return info, nil, fmt.Errorf("fetch %s: %w", mailbox, err)
	}

	messages := make([]Message, 0, len(buffers))
	for _, buffer := range buffers {
		messages = append(messages, messageFromBuffer(buffer))
	}
	return info, messages, nil
}

func messageFromBuffer(buffer *imapclient.FetchMessageBuffer) Message {
	message := Message{UID: uint32(buffer.UID)}
	if buffer.Envelope != nil {
		message.Subject = buffer.Envelope.Subject
		message.MessageID = buffer.Envelope.MessageID
		message.Date = buffer.Envelope.Date
		if len(buffer.Envelope.From) > 0 {
			message.From = formatAddress(buffer.Envelope.From[0])
		}
	}
	if message.Date.IsZero() {
		message.Date = buffer.InternalDate
	}
	if len(buffer.BodySection) > 0 {
		if parsed, err := ParseMessage(buffer.BodySection[0].Bytes); err == nil {
			if parsed.Subject != "" {
				message.Subject = parsed.Subject
			}
			if parsed.From != "" {
				message.From = parsed.From
			}
			if parsed.MessageID != "" {
				message.MessageID = parsed.MessageID
			}
			if !parsed.Date.IsZero() {
				message.Date = parsed.Date
			}
			message.Text = parsed.Body
		}
	}
	return message
}

func formatAddress(addr imap.Address) string {
	email := addr.Addr()
	switch {
	case addr.Name != "" && email != "":
		return fmt.Sprintf("%s <%s>", addr.Name, email)
	case addr.Name != "":
		return addr.Name
	default:
		return email
	}
}

// Abort closes the connection immediately without a LOGOUT round trip. It is
// used to unblock a sync when its context is cancelled.
func (c *Client) Abort() error {
	if c == nil || c.c == nil {
		return nil
	}
	return c.c.Close()
}

// Close logs out and closes the connection. It is safe to call on a nil client.
func (c *Client) Close() error {
	if c == nil || c.c == nil {
		return nil
	}
	if err := c.c.Logout().Wait(); err != nil {
		_ = c.c.Close()
		return fmt.Errorf("logout: %w", err)
	}
	return c.c.Close()
}

// Package mail wraps the IMAP client used to read subscription mailboxes.
//
// It is a thin layer over github.com/emersion/go-imap/v2 so the rest of the app
// does not depend on the client API directly.
package mail

import (
	"fmt"
	"sort"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Config describes how to connect to an IMAP server.
type Config struct {
	Address  string // host:port, e.g. imap.example.com:993
	Username string
	Password string
	// Insecure dials plain TCP instead of implicit TLS. Intended for local test
	// servers only; real servers should use TLS on port 993.
	Insecure bool
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
	var (
		c   *imapclient.Client
		err error
	)
	if cfg.Insecure {
		c, err = imapclient.DialInsecure(cfg.Address, nil)
	} else {
		c, err = imapclient.DialTLS(cfg.Address, nil)
	}
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", cfg.Address, err)
	}

	if err := c.Login(cfg.Username, cfg.Password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("login as %s: %w", cfg.Username, err)
	}
	return &Client{c: c}, nil
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

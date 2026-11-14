// Package mail wraps the IMAP client used to read subscription mailboxes.
//
// It is a thin layer over github.com/emersion/go-imap/v2 so the rest of the app
// does not depend on the client API directly.
package mail

import (
	"fmt"

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
	data, err := c.c.Select(mailbox, nil).Wait()
	if err != nil {
		return 0, fmt.Errorf("select %s: %w", mailbox, err)
	}
	return data.NumMessages, nil
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

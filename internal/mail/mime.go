package mail

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-message/mail"
)

const maxBodyBytes = 256 * 1024

var (
	htmlIgnored = regexp.MustCompile(`(?is)<(?:script|style|head)[^>]*>.*?</(?:script|style|head)>`)
	htmlBlocks  = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|table|section|article|header|footer|blockquote)[^>]*>`)
	htmlTags    = regexp.MustCompile(`(?s)<[^>]+>`)
)

// ParsedMessage is the canonical text and metadata of an email.
type ParsedMessage struct {
	Subject   string
	From      string
	Body      string
	MessageID string
	Date      time.Time
}

// ParseMessage reads MIME headers and the best text part. Attachments are
// ignored; HTML-only mail is converted to plain text.
func ParseMessage(raw []byte) (ParsedMessage, error) {
	reader, err := mail.CreateReader(bytes.NewReader(raw))
	if reader == nil {
		return ParsedMessage{}, fmt.Errorf("parse MIME message: %w", err)
	}
	// Unknown charsets are common in old mail; CreateReader still returns a
	// usable reader, so only treat a nil reader as fatal.

	var parsed ParsedMessage
	if subject, err := reader.Header.Subject(); err == nil {
		parsed.Subject = subject
	}
	if date, err := reader.Header.Date(); err == nil {
		parsed.Date = date
	}
	if msgID, err := reader.Header.MessageID(); err == nil {
		parsed.MessageID = msgID
	}
	if addrs, err := reader.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		parsed.From = addrs[0].String()
	}

	var plain, rich strings.Builder
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if part == nil {
			break
		}
		inline, ok := part.Header.(*mail.InlineHeader)
		if !ok {
			_, _ = io.Copy(io.Discard, io.LimitReader(part.Body, maxBodyBytes))
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(part.Body, maxBodyBytes))
		contentType, _, _ := inline.ContentType()
		if strings.HasPrefix(strings.ToLower(contentType), "text/html") {
			rich.Write(body)
		} else {
			plain.Write(body)
		}
	}

	body := strings.TrimSpace(plain.String())
	if body == "" {
		body = htmlToText(rich.String())
	}
	parsed.Body = strings.TrimSpace(body)
	return parsed, nil
}

func htmlToText(source string) string {
	source = htmlIgnored.ReplaceAllString(source, " ")
	source = htmlBlocks.ReplaceAllString(source, "\n")
	source = htmlTags.ReplaceAllString(source, " ")
	return strings.TrimSpace(html.UnescapeString(source))
}

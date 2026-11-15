package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxContentBytes = 64 * 1024

var (
	cardLikePattern = regexp.MustCompile(`\b(?:\d[ -]?){12,18}\d\b`)
	cvvPattern      = regexp.MustCompile(`(?i)\b(cvv|cvc|cid|security code)\s*[:#]?\s*\d{3,4}\b`)
)

// NormalizeText canonicalises a raw document: line endings, control characters,
// whitespace and length. Card numbers are redacted before the text is hashed or
// stored, because evidence can legitimately contain a receipt.
func NormalizeText(content string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	content = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\t':
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, content)

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	content = strings.TrimSpace(strings.Join(out, "\n"))
	content = RedactSensitive(content)

	if len(content) > maxContentBytes {
		content = content[:maxContentBytes]
		for !utf8.ValidString(content) && len(content) > 0 {
			content = content[:len(content)-1]
		}
	}
	return content
}

// RedactSensitive masks card-number-like digit runs and labelled security codes.
func RedactSensitive(content string) string {
	content = cardLikePattern.ReplaceAllStringFunc(content, func(match string) string {
		digits := make([]byte, 0, len(match))
		for i := 0; i < len(match); i++ {
			if match[i] >= '0' && match[i] <= '9' {
				digits = append(digits, match[i])
			}
		}
		if len(digits) < 13 || len(digits) > 19 {
			return match
		}
		return "[card ending " + string(digits[len(digits)-4:]) + "]"
	})
	content = cvvPattern.ReplaceAllStringFunc(content, func(match string) string {
		lower := strings.ToLower(match)
		for _, prefix := range []string{"cvv", "cvc", "cid", "security code"} {
			if strings.HasPrefix(lower, prefix) {
				return match[:len(prefix)] + ": [redacted]"
			}
		}
		return "[redacted]"
	})
	return content
}

// ContentHash returns a stable SHA-256 key for deduplication.
func ContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

package domain

import (
	"crypto/rand"
	"encoding/binary"
	"time"
)

// Crockford base32 alphabet used by ULIDs.
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewID returns a lexicographically sortable ULID: 26 characters, 48-bit
// millisecond timestamp followed by 80 bits of randomness.
func NewID() string {
	var b [16]byte
	ms := uint64(time.Now().UnixMilli())
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	if _, err := rand.Read(b[6:]); err != nil {
		// crypto/rand failing is fatal for identity generation; fall back to a
		// time-derived tail rather than panicking in a UI thread.
		binary.BigEndian.PutUint64(b[8:], uint64(time.Now().UnixNano()))
	}
	return encodeULID(b)
}

func encodeULID(b [16]byte) string {
	hi := binary.BigEndian.Uint64(b[:8])
	lo := binary.BigEndian.Uint64(b[8:])

	var out [26]byte
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = ulidAlphabet[lo&0x1f]
		lo = lo>>5 | hi<<59
		hi >>= 5
	}
	return string(out[:])
}

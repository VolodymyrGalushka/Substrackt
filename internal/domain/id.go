package domain

import (
	"crypto/rand"
	"encoding/binary"
	"sync"
	"time"
)

// Crockford base32 alphabet used by ULIDs.
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	idMu       sync.Mutex
	lastMillis uint64
	lastTail   [10]byte
	haveLast   bool
)

// NewID returns a lexicographically sortable ULID: 26 characters, 48-bit
// millisecond timestamp followed by 80 bits of randomness. IDs created in the
// same millisecond increment the previous random tail, so their order is
// monotonic rather than random.
func NewID() string {
	idMu.Lock()
	defer idMu.Unlock()

	ms := uint64(time.Now().UnixMilli())
	var tail [10]byte
	if !haveLast || ms > lastMillis {
		if _, err := rand.Read(tail[:]); err != nil {
			// crypto/rand failing is fatal for identity generation; fall back to
			// a time-derived tail rather than panicking in a UI thread.
			binary.BigEndian.PutUint64(tail[:8], uint64(time.Now().UnixNano()))
		}
		lastMillis, lastTail, haveLast = ms, tail, true
	} else {
		// Same millisecond, or the clock moved backwards: keep the last
		// timestamp and bump the tail to preserve ordering.
		ms = lastMillis
		tail = lastTail
		for i := len(tail) - 1; i >= 0; i-- {
			tail[i]++
			if tail[i] != 0 {
				break
			}
		}
		lastTail = tail
	}

	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	copy(b[6:], tail[:])
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

package avatica

import (
	"strconv"
	"unicode/utf8"
)

// ascii returns the JSON b with each character outside ASCII written as an
// escape of JSON, \uXXXX, with a pair of surrogates above U+FFFF. The Phoenix
// Query Server misreads the raw bytes of UTF-8 in a request, and reads the
// escape (D158). A character outside ASCII appears only inside a string of
// JSON, where the escape means the same.
func ascii(b []byte) []byte {
	n := 0
	for _, c := range b {
		if c >= utf8.RuneSelf {
			n++
		}
	}
	if n == 0 {
		return b
	}
	out := make([]byte, 0, len(b)+5*n)
	for len(b) > 0 {
		if b[0] < utf8.RuneSelf {
			out = append(out, b[0])
			b = b[1:]
			continue
		}
		r, size := utf8.DecodeRune(b)
		b = b[size:]
		if r > 0xFFFF {
			r -= 0x10000
			out = appendEscape(out, 0xD800+(r>>10))
			out = appendEscape(out, 0xDC00+(r&0x3FF))
			continue
		}
		out = appendEscape(out, r)
	}
	return out
}

// appendEscape appends \uXXXX for r, which is at most U+FFFF.
func appendEscape(b []byte, r rune) []byte {
	b = append(b, '\\', 'u')
	h := strconv.FormatInt(int64(r), 16)
	for range 4 - len(h) {
		b = append(b, '0')
	}
	return append(b, h...)
}

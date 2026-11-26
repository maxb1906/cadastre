// Package wtf8 converts between UTF-16 (as used by NTFS and the Win32 API)
// and WTF-8, a superset of UTF-8 that can also represent unpaired surrogates.
//
// Windows file names are arbitrary sequences of 16-bit units: they may contain
// lone surrogates that have no valid UTF-8 encoding. Decoding them with
// replacement characters would make it impossible to rebuild the exact path
// needed to delete such a file, so Cadastre stores names in WTF-8 and converts
// them back losslessly.
package wtf8

import "unicode/utf8"

const (
	surrHigh = 0xD800
	surrLow  = 0xDC00
	surrEnd  = 0xE000
)

// AppendUTF16 appends the WTF-8 encoding of the UTF-16 units to dst.
func AppendUTF16(dst []byte, units []uint16) []byte {
	for i := 0; i < len(units); i++ {
		u := rune(units[i])
		switch {
		case u < 0x80:
			dst = append(dst, byte(u))
			continue
		case u >= surrHigh && u < surrLow && i+1 < len(units):
			if l := rune(units[i+1]); l >= surrLow && l < surrEnd {
				u = (u-surrHigh)<<10 | (l - surrLow) + 0x10000
				i++
			}
		}
		dst = appendRune(dst, u)
	}
	return dst
}

// AppendUTF16LE is like AppendUTF16 but reads little-endian bytes directly,
// which avoids an intermediate []uint16 when parsing on-disk structures.
// An odd trailing byte is ignored.
func AppendUTF16LE(dst []byte, b []byte) []byte {
	n := len(b) / 2
	for i := 0; i < n; i++ {
		u := rune(b[2*i]) | rune(b[2*i+1])<<8
		switch {
		case u < 0x80:
			dst = append(dst, byte(u))
			continue
		case u >= surrHigh && u < surrLow && i+1 < n:
			if l := rune(b[2*i+2]) | rune(b[2*i+3])<<8; l >= surrLow && l < surrEnd {
				u = (u-surrHigh)<<10 | (l - surrLow) + 0x10000
				i++
			}
		}
		dst = appendRune(dst, u)
	}
	return dst
}

// appendRune encodes r like UTF-8, except that surrogate code points are
// encoded as three-byte sequences instead of being rejected.
func appendRune(dst []byte, r rune) []byte {
	if r >= surrHigh && r < surrEnd {
		return append(dst, 0xE0|byte(r>>12), 0x80|byte(r>>6)&0x3F, 0x80|byte(r)&0x3F)
	}
	return utf8.AppendRune(dst, r)
}

// FromUTF16 returns the WTF-8 string for the UTF-16 units.
func FromUTF16(units []uint16) string {
	return string(AppendUTF16(make([]byte, 0, len(units)), units))
}

// ToUTF16 converts a WTF-8 string back to UTF-16 units. Bytes that are not
// valid WTF-8 are converted to U+FFFD.
func ToUTF16(s string) []uint16 {
	out := make([]uint16, 0, len(s)+1)
	for i := 0; i < len(s); {
		c := s[i]
		if c < 0x80 {
			out = append(out, uint16(c))
			i++
			continue
		}
		// Encoded surrogate: ED A0..BF 80..BF.
		if c == 0xED && i+2 < len(s) && s[i+1] >= 0xA0 && s[i+1] <= 0xBF && s[i+2]&0xC0 == 0x80 {
			out = append(out, uint16(c&0x0F)<<12|uint16(s[i+1]&0x3F)<<6|uint16(s[i+2]&0x3F))
			i += 3
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(surrHigh+(r>>10)), uint16(surrLow+(r&0x3FF)))
		} else {
			out = append(out, uint16(r))
		}
	}
	return out
}

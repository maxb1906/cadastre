package ntfs

import (
	"errors"
	"fmt"
)

// Run is one extent of a non-resident attribute. LCN is -1 for a sparse run
// (no clusters allocated).
type Run struct {
	LCN    int64
	Length uint64 // in clusters
}

// Sparse reports whether the run has no allocated clusters.
func (r Run) Sparse() bool { return r.LCN < 0 }

var ErrBadRunlist = errors.New("ntfs: malformed runlist")

// maxRuns bounds the decoder so a corrupt record cannot make it allocate
// without limit. A 64 KiB record cannot hold more runs than this anyway.
const maxRuns = 1 << 15

// DecodeRunlist decodes a mapping pairs array. Decoding stops at the 0x00
// terminator or at the end of b.
func DecodeRunlist(b []byte) ([]Run, error) {
	return AppendRunlist(nil, b)
}

// AppendRunlist is DecodeRunlist appending to dst.
func AppendRunlist(dst []Run, b []byte) ([]Run, error) {
	var lcn int64
	for i := 0; i < len(b); {
		h := b[i]
		if h == 0 {
			return dst, nil
		}
		lenSize, offSize := int(h&0x0F), int(h>>4)
		i++
		if lenSize == 0 || lenSize > 8 || offSize > 8 || i+lenSize+offSize > len(b) {
			return dst, ErrBadRunlist
		}
		var length uint64
		for k := lenSize - 1; k >= 0; k-- {
			length = length<<8 | uint64(b[i+k])
		}
		i += lenSize
		if length == 0 || length > 1<<48 {
			return dst, ErrBadRunlist
		}
		if offSize == 0 {
			dst = append(dst, Run{LCN: -1, Length: length})
		} else {
			var delta int64
			for k := offSize - 1; k >= 0; k-- {
				delta = delta<<8 | int64(b[i+k])
			}
			// Sign-extend.
			shift := uint(64 - 8*offSize)
			delta = delta << shift >> shift
			i += offSize
			lcn += delta
			if lcn < 0 {
				return dst, fmt.Errorf("%w: negative LCN", ErrBadRunlist)
			}
			dst = append(dst, Run{LCN: lcn, Length: length})
		}
		if len(dst) > maxRuns {
			return dst, fmt.Errorf("%w: too many runs", ErrBadRunlist)
		}
	}
	return dst, nil
}

// EncodeRunlist builds a mapping pairs array. It is the inverse of
// DecodeRunlist and is used to build synthetic test records.
func EncodeRunlist(runs []Run) []byte {
	var out []byte
	var prev int64
	for _, r := range runs {
		lb := minBytesUnsigned(r.Length)
		if r.Sparse() {
			out = append(out, byte(lb))
			out = appendLE(out, uint64(r.Length), lb)
			continue
		}
		delta := r.LCN - prev
		ob := minBytesSigned(delta)
		out = append(out, byte(ob<<4|lb))
		out = appendLE(out, r.Length, lb)
		out = appendLE(out, uint64(delta), ob)
		prev = r.LCN
	}
	return append(out, 0)
}

func appendLE(b []byte, v uint64, n int) []byte {
	for i := 0; i < n; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

func minBytesUnsigned(v uint64) int {
	n := 1
	for v >= 1<<(8*n) && n < 8 {
		n++
	}
	return n
}

func minBytesSigned(v int64) int {
	for n := 1; n < 8; n++ {
		lim := int64(1) << (8*n - 1)
		if v >= -lim && v < lim {
			return n
		}
	}
	return 8
}

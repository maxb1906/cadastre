package volume

import (
	"bytes"
	"io"
	"testing"
)

func TestAlignedReader(t *testing.T) {
	data := make([]byte, 8192)
	for i := range data {
		data[i] = byte(i * 7)
	}
	calls := 0
	a := &alignedReader{sector: 512, read: func(p []byte, off int64) (int, error) {
		calls++
		if off%512 != 0 || len(p)%512 != 0 {
			t.Fatalf("unaligned read off=%d len=%d", off, len(p))
		}
		return bytes.NewReader(data).ReadAt(p, off)
	}}
	for _, c := range []struct{ off, n int }{{0, 1024}, {100, 50}, {511, 2}, {1000, 3000}, {8000, 192}} {
		p := make([]byte, c.n)
		if _, err := a.ReadAt(p, int64(c.off)); err != nil {
			t.Fatalf("%v: %v", c, err)
		}
		if !bytes.Equal(p, data[c.off:c.off+c.n]) {
			t.Fatalf("%v: wrong data", c)
		}
	}
	p := make([]byte, 100)
	if n, err := a.ReadAt(p, 8150); n != 42 || err == nil {
		t.Fatalf("short read: n=%d err=%v", n, err)
	}
	_ = io.EOF
}

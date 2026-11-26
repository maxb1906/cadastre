package ntfstest

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// Fixture returns the decompressed real NTFS image built by
// scripts/make-ntfs-fixture.sh.
func Fixture(tb testing.TB) []byte {
	tb.Helper()
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(file), "..", "..", "..", "testdata", "ntfs-fixture.img.zst")
	raw, err := os.ReadFile(p)
	if err != nil {
		tb.Fatalf("fixture: %v", err)
	}
	d, err := zstd.NewReader(bytes.NewReader(raw))
	if err != nil {
		tb.Fatal(err)
	}
	defer d.Close()
	var out bytes.Buffer
	if _, err := out.ReadFrom(d); err != nil {
		tb.Fatal(err)
	}
	return out.Bytes()
}

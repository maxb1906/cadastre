package mftscan

import (
	"context"
	"fmt"
	"runtime"
	"testing"

	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

func TestSyntheticVolume(t *testing.T) {
	const n = 100000
	s := ntfstest.NewSynthetic(n)
	ix, st, err := Scan(context.Background(), s, s.Info, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Bad != 0 {
		t.Fatalf("%d bad records", st.Bad)
	}
	var want uint64
	for r := uint64(24); r < n; r++ {
		if (r-24)%s.DirEvery != 0 {
			want += s.FileSize(r)
		}
	}
	// Root total = files + $MFT (metafile under the root).
	if got := ix.Size[ix.Root] - ix.Size[0]; got != want {
		t.Fatalf("root size %d want %d", got, want)
	}
	if tot := ix.Totals(); tot.Orphans != 0 || tot.Dirs != (n-24+s.DirEvery-1)/s.DirEvery+1 || tot.Files != n-24-tot.Dirs+2 {
		t.Fatalf("totals %+v", tot)
	}
	for _, w := range []int{1, 3} {
		ix2, _, err := Scan(context.Background(), s, s.Info, Options{Workers: w, ChunkBytes: 256 << 10})
		if err != nil {
			t.Fatal(err)
		}
		if ix2.Size[ix2.Root] != ix.Size[ix.Root] || ix2.Path(99999) != ix.Path(99999) {
			t.Fatalf("workers=%d differs", w)
		}
	}
}

func TestCancel(t *testing.T) {
	s := ntfstest.NewSynthetic(200000)
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, _, err := Scan(ctx, s, s.Info, Options{ChunkBytes: 64 << 10, Progress: func(done, total uint64) {
		calls++
		if calls == 3 {
			cancel()
		}
	}})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

// BenchmarkScan measures throughput and memory per entry. Run with
//
//	go test ./internal/mftscan -run '^$' -bench Scan -benchtime 1x
func BenchmarkScan(b *testing.B) {
	for _, n := range []uint64{1_000_000, 5_000_000, 10_000_000} {
		b.Run(fmt.Sprintf("%dM", n/1_000_000), func(b *testing.B) {
			if testing.Short() && n > 1_000_000 {
				b.Skip("short")
			}
			s := ntfstest.NewSynthetic(n)
			for k := 0; k < b.N; k++ {
				runtime.GC()
				var before runtime.MemStats
				runtime.ReadMemStats(&before)
				ix, _, err := Scan(context.Background(), s, s.Info, Options{})
				if err != nil {
					b.Fatal(err)
				}
				runtime.GC()
				var after runtime.MemStats
				runtime.ReadMemStats(&after)
				b.ReportMetric(float64(after.HeapAlloc-before.HeapAlloc)/float64(n), "heap-B/entry")
				b.ReportMetric(float64(ix.Memory())/float64(n), "index-B/entry")
				b.ReportMetric(float64(after.NumGC-before.NumGC), "GCs")
				runtime.KeepAlive(ix)
			}
		})
	}
}

package ntfs

import (
	"errors"
	"fmt"
	"io"
	"math/bits"
)

// UsedClusters counts the clusters marked in use in the $Bitmap metafile
// (record 6). It is how NTFS itself computes free space.
func UsedClusters(r io.ReaderAt, l *Layout) (uint64, error) {
	vi := l.Info
	mft := NewStreamReader(r, l.Runs, vi.BytesPerCluster, l.MFTSize())
	buf := make([]byte, vi.BytesPerRecord)
	if _, err := mft.ReadAt(buf, int64(RecBitmap)*int64(vi.BytesPerRecord)); err != nil {
		return 0, err
	}
	var rec Record
	if err := Parse(buf, RecBitmap, &rec, ParseOptions{WantDataRuns: true}); err != nil {
		return 0, fmt.Errorf("ntfs: $Bitmap record: %w", err)
	}
	main, ok := rec.MainStream()
	if !ok || main.Resident || len(rec.DataRuns) == 0 {
		return 0, errors.New("ntfs: $Bitmap has no non-resident data")
	}
	var runs []Run
	for _, s := range rec.DataRuns {
		runs = append(runs, s.Runs...)
	}
	need := (vi.TotalClusters + 7) / 8
	size := min(main.Size, need)
	sr := NewStreamReader(r, runs, vi.BytesPerCluster, size)
	chunk := make([]byte, 1<<20)
	var used, off uint64
	for off < size {
		n := min(uint64(len(chunk)), size-off)
		if _, err := sr.ReadAt(chunk[:n], int64(off)); err != nil {
			return 0, err
		}
		for k := uint64(0); k < n; k++ {
			b := chunk[k]
			if bit := (off + k) * 8; bit+8 > vi.TotalClusters && vi.TotalClusters > bit {
				b &= byte(1<<(vi.TotalClusters-bit) - 1) // ignore bits past the end
			}
			used += uint64(bits.OnesCount8(b))
		}
		off += n
	}
	return used, nil
}

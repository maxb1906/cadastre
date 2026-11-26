package mftscan

import (
	"context"
	"io"
	"runtime"
	"time"

	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/ntfs"
)

// Options tunes a scan.
type Options struct {
	// MinSize: files smaller than this are indexed without their name and
	// shown grouped. 0 keeps every name.
	MinSize uint64
	// ChunkBytes is the size of the sequential reads (default 4 MiB).
	ChunkBytes int
	// Workers is the number of parser goroutines (default: number of CPUs).
	Workers int
	// Progress, if set, is called from time to time with records processed.
	Progress func(done, total uint64)
}

// Stats describes what the scan read.
type Stats struct {
	Records    uint64 // records in the MFT
	InUse      uint64
	Bad        uint64 // corrupt records skipped
	Extensions uint64 // extension records merged into their base
	MFTRuns    int
	MFTBytes   uint64
	Duration   time.Duration
}

const defaultChunk = 4 << 20

// Scan reads the whole MFT of the volume and returns a finalized index whose
// slots are MFT record numbers. RootPath is left empty for the caller.
func Scan(ctx context.Context, r io.ReaderAt, vi ntfs.VolumeInfo, opt Options) (*index.Index, Stats, error) {
	start := time.Now()
	var st Stats
	layout, err := ntfs.ReadLayout(r, vi)
	if err != nil {
		return nil, st, err
	}
	st.Records = layout.RecordCount
	st.MFTRuns = len(layout.Runs)
	st.MFTBytes = layout.MFTSize()

	recSize := int(vi.BytesPerRecord)
	chunk := opt.ChunkBytes
	if chunk <= 0 {
		chunk = defaultChunk
	}
	// Whole clusters and whole records, so reads stay sector-aligned.
	unit := max(recSize, int(vi.BytesPerCluster))
	chunk = max(unit, chunk/unit*unit)
	perChunk := uint64(chunk / recSize)

	ix := index.New(int(layout.RecordCount))
	ix.Root = ntfs.RecRoot
	b := &builder{ix: ix, parentSeq: make([]uint16, layout.RecordCount), minSize: opt.MinSize, recSize: recSize}
	sr := ntfs.NewStreamReader(r, layout.Runs, vi.BytesPerCluster, layout.MFTSize())

	workers := opt.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	pending, dosOnly, err := b.pipeline(ctx, sr, layout.RecordCount, perChunk, chunk, workers, &st, opt.Progress)
	if err != nil {
		return nil, st, err
	}
	st.Extensions = uint64(len(pending))
	finish(b, pending, dosOnly)
	st.Duration = time.Since(start)
	return ix, st, nil
}

func finish(b *builder, pending []extPart, dosOnly map[uint32]bool) {
	b.applyExtensions(pending, dosOnly)
	b.checkParents()
	b.parentSeq = nil
	if !b.ix.Valid(ntfs.RecRoot) {
		// Corrupt root record: synthesize one so the tree still has a top.
		b.ix.Attr[ntfs.RecRoot] = index.FlagValid | index.FlagDir | index.AttrDirectory
		b.ix.Parent[ntfs.RecRoot] = index.NoParent
	}
	b.ix.Finalize()
	markMeta(b.ix)
}

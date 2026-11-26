package mftscan

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/maxb1906/cadastre/internal/ntfs"
)

type job struct {
	buf   []byte
	first uint64
	count int
}

// pipeline runs one reader goroutine (large sequential reads), workers
// parser goroutines writing disjoint slots of the index, and merges their
// side results in the calling goroutine.
func (b *builder) pipeline(ctx context.Context, sr io.ReaderAt, total, perChunk uint64, chunk, workers int,
	st *Stats, progress func(done, total uint64)) ([]extPart, map[uint32]bool, error) {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Bounded pools: at most nbuf chunks in flight, which caps memory at
	// nbuf × chunk (e.g. 16 × 4 MiB) whatever the MFT size.
	nbuf := 2*workers + 2
	bufs := make(chan []byte, nbuf)
	results := make(chan *chunkResult, nbuf)
	for k := 0; k < nbuf; k++ {
		bufs <- make([]byte, chunk)
		results <- newChunkResult()
	}
	jobs := make(chan job, nbuf)
	done := make(chan *chunkResult, nbuf)
	var readErr error

	// Reader.
	go func() {
		defer close(jobs)
		for first := uint64(0); first < total; first += perChunk {
			var buf []byte
			select {
			case buf = <-bufs:
			case <-ctx.Done():
				return
			}
			count := min(perChunk, total-first)
			p := buf[:count*uint64(b.recSize)]
			if _, err := sr.ReadAt(p, int64(first)*int64(b.recSize)); err != nil {
				readErr = fmt.Errorf("mftscan: reading records %d-%d: %w", first, first+count-1, err)
				cancel()
				return
			}
			select {
			case jobs <- job{buf: buf, first: first, count: int(count)}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Parsers.
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var rec ntfs.Record
			for j := range jobs {
				res := <-results
				b.processChunk(j.buf[:j.count*b.recSize], j.first, j.count, &rec, res)
				bufs <- j.buf
				done <- res
			}
		}()
	}
	go func() {
		wg.Wait()
		close(done)
	}()

	// Merger.
	var pending []extPart
	dosOnly := map[uint32]bool{}
	var processed uint64
	for res := range done {
		st.InUse += res.inUse
		st.Bad += res.bad
		processed += res.count
		b.merge(res, &pending, dosOnly)
		results <- res
		if progress != nil {
			progress(processed, total)
		}
	}
	if readErr != nil {
		return nil, nil, readErr
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return pending, dosOnly, nil
}

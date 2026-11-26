// Package walker implements engine C: a multithreaded classic directory walk
// that works without administrator rights and on any file system (ReFS, FAT,
// exFAT, network shares, non-Windows systems).
package walker

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxb1906/cadastre/internal/index"
)

// dirEntry is one entry returned by listDir.
type dirEntry struct {
	name    string
	size    uint64
	alloc   uint64
	mtime   uint32 // Unix seconds
	attr    uint32 // index.Attr* and index.Flag* bits
	inode   uint64 // for hard link detection (0 when unknown)
	nlink   uint32
	descend bool // directory to walk into (not a reparse point / mount)
}

// Options tunes a walk.
type Options struct {
	Workers     int
	MinSize     uint64
	ClusterSize uint32
	Progress    func(files uint64)
}

// Stats describes a walk.
type Stats struct {
	Files, Dirs uint64
	Denied      uint64 // directories that could not be listed
	Errors      uint64
	Duration    time.Duration
}

type task struct {
	idx  uint32
	path string
}

// Walk scans the tree under root and returns a finalized index whose
// RootPath is root.
func Walk(ctx context.Context, root string, opt Options) (*index.Index, Stats, error) {
	start := time.Now()
	var st Stats
	ix := index.New(0)
	ix.RootPath = root
	ix.Sep = pathSep
	ix.Root = ix.Append(index.Entry{Parent: index.NoParent, Attr: index.FlagDir | index.AttrDirectory})
	if opt.ClusterSize == 0 {
		opt.ClusterSize = 4096
	}
	workers := opt.Workers
	if workers <= 0 {
		// Directory listing is latency bound (especially on network shares):
		// use more goroutines than CPUs.
		workers = min(4*runtime.GOMAXPROCS(0), 32)
	}
	rootDev := deviceOf(root)

	var (
		mu      sync.Mutex // protects ix and queue
		cond    = sync.NewCond(&mu)
		queue   = []task{{idx: ix.Root, path: root}}
		active  int
		files   atomic.Uint64
		seen    = map[uint64]bool{} // hard links (inode) already counted
		walkErr error
	)
	worker := func() {
		var batch []dirEntry
		for {
			mu.Lock()
			for len(queue) == 0 && active > 0 && ctx.Err() == nil {
				cond.Wait()
			}
			if len(queue) == 0 || ctx.Err() != nil {
				mu.Unlock()
				cond.Broadcast()
				return
			}
			t := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			active++
			mu.Unlock()

			batch = batch[:0]
			var err error
			batch, err = listDir(t.path, batch, opt.ClusterSize, rootDev)

			mu.Lock()
			if err != nil {
				if isDenied(err) {
					ix.Attr[t.idx] |= index.FlagDenied
					st.Denied++
				} else {
					st.Errors++
				}
			}
			for _, e := range batch {
				attr := e.attr
				size, alloc := e.size, e.alloc
				if e.nlink > 1 && e.inode != 0 && attr&index.FlagDir == 0 {
					attr |= index.FlagHardlink
					if seen[e.inode] {
						size, alloc = 0, 0 // counted once, under its first name
					}
					seen[e.inode] = true
				}
				name := e.name
				if attr&index.FlagDir == 0 && opt.MinSize > 0 && size < opt.MinSize && attr&index.FlagHardlink == 0 {
					attr |= index.FlagFolded
					name = ""
				}
				i := ix.Append(index.Entry{Parent: t.idx, Name: name, Size: size, Alloc: alloc, MTime: e.mtime, Attr: attr})
				if attr&index.FlagDir != 0 {
					st.Dirs++
					if e.descend {
						queue = append(queue, task{idx: i, path: joinPath(t.path, e.name)})
					}
				} else {
					st.Files++
				}
			}
			active--
			n := files.Add(uint64(len(batch)))
			cond.Broadcast()
			mu.Unlock()
			if opt.Progress != nil {
				opt.Progress(n)
			}
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() { defer wg.Done(); worker() }()
	}
	// Wake waiting workers on cancellation.
	stop := context.AfterFunc(ctx, func() { mu.Lock(); cond.Broadcast(); mu.Unlock() })
	wg.Wait()
	stop()
	if err := ctx.Err(); err != nil {
		return nil, st, err
	}
	if walkErr != nil {
		return nil, st, walkErr
	}
	ix.Finalize()
	st.Duration = time.Since(start)
	return ix, st, nil
}

func roundUp(n uint64, cluster uint32) uint64 {
	c := uint64(cluster)
	return (n + c - 1) / c * c
}

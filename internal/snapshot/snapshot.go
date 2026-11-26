// Package snapshot saves and reopens scans as .cad files: a small JSON
// header followed by the Structure-of-Arrays index written as is, the whole
// compressed with zstd. Reopening is a few sequential reads, no rescan.
//
// A reopened snapshot is marked FromSnapshot: Cadastre never deletes
// anything from it without rescanning the items concerned first.
package snapshot

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
	"unsafe"

	"github.com/klauspost/compress/zstd"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/index"
	"github.com/maxb1906/cadastre/internal/volume"
)

const (
	magic   = "CADSNAP\x01"
	version = 1
	// maxEntries bounds what a corrupt or hostile file can make us allocate.
	maxEntries = 1 << 31
	maxNames   = 1 << 36
)

var ErrFormat = errors.New("snapshot: not a Cadastre snapshot or unsupported version")

type header struct {
	Version     int              `json:"version"`
	Created     time.Time        `json:"created"`
	Tool        string           `json:"tool"`
	Engine      string           `json:"engine"`
	Volume      volume.Info      `json:"volume"`
	Started     time.Time        `json:"started"`
	Duration    time.Duration    `json:"duration"`
	ClusterSize uint32           `json:"clusterSize"`
	Admin       bool             `json:"admin"`
	Warnings    []engine.Warning `json:"warnings"`
	RootPath    string           `json:"rootPath"`
	Sep         byte             `json:"sep"`
	Root        uint32           `json:"root"`
	Orphans     uint32           `json:"orphans"`
	MFTRecords  uint64           `json:"mftRecords,omitempty"`
	MFTBad      uint64           `json:"mftBad,omitempty"`
	MFTBytes    uint64           `json:"mftBytes,omitempty"`
	MFTRuns     int              `json:"mftRuns,omitempty"`
	WalkErrors  uint64           `json:"walkErrors,omitempty"`
	WholeVolume bool             `json:"wholeVolume"`
	Entries     uint64           `json:"entries"`
	NamesLen    uint64           `json:"namesLen"`
}

type sidecar struct {
	ADS         map[uint32][]index.Stream `json:"ads"`
	Links       map[uint32][]index.Link   `json:"links"`
	DirOwnAlloc map[uint32]uint64         `json:"dirOwnAlloc"`
}

var littleEndian = func() bool { x := uint16(1); return *(*byte)(unsafe.Pointer(&x)) == 1 }()

// asBytes views a slice of fixed-size integers as bytes (little-endian
// hosts; others go through encoding/binary).
func asBytes[T uint16 | uint32 | uint64](s []T) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
}

func writeArray[T uint16 | uint32 | uint64](w io.Writer, s []T) error {
	if littleEndian {
		_, err := w.Write(asBytes(s))
		return err
	}
	return binary.Write(w, binary.LittleEndian, s)
}

func readArray[T uint16 | uint32 | uint64](r io.Reader, n int) ([]T, error) {
	s := make([]T, n)
	if littleEndian {
		_, err := io.ReadFull(r, asBytes(s))
		return s, err
	}
	return s, binary.Read(r, binary.LittleEndian, s)
}

// Save writes res to path atomically (temporary file then rename).
func Save(path string, res *engine.Result) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = Write(f, res)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Write encodes res to w.
func Write(w io.Writer, res *engine.Result) error {
	ix := res.Index
	h := header{
		Version: version, Created: time.Now(), Tool: "Cadastre", Engine: res.Engine, Volume: res.Volume,
		Started: res.Started, Duration: res.Duration, ClusterSize: res.ClusterSize, Admin: res.Admin, Warnings: res.Warnings,
		RootPath: ix.RootPath, Sep: ix.Sep, Root: ix.Root, Orphans: ix.Orphans,
		MFTRecords: res.MFTRecords, MFTBad: res.MFTBad, MFTBytes: res.MFTBytes, MFTRuns: res.MFTRuns, WalkErrors: res.WalkErrors, WholeVolume: res.WholeVolume,
		Entries: uint64(ix.Len()), NamesLen: uint64(len(ix.Names)),
	}
	if _, err := io.WriteString(w, magic); err != nil {
		return err
	}
	zw, err := zstd.NewWriter(w, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(4))
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(zw, 1<<20)
	if err := writeBlob(bw, h); err != nil {
		return err
	}
	for _, step := range []func() error{
		func() error { return writeArray(bw, ix.Parent) },
		func() error { return writeArray(bw, ix.Size) },
		func() error { return writeArray(bw, ix.Alloc) },
		func() error { return writeArray(bw, ix.MTime) },
		func() error { return writeArray(bw, ix.Attr) },
		func() error { return writeArray(bw, ix.NameOff) },
		func() error { return writeArray(bw, ix.NameLen) },
		func() error { return writeArray(bw, ix.Seq) },
		func() error { _, err := bw.Write(ix.Names); return err },
		func() error { return writeBlob(bw, sidecar{ADS: ix.ADS, Links: ix.Links, DirOwnAlloc: ix.DirOwnAlloc}) },
	} {
		if err := step(); err != nil {
			zw.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		zw.Close()
		return err
	}
	return zw.Close()
}

func writeBlob(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var n [8]byte
	binary.LittleEndian.PutUint64(n[:], uint64(len(b)))
	if _, err := w.Write(n[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

func readBlob(r io.Reader, v any, limit uint64) error {
	var n [8]byte
	if _, err := io.ReadFull(r, n[:]); err != nil {
		return err
	}
	size := binary.LittleEndian.Uint64(n[:])
	if size > limit {
		return fmt.Errorf("%w: block of %d bytes", ErrFormat, size)
	}
	b := make([]byte, size)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Load reopens a snapshot file.
func Load(path string) (*engine.Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Read(bufio.NewReaderSize(f, 1<<20))
}

// Read decodes a snapshot. The index is validated and finalized again, so a
// damaged file cannot produce an inconsistent tree.
func Read(r io.Reader) (*engine.Result, error) {
	m := make([]byte, len(magic))
	if _, err := io.ReadFull(r, m); err != nil || string(m) != magic {
		return nil, ErrFormat
	}
	zr, err := zstd.NewReader(r, zstd.WithDecoderConcurrency(4), zstd.WithDecoderMaxMemory(64<<30))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var h header
	if err := readBlob(zr, &h, 16<<20); err != nil {
		return nil, fmt.Errorf("snapshot: header: %w", err)
	}
	if h.Version != version {
		return nil, ErrFormat
	}
	if h.Entries > maxEntries || h.NamesLen > maxNames || uint64(h.Root) >= h.Entries {
		return nil, fmt.Errorf("%w: implausible sizes", ErrFormat)
	}
	n := int(h.Entries)
	ix := index.New(0)
	ix.RootPath, ix.Sep, ix.Root, ix.Orphans = h.RootPath, h.Sep, h.Root, h.Orphans
	fail := func(what string, err error) (*engine.Result, error) {
		return nil, fmt.Errorf("snapshot: %s: %w", what, err)
	}
	if ix.Parent, err = readArray[uint32](zr, n); err != nil {
		return fail("parents", err)
	}
	if ix.Size, err = readArray[uint64](zr, n); err != nil {
		return fail("sizes", err)
	}
	if ix.Alloc, err = readArray[uint64](zr, n); err != nil {
		return fail("allocations", err)
	}
	if ix.MTime, err = readArray[uint32](zr, n); err != nil {
		return fail("dates", err)
	}
	if ix.Attr, err = readArray[uint32](zr, n); err != nil {
		return fail("attributes", err)
	}
	if ix.NameOff, err = readArray[uint32](zr, n); err != nil {
		return fail("names", err)
	}
	if ix.NameLen, err = readArray[uint16](zr, n); err != nil {
		return fail("names", err)
	}
	if ix.Seq, err = readArray[uint16](zr, n); err != nil {
		return fail("sequence numbers", err)
	}
	ix.Names = make([]byte, h.NamesLen)
	if _, err := io.ReadFull(zr, ix.Names); err != nil {
		return fail("names", err)
	}
	var sc sidecar
	if err := readBlob(zr, &sc, 4<<30); err != nil {
		return fail("streams", err)
	}
	if sc.ADS != nil {
		ix.ADS = sc.ADS
	}
	if sc.Links != nil {
		ix.Links = sc.Links
	}
	if sc.DirOwnAlloc != nil {
		ix.DirOwnAlloc = sc.DirOwnAlloc
	}
	for i := 0; i < n; i++ {
		if uint64(ix.NameOff[i])+uint64(ix.NameLen[i]) > h.NamesLen {
			return nil, fmt.Errorf("%w: name out of range at entry %d", ErrFormat, i)
		}
		if p := ix.Parent[i]; p != index.NoParent && int(p) >= n {
			ix.Parent[i] = uint32(n) // repaired (orphaned) by Finalize
		}
	}
	if ix.Orphans != index.NoParent && int(ix.Orphans) >= n {
		ix.Orphans = index.NoParent
	}
	ix.Finalize()
	res := &engine.Result{
		Index: ix, Volume: h.Volume, Engine: h.Engine, Started: h.Started, Duration: h.Duration, ClusterSize: h.ClusterSize,
		Admin: h.Admin, FromSnapshot: true, Warnings: h.Warnings,
		MFTRecords: h.MFTRecords, MFTBad: h.MFTBad, MFTBytes: h.MFTBytes, MFTRuns: h.MFTRuns, WalkErrors: h.WalkErrors, WholeVolume: h.WholeVolume,
	}
	return res, nil
}

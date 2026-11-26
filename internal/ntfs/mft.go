package ntfs

import (
	"errors"
	"fmt"
	"io"
	"sort"
)

// Well-known MFT record numbers.
const (
	RecMFT     = 0
	RecMFTMirr = 1
	RecLogFile = 2
	RecVolume  = 3
	RecAttrDef = 4
	RecRoot    = 5
	RecBitmap  = 6
	RecBoot    = 7
	RecBadClus = 8
	RecSecure  = 9
	RecUpCase  = 10
	RecExtend  = 11
	// Records 12-15 are reserved, 16-23 are used for extensions of the
	// system files. Regular files start at 24.
	FirstUserRecord = 24
)

// Layout describes where the $MFT lives on the volume.
type Layout struct {
	Info        VolumeInfo
	Runs        []Run  // runlist of $MFT::$DATA, VCN 0 onwards
	DataSize    uint64 // logical size of $MFT::$DATA
	RecordCount uint64 // number of records to read
}

// MFTSize returns the number of bytes of $MFT that contain records.
func (l *Layout) MFTSize() uint64 { return l.RecordCount * uint64(l.Info.BytesPerRecord) }

// ReadLayout reads record 0 of the MFT and reconstructs the full runlist of
// $MFT::$DATA, following the $ATTRIBUTE_LIST when the MFT is so fragmented
// that its runlist spills into extension records.
func ReadLayout(r io.ReaderAt, vi VolumeInfo) (*Layout, error) {
	if err := vi.Validate(); err != nil {
		return nil, err
	}
	rs := int(vi.BytesPerRecord)
	cs := uint64(vi.BytesPerCluster)
	buf := make([]byte, rs)
	if _, err := r.ReadAt(buf, int64(vi.MftStartLcn*cs)); err != nil {
		return nil, fmt.Errorf("ntfs: reading MFT record 0: %w", err)
	}
	var rec Record
	opt := ParseOptions{WantAttrList: true, WantDataRuns: true}
	if err := Parse(buf, 0, &rec, opt); err != nil {
		return nil, fmt.Errorf("ntfs: parsing MFT record 0: %w", err)
	}
	main, ok := rec.MainStream()
	if !ok || main.Resident {
		return nil, errors.New("ntfs: $MFT has no non-resident $DATA attribute")
	}
	segs := append([]RunSegment(nil), rec.DataRuns...)

	if rec.HasAttrList {
		entries := rec.AttrList
		if len(rec.AttrListNonResident) > 0 {
			v, err := readRuns(r, rec.AttrListNonResident, cs, rec.AttrListSize)
			if err != nil {
				return nil, fmt.Errorf("ntfs: reading $MFT attribute list: %w", err)
			}
			entries = AppendAttrList(nil, v)
		}
		seen := map[uint64]bool{0: true}
		for _, e := range entries {
			if e.Type != AttrData || e.NameLen != 0 || seen[e.Rec] {
				continue
			}
			seen[e.Rec] = true
			partial := flatten(segs)
			off, err := recordOffset(partial, e.Rec, vi)
			if err != nil {
				return nil, fmt.Errorf("ntfs: locating $MFT extension record %d: %w", e.Rec, err)
			}
			if _, err := r.ReadAt(buf, off); err != nil {
				return nil, fmt.Errorf("ntfs: reading $MFT extension record %d: %w", e.Rec, err)
			}
			var ext Record
			if err := Parse(buf, e.Rec, &ext, opt); err != nil {
				return nil, fmt.Errorf("ntfs: parsing $MFT extension record %d: %w", e.Rec, err)
			}
			segs = append(segs, ext.DataRuns...)
		}
	}
	runs, err := mergeSegments(segs)
	if err != nil {
		return nil, err
	}
	l := &Layout{Info: vi, Runs: runs, DataSize: main.Size}
	size := main.Size
	if vi.MftValidDataLength != 0 && vi.MftValidDataLength < size {
		size = vi.MftValidDataLength
	}
	var mapped uint64
	for _, run := range runs {
		mapped += run.Length * cs
	}
	if mapped < size {
		size = mapped
	}
	l.RecordCount = size / uint64(rs)
	if l.RecordCount > MaxRecordNumber {
		return nil, fmt.Errorf("ntfs: MFT too large (%d records)", l.RecordCount)
	}
	return l, nil
}

func flatten(segs []RunSegment) []Run {
	s := append([]RunSegment(nil), segs...)
	sort.Slice(s, func(i, j int) bool { return s[i].LowestVCN < s[j].LowestVCN })
	var out []Run
	var next uint64
	for _, seg := range s {
		if seg.LowestVCN != next {
			break
		}
		out = append(out, seg.Runs...)
		for _, r := range seg.Runs {
			next += r.Length
		}
	}
	return out
}

func mergeSegments(segs []RunSegment) ([]Run, error) {
	sort.Slice(segs, func(i, j int) bool { return segs[i].LowestVCN < segs[j].LowestVCN })
	var out []Run
	var next uint64
	for _, seg := range segs {
		if seg.LowestVCN != next {
			return nil, fmt.Errorf("ntfs: $MFT runlist has a gap at VCN %d (segment starts at %d)", next, seg.LowestVCN)
		}
		var n uint64
		for _, r := range seg.Runs {
			n += r.Length
		}
		if n == 0 || seg.HighestVCN+1 != seg.LowestVCN+n {
			return nil, fmt.Errorf("ntfs: $MFT runlist segment at VCN %d is inconsistent", seg.LowestVCN)
		}
		out = append(out, seg.Runs...)
		next += n
	}
	if len(out) == 0 {
		return nil, errors.New("ntfs: empty $MFT runlist")
	}
	return out, nil
}

// recordOffset returns the volume byte offset of record rec, which must not
// straddle two runs.
func recordOffset(runs []Run, rec uint64, vi VolumeInfo) (int64, error) {
	cs := uint64(vi.BytesPerCluster)
	voff := rec * uint64(vi.BytesPerRecord)
	for _, r := range runs {
		n := r.Length * cs
		if voff < n {
			if r.Sparse() || voff+uint64(vi.BytesPerRecord) > n {
				return 0, errors.New("record not contiguous")
			}
			return int64(uint64(r.LCN)*cs + voff), nil
		}
		voff -= n
	}
	return 0, errors.New("record beyond mapped runs")
}

func readRuns(r io.ReaderAt, runs []Run, cs, size uint64) ([]byte, error) {
	if size > 16<<20 {
		return nil, errors.New("attribute too large")
	}
	out := make([]byte, 0, size)
	for _, run := range runs {
		if uint64(len(out)) >= size {
			break
		}
		n := run.Length * cs
		if rem := size - uint64(len(out)); n > rem {
			n = rem
		}
		chunk := make([]byte, n)
		if !run.Sparse() {
			if _, err := r.ReadAt(chunk, int64(uint64(run.LCN)*cs)); err != nil {
				return nil, err
			}
		}
		out = append(out, chunk...)
	}
	return out, nil
}

// StreamReader reads the virtual byte stream of a non-resident attribute
// (typically $MFT::$DATA) by mapping offsets through its runlist. Reads may
// span several runs. Sparse runs read as zeros.
type StreamReader struct {
	r    io.ReaderAt
	cs   uint64
	runs []Run
	// vstart[i] is the virtual byte offset of runs[i].
	vstart []uint64
	size   uint64
}

// NewStreamReader returns a reader over the first size bytes of the stream.
func NewStreamReader(r io.ReaderAt, runs []Run, clusterSize uint32, size uint64) *StreamReader {
	s := &StreamReader{r: r, cs: uint64(clusterSize), runs: runs, size: size}
	var v uint64
	for _, run := range runs {
		s.vstart = append(s.vstart, v)
		v += run.Length * s.cs
	}
	if v < size {
		s.size = v
	}
	return s
}

// Size returns the readable size of the stream.
func (s *StreamReader) Size() uint64 { return s.size }

// ReadAt implements io.ReaderAt over the virtual stream.
func (s *StreamReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("ntfs: negative offset")
	}
	voff := uint64(off)
	if voff >= s.size {
		return 0, io.EOF
	}
	want := len(p)
	if rem := s.size - voff; uint64(want) > rem {
		want = int(rem)
	}
	i := sort.Search(len(s.runs), func(i int) bool { return s.vstart[i] > voff }) - 1
	done := 0
	for done < want && i < len(s.runs) {
		run := s.runs[i]
		inRun := voff - s.vstart[i]
		n := run.Length*s.cs - inRun
		if n > uint64(want-done) {
			n = uint64(want - done)
		}
		dst := p[done : done+int(n)]
		if run.Sparse() {
			clear(dst)
		} else if _, err := s.r.ReadAt(dst, int64(uint64(run.LCN)*s.cs+inRun)); err != nil {
			return done, err
		}
		done += int(n)
		voff += n
		i++
	}
	if done < len(p) {
		return done, io.EOF
	}
	return done, nil
}

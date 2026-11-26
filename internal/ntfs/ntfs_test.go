package ntfs_test

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/maxb1906/cadastre/internal/ntfs"
	"github.com/maxb1906/cadastre/internal/ntfs/ntfstest"
)

func TestParseBootSector(t *testing.T) {
	b := ntfstest.BootSector(512, 8, 1<<20, 4, 1024)
	vi, err := ntfs.ParseBootSector(b)
	if err != nil {
		t.Fatal(err)
	}
	if vi.BytesPerCluster != 4096 || vi.BytesPerRecord != 1024 || vi.MftStartLcn != 4 || vi.TotalClusters != 1<<17 {
		t.Fatalf("unexpected %+v", vi)
	}
	// Record larger than a cluster expressed in clusters.
	b = ntfstest.BootSector(512, 1, 1<<20, 4, 1024)
	if vi, err = ntfs.ParseBootSector(b); err != nil || vi.BytesPerRecord != 1024 || vi.BytesPerCluster != 512 {
		t.Fatalf("got %+v %v", vi, err)
	}
	b[3] = 'X'
	if _, err := ntfs.ParseBootSector(b); !errors.Is(err, ntfs.ErrNotNTFS) {
		t.Fatalf("want ErrNotNTFS, got %v", err)
	}
}

func TestRunlistRoundTrip(t *testing.T) {
	runs := []ntfs.Run{{LCN: 100, Length: 8}, {LCN: -1, Length: 16}, {LCN: 50, Length: 1}, {LCN: 1 << 40, Length: 300000}, {LCN: 3, Length: 2}}
	got, err := ntfs.DecodeRunlist(ntfs.EncodeRunlist(runs))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, runs) {
		t.Fatalf("got %v want %v", got, runs)
	}
	// Known encoding: 0x21 0x18 0x34 0x56 → 0x18 clusters at LCN 0x5634.
	got, err = ntfs.DecodeRunlist([]byte{0x21, 0x18, 0x34, 0x56, 0x00})
	if err != nil || len(got) != 1 || got[0] != (ntfs.Run{LCN: 0x5634, Length: 0x18}) {
		t.Fatalf("got %v %v", got, err)
	}
	// Negative delta.
	got, err = ntfs.DecodeRunlist([]byte{0x11, 0x10, 0x40, 0x11, 0x10, 0xF0, 0x00})
	if err != nil || got[1].LCN != 0x30 {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := ntfs.DecodeRunlist([]byte{0x11, 0x10, 0x80}); err == nil {
		t.Fatal("expected error on negative LCN")
	}
	if _, err := ntfs.DecodeRunlist([]byte{0x31, 0x10}); err == nil {
		t.Fatal("expected error on truncated runlist")
	}
}

func fileRecord(attrs ...ntfstest.Attr) ntfstest.Record {
	return ntfstest.Record{Seq: 3, Flags: ntfs.RecordInUse, Links: 1, Attrs: attrs}
}

func TestParseRegularFile(t *testing.T) {
	r := fileRecord(
		ntfstest.StdInfo(100, 200, 0x20),
		ntfstest.FileName(5, 5, "LONGFI~1.TXT", ntfs.NamespaceDOS),
		ntfstest.FileName(5, 5, "Long file name été.txt", ntfs.NamespaceWin32),
		ntfstest.NonResidentData("", []ntfs.Run{{LCN: 1000, Length: 3}}, 10000, 12288),
		ntfstest.ResidentData("Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n")),
	).Bytes()
	var rec ntfs.Record
	if err := ntfs.Parse(r, 42, &rec, ntfs.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if rec.RecNo != 42 || rec.Seq != 3 || !rec.InUse() || rec.IsDir() || !rec.IsBase() {
		t.Fatalf("header %+v", rec)
	}
	if !rec.HasStdInfo || rec.Created != 100 || rec.Modified != 200 || rec.FileAttrs != 0x20 {
		t.Fatalf("stdinfo %+v", rec)
	}
	fn, ok := rec.BestName()
	if !ok || string(rec.Name(fn)) != "Long file name été.txt" || fn.ParentRec != 5 || fn.ParentSeq != 5 {
		t.Fatalf("best name %q %+v", rec.Name(fn), fn)
	}
	main, ok := rec.MainStream()
	if !ok || main.Size != 10000 || main.Alloc != 12288 || main.Resident {
		t.Fatalf("main %+v", main)
	}
	if len(rec.Streams) != 2 || string(rec.StreamName(rec.Streams[1])) != "Zone.Identifier" || rec.Streams[1].Size != 26 {
		t.Fatalf("streams %+v", rec.Streams)
	}
}

func TestParseCompressedAndSparse(t *testing.T) {
	compressed := ntfstest.Attr{Type: ntfs.AttrData, Flags: 0x0001, Runs: []ntfs.Run{{LCN: 10, Length: 16}}, Real: 65536, Alloc: 65536, Compressed: 8192}
	r := fileRecord(ntfstest.StdInfo(1, 1, 0x800), ntfstest.FileName(5, 5, "c.bin", ntfs.NamespaceWin32), compressed).Bytes()
	var rec ntfs.Record
	if err := ntfs.Parse(r, 30, &rec, ntfs.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if m, _ := rec.MainStream(); m.Size != 65536 || m.Alloc != 8192 {
		t.Fatalf("compressed %+v", m)
	}
	sparse := ntfstest.Attr{Type: ntfs.AttrData, Flags: 0x8000, Runs: []ntfs.Run{{LCN: -1, Length: 1 << 20}, {LCN: 10, Length: 1}}, Real: 4 << 30, Alloc: 4 << 30, Compressed: 4096}
	r = fileRecord(ntfstest.StdInfo(1, 1, 0x200), ntfstest.FileName(5, 5, "s.bin", ntfs.NamespacePOSIX), sparse).Bytes()
	if err := ntfs.Parse(r, 31, &rec, ntfs.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if m, _ := rec.MainStream(); m.Size != 4<<30 || m.Alloc != 4096 {
		t.Fatalf("sparse %+v", m)
	}
}

func TestParseDirectoryReparseAndContinuation(t *testing.T) {
	r := ntfstest.Record{Seq: 1, Flags: ntfs.RecordInUse | ntfs.RecordDirectory, Attrs: []ntfstest.Attr{
		ntfstest.StdInfo(1, 2, 0x10|0x400),
		ntfstest.FileName(5, 5, "Junction", ntfs.NamespaceWin32DOS),
		{Type: ntfs.AttrIndexAllocation, Name: "$I30", Runs: []ntfs.Run{{LCN: 77, Length: 2}}, Real: 8192, Alloc: 8192},
		ntfstest.Reparse(ntfs.ReparseTagMountPoint),
	}}.Bytes()
	var rec ntfs.Record
	if err := ntfs.Parse(r, 40, &rec, ntfs.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if !rec.IsDir() || !rec.HasReparse || rec.ReparseTag != ntfs.ReparseTagMountPoint || rec.ExtraAlloc != 8192 {
		t.Fatalf("%+v", rec)
	}
	// Extension record holding a continuation segment of $DATA: no sizes.
	r = ntfstest.Record{Seq: 2, Flags: ntfs.RecordInUse, BaseRec: 40, BaseSeq: 1, Attrs: []ntfstest.Attr{
		{Type: ntfs.AttrData, LowestVCN: 100, Runs: []ntfs.Run{{LCN: 5, Length: 10}}, Real: 999, Alloc: 999},
	}}.Bytes()
	if err := ntfs.Parse(r, 41, &rec, ntfs.ParseOptions{WantDataRuns: true}); err != nil {
		t.Fatal(err)
	}
	if rec.IsBase() || rec.BaseRec != 40 || len(rec.Streams) != 0 || len(rec.DataRuns) != 1 || rec.DataRuns[0].LowestVCN != 100 || rec.DataRuns[0].HighestVCN != 109 {
		t.Fatalf("%+v", rec)
	}
}

func TestParseNotInUseAndCorrupt(t *testing.T) {
	r := ntfstest.Record{Seq: 1, Flags: 0}.Bytes()
	var rec ntfs.Record
	if err := ntfs.Parse(r, 50, &rec, ntfs.ParseOptions{}); !errors.Is(err, ntfs.ErrNotInUse) {
		t.Fatalf("want ErrNotInUse, got %v", err)
	}
	if err := ntfs.Parse(make([]byte, 1024), 51, &rec, ntfs.ParseOptions{}); !errors.Is(err, ntfs.ErrNotInUse) {
		t.Fatalf("zeroed record: %v", err)
	}
	r = fileRecord(ntfstest.StdInfo(1, 1, 0)).Bytes()
	r[510] ^= 0xFF // torn write
	if err := ntfs.Parse(r, 52, &rec, ntfs.ParseOptions{}); !errors.Is(err, ntfs.ErrBadFixup) {
		t.Fatalf("want ErrBadFixup, got %v", err)
	}
}

func TestFixupRestoresSectorTails(t *testing.T) {
	data := bytes.Repeat([]byte{0xAB}, 600) // spans the first sector boundary
	r := fileRecord(ntfstest.FileName(5, 5, "x", ntfs.NamespaceWin32), ntfstest.ResidentData("", data)).Bytes()
	var rec ntfs.Record
	if err := ntfs.Parse(r, 60, &rec, ntfs.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if m, _ := rec.MainStream(); m.Size != 600 {
		t.Fatalf("size %d", m.Size)
	}
	if r[510] != 0xAB || r[511] != 0xAB {
		t.Fatalf("sector tail not restored: % x", r[508:512])
	}
}

// buildVolume returns a volume image with an MFT split in two fragments, the
// second one described by an extension record reached via $ATTRIBUTE_LIST.
func buildVolume(t *testing.T) ([]byte, ntfs.VolumeInfo) {
	const cs = 4096
	const rs = 1024
	// MFT: 64 records = 16 clusters, fragment A at LCN 10 (8 clusters, records
	// 0-31), fragment B at LCN 40 (8 clusters, records 32-63).
	img := make([]byte, 64*cs)
	copy(img, ntfstest.BootSector(512, 8, uint64(len(img)/512), 10, rs))
	put := func(recNo int, r ntfstest.Record) {
		var off int
		if recNo < 32 {
			off = 10*cs + recNo*rs
		} else {
			off = 40*cs + (recNo-32)*rs
		}
		copy(img[off:], r.Bytes())
	}
	rec0 := ntfstest.Record{Seq: 1, Flags: ntfs.RecordInUse, Attrs: []ntfstest.Attr{
		ntfstest.StdInfo(1, 1, 6),
		ntfstest.AttrList([]ntfs.AttrListEntry{
			{Type: ntfs.AttrStandardInformation, Rec: 0, Seq: 1},
			{Type: ntfs.AttrFileName, Rec: 0, Seq: 1},
			{Type: ntfs.AttrData, Rec: 0, Seq: 1},
			{Type: ntfs.AttrData, LowestVCN: 8, Rec: 17, Seq: 1},
		}),
		ntfstest.FileName(5, 5, "$MFT", ntfs.NamespaceWin32DOS),
		{Type: ntfs.AttrData, Runs: []ntfs.Run{{LCN: 10, Length: 8}}, HighestVCN: 7, Real: 64 * rs, Alloc: 16 * cs},
	}}
	put(0, rec0)
	put(17, ntfstest.Record{Seq: 1, Flags: ntfs.RecordInUse, BaseRec: 0, Attrs: []ntfstest.Attr{
		{Type: ntfs.AttrData, LowestVCN: 8, Runs: []ntfs.Run{{LCN: 40, Length: 8}}, Real: 0, Alloc: 0},
	}})
	// A regular file in the second fragment.
	put(50, fileRecord(ntfstest.StdInfo(1, 2, 0x20), ntfstest.FileName(5, 5, "far.txt", ntfs.NamespaceWin32), ntfstest.ResidentData("", []byte("hi"))))
	vi, err := ntfs.ParseBootSector(img)
	if err != nil {
		t.Fatal(err)
	}
	return img, vi
}

func TestReadLayoutFragmentedWithAttributeList(t *testing.T) {
	img, vi := buildVolume(t)
	l, err := ntfs.ReadLayout(bytes.NewReader(img), vi)
	if err != nil {
		t.Fatal(err)
	}
	want := []ntfs.Run{{LCN: 10, Length: 8}, {LCN: 40, Length: 8}}
	if !slices.Equal(l.Runs, want) || l.RecordCount != 64 {
		t.Fatalf("layout %+v", l)
	}
	sr := ntfs.NewStreamReader(bytes.NewReader(img), l.Runs, vi.BytesPerCluster, l.MFTSize())
	// Read records 30..33, straddling the two fragments.
	buf := make([]byte, 4*1024)
	if _, err := sr.ReadAt(buf, 30*1024); err != nil {
		t.Fatal(err)
	}
	var rec ntfs.Record
	if err := ntfs.Parse(buf[0:1024], 30, &rec, ntfs.ParseOptions{}); !errors.Is(err, ntfs.ErrNotInUse) {
		t.Fatalf("record 30: %v", err)
	}
	all := make([]byte, l.MFTSize())
	if _, err := sr.ReadAt(all, 0); err != nil {
		t.Fatal(err)
	}
	if err := ntfs.Parse(all[50*1024:51*1024], 50, &rec, ntfs.ParseOptions{}); err != nil {
		t.Fatal(err)
	}
	if fn, _ := rec.BestName(); string(rec.Name(fn)) != "far.txt" {
		t.Fatalf("record 50 name %q", rec.Name(fn))
	}
	if _, err := sr.ReadAt(buf, int64(l.MFTSize())); err != io.EOF {
		t.Fatalf("read past end: %v", err)
	}
}

func TestCloudTag(t *testing.T) {
	for _, tag := range []uint32{0x9000001A, 0x9000101A, 0x9000F01A} {
		if !ntfs.IsCloudReparseTag(tag) {
			t.Errorf("%x should be cloud", tag)
		}
	}
	if ntfs.IsCloudReparseTag(ntfs.ReparseTagSymlink) {
		t.Error("symlink is not cloud")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(fileRecord(
		ntfstest.StdInfo(100, 200, 0x20),
		ntfstest.FileName(5, 5, "a.txt", ntfs.NamespaceWin32),
		ntfstest.NonResidentData("", []ntfs.Run{{LCN: 1000, Length: 3}}, 10000, 12288),
		ntfstest.ResidentData("ads", []byte("x")),
		ntfstest.Reparse(ntfs.ReparseTagSymlink),
	).Bytes())
	f.Add(ntfstest.Record{Seq: 1, Flags: ntfs.RecordInUse, Attrs: []ntfstest.Attr{
		ntfstest.AttrList([]ntfs.AttrListEntry{{Type: ntfs.AttrData, Rec: 17}}),
	}}.Bytes())
	f.Fuzz(func(t *testing.T, b []byte) {
		var rec ntfs.Record
		_ = ntfs.Parse(b, 1, &rec, ntfs.ParseOptions{WantAttrList: true, WantDataRuns: true})
		for _, n := range rec.Names {
			_ = rec.Name(n)
		}
		for _, s := range rec.Streams {
			_ = rec.StreamName(s)
		}
	})
}

func FuzzRunlist(f *testing.F) {
	f.Add(ntfs.EncodeRunlist([]ntfs.Run{{LCN: 5, Length: 3}, {LCN: -1, Length: 9}}))
	f.Fuzz(func(t *testing.T, b []byte) {
		runs, err := ntfs.DecodeRunlist(b)
		if err != nil {
			return
		}
		again, err := ntfs.DecodeRunlist(ntfs.EncodeRunlist(runs))
		if err != nil || !slices.Equal(again, runs) {
			t.Fatalf("re-encode mismatch: %v vs %v (%v)", runs, again, err)
		}
	})
}

func FuzzReadLayout(f *testing.F) {
	f.Add(ntfstest.BootSector(512, 8, 1<<12, 2, 1024), fileRecord(ntfstest.NonResidentData("", []ntfs.Run{{LCN: 2, Length: 1}}, 4096, 4096)).Bytes())
	f.Fuzz(func(t *testing.T, boot, rec0 []byte) {
		vi, err := ntfs.ParseBootSector(boot)
		if err != nil || vi.MftStartLcn > 64 || vi.BytesPerCluster > 65536 {
			return
		}
		img := make([]byte, 128*int(vi.BytesPerCluster))
		copy(img, boot)
		copy(img[vi.MftStartLcn*uint64(vi.BytesPerCluster):], rec0)
		_, _ = ntfs.ReadLayout(bytes.NewReader(img), vi)
	})
}

// Package ntfs parses NTFS on-disk structures (boot sector, FILE records,
// attributes, runlists). It works exclusively on byte slices and io.ReaderAt,
// without any system call, so it is testable and fuzzable on any platform.
package ntfs

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// VolumeInfo holds the geometry needed to locate and read the MFT.
// On Windows it is filled from FSCTL_GET_NTFS_VOLUME_DATA; elsewhere (tests,
// disk images) from the boot sector.
type VolumeInfo struct {
	BytesPerSector     uint32
	BytesPerCluster    uint32
	BytesPerRecord     uint32 // BytesPerFileRecordSegment, usually 1024
	TotalClusters      uint64
	MftStartLcn        uint64
	MftValidDataLength uint64 // 0 if unknown (taken from record 0 then)
	SerialNumber       uint64
}

// ErrNotNTFS is returned when a boot sector does not carry the NTFS signature.
var ErrNotNTFS = errors.New("ntfs: not an NTFS boot sector")

// ParseBootSector decodes the first sector of an NTFS volume.
func ParseBootSector(b []byte) (VolumeInfo, error) {
	var vi VolumeInfo
	if len(b) < 512 {
		return vi, fmt.Errorf("ntfs: boot sector too short (%d bytes)", len(b))
	}
	if string(b[3:11]) != "NTFS    " {
		return vi, ErrNotNTFS
	}
	bps := uint32(binary.LittleEndian.Uint16(b[0x0B:]))
	if bps < 256 || bps > 4096 || bps&(bps-1) != 0 {
		return vi, fmt.Errorf("ntfs: invalid bytes per sector %d", bps)
	}
	spcRaw := b[0x0D]
	var spc uint32
	if spcRaw > 0x80 {
		// Clusters of 128 KiB and more: stored as a negative power of two.
		shift := 256 - uint32(spcRaw)
		if shift > 31 {
			return vi, fmt.Errorf("ntfs: invalid sectors per cluster 0x%x", spcRaw)
		}
		spc = 1 << shift
	} else {
		spc = uint32(spcRaw)
	}
	if spc == 0 || spc&(spc-1) != 0 {
		return vi, fmt.Errorf("ntfs: invalid sectors per cluster %d", spc)
	}
	vi.BytesPerSector = bps
	vi.BytesPerCluster = bps * spc
	if vi.BytesPerCluster > 2<<20 {
		return vi, fmt.Errorf("ntfs: cluster size %d too large", vi.BytesPerCluster)
	}
	totalSectors := binary.LittleEndian.Uint64(b[0x28:])
	vi.TotalClusters = totalSectors / uint64(spc)
	vi.MftStartLcn = binary.LittleEndian.Uint64(b[0x30:])
	vi.SerialNumber = binary.LittleEndian.Uint64(b[0x48:])
	cpr := int8(b[0x40])
	switch {
	case cpr < 0:
		if -int(cpr) > 16 {
			return vi, fmt.Errorf("ntfs: invalid record size exponent %d", cpr)
		}
		vi.BytesPerRecord = 1 << uint(-int(cpr))
	case cpr > 0:
		vi.BytesPerRecord = uint32(cpr) * vi.BytesPerCluster
	default:
		return vi, errors.New("ntfs: zero file record size")
	}
	if err := vi.Validate(); err != nil {
		return vi, err
	}
	return vi, nil
}

// Validate checks that the geometry is plausible before using it to size reads.
func (vi VolumeInfo) Validate() error {
	if vi.BytesPerCluster == 0 || vi.BytesPerCluster&(vi.BytesPerCluster-1) != 0 {
		return fmt.Errorf("ntfs: invalid cluster size %d", vi.BytesPerCluster)
	}
	r := vi.BytesPerRecord
	if r < 512 || r > 64<<10 || r&(r-1) != 0 {
		return fmt.Errorf("ntfs: invalid file record size %d", r)
	}
	if vi.TotalClusters != 0 && vi.MftStartLcn >= vi.TotalClusters {
		return fmt.Errorf("ntfs: MFT start LCN %d beyond volume (%d clusters)", vi.MftStartLcn, vi.TotalClusters)
	}
	return nil
}

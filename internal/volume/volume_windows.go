//go:build windows

package volume

import (
	"encoding/binary"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/maxb1906/cadastre/internal/ntfs"
)

const fsctlGetNTFSVolumeData = 0x00090064

var procGetDiskFreeSpaceW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")

type winRaw struct {
	h windows.Handle
	alignedReader
	info ntfs.VolumeInfo
}

// OpenRaw opens the volume of root (e.g. "C:" or "C:\") for raw reading.
// It requires administrator rights.
func OpenRaw(root string) (Raw, error) {
	letter := strings.TrimRight(root, `\/`)
	if len(letter) != 2 || letter[1] != ':' {
		return nil, fmt.Errorf("volume: %q is not a drive letter", root)
	}
	path, err := windows.UTF16PtrFromString(`\\.\` + letter)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(path, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, fmt.Errorf("volume: opening %s: %w", letter, err)
	}
	r := &winRaw{h: h}
	var buf [128]byte
	var n uint32
	if err := windows.DeviceIoControl(h, fsctlGetNTFSVolumeData, nil, 0, &buf[0], uint32(len(buf)), &n, nil); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("volume: FSCTL_GET_NTFS_VOLUME_DATA on %s: %w", letter, err)
	}
	le := binary.LittleEndian
	r.info = ntfs.VolumeInfo{
		SerialNumber:       le.Uint64(buf[0:]),
		TotalClusters:      le.Uint64(buf[16:]),
		BytesPerSector:     le.Uint32(buf[40:]),
		BytesPerCluster:    le.Uint32(buf[44:]),
		BytesPerRecord:     le.Uint32(buf[48:]),
		MftValidDataLength: le.Uint64(buf[56:]),
		MftStartLcn:        le.Uint64(buf[64:]),
	}
	sector := int64(r.info.BytesPerSector)
	if sector == 0 {
		sector = 512
	}
	r.alignedReader = alignedReader{sector: sector, read: r.readAligned}
	return r, nil
}

func (r *winRaw) readAligned(p []byte, off int64) (int, error) {
	var ov windows.Overlapped
	ov.Offset = uint32(off)
	ov.OffsetHigh = uint32(off >> 32)
	var done uint32
	err := windows.ReadFile(r.h, p, &done, &ov)
	if err == windows.ERROR_HANDLE_EOF {
		return int(done), fmt.Errorf("volume: read past end at %d", off)
	}
	return int(done), err
}

func (r *winRaw) NTFSInfo() (ntfs.VolumeInfo, error) { return r.info, r.info.Validate() }
func (r *winRaw) Close() error                       { return windows.CloseHandle(r.h) }

// IsAdmin reports whether the process runs elevated.
func IsAdmin() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// List returns the mounted volumes with a drive letter.
func List() ([]Info, error) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, err
	}
	var out []Info
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		info, err := Stat(root)
		if err != nil {
			continue // empty card reader, disconnected share...
		}
		out = append(out, info)
	}
	return out, nil
}

// Stat returns information about the volume containing root.
func Stat(root string) (Info, error) {
	info := Info{Root: root}
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return info, err
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_FIXED:
		info.Kind = "fixed"
	case windows.DRIVE_REMOVABLE:
		info.Kind = "removable"
	case windows.DRIVE_REMOTE:
		info.Kind = "network"
	case windows.DRIVE_CDROM:
		info.Kind = "cdrom"
	case windows.DRIVE_RAMDISK:
		info.Kind = "ram"
	default:
		info.Kind = "unknown"
	}
	var label, fs [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformation(p, &label[0], uint32(len(label)), nil, nil, nil, &fs[0], uint32(len(fs))); err == nil {
		info.Label = windows.UTF16ToString(label[:])
		info.FileSystem = windows.UTF16ToString(fs[:])
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return info, err
	}
	info.Total, info.Free = total, free
	return info, nil
}

// ClusterSize returns the allocation unit of the volume containing root.
func ClusterSize(root string) uint32 {
	p, err := windows.UTF16PtrFromString(volumeRoot(root))
	if err != nil {
		return 4096
	}
	var spc, bps, freeC, totalC uint32
	ok, _, _ := procGetDiskFreeSpaceW.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&spc)),
		uintptr(unsafe.Pointer(&bps)), uintptr(unsafe.Pointer(&freeC)), uintptr(unsafe.Pointer(&totalC)))
	if ok == 0 || spc*bps == 0 {
		return 4096
	}
	return spc * bps
}

// GUIDPath returns the \\?\Volume{...}\ name of the volume mounted at root.
func GUIDPath(root string) string {
	p, err := windows.UTF16PtrFromString(volumeRoot(root))
	if err != nil {
		return ""
	}
	var buf [64]uint16
	if err := windows.GetVolumeNameForVolumeMountPoint(p, &buf[0], uint32(len(buf))); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:])
}

func volumeRoot(root string) string {
	var buf [windows.MAX_PATH + 1]uint16
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return root
	}
	if err := windows.GetVolumePathName(p, &buf[0], uint32(len(buf))); err != nil {
		return root
	}
	return windows.UTF16ToString(buf[:])
}

package ntfs

import (
	"encoding/binary"
	"errors"
)

// fixupStride is the size of the blocks protected by the update sequence
// array. NTFS always uses 512 bytes, regardless of the physical sector size.
const fixupStride = 512

var (
	ErrBadSignature = errors.New("ntfs: bad record signature")
	ErrBadFixup     = errors.New("ntfs: update sequence mismatch (torn or corrupt record)")
	ErrBadUSA       = errors.New("ntfs: invalid update sequence array")
)

// ApplyFixups validates and removes the update sequence protection of a
// multi-sector structure (FILE or INDX record) in place. The last two bytes
// of every 512-byte block must equal the update sequence number; they are
// replaced by the original values saved in the array.
func ApplyFixups(rec []byte) error {
	if len(rec) < 8 {
		return ErrBadUSA
	}
	off := int(binary.LittleEndian.Uint16(rec[4:]))
	count := int(binary.LittleEndian.Uint16(rec[6:]))
	if count < 2 || off < 8 || off&1 != 0 {
		return ErrBadUSA
	}
	blocks := count - 1
	if blocks*fixupStride > len(rec) || off+2*count > len(rec) || off+2*count > fixupStride {
		return ErrBadUSA
	}
	usn0, usn1 := rec[off], rec[off+1]
	for i := 0; i < blocks; i++ {
		end := (i+1)*fixupStride - 2
		if rec[end] != usn0 || rec[end+1] != usn1 {
			return ErrBadFixup
		}
	}
	for i := 0; i < blocks; i++ {
		end := (i+1)*fixupStride - 2
		src := off + 2 + 2*i
		rec[end], rec[end+1] = rec[src], rec[src+1]
	}
	return nil
}

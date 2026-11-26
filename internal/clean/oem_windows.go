//go:build windows

package clean

import (
	"os"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procMultiByteToWideChar = windows.NewLazySystemDLL("kernel32.dll").NewProc("MultiByteToWideChar")

// decodeOEM converts console output (OEM code page, e.g. 850 on French
// systems) to UTF-8; UTF-8 output is kept as is.
func decodeOEM(b []byte) string {
	if len(b) == 0 || utf8.Valid(b) {
		return string(b)
	}
	const cpOEM = 1
	n, _, _ := procMultiByteToWideChar.Call(cpOEM, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return string(b)
	}
	w := make([]uint16, n)
	procMultiByteToWideChar.Call(cpOEM, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), uintptr(unsafe.Pointer(&w[0])), n)
	return windows.UTF16ToString(w)
}

func osMkdirAll(d string) error { return os.MkdirAll(d, 0o755) }

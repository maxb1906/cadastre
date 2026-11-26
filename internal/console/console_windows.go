//go:build windows

// Package console reattaches a GUI-subsystem executable to the console of
// its parent process, so that the CLI works from the same binary.
package console

import (
	"os"

	"golang.org/x/sys/windows"
)

const attachParentProcess = ^uint32(0) // ATTACH_PARENT_PROCESS = (DWORD)-1

var procAttachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

// Attach keeps redirected standard handles (pipes, files) as they are and
// otherwise attaches to the parent's console and reopens CONOUT$/CONIN$.
func Attach() {
	out, _ := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	if out != 0 && out != windows.InvalidHandle {
		if t, err := windows.GetFileType(out); err == nil && (t == windows.FILE_TYPE_PIPE || t == windows.FILE_TYPE_DISK) {
			return // output redirected: already usable
		}
	}
	if r, _, _ := procAttachConsole.Call(uintptr(attachParentProcess)); r == 0 {
		return // started from Explorer: nowhere to print
	}
	if f, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
		windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, windows.Handle(f.Fd()))
		windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
		windows.SetStdHandle(windows.STD_INPUT_HANDLE, windows.Handle(f.Fd()))
	}
}

// Detached reports whether the process has no console at all (launched by a
// double click).
func Detached() bool {
	h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
	return err != nil || h == 0 || h == windows.InvalidHandle
}
